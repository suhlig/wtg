package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

// NewCommand returns the `wtg new` command.
func NewCommand(runner git.Runner, fsys ...trace.FS) *cli.Command {
	return &cli.Command{
		Name:      "new",
		Usage:     "create a workspace",
		ArgsUsage: "<workspace> [<repo>...]",
		Description: `Creates a workspace: a directory containing one linked git worktree per
repo, all on the same branch. A go.work file is written automatically when
any of the included repos have a go.mod.

At least one repo must be specified. If the branch already exists in a repo
it is checked out as-is — no reset or rebase is performed.
If the branch already exists in a repo it is checked out as-is — no reset or rebase is performed.

--read-only <repo> adds a repo as a member that is consumed, not changed: a
detached worktree at a pinned ref, with no branch of its own (see wtg add).
Repeat the flag for several repos; the positional repos stay ordinary
members, so at least one positional repo is still required. Without --ref the
ref is the version the positional repos' go.mod files select, else
origin/<default branch>. --ref applies to every --read-only repo.

A repo argument of "." means the repo containing the current directory; it is
accepted as a --read-only value too.

Any always.repos are symlinked into the workspace, any always.files are copied
into its root, always.secrets are seeded into each worktree when present in
the source repo, and any always.run hook fires once the workspace is created.`,
		ShellComplete: completeReposAfterFirst,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "branch",
				Usage: "branch name (default: <git.branch_prefix><workspace>)",
			},
			&cli.StringFlag{
				Name:  "path",
				Usage: "workspace root path (default: <spaces.root_dir>/<workspace>)",
			},
			&cli.StringFlag{
				Name:  "base",
				Usage: "commit-ish to branch from (default: HEAD); origin/<branch> is also recorded as the base branch for rebase and land",
			},
			&cli.StringSliceFlag{
				Name:  "read-only",
				Usage: "add <repo> as a read-only member: a detached worktree at a pinned ref (repeatable)",
			},
			&cli.StringFlag{
				Name:  "ref",
				Usage: "ref the read-only members are pinned to or follow (requires --read-only)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return fmt.Errorf("missing required argument: <workspace>")
			}
			if err := rejectDotAsWorkspace(cmd.Args().First()); err != nil {
				return err
			}
			if cmd.Args().Len() < 2 {
				return fmt.Errorf("at least one repo is required; use 'wtg repo status' to see available repos")
			}
			readOnly := cmd.StringSlice("read-only")
			if cmd.String("ref") != "" && len(readOnly) == 0 {
				return fmt.Errorf("--ref only applies to a read-only member; add --read-only <repo>, or omit --ref")
			}
			cfg, err := config.Load(cmd.Root().String("config"))
			if err != nil {
				return err
			}
			return RunSpaceNew(cfg, runner, SpaceNewArgs{
				Name:     cmd.Args().First(),
				Branch:   cmd.String("branch"),
				Path:     cmd.String("path"),
				Base:     cmd.String("base"),
				Repos:    cmd.Args().Tail(),
				ReadOnly: readOnly,
				Ref:      cmd.String("ref"),
			}, os.Stdout, fsys...)
		},
	}
}

// SpaceNewArgs holds the parsed arguments for RunSpaceNew.
type SpaceNewArgs struct {
	Name   string
	Branch string   // overrides cfg.Git.BranchPrefix+Name when set
	Path   string   // overrides cfg.Spaces.RootDir/Name when set
	Base   string   // commit-ish to branch from (default: HEAD)
	Repos  []string // short names; at least one required

	// ReadOnly names repos added as read-only members (ADR 0024): detached
	// worktrees at Ref, not on the workspace branch. Disjoint from Repos.
	ReadOnly []string
	Ref      string // --ref: the ref the read-only members are pinned to or follow

	// Go resolves what the read-write members require when Ref is empty. It is
	// nil in normal use (the go binary is used); tests inject a fake here.
	Go gomod.Runner
}

// RunSpaceNew creates a new workspace, checking out a worktree in each
// selected repo and writing a go.work file if any repos are Go modules.
// Repos listed in cfg.Always.Repos are symlinked into the space unless they
// are also explicitly named in args.Repos or args.ReadOnly (in which case they
// get a worktree).
// Files listed in cfg.Always.Files are copied into the space root.
// Paths in cfg.Always.Secrets are copied from each source repo into its
// worktree when present.
//
// Repos in args.ReadOnly become detached worktrees at a pinned ref instead of
// worktrees on the branch (ADR 0024); every ref is resolved before anything is
// created.
func RunSpaceNew(cfg *config.Config, runner git.Runner, args SpaceNewArgs, out io.Writer, fsys ...trace.FS) error {
	var f trace.FS = trace.NewOsFS(nil)
	if len(fsys) > 0 && fsys[0] != nil {
		f = fsys[0]
	}
	branch := args.Branch
	if branch == "" {
		branch = cfg.Git.BranchPrefix + args.Name
	}
	spacePath := args.Path
	if spacePath == "" {
		spacePath = filepath.Join(cfg.Spaces.RootDir, args.Name)
	}

	if _, err := state.Load(args.Name); err == nil {
		return fmt.Errorf("space %q already exists", args.Name)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("check state for %q: %w", args.Name, err)
	}

	roots := cfg.DiscoveryRootDirs()
	allPaths, err := discoverAllRepoPaths(roots, cfg.Discovery.MaxDepth)
	if err != nil {
		return err
	}
	sort.Strings(allPaths)

	// "." means the repo containing the current directory; resolve it to a
	// short name before any name matching (ADR 0025 D4).
	repos, err := resolveRepoArgs(runner, cfg, args.Repos)
	if err != nil {
		return err
	}
	readOnlyRepos, err := resolveRepoArgs(runner, cfg, args.ReadOnly)
	if err != nil {
		return err
	}

	targets, err := buildTargets(roots, spacePath, allPaths, repos)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return fmt.Errorf("no repos found")
	}

	// buildTargets treats an empty name list as "every repo", which is wrong
	// for the read-only list: only build targets when some were named.
	var readOnlyTargets []*repoTarget
	if len(readOnlyRepos) > 0 {
		readOnlyTargets, err = buildTargets(roots, spacePath, allPaths, readOnlyRepos)
		if err != nil {
			return err
		}
		if err := rejectOverlap(targets, readOnlyTargets); err != nil {
			return err
		}
	}

	if err := classifyBranchTargets(runner, targets, branch); err != nil {
		return err
	}

	// Pre-flight, before anything is created: resolve every read-only ref
	// (ADR 0024 E1). Nothing of this workspace exists yet, so the members whose
	// go.mod is consulted are the read-write repos' main clones, which is what
	// the new branch is cut from by default.
	if len(readOnlyTargets) > 0 {
		goRunner := args.Go
		if goRunner == nil {
			goRunner = gomod.New()
		}
		consumers := &state.Space{Name: args.Name, Path: spacePath, Branch: branch}
		for _, t := range targets {
			consumers.Repos = append(consumers.Repos, state.RepoEntry{
				Name:         t.name,
				RepoPath:     t.repoPath,
				WorktreePath: t.repoPath,
			})
		}
		for _, t := range readOnlyTargets {
			if err := prepareReadOnlyTarget(runner, goRunner, consumers, t, args.Ref, out); err != nil {
				return err
			}
		}
	}

	// A base of origin/<branch> is recorded per repo, so rebase and land know
	// where this branch belongs beyond its lifetime as a worktree (ADR 0018 D1).
	recordBaseBranches(runner, targets, args.Base)

	symlinkTargets, err := resolveAlwaysRepos(cfg, roots, spacePath, allPaths, append(append([]string{}, repos...), readOnlyRepos...))
	if err != nil {
		return err
	}

	allTargets := append(append(append([]*repoTarget{}, targets...), readOnlyTargets...), symlinkTargets...)
	hasGoMod, _ := detectGoMods(allTargets)
	anyGoMod := applyReadOnlyGoMods(allTargets, hasGoMod)

	savedState := false
	steps := []saga.Step{{
		Name: "save state",
		Do: func(ctx context.Context) error {
			sp := buildSpaceState(args.Name, spacePath, branch, anyGoMod, allTargets)
			if err := state.Save(sp, f); err != nil {
				return err
			}
			savedState = true
			return nil
		},
		Undo: func(ctx context.Context) error {
			if savedState {
				return state.Delete(args.Name, f)
			}
			return nil
		},
	}}
	for i := range targets {
		steps = append(steps, worktreeStep(runner, targets[i], branch, args.Base, f))
		sec, err := secretCopySteps(cfg, targets[i], f)
		if err != nil {
			return err
		}
		steps = append(steps, sec...)
	}
	for i := range readOnlyTargets {
		steps = append(steps, detachedWorktreeStep(runner, readOnlyTargets[i], f))
		sec, err := secretCopySteps(cfg, readOnlyTargets[i], f)
		if err != nil {
			return err
		}
		steps = append(steps, sec...)
	}
	for i := range symlinkTargets {
		steps = append(steps, symlinkStep(symlinkTargets[i], f))
	}
	for _, fPath := range cfg.Always.Files {
		steps = append(steps, copyFileStep(fPath, spacePath, f))
	}
	goWorkPath := filepath.Join(spacePath, "go.work")
	if anyGoMod {
		goVersion := detectGoVersion(allTargets, hasGoMod)
		steps = append(steps, goWorkStep(goWorkPath, spacePath, allTargets, hasGoMod, goVersion, f))
	}

	if err := saga.Run(context.Background(), steps); err != nil {
		return err
	}

	dry := isDry(runner, f)
	if dry {
		_, _ = fmt.Fprintf(out, "%s would create space %q on branch %s\n", ui.SymPlan, args.Name, branch)
	} else {
		_, _ = fmt.Fprintf(out, "%s created space %q on branch %s\n", ui.SymOK, args.Name, branch)
	}
	tbl := ui.NewTableWriter(out)
	for _, t := range targets {
		if dry {
			tbl.Row("  "+t.name, ui.SymPlan+" would create worktree "+t.worktreePath)
		} else {
			tbl.Row("  "+t.name, t.worktreePath)
		}
	}
	for _, t := range readOnlyTargets {
		ref := ui.SymReadOnly + " read-only @ " + t.ref
		if dry {
			tbl.Row("  "+t.name, ui.Muted.Render(ref), ui.SymPlan+" would create worktree "+t.worktreePath)
		} else {
			tbl.Row("  "+t.name, ui.Muted.Render(ref), t.worktreePath)
		}
	}
	for _, t := range symlinkTargets {
		if dry {
			tbl.Row("  "+t.name, ui.Muted.Render(ui.SymPlan+" would symlink "+t.repoPath))
		} else {
			tbl.Row("  "+t.name, ui.Muted.Render(ui.SymLink+" "+t.repoPath))
		}
	}
	tbl.Flush()
	_, _ = fmt.Fprintf(out, "Workspace root: %s\n", spacePath)

	sp := buildSpaceState(args.Name, spacePath, branch, anyGoMod, allTargets)
	runSpaceScript(cfg, "create", sp, repoNames(sp), out, f)
	return nil
}

// rejectOverlap returns an error when a repo is named both as a read-write
// member and as a read-only one: it would be two worktrees at the same path.
func rejectOverlap(readWrite, readOnly []*repoTarget) error {
	named := make(map[string]bool, len(readWrite))
	for _, t := range readWrite {
		named[t.name] = true
	}
	for _, t := range readOnly {
		if named[t.name] {
			return fmt.Errorf("repo %q is named both as a read-write repo and with --read-only; it can be only one", t.name)
		}
	}
	return nil
}

// resolveAlwaysRepos builds symlink targets for cfg.Always.Repos, skipping any
// repo that appears in explicitRepos (those will get a proper worktree instead).
// Returns an error if any always-repo name is not found under any discovery root.
func resolveAlwaysRepos(cfg *config.Config, roots []string, spacePath string, allPaths, explicitRepos []string) ([]*repoTarget, error) {
	if len(cfg.Always.Repos) == 0 {
		return nil, nil
	}

	explicitSet := make(map[string]bool, len(explicitRepos))
	for _, r := range explicitRepos {
		explicitSet[r] = true
	}

	byName := make(map[string]string, len(allPaths))
	for _, p := range allPaths {
		name := repoName(roots, p)
		byName[name] = p
	}

	var out []*repoTarget
	for _, name := range cfg.Always.Repos {
		if explicitSet[name] {
			continue
		}
		p, ok := byName[name]
		if !ok {
			if len(roots) == 1 {
				return nil, fmt.Errorf("always.repos entry %q not found under %s", name, roots[0])
			}
			return nil, fmt.Errorf("always.repos entry %q not found under any discovery root dir", name)
		}
		out = append(out, &repoTarget{
			name:         name,
			repoPath:     p,
			worktreePath: filepath.Join(spacePath, filepath.FromSlash(name)),
			symlink:      true,
		})
	}
	return out, nil
}
