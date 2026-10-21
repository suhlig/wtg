package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// SystemRunner is the production Runner implementation that shells out to system git.
type SystemRunner struct{}

// New returns a SystemRunner.
func New() *SystemRunner { return &SystemRunner{} }

// runError wraps a failed git command with its exit code and stderr output.
type runError struct {
	args     []string
	exitCode int
	stderr   string
}

func (e *runError) Error() string {
	msg := fmt.Sprintf("git %s: exit %d", strings.Join(e.args, " "), e.exitCode)
	if s := strings.TrimSpace(e.stderr); s != "" {
		msg += ": " + s
	}
	return msg
}

// run executes git -C repoPath <args> and returns trimmed stdout.
// Any non-zero exit is returned as a *runError.
func (r *SystemRunner) run(repoPath string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...) // #nosec G204 -- git with caller-controlled args
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
		return "", &runError{args: args, exitCode: code, stderr: stderr.String()}
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

// --- Info ---

func (r *SystemRunner) RemoteURL(repoPath, remote string) (string, error) {
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
