package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/suhlig/wtg/internal/config"
	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/gomod"
	"github.com/suhlig/wtg/internal/saga"
	"github.com/suhlig/wtg/internal/state"
	"github.com/suhlig/wtg/internal/trace"
	"github.com/suhlig/wtg/internal/ui"
)

// originRefPrefix marks a ref whose target may move. A read-only member that
// follows origin/<branch> is advanced by `wtg rebase`; every other ref (a tag, a
// commit, a local branch, another remote) is pinned (ADR 0024 D3, D4).
const originRefPrefix = "origin/"

// readOnlySkipMsg is the per-repo message for a read-only member in commands
// that publish branch work: it has no branch of ours to push or land
// (ADR 0024 D7).
const readOnlySkipMsg = "skipped — read-only member"

// isFollowingRef reports whether ref follows a remote branch rather than
// pinning a commit.
func isFollowingRef(ref string) bool { return strings.HasPrefix(ref, originRefPrefix) }

// prepareReadOnlyTarget resolves the ref a read-only member is detached at,
// records the user's intent in t.ref, and notes whether the pinned commit has a
// go.mod. Everything here is pre-flight: a ref that does not resolve fails the
// whole invocation before anything is created (ADR 0024 D3, E1).
func prepareReadOnlyTarget(runner git.Runner, goRunner gomod.Runner, sp *state.Space, t *repoTarget, explicitRef string, out io.Writer) error {
	// Fetch first, so a tag or commit pushed since the last fetch is found. A
	// repo without a remote has nothing to fetch and can still be pinned.
	if none, _ := hasNoRemotes(t.repoPath, runner); !none {
		if err := runner.Fetch(t.repoPath); err != nil {
			return fmt.Errorf("%s: fetch failed: %w", t.name, err)
		}
	}

	ref := explicitRef
	if ref == "" {
		derived, err := deriveReadOnlyRef(runner, goRunner, sp, t, out)
		if err != nil {
			return err
		}
		ref = derived
	}

	commit, err := runner.ResolveRef(t.repoPath, ref)
	if err != nil {
		hint := ""
		if trace.IsDry(runner) {
			hint = " (a dry run does not fetch; a ref pushed since the last fetch would resolve after one)"
		}
		return fmt.Errorf("%s: ref %q does not resolve%s", t.name, ref, hint)
	}

	t.readOnly = true
	t.ref = ref
	t.detachAt = commit
	_, goModErr := runner.ShowFile(t.repoPath, commit, "go.mod")
	t.hasGoMod = goModErr == nil
	return nil
}

// deriveReadOnlyRef returns the ref a read-only member follows when the user
// gave no --ref: the version the workspace's other members' build lists select
// (ADR 0024 D8), else origin/<default branch>.
func deriveReadOnlyRef(runner git.Runner, goRunner gomod.Runner, sp *state.Space, t *repoTarget, out io.Writer) (string, error) {
	defaultBranch, err := runner.DefaultBranch(t.repoPath)
	if err != nil {
		return "", fmt.Errorf("%s: cannot determine default branch: %w", t.name, err)
	}
	fallback := originRefPrefix + defaultBranch

	// The module path comes from the repo itself, at the ref being considered,
	// so a /vN suffix needs no mapping from module to repo.
	goMod, err := runner.ShowFile(t.repoPath, fallback, "go.mod")
	if err != nil {
		return fallback, nil // not a Go module: nothing can require it
	}
	modulePath := gomod.ModulePath(goMod)
	if modulePath == "" {
		return fallback, nil
	}

	type vote struct{ consumer, version string }
	var votes []vote
	for _, m := range sp.Repos {
		if m.Name == t.name {
			continue
		}
		if _, err := os.Stat(filepath.Join(m.WorktreePath, "go.mod")); err != nil {
			continue // not a consumer of anything
		}
		version, replaced, reqErr := goRunner.RequiredVersion(m.WorktreePath, modulePath)
		switch {
		case errors.Is(reqErr, gomod.ErrGoMissing):
			// E14: fall back with one explanatory line.
			_, _ = fmt.Fprintf(out, "  %s %s: %v; pinning %s to %s instead\n", ui.SymWarn, t.name, reqErr, t.name, fallback)
			return fallback, nil
		case errors.Is(reqErr, gomod.ErrNotDependency):
			continue
		case reqErr != nil:
			// E16: the build list cannot be loaded; the consumer does not vote.
			_, _ = fmt.Fprintf(out, "  %s %s does not vote for %s: %v\n", ui.SymWarn, m.Name, t.name, reqErr)
			continue
		case replaced:
			continue // the user decided; never second-guess a replace
		}
		votes = append(votes, vote{consumer: m.Name, version: version})
	}
	if len(votes) == 0 {
		return fallback, nil
	}

	// Consumers must agree; they disagree only if their build lists select
	// different versions of the same module (E15).
	consumersByVersion := make(map[string][]string, len(votes))
	var versions []string
	for _, v := range votes {
		if _, seen := consumersByVersion[v.version]; !seen {
			versions = append(versions, v.version)
		}
		consumersByVersion[v.version] = append(consumersByVersion[v.version], v.consumer)
	}
	if len(versions) > 1 {
		var lines []string
		for _, version := range versions {
			lines = append(lines, fmt.Sprintf("%s → %s", strings.Join(consumersByVersion[version], ", "), version))
		}
		return "", fmt.Errorf("%s: workspace members require different versions of %s:\n  %s\nUse --ref to choose one",
			t.name, modulePath, strings.Join(lines, "\n  "))
	}

	version := versions[0]
	ref, _ := gomod.RefForVersion(version)
	resolveAs := ref
	if !gomod.IsPseudoVersion(version) {
		// A version is a tag, not whatever else may share its name.
		resolveAs = "refs/tags/" + version
	}
	if _, err := runner.ResolveRef(t.repoPath, resolveAs); err != nil {
		// E17: say which consumer and which version, never fall back silently.
		return "", fmt.Errorf("%s: version %s, required by %s, does not resolve as %s in this repo; pass --ref",
			t.name, version, strings.Join(consumersByVersion[version], ", "), resolveAs)
	}
	return ref, nil
}

// detachedWorktreeStep returns a saga.Step that creates a read-only member's
// detached worktree and removes it again on rollback. No branch is created,
// checked out, pushed or deleted (ADR 0024 D1).
func detachedWorktreeStep(runner git.Runner, t *repoTarget, fsys ...trace.FS) saga.Step {
	var f trace.FS = trace.NewOsFS(nil)
	if len(fsys) > 0 && fsys[0] != nil {
		f = fsys[0]
	}
	return saga.Step{
		Name: fmt.Sprintf("create read-only worktree %s", t.name),
		Do: func(_ context.Context) error {
			parentDir := filepath.Dir(t.worktreePath)
			if err := f.MkdirAll(parentDir, 0o750); err != nil {
				return fmt.Errorf("create parent dir: %w", err)
			}
			if err := runner.WorktreeAddDetached(t.repoPath, t.worktreePath, t.detachAt); err != nil {
				_ = f.Remove(parentDir)
				return err
			}
			return nil
		},
		Undo: func(_ context.Context) error {
			removeErr := runner.WorktreeRemove(t.repoPath, t.worktreePath, true)
			_ = f.Remove(filepath.Dir(t.worktreePath))
			return removeErr
		},
	}
}

// promotionSteps returns the steps that turn a read-only member into a normal
// worktree on the space's branch (ADR 0024 D5): remove the detached worktree,
// create the branch worktree through the ordinary path (so ADR 0007's branch
// rules apply), then copy secrets. Undo re-creates the detached worktree at the
// recorded ref.
func promotionSteps(cfg *config.Config, runner git.Runner, t *repoTarget, sp *state.Space, f trace.FS) ([]saga.Step, error) {
	steps := []saga.Step{{
		Name: fmt.Sprintf("remove read-only worktree %s", t.name),
		Do: func(_ context.Context) error {
			return runner.WorktreeRemove(t.repoPath, t.worktreePath, true)
		},
		Undo: func(_ context.Context) error {
			if err := f.MkdirAll(filepath.Dir(t.worktreePath), 0o750); err != nil {
				return err
			}
			return runner.WorktreeAddDetached(t.repoPath, t.worktreePath, t.ref)
		},
	}}
	steps = append(steps, worktreeStep(runner, t, sp.Branch, "", f))
	sec, err := secretCopySteps(cfg, t, f)
	if err != nil {
		return nil, err
	}
	return append(steps, sec...), nil
}

// promotionPreflight refuses to promote a modified read-only member. Switching
// the detached worktree to a branch in place would keep the changes, but cannot
// honour ADR 0007's "branch exists → check it out, do not reset" rule, so the
// worktree must be clean first (ADR 0024 D5).
func promotionPreflight(runner git.Runner, r state.RepoEntry) error {
	st, err := runner.Status(r.WorktreePath)
	if err != nil {
		return fmt.Errorf("%s: cannot check the read-only worktree: %w", r.Name, err)
	}
	if len(st.Files) == 0 {
		return nil
	}
	lines := make([]string, len(st.Files))
	for i, f := range st.Files {
		lines[i] = "    " + f.Path
	}
	return fmt.Errorf("%s is a read-only member with uncommitted changes; commit or stash them first, or discard them with `git -C %s checkout -- .`:\n%s",
		r.Name, r.WorktreePath, strings.Join(lines, "\n"))
}
