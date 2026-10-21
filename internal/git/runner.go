// Package git provides the Runner interface and its system-git implementation.
package git

import "errors"

// WorktreeInfo describes a single git worktree.
type WorktreeInfo struct {
	Path   string
	HEAD   string // commit hash
	Branch string // empty if HEAD is detached
	Bare   bool
	Locked bool
}

// FileStatus holds the porcelain v2 XY status codes for a single file.
type FileStatus struct {
	Path     string
	Index    byte // X: staged change ('.' = clean, 'M' = modified, 'A' = added, etc.)
	Worktree byte // Y: unstaged change
}

// RepoStatus summarises the working-tree and branch state of a repository.
type RepoStatus struct {
	Branch   string
	Upstream string // empty if no upstream is configured
	Ahead    int
	Behind   int
	Files    []FileStatus
}

// ErrRepairUnsupported is returned by WorktreeRepair when the installed git
// version predates 2.29 and does not support the repair subcommand.
var ErrRepairUnsupported = errors.New("git worktree repair requires git ≥ 2.29")

// Sentinel errors returned (wrapped) by Rebase so callers can tell the
// recoverable failure modes apart from other errors; see
// docs/adr/0018-workspace-rebase.md (D2, D6).
var (
	// ErrRebaseConflict means the rebase stopped on a conflict and is still in
	// progress; the caller should run RebaseAbort.
	ErrRebaseConflict = errors.New("rebase conflict")
	// ErrAutostashConflict means the rebase completed but re-applying the
	// autostash conflicted; the changes are kept in the stash.
	ErrAutostashConflict = errors.New("autostash could not be re-applied cleanly")
	// ErrUntrackedBlocked means untracked files would be overwritten by the new
	// base; git refused to rebase and the worktree is unchanged.
	ErrUntrackedBlocked = errors.New("untracked files would be overwritten")
)

// Runner is the interface through which all git operations are performed.
// The production implementation shells out to system git; tests use a mock or a
// real git repo created with internal/git/testhelper.
type Runner interface {
	// Worktrees
	WorktreeAdd(repoPath, worktreePath, branch, base string, createBranch bool) error
	WorktreeRemove(repoPath, worktreePath string, force bool) error
	WorktreeList(repoPath string) ([]WorktreeInfo, error)
	WorktreeRepair(repoPath string, paths ...string) error // ErrRepairUnsupported on git < 2.29

	// Branches
	BranchExists(repoPath, branch string) (bool, error)
	RemoteBranchExists(repoPath, branch string) (bool, error)
	BranchDelete(repoPath, branch string, force bool) error
	BranchMerged(repoPath, branch string) (bool, error) // is branch merged into HEAD?

	// Status
	Status(repoPath string) (RepoStatus, error) // git status --porcelain=v2 --branch

	// Pending work
	// PendingWork returns the number of commits on any local branch that exist
	// on no remote, plus the number of stash entries. Both travel with a repo
	// that is moved rather than deleted, so callers treat them as advisory
	// warnings (see docs/adr/0013-repo-archive.md).
	PendingWork(repoPath string) (unpushedCommits, stashes int, err error)

	// Sync
	DefaultBranch(repoPath string) (string, error)
	Fetch(repoPath string) error
	FastForward(repoPath, branch string) error
	Push(repoPath, branch string) error
	// Rebase runs "git rebase --autostash <onto>". Failures are wrapped with
	// ErrRebaseConflict, ErrAutostashConflict or ErrUntrackedBlocked when
	// they match; use errors.Is.
	Rebase(repoPath, onto string) error
	RebaseAbort(repoPath string) error
	// IsAncestor reports whether ancestor is an ancestor of (or equal to) descendant.
	IsAncestor(repoPath, ancestor, descendant string) (bool, error)
	// InProgress returns the multi-step operation under way in the worktree
	// ("rebase", "merge", "cherry-pick" or "revert"), or "" if there is none.
	InProgress(repoPath string) (string, error)
	// ShortRev resolves rev to an abbreviated commit hash.
	ShortRev(repoPath, rev string) (string, error)

	// Info
	RemoteURL(repoPath, remote string) (string, error)
	Remotes(repoPath string) ([]string, error) // list configured remote names
}
