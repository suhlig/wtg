package cmd

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/suhlig/wtg/internal/config"
	"github.com/suhlig/wtg/internal/git"
)

// dotArg is the reserved repo argument that names the repo containing the
// current directory (ADR 0025). It is never passed to repoInSet: "." occurs in
// host segments (github.com) and file-name segments (notes.md), so partial
// matching would make it match most repos.
const dotArg = "."

// isDotArg reports whether arg is the reserved "." argument.
func isDotArg(arg string) bool { return arg == dotArg }

// resolveRepoArgs replaces every "." in names with the canonical short name of
// the repo containing the current directory. Names without a "." are returned
// unchanged and no git call is made.
func resolveRepoArgs(runner git.Runner, cfg *config.Config, names []string) ([]string, error) {
	if !slices.Contains(names, dotArg) {
		return names, nil
	}
	name, err := currentRepoName(runner, cfg)
	if err != nil {
		return nil, err
	}
	resolved := make([]string, len(names))
	for i, n := range names {
		if isDotArg(n) {
			resolved[i] = name
			continue
		}
		resolved[i] = n
	}
	return resolved, nil
}

// currentRepoName returns the canonical short name of the repo containing the
// current directory (ADR 0025 D2–D5). Git finds the worktree root, so running
// from a subdirectory, a linked worktree, a submodule or via GIT_DIR all work;
// a linked worktree is then mapped to its main clone, because a worktree is a
// second checkout of a repo, not a separate repo.
func currentRepoName(runner git.Runner, cfg *config.Config) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", dotArg, err)
	}
	toplevel, err := runner.Toplevel(cwd)
	if err != nil || toplevel == "" {
		return "", fmt.Errorf("%q means the repo containing the current directory, but %s is not inside a git repository; name the repo explicitly", dotArg, cwd)
	}

	main := toplevel
	if wts, listErr := runner.WorktreeList(toplevel); listErr == nil && len(wts) > 0 {
		// The main worktree is listed first.
		main = wts[0].Path
	}

	roots := cfg.DiscoveryRootDirs()
	allPaths, err := discoverAllRepoPaths(roots, cfg.Discovery.MaxDepth)
	if err != nil {
		return "", err
	}
	for _, p := range allPaths {
		if samePath(p, main) {
			return repoName(roots, p), nil
		}
	}
	return "", fmt.Errorf("%q resolved to %s, which is not under any discovery root dir (%s); add a discovery root or name the repo explicitly",
		dotArg, main, strings.Join(roots, ", "))
}

// samePath reports whether a and b are the same file. It compares physical
// paths rather than strings: a discovery root may be reached through a symlink,
// and on macOS /tmp is /private/tmp (ADR 0025 D5).
func samePath(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}

// rejectDotAsWorkspace returns an error when name is the reserved "." argument
// in a position that names a workspace: a workspace name becomes part of a
// branch name and a state file name, and "." is neither (ADR 0025 D7).
func rejectDotAsWorkspace(name string) error {
	if !isDotArg(name) {
		return nil
	}
	return fmt.Errorf("%q is not a workspace name; it always denotes a repo (ADR 0025)", dotArg)
}
