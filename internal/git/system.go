package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/suhlig/wtg/internal/trace"
)

// DefaultNetworkTimeout bounds a single git command that talks to a remote
// (fetch, push, ls-remote, remote set-head). It matches the default of
// git.network_timeout in the config.
const DefaultNetworkTimeout = 5 * time.Minute

// cancelGrace is how long a timed-out git command gets to clean up (remove its
// temporary pack, stop its helpers) after being interrupted before it is killed.
const cancelGrace = 10 * time.Second

// ActiveOp describes a git command that is currently running.
type ActiveOp struct {
	Repo  string    // directory passed to git -C
	Args  []string  // git arguments after -C <repo>
	Since time.Time // when the command started
}

// SystemRunner is the production Runner implementation that shells out to system git.
type SystemRunner struct {
	rec *trace.Recorder

	// base is the parent of every command's context; Interrupt cancels it.
	base       context.Context
	cancelBase context.CancelFunc

	mu             sync.Mutex
	networkTimeout time.Duration // <= 0 disables the timeout
	active         map[int]ActiveOp
	nextID         int
}

// New returns a SystemRunner. rec may be nil (no recording, no dry-run).
func New(rec ...*trace.Recorder) *SystemRunner {
	var r *trace.Recorder
	if len(rec) > 0 {
		r = rec[0]
	}
	base, cancel := context.WithCancel(context.Background())
	return &SystemRunner{rec: r, base: base, cancelBase: cancel, networkTimeout: DefaultNetworkTimeout, active: map[int]ActiveOp{}}
}

// Interrupt stops every running git command, and any started afterwards, along
// with the helpers they spawned. git runs in its own process group (so a
// timeout can stop the whole tree), which means a terminal's Ctrl-C no longer
// reaches it; the caller that catches the signal must call this instead.
func (r *SystemRunner) Interrupt() { r.cancelBase() }

// SetNetworkTimeout sets the longest a single network git command may run
// before it is interrupted. Zero or negative disables the timeout.
func (r *SystemRunner) SetNetworkTimeout(d time.Duration) {
	r.mu.Lock()
	r.networkTimeout = d
	r.mu.Unlock()
}

// Active returns the git commands that are running right now, oldest first.
func (r *SystemRunner) Active() []ActiveOp {
	r.mu.Lock()
	ops := make([]ActiveOp, 0, len(r.active))
	for _, op := range r.active {
		ops = append(ops, op)
	}
	r.mu.Unlock()
	sort.Slice(ops, func(i, j int) bool { return ops[i].Since.Before(ops[j].Since) })
	return ops
}

// FormatActive renders the running commands for a human, one per line with how
// long each has been running, e.g. "  git -C /repos/api fetch origin (1m5s)".
func FormatActive(ops []ActiveOp, now time.Time) string {
	var b strings.Builder
	for _, op := range ops {
		fmt.Fprintf(&b, "  %s (%s)\n", trace.FormatGitLine(op.Repo, op.Args), now.Sub(op.Since).Round(time.Second))
	}
	return b.String()
}

// begin registers a running command and returns the function that removes it.
func (r *SystemRunner) begin(repoPath string, args []string) (end func()) {
	r.mu.Lock()
	id := r.nextID
	r.nextID++
	r.active[id] = ActiveOp{Repo: repoPath, Args: args, Since: time.Now()}
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.active, id)
		r.mu.Unlock()
	}
}

// DryRun reports whether the runner is skipping mutating calls (--dry-run).
func (r *SystemRunner) DryRun() bool { return r.rec.DryRun() }

// isNetwork reports whether a git invocation contacts a remote and so can
// stall on a slow or unreachable server.
func isNetwork(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "fetch", "push", "ls-remote":
		return true
	case "remote":
		return len(args) > 1 && args[1] == "set-head"
	}
	return false
}

// runError wraps a failed git command with its exit code and stderr output.
type runError struct {
	args     []string
	exitCode int
	stderr   string
	timeout  time.Duration // non-zero when the command was interrupted for running too long
	canceled bool          // the command was stopped by Interrupt (Ctrl-C)
}

func (e *runError) Error() string {
	if e.canceled {
		return fmt.Sprintf("git %s: interrupted", strings.Join(e.args, " "))
	}
	if e.timeout > 0 {
		return fmt.Sprintf("git %s: timed out after %s (raise git.network_timeout or WTG_GIT_NETWORK_TIMEOUT, or set it to 0 to disable)",
			strings.Join(e.args, " "), e.timeout)
	}
	msg := fmt.Sprintf("git %s: exit %d", strings.Join(e.args, " "), e.exitCode)
	if s := strings.TrimSpace(e.stderr); s != "" {
		msg += ": " + s
	}
	return msg
}

// run executes git -C repoPath <args> and returns trimmed stdout.
// Any non-zero exit is returned as a *runError. Mutating calls are recorded
// in the Recorder when one is present (see internal/trace). In dry-run mode,
// mutating calls are recorded as skipped and return success without executing.
func (r *SystemRunner) run(repoPath string, args ...string) (string, error) {
	isMutating := !trace.Classify(args)
	if r.rec != nil && r.rec.DryRun() && isMutating {
		r.rec.Record(repoPath, trace.FormatGitLine(repoPath, args), -1, true)
		return "", nil
	}
	ctx := r.base
	var timeout time.Duration
	if isNetwork(args) {
		r.mu.Lock()
		timeout = r.networkTimeout
		r.mu.Unlock()
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	defer r.begin(repoPath, args)()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoPath}, args...)...) // #nosec G204 -- git with caller-controlled args
	// Stop the whole process group on timeout, not just git: its transport
	// helper (git-remote-https, ssh) and index-pack would otherwise outlive it
	// and keep the output pipes open. SIGTERM lets git remove its temporary
	// pack; cancelGrace bounds the wait if something ignores it.
	trace.OwnProcessGroup(cmd)
	cmd.Cancel = func() error { return trace.TerminateGroup(cmd) }
	cmd.WaitDelay = cancelGrace
	// Never let git (or the SSH subprocess it spawns) block on a terminal
	// prompt. GIT_TERMINAL_PROMPT=0 suppresses HTTPS credential prompts;
	// BatchMode=yes in GIT_SSH_COMMAND makes SSH fail immediately instead of
	// asking about unknown host keys or passphrases.
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		code := -1
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			code = exitErr.ExitCode()
		}
		re := &runError{args: args, exitCode: code, stderr: stderr.String()}
		if r.base.Err() != nil {
			re.canceled = true
		} else if timeout > 0 && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			re.timeout = timeout
		}
		if r.rec != nil && isMutating {
			r.rec.Record(repoPath, trace.FormatGitLine(repoPath, args), code, false)
		}
		return "", re
	}
	if r.rec != nil && isMutating {
		r.rec.Record(repoPath, trace.FormatGitLine(repoPath, args), 0, false)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// exitCode extracts the exit code from an error returned by run, or -1 if unavailable.
func exitCode(err error) int {
	if re, ok := errors.AsType[*runError](err); ok {
		return re.exitCode
	}
	return -1
}

// --- Worktrees ---

func (r *SystemRunner) WorktreeAdd(repoPath, worktreePath, branch, base string, createBranch bool) error {
	var args []string
	if createBranch {
		// git worktree add -b <new-branch> <path> [<base>]
		args = []string{"worktree", "add", "-b", branch, worktreePath}
		if base != "" {
			args = append(args, base)
		}
	} else {
		// git worktree add <path> <branch>
		args = []string{"worktree", "add", worktreePath, branch}
	}
	_, err := r.run(repoPath, args...)
	return err
}

// WorktreeAddDetached creates a worktree whose HEAD is detached at ref. Git
// refuses to check out a branch that another worktree already has, which is why
// a read-only member is detached (ADR 0024 D1).
func (r *SystemRunner) WorktreeAddDetached(repoPath, worktreePath, ref string) error {
	_, err := r.run(repoPath, "worktree", "add", "--detach", worktreePath, ref)
	return err
}

func (r *SystemRunner) Toplevel(dir string) (string, error) {
	return r.run(dir, "rev-parse", "--show-toplevel")
}

func (r *SystemRunner) CheckoutDetached(worktreePath, ref string) error {
	_, err := r.run(worktreePath, "checkout", "--detach", ref)
	return err
}

func (r *SystemRunner) WorktreeRemove(repoPath, worktreePath string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, worktreePath)
	_, err := r.run(repoPath, args...)
	return err
}

func (r *SystemRunner) WorktreeList(repoPath string) ([]WorktreeInfo, error) {
	out, err := r.run(repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktreeList(out)
}

func (r *SystemRunner) WorktreeRepair(repoPath string, paths ...string) error {
	args := append([]string{"worktree", "repair"}, paths...)
	_, err := r.run(repoPath, args...)
	if err != nil {
		if re, ok := errors.AsType[*runError](err); ok && strings.Contains(re.stderr, "unknown subcommand") {
			return ErrRepairUnsupported
		}
		return err
	}
	return nil
}

// --- Branches ---

func (r *SystemRunner) BranchExists(repoPath, branch string) (bool, error) {
	_, err := r.run(repoPath, "rev-parse", "--verify", "refs/heads/"+branch)
	if err != nil {
		// git exits 128 when the ref does not exist. Any other code (including
		// -1 for "git failed to start") is a real error we should surface.
		if exitCode(err) == 128 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *SystemRunner) RemoteBranchExists(repoPath, branch string) (bool, error) {
	_, err := r.run(repoPath, "rev-parse", "--verify", "refs/remotes/origin/"+branch)
	if err != nil {
		if exitCode(err) == 128 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *SystemRunner) BranchDelete(repoPath, branch string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := r.run(repoPath, "branch", flag, branch)
	return err
}

func (r *SystemRunner) BranchMerged(repoPath, branch string) (bool, error) {
	_, err := r.run(repoPath, "merge-base", "--is-ancestor", branch, "HEAD")
	if err != nil {
		if exitCode(err) == 1 {
			return false, nil // not an ancestor — not merged
		}
		return false, err
	}
	return true, nil
}

// --- Status ---

func (r *SystemRunner) Status(repoPath string) (RepoStatus, error) {
	out, err := r.run(repoPath, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return RepoStatus{}, err
	}
	return parseStatus(out)
}

func (r *SystemRunner) PendingWork(repoPath string) (int, int, error) {
	out, err := r.run(repoPath, "rev-list", "--count", "--branches", "--not", "--remotes")
	if err != nil {
		return 0, 0, err
	}
	unpushed, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, 0, fmt.Errorf("parse rev-list count %q: %w", out, err)
	}

	stashOut, err := r.run(repoPath, "stash", "list")
	if err != nil {
		return 0, 0, err
	}
	stashes := 0
	if s := strings.TrimSpace(stashOut); s != "" {
		stashes = len(strings.Split(s, "\n"))
	}
	return unpushed, stashes, nil
}

// --- Sync ---

// remoteHeadRef is the local remote-tracking symbolic ref that records the
// default branch of the "origin" remote.
const remoteHeadRef = "refs/remotes/origin/HEAD"

// DefaultBranch returns the default branch of the "origin" remote, e.g. "main".
//
// It reads the cached origin/HEAD symbolic ref, which git populates on clone.
// That ref is absent in some repos (e.g. those not created by "git clone") and
// can also be a plain commit ref rather than a symbolic one, in which case
// "git symbolic-ref" fails with "is not a symbolic ref". When the cached ref
// cannot be read, DefaultBranch asks the remote via "git remote set-head origin
// --auto" — which discovers and repairs origin/HEAD — and reads it again.
//
// That repair fails when origin's default branch has never been fetched, since
// git refuses to point origin/HEAD at a remote-tracking ref that does not exist
// locally ("Not a valid ref: refs/remotes/origin/<branch>"). DefaultBranch then
// falls back to asking the remote directly with "git ls-remote --symref origin
// HEAD", which does not depend on any local ref.
func (r *SystemRunner) DefaultBranch(repoPath string) (string, error) {
	out, err := r.run(repoPath, "symbolic-ref", remoteHeadRef)
	if err == nil {
		return branchFromSymref(out)
	}

	// The cached ref is missing or is not a symbolic ref; ask the remote to
	// discover and repair it, then read it back.
	if _, repairErr := r.run(repoPath, "remote", "set-head", "origin", "--auto"); repairErr != nil {
		err = repairErr
	} else if out, err = r.run(repoPath, "symbolic-ref", remoteHeadRef); err == nil {
		return branchFromSymref(out)
	}

	// Neither the cached ref nor its repair produced a value. Ask the remote
	// directly: "ls-remote" reports the branch HEAD points at without relying on
	// any local ref, so it also works before the default branch has been fetched.
	if lsOut, lsErr := r.run(repoPath, "ls-remote", "--symref", "origin", "HEAD"); lsErr == nil {
		if branch, parseErr := parseRemoteHead(lsOut); parseErr == nil {
			return branch, nil
		}
	}
	return "", fmt.Errorf("cannot determine default branch: %w", err)
}

// branchFromSymref converts the output of
// "git symbolic-ref refs/remotes/origin/HEAD" into a branch name, e.g.
// "refs/remotes/origin/main" yields "main".
func branchFromSymref(out string) (string, error) {
	_, branch, ok := strings.Cut(out, "refs/remotes/origin/")
	if !ok {
		return "", fmt.Errorf("unexpected symbolic-ref output: %q", out)
	}
	return branch, nil
}

func (r *SystemRunner) Fetch(repoPath string) error {
	_, err := r.run(repoPath, "fetch", "origin")
	return err
}

func (r *SystemRunner) FastForward(repoPath, branch string) error {
	_, err := r.run(repoPath, "merge", "--ff-only", "origin/"+branch)
	return err
}

func (r *SystemRunner) Push(repoPath, branch string) error {
	_, err := r.run(repoPath, "push", "--set-upstream", "origin", branch)
	return err
}

// PushTo pushes branch to remoteBranch on origin as a fast-forward refspec.
func (r *SystemRunner) PushTo(repoPath, branch, remoteBranch string) error {
	_, err := r.run(repoPath, "push", "origin", branch+":"+remoteBranch)
	return err
}

// PushDeleteRemote deletes branch on origin. Removing a remote-tracking ref is
// a push, so it is recorded and skipped under --dry-run like any other push.
func (r *SystemRunner) PushDeleteRemote(repoPath, branch string) error {
	_, err := r.run(repoPath, "push", "origin", "--delete", branch)
	return err
}

// CommitsAhead counts commits reachable from branch but not from base.
func (r *SystemRunner) CommitsAhead(repoPath, base, branch string) (int, error) {
	out, err := r.run(repoPath, "rev-list", "--count", base+".."+branch)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("parse rev-list count %q: %w", out, err)
	}
	return n, nil
}

// CommitsOnNoRemote counts commits reachable from ref that are on no remote.
func (r *SystemRunner) CommitsOnNoRemote(repoPath, ref string) (int, error) {
	out, err := r.run(repoPath, "rev-list", "--count", ref, "--not", "--remotes")
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("parse rev-list count %q: %w", out, err)
	}
	return n, nil
}

// ConfigGet reads a git config key from repoPath. found is false when the key
// is absent (git exits 1); other errors are surfaced.
func (r *SystemRunner) ConfigGet(repoPath, key string) (string, bool, error) {
	out, err := r.run(repoPath, "config", "--get", key)
	if err != nil {
		if exitCode(err) == 1 {
			return "", false, nil // key not set
		}
		return "", false, err
	}
	return out, true, nil
}

// Rebase runs "git rebase --autostash onto". It classifies failures instead of
// trusting git's exit status alone:
//
//   - a rebase still in progress afterwards means a conflict (ErrRebaseConflict);
//   - a stash entry that was not there before means the autostash could not be
//     re-applied (ErrAutostashConflict), whatever the exit status was;
//   - git's "untracked working tree files would be overwritten" refusal means
//     ErrUntrackedBlocked.
func (r *SystemRunner) Rebase(repoPath, onto string) error {
	stashBefore := r.stashTip(repoPath)
	_, err := r.run(repoPath, "rebase", "--autostash", onto)

	if op, _ := r.InProgress(repoPath); op == "rebase" {
		return fmt.Errorf("%w: %w", ErrRebaseConflict, err)
	}
	if r.stashTip(repoPath) != stashBefore {
		// git may exit 0 here, so err can be nil; do not rely on it.
		if err != nil {
			return fmt.Errorf("%w: %w", ErrAutostashConflict, err)
		}
		return ErrAutostashConflict
	}
	if err == nil {
		return nil
	}
	if re, ok := errors.AsType[*runError](err); ok &&
		strings.Contains(re.stderr, "untracked working tree files would be overwritten") {
		return fmt.Errorf("%w: %w", ErrUntrackedBlocked, err)
	}
	return err
}

// stashTip returns the commit refs/stash points at, or "" if there is no stash.
func (r *SystemRunner) stashTip(repoPath string) string {
	out, err := r.run(repoPath, "rev-parse", "--verify", "--quiet", "refs/stash")
	if err != nil {
		return ""
	}
	return out
}

func (r *SystemRunner) RebaseAbort(repoPath string) error {
	_, err := r.run(repoPath, "rebase", "--abort")
	return err
}

func (r *SystemRunner) IsAncestor(repoPath, ancestor, descendant string) (bool, error) {
	_, err := r.run(repoPath, "merge-base", "--is-ancestor", ancestor, descendant)
	if err != nil {
		if exitCode(err) == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// inProgressMarkers maps a git-dir entry to the operation it indicates.
var inProgressMarkers = []struct{ path, op string }{
	{"rebase-merge", "rebase"},
	{"rebase-apply", "rebase"},
	{"MERGE_HEAD", "merge"},
	{"CHERRY_PICK_HEAD", "cherry-pick"},
	{"REVERT_HEAD", "revert"},
}

func (r *SystemRunner) InProgress(repoPath string) (string, error) {
	for _, m := range inProgressMarkers {
		out, err := r.run(repoPath, "rev-parse", "--git-path", m.path)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(out) {
			out = filepath.Join(repoPath, out)
		}
		if _, err := os.Stat(out); err == nil {
			return m.op, nil
		}
	}
	return "", nil
}

func (r *SystemRunner) ShortRev(repoPath, rev string) (string, error) {
	return r.run(repoPath, "rev-parse", "--short", rev)
}

// ResolveRef resolves ref to a full commit hash. "--quiet" makes git exit 1
// without noise for a ref that does not exist.
func (r *SystemRunner) ResolveRef(repoPath, ref string) (string, error) {
	return r.run(repoPath, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
}

// RevListCount runs "git rev-list --left-right --count a...b" and returns the
// two counts: commits reachable from a but not b, then the reverse.
func (r *SystemRunner) RevListCount(repoPath, a, b string) (aOnly, bOnly int, err error) {
	out, err := r.run(repoPath, "rev-list", "--left-right", "--count", a+"..."+b)
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list output %q", out)
	}
	aOnly, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("parse rev-list count %q: %w", fields[0], err)
	}
	bOnly, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("parse rev-list count %q: %w", fields[1], err)
	}
	return aOnly, bOnly, nil
}

// ShowFile returns the content of path as of rev, e.g. "origin/main:go.mod".
func (r *SystemRunner) ShowFile(repoPath, rev, path string) ([]byte, error) {
	out, err := r.run(repoPath, "show", rev+":"+path)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

// --- Info ---

func (r *SystemRunner) RemoteURL(repoPath, remote string) (string, error) {
	if url, found, _ := r.ConfigGet(repoPath, "remote."+remote+".url"); found && url != "" {
		return url, nil
	}
	return r.run(repoPath, "remote", "get-url", remote)
}

func (r *SystemRunner) Remotes(repoPath string) ([]string, error) {
	out, err := r.run(repoPath, "remote")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}
