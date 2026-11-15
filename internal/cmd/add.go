package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/urfave/cli/v3"

	"github.com/suhlig/wtg/internal/config"
	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/gomod"
	"github.com/suhlig/wtg/internal/saga"
	"github.com/suhlig/wtg/internal/state"
	"github.com/suhlig/wtg/internal/trace"
	"github.com/suhlig/wtg/internal/ui"
)

// AddCommand returns the `wtg add` command.
func AddCommand(runner git.Runner, fsys ...trace.FS) *cli.Command {
	return &cli.Command{
		Name:      "add",
		Usage:     "add repos to a workspace",
		ArgsUsage: "[<workspace>] <repo>...",
		Description: `Creates a new worktree for each specified repo inside an existing workspace,
checking out the workspace's branch. Updates go.work automatically if the
workspace already has one.

If the branch already exists in a repo (locally or on the remote) it is
checked out as-is — no reset or rebase is performed.

--base names the commit-ish a new branch is created from. When it is
origin/<branch>, that branch is recorded as the repo's base, so wtg rebase
and wtg land target it instead of the repo's default branch.

Paths in always.secrets are copied from each source repo into its new
worktree when present.

With --read-only, a repo is added as a member that is consumed, not changed:
a detached worktree at a pinned ref, listed in go.work if it has a go.mod,
and invisible to push and land. Without --ref the ref is the version the other
members' go.mod files select, else origin/<default branch>. A plain
wtg add without --read-only on a read-only member promotes it to an ordinary
worktree.

A repo argument of "." means the repo containing the current directory.

The workspace argument is optional when the current directory is inside a
known workspace — it will be inferred automatically.`,
		ShellComplete: completeSpaceThenRepos,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "read-only",
				Usage: "add repos as read-only members: a detached worktree at a pinned ref",
			},
			&cli.StringFlag{
				Name:  "ref",
				Usage: "ref a read-only member is pinned to or follows (requires --read-only)",
			},
			&cli.StringFlag{
				Name:  "base",
				Usage: "commit-ish to create the branch from (default: HEAD); origin/<branch> is also recorded as the base branch for rebase and land",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() < 1 {
				return fmt.Errorf("usage: wtg add [<workspace>] <repo>...") //nolint:staticcheck
			}
			if cmd.String("ref") != "" && !cmd.Bool("read-only") {
				return fmt.Errorf("--ref only applies to a read-only member; add --read-only, or omit --ref")
			}
			if cmd.String("base") != "" && cmd.Bool("read-only") {
				return fmt.Errorf("--base only applies to a worktree on the workspace branch; omit it with --read-only")
			}
			cfg, err := config.Load(cmd.Root().String("config"))
			if err != nil {
				return err
			}
			spaceName, repos, err := resolveSpaceAndRepos(runner, cfg, cmd.Args().Slice())
			if err != nil {
				return err
			}
			return RunSpaceAdd(cfg, runner, SpaceAddArgs{
				Name:     spaceName,
				Repos:    repos,
				ReadOnly: cmd.Bool("read-only"),
				Ref:      cmd.String("ref"),
				Base:     cmd.String("base"),
			}, os.Stdout, fsys...)
		},
	}
}

// SpaceAddArgs holds the parsed arguments for RunSpaceAdd.
type SpaceAddArgs struct {
	Name     string
	Repos    []string // short names of repos to add (required)
	ReadOnly bool     // --read-only: add as a detached worktree at Ref, not on the branch
	Ref      string   // --ref: the ref to pin or follow (read-only only)
	Base     string   // --base: commit-ish to create the branch from; origin/<branch> is recorded

	// Go resolves what the workspace's other members require when Ref is empty.
	// It is nil in normal use (the go binary is used); tests inject a fake here.
	Go gomod.Runner
}

// RunSpaceAdd adds one or more repos to a workspace by creating worktrees on
// the space's branch and updating the go.work file and state. If a requested
// repo is currently a symlink in the space (added via always.repos), the
// symlink is replaced with a proper checkout.
//
// With args.ReadOnly the checkout is a detached worktree at a pinned ref
// instead of a branch worktree (ADR 0024). Adding a repo that is already a
// read-only member without --read-only promotes it.
func RunSpaceAdd(cfg *config.Config, runner git.Runner, args SpaceAddArgs, out io.Writer, fsys ...trace.FS) error {
	var f trace.FS = trace.NewOsFS(nil)
	if len(fsys) > 0 && fsys[0] != nil {
		f = fsys[0]
	}
	if len(args.Repos) == 0 {
		return fmt.Errorf("no repos specified")
	}
	goRunner := args.Go
	if goRunner == nil {
		goRunner = gomod.New()
	}

	sp, err := state.Load(args.Name)
	if err != nil {
		return fmt.Errorf("load space %q: %w", args.Name, err)
	}

	// Partition requested repos into: upgrade from symlink, promote from
	// read-only, or add fresh. Names are matched against the space's existing
	// repos exactly, by a unique basename, or by a unique partial match,
	// matching how they were added (see repoInSet).
	stateNames := make([]string, 0, len(sp.Repos))
	existingByName := make(map[string]state.RepoEntry, len(sp.Repos))
	for _, r := range sp.Repos {
		stateNames = append(stateNames, r.Name)
		existingByName[r.Name] = r
	}
	var toUpgrade []state.RepoEntry // currently symlinks; will become checkouts
	var toPromote []state.RepoEntry // currently read-only; will become branch worktrees
	var toAdd []string              // repos not yet in the space
	for _, name := range args.Repos {
		canonical, ok, err := repoInSet(stateNames, name)
		if err != nil {
			return err
		}
		if !ok {
			toAdd = append(toAdd, name)
			continue
		}
		r := existingByName[canonical]
		switch {
		case r.Symlink:
			// E2: a symlink becomes whatever kind was asked for.
			toUpgrade = append(toUpgrade, r)
		case r.ReadOnly && args.ReadOnly:
			return fmt.Errorf("repo %q is already a read-only member of space %q; to re-pin it, `wtg remove %s` and then `wtg add --read-only --ref <ref> %s`",
				canonical, args.Name, canonical, canonical)
		case r.ReadOnly:
			if err := promotionPreflight(runner, r); err != nil {
				return err
			}
			toPromote = append(toPromote, r)
		case args.ReadOnly:
			return fmt.Errorf("repo %q is already a read-write member of space %q; use `wtg remove %s` first",
				canonical, args.Name, canonical)
		default:
			return fmt.Errorf("repo %q is already in space %q", canonical, args.Name)
		}
	}

	roots := cfg.DiscoveryRootDirs()
	allPaths, err := discoverAllRepoPaths(roots, cfg.Discovery.MaxDepth)
	if err != nil {
		return err
	}
	sort.Strings(allPaths)

	// Build repoTargets for brand-new repos (not upgrades or promotions).
	var newTargets []*repoTarget
	if len(toAdd) > 0 {
		newTargets, err = buildTargets(roots, sp.Path, allPaths, toAdd)
		if err != nil {
			return err
		}
	}

	// Build repoTargets for symlink upgrades (same path, now a checkout).
	upgradeTargets := make([]*repoTarget, len(toUpgrade))
	for i, r := range toUpgrade {
		upgradeTargets[i] = &repoTarget{
			name:         r.Name,
			repoPath:     r.RepoPath,
			worktreePath: r.WorktreePath,
		}
	}

	// Build repoTargets for promotions (same path, now on the space's branch).
	promotionTargets := make([]*repoTarget, len(toPromote))
	for i, r := range toPromote {
		promotionTargets[i] = &repoTarget{
			name:         r.Name,
			repoPath:     r.RepoPath,
			worktreePath: r.WorktreePath,
		}
	}

	// Pre-flight, before anything is created: resolve every read-only ref (E1)
	// and, for the read-write targets, let ADR 0007's branch rules run.
	if args.ReadOnly {
		readOnlyTargets := append(append([]*repoTarget{}, upgradeTargets...), newTargets...)
		for _, t := range readOnlyTargets {
			if err := prepareReadOnlyTarget(runner, goRunner, sp, t, args.Ref, out); err != nil {
				return err
			}
		}
	} else {
		branchTargets := append(append([]*repoTarget{}, upgradeTargets...), newTargets...)
		branchTargets = append(branchTargets, promotionTargets...)
		if err := classifyBranchTargets(runner, branchTargets, sp.Branch); err != nil {
			return err
		}
	}

	allNew := make([]*repoTarget, 0, len(upgradeTargets)+len(promotionTargets)+len(newTargets))
	allNew = append(allNew, upgradeTargets...)
	allNew = append(allNew, promotionTargets...)
	allNew = append(allNew, newTargets...)

	// A base of origin/<branch> is recorded per repo, so rebase and land know
	// where these branches belong (ADR 0018 D1).
	recordBaseBranches(runner, allNew, args.Base)

	// Build the full target list for go.work: untouched existing members, then
	// the upgraded, promoted and brand-new repos.
	replaced := make(map[string]bool, len(allNew))
	for _, t := range allNew {
		replaced[t.name] = true
	}
	var keepFromState []*repoTarget
	for _, t := range targetsFromState(sp) {
		if !replaced[t.name] {
			keepFromState = append(keepFromState, t)
		}
	}
	allTargets := append(keepFromState, allNew...)
	hasGoMod, _ := detectGoMods(allTargets)
	anyGoMod := applyReadOnlyGoMods(allTargets, hasGoMod)

	var steps []saga.Step
	// For each symlink being upgraded: remove the symlink, then add the checkout.
	for _, t := range upgradeTargets {
		steps = append(steps, saga.Step{
			Name: fmt.Sprintf("remove symlink %s", t.name),
			Do:   func(_ context.Context) error { return f.Remove(t.worktreePath) },
			Undo: func(_ context.Context) error { return f.Symlink(t.repoPath, t.worktreePath) },
		})
		if t.readOnly {
			steps = append(steps, detachedWorktreeStep(runner, t, f))
		} else {
			steps = append(steps, worktreeStep(runner, t, sp.Branch, args.Base, f))
		}
		sec, err := secretCopySteps(cfg, t, f)
		if err != nil {
			return err
		}
		steps = append(steps, sec...)
	}
	for _, t := range promotionTargets {
		ps, err := promotionSteps(cfg, runner, t, sp, f)
		if err != nil {
			return err
		}
		steps = append(steps, ps...)
	}
	for _, t := range newTargets {
		if t.readOnly {
			steps = append(steps, detachedWorktreeStep(runner, t, f))
		} else {
			steps = append(steps, worktreeStep(runner, t, sp.Branch, args.Base, f))
		}
		sec, err := secretCopySteps(cfg, t, f)
		if err != nil {
			return err
		}
		steps = append(steps, sec...)
	}

	goWorkPath := filepath.Join(sp.Path, "go.work")
	if anyGoMod {
		goVersion := detectGoVersion(allTargets, hasGoMod)
		steps = append(steps, goWorkStep(goWorkPath, sp.Path, allTargets, hasGoMod, goVersion, f))
	}

	oldState := *sp
	steps = append(steps, saga.Step{
		Name: "save state",
		Do: func(_ context.Context) error {
			updated := oldState
			updated.GoWorkspace = anyGoMod
			// Rebuild repo list: keep untouched existing entries, then add
			// upgraded, promoted and new repos.
			var repos []state.RepoEntry
			for _, r := range updated.Repos {
				if !replaced[r.Name] {
					repos = append(repos, r)
				}
			}
			for _, t := range allNew {
				repos = append(repos, state.RepoEntry{
					Name:         t.name,
					RepoPath:     t.repoPath,
					WorktreePath: t.worktreePath,
					Base:         t.base,
					ReadOnly:     t.readOnly,
					Ref:          t.ref,
				})
			}
			updated.Repos = repos
			return state.Save(&updated, f)
		},
		Undo: func(_ context.Context) error {
			return state.Save(&oldState, f)
		},
	})

	if err := saga.Run(context.Background(), steps); err != nil {
		return err
	}

	dry := isDry(runner, f)
	if dry {
		_, _ = fmt.Fprintf(out, "%s would add to space %q\n", ui.SymPlan, args.Name)
	} else {
		_, _ = fmt.Fprintf(out, "%s added to space %q\n", ui.SymOK, args.Name)
	}
	tbl := ui.NewTableWriter(out)
	addedNames := make([]string, len(allNew))
	for i, t := range allNew {
		if t.readOnly {
			ref := ui.SymReadOnly + " read-only @ " + t.ref
			if dry {
				tbl.Row("  "+t.name, ui.Muted.Render(ref), ui.SymPlan+" would create worktree "+t.worktreePath)
			} else {
				tbl.Row("  "+t.name, ui.Muted.Render(ref), t.worktreePath)
			}
		} else if dry {
			tbl.Row("  "+t.name, ui.SymPlan+" would create worktree "+t.worktreePath)
		} else {
			tbl.Row("  "+t.name, t.worktreePath)
		}
		addedNames[i] = t.name
	}
	tbl.Flush()
	_, _ = fmt.Fprintf(out, "Workspace root: %s\n", sp.Path)

	if sp, err := state.Load(args.Name); err == nil {
		runSpaceScript(cfg, "add", sp, addedNames, out, f)
	}
	return nil
}

// applyReadOnlyGoMods overrides the go.mod detection for read-only members: the
// go.mod that matters is the one at the pinned commit, not in the main clone,
// because a pin may predate the module (ADR 0024 D7). It returns whether any
// member contributes a go.mod.
func applyReadOnlyGoMods(targets []*repoTarget, hasGoMod []bool) (anyGoMod bool) {
	for i, t := range targets {
		if t.readOnly {
			hasGoMod[i] = t.hasGoMod
		}
		if hasGoMod[i] {
			anyGoMod = true
		}
	}
	return anyGoMod
}
