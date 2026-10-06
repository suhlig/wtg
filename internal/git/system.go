package git

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
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
func (r *SystemRunner) DefaultBranch(repoPath string) (string, error) {
	out, err := r.run(repoPath, "symbolic-ref", remoteHeadRef)
	if err != nil {
		if _, repairErr := r.run(repoPath, "remote", "set-head", "origin", "--auto"); repairErr != nil {
			return "", fmt.Errorf("cannot determine default branch (is origin/HEAD set?): %w", err)
		}
		if out, err = r.run(repoPath, "symbolic-ref", remoteHeadRef); err != nil {
			return "", fmt.Errorf("cannot determine default branch (is origin/HEAD set?): %w", err)
		}
	}
	// "refs/remotes/origin/main" → "main"
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

func (r *SystemRunner) Rebase(repoPath, onto string) error {
	_, err := r.run(repoPath, "rebase", onto)
	return err
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
