package cmd

import (
	"fmt"
	"os"
	"path/filepath"
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

// isPathArg reports whether arg has an explicit path prefix (., .., ./, ../,
// absolute paths, or tilde paths) and should be resolved as a filesystem path
// to an enclosing repo rather than matched as a short name (ADR 0025 D8).
func isPathArg(arg string) bool {
	if arg == "." || arg == ".." {
		return true
	}
	if strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, "../") ||
		strings.HasPrefix(arg, `.\`) || strings.HasPrefix(arg, `..\`) {
		return true
	}
	if filepath.IsAbs(arg) {
		return true
	}
	if arg == "~" || strings.HasPrefix(arg, "~/") || strings.HasPrefix(arg, `~\`) {
		return true
	}
	return false
}

// resolveRepoArgs replaces "." and path-like arguments in names with the
// canonical short name of the repo containing the referenced directory.
// Non-path names are returned unchanged and no git call is made.
func resolveRepoArgs(runner git.Runner, cfg *config.Config, names []string) ([]string, error) {
	hasPath := false
	for _, n := range names {
		if isPathArg(n) {
			hasPath = true
			break
		}
	}
	if !hasPath {
		return names, nil
	}
	resolved := make([]string, len(names))
	for i, n := range names {
		if isPathArg(n) {
			canonical, err := pathToRepoName(runner, cfg, n)
			if err != nil {
				return nil, err
			}
			resolved[i] = canonical
			continue
		}
		resolved[i] = n
	}
	return resolved, nil
}

// expandPath expands a leading ~ or ~/ to the user's home directory.
func expandPath(path string) (string, error) {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve %q: %w", path, err)
		}
		return home, nil
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve %q: %w", path, err)
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

// pathToRepoName resolves a path or "." to the canonical short name of the repo
// containing it (ADR 0025 D2–D5, D8). Git finds the worktree root, so running
// from a subdirectory, a linked worktree, a submodule or via GIT_DIR all work;
// a linked worktree is then mapped to its main clone, because a worktree is a
// second checkout of a repo, not a separate repo.
func pathToRepoName(runner git.Runner, cfg *config.Config, arg string) (string, error) {
	if isDotArg(arg) {
		return currentRepoName(runner, cfg)
	}

	expanded, err := expandPath(arg)
	if err != nil {
		return "", err
	}

	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", arg, err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("path %q: %w", arg, err)
	}

	dir := abs
	if !info.IsDir() {
		dir = filepath.Dir(abs)
	}

	toplevel, err := runner.Toplevel(dir)
	if err != nil || toplevel == "" {
		return "", fmt.Errorf("%q is not inside a git repository; name the repo explicitly", arg)
	}

	main := toplevel
	if wts, listErr := runner.WorktreeList(toplevel); listErr == nil && len(wts) > 0 {
		// The main worktree is listed first.
		main = wts[0].Path
	}

	return mapMainToDiscoveryName(cfg, main, arg)
}

// currentRepoName returns the canonical short name of the repo containing the
// current directory (ADR 0025 D2–D5).
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

	return mapMainToDiscoveryName(cfg, main, dotArg)
}

// mapMainToDiscoveryName matches main against configured discovery roots and
// returns the repo's short name, or an error if it is not managed.
func mapMainToDiscoveryName(cfg *config.Config, main, argForErr string) (string, error) {
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
		argForErr, main, strings.Join(roots, ", "))
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
// or a path-like argument in a position that names a workspace: a workspace name
// becomes part of a branch name and a state file name, and paths and "." are neither (ADR 0025 D7, D8).
func rejectDotAsWorkspace(name string) error {
	if isDotArg(name) {
		return fmt.Errorf("%q is not a workspace name; it always denotes a repo (ADR 0025)", dotArg)
	}
	if isPathArg(name) {
		return fmt.Errorf("%q is a path, not a workspace name (ADR 0025)", name)
	}
	return nil
}
