package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/suhlig/wtg/internal/archive"
	"github.com/suhlig/wtg/internal/config"
	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/remote"
	"github.com/suhlig/wtg/internal/saga"
	"github.com/suhlig/wtg/internal/state"
	"github.com/suhlig/wtg/internal/ui"
)

// ArchiveCommand returns the `wtg repo archive` subcommand.
func ArchiveCommand(runner git.Runner) *cli.Command {
	return &cli.Command{
		Name:      "archive",
		Usage:     "retire main repo clones by moving them to an archive directory",
		ArgsUsage: "<repo>...",
		Description: `Moves each named main repo clone out of the discovery area into
archive.root_dir, keeping the clone intact, and records where it came from.
Nothing is deleted, and without --remote no remote is touched.

Refuses if a clone has uncommitted changes, commits not on any remote, or
stashes unless --force is given. A clone with live (or stale) worktrees, one
referenced by a space, or one listed in always.repos is never archived; --force
does not override these.

When a repo's origin is on github.com, the exact gh command to archive it
upstream is printed at the end. Pass --remote to run that command instead,
archiving each repo upstream as part of the same all-or-nothing operation; it
requires the gh CLI (2.32+) and a github.com origin, and is idempotent (a repo
that is already archived upstream is left as is). Use --dry-run to see the plan
first.`,
		ShellComplete: completeRepos,
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "force", Usage: "proceed despite uncommitted, unpushed, or stashed work"},
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would happen without moving anything"},
			&cli.BoolFlag{Name: "remote", Usage: "also archive each repo on GitHub via the gh CLI"},
			&cli.StringFlag{Name: "archive-dir", Usage: "override archive.root_dir for this run"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return errors.New("at least one repo is required")
			}
			cfg, err := config.Load(cmd.Root().String("config"))
			if err != nil {
				return err
			}
			return RunRepoArchive(cfg, runner, RepoArchiveArgs{
				Repos:      cmd.Args().Slice(),
				Force:      cmd.Bool("force"),
				DryRun:     cmd.Bool("dry-run"),
				ArchiveDir: cmd.String("archive-dir"),
				Remote:     cmd.Bool("remote"),
			}, os.Stdout)
		},
	}
}

// RepoArchiveArgs holds the parsed arguments for RunRepoArchive.
type RepoArchiveArgs struct {
	Repos      []string // repo names to archive (at least one)
	Force      bool     // proceed despite pending work
	DryRun     bool     // report the plan without moving anything
	ArchiveDir string   // overrides cfg.Archive.RootDir when non-empty
	Remote     bool     // also archive each repo upstream on GitHub via gh

	// GH is the gh client used when Remote is set. It is nil in normal use (the
	// system gh CLI is used); tests inject a fake here.
	GH remote.Runner
}

// remoteTimeout bounds the whole remote phase: the gh auth check, the
// per-repo upstream-state checks, and the gh archival that surrounds the local
// moves.
const remoteTimeout = 2 * time.Minute

// archiveTarget is one resolved repo queued for archiving.
type archiveTarget struct {
	name      string // short name, e.g. "github.com/something/foo"
	clone     string // absolute path to the main clone
	dest      string // absolute archive destination
	remoteURL string // origin URL, empty when the clone has no origin
	host      string // origin hostname, empty when there is no origin
	ghSlug    string // "owner/repo" when the origin is on github.com
	ghOK      bool   // whether a `gh repo archive` suggestion applies
}

// RunRepoArchive moves one or more main repo clones into archive.root_dir and
// records their provenance. It never deletes anything. Pending work
// (uncommitted files, commits not on any remote, stashes) aborts the run unless
// args.Force is set; a clone that is in use (live or stale worktrees, referenced
// by a space, or listed in always.repos) aborts the run unconditionally.
//
// With args.Remote, each repo is also archived upstream on GitHub via gh, after
// an auth pre-flight and as part of the same saga, so a failure rolls the local
// moves back. See docs/adr/0013-repo-archive.md.
func RunRepoArchive(cfg *config.Config, runner git.Runner, args RepoArchiveArgs, out io.Writer) error {
	if len(args.Repos) == 0 {
		return errors.New("at least one repo is required")
	}

	archiveRoot, err := resolveArchiveRoot(cfg, args.ArchiveDir)
	if err != nil {
		return err
	}

	roots := cfg.DiscoveryRootDirs()
	allPaths, err := discoverAllRepoPaths(roots, cfg.Discovery.MaxDepth)
	if err != nil {
		return err
	}
	sort.Strings(allPaths)

	names := make([]string, 0, len(allPaths))
	byName := make(map[string]string, len(allPaths))
	for _, p := range allPaths {
		n := repoName(roots, p)
		names = append(names, n)
		byName[n] = p
	}

	targets, err := resolveArchiveTargets(runner, roots, names, byName, archiveRoot, args.Repos)
	if err != nil {
		return err
	}

	refs, err := spaceRefIndex()
	if err != nil {
		return err
	}
	alwaysSet, err := alwaysRepoSet(cfg, names)
	if err != nil {
		return err
	}

	warnings, hardErrs := archivePreflight(runner, targets, refs, alwaysSet)
	if err := requireGitHubTargets(args.Remote, targets); err != nil {
		hardErrs = append(hardErrs, err.Error())
	}
	if len(hardErrs) > 0 {
		return errors.New(strings.Join(hardErrs, "\n\n"))
	}
	if len(warnings) > 0 && !args.Force {
		return fmt.Errorf("%s\nRefusing to archive with pending work; re-run with --force to proceed", strings.Join(warnings, "\n"))
	}

	if args.DryRun {
		printArchivePlan(out, targets, args.Remote)
		return nil
	}

	ctx := context.Background()
	already := map[string]bool{}
	if args.Remote {
		gh := args.GH
		if gh == nil {
			gh = remote.SystemRunner{}
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, remoteTimeout)
		defer cancel()
		if err := gh.Available(); err != nil {
			return err
		}
		if err := gh.AuthStatus(ctx, remote.GitHubHost); err != nil {
			return err
		}
		if already, err = resolveRemoteState(ctx, gh, targets); err != nil {
			return err
		}
		args.GH = gh
	}

	steps := make([]saga.Step, 0, len(targets)*2+1)
	for _, t := range targets {
		steps = append(steps, archiveMoveStep(t, archiveRoot))
	}
	steps = append(steps, archiveRecordStep(targets))
	if args.Remote {
		for _, t := range targets {
			if !already[t.name] {
				steps = append(steps, archiveRemoteStep(args.GH, t))
			}
		}
	}

	if err := saga.Run(ctx, steps); err != nil {
		return err
	}

	printArchiveResult(out, targets, args.Remote, already)
	return nil
}

// resolveArchiveTargets resolves the requested names to canonical repo names and
// builds the archive targets, deriving the GitHub slug from each origin.
func resolveArchiveTargets(runner git.Runner, roots, names []string, byName map[string]string, archiveRoot string, inputs []string) ([]*archiveTarget, error) {
	targets := make([]*archiveTarget, 0, len(inputs))
	seen := make(map[string]bool, len(inputs))
	for _, input := range inputs {
		canonical, ok, err := repoInSet(names, input)
		if err != nil {
			return nil, err
		}
		if !ok {
			if len(roots) == 1 {
				return nil, fmt.Errorf("repo %q not found under %s", input, roots[0])
			}
			return nil, fmt.Errorf("repo %q not found under any discovery root dir", input)
		}
		if seen[canonical] {
			continue
		}
		seen[canonical] = true

		clone := byName[canonical]
		remoteURL, _ := runner.RemoteURL(clone, "origin")
		slug, host, ghOK := remote.ParseGitHubRemote(remoteURL)
		targets = append(targets, &archiveTarget{
			name:      canonical,
			clone:     clone,
			dest:      filepath.Join(archiveRoot, filepath.FromSlash(canonical)),
			remoteURL: remoteURL,
			host:      host,
			ghSlug:    slug,
			ghOK:      ghOK,
		})
	}
	return targets, nil
}

// requireGitHubTargets rejects a --remote run when any requested repo is not on
// github.com, so upstream archival is never silently skipped. It makes no
// network calls, so it also runs under --dry-run.
func requireGitHubTargets(remoteMode bool, targets []*archiveTarget) error {
	if !remoteMode {
		return nil
	}
	var bad []string
	for _, t := range targets {
		if !t.ghOK {
			bad = append(bad, githubSkipReason(t.name, t.remoteURL, t.host))
		}
	}
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf("--remote archives repos on GitHub, but some requested repos are not:\n%s\nArchive them without --remote, or add provider support for those hosts first", strings.Join(bad, "\n"))
}

// githubSkipReason explains, for --remote, why a repo cannot be acted on
// upstream. Callers use it only when the repo is not GitHub-eligible.
func githubSkipReason(name, remoteURL, host string) string {
	switch {
	case remoteURL == "":
		return fmt.Sprintf("  %s: has no origin remote", name)
	case host == "":
		return fmt.Sprintf("  %s: has an origin that cannot be parsed as a git URL: %s", name, remoteURL)
	default:
		return fmt.Sprintf("  %s: origin is on %s, not github.com", name, host)
	}
}

// ghCheck pairs a repo's display name with its owner/repo slug for the read-only
// upstream state check.
type ghCheck struct {
	name string
	slug string
}

// githubState records, per repo, whether it is currently archived upstream. It
// is a read-only gh call, used both for idempotency and output.
func githubState(ctx context.Context, gh remote.Runner, checks []ghCheck) (map[string]bool, error) {
	archived := make(map[string]bool, len(checks))
	var errs []string
	for _, c := range checks {
		ok, err := gh.IsArchived(ctx, remote.GitHubHost, c.slug)
		if err != nil {
			errs = append(errs, fmt.Sprintf("  %s: %v", c.name, err))
			continue
		}
		archived[c.name] = ok
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("cannot check upstream archive state:\n%s", strings.Join(errs, "\n"))
	}
	return archived, nil
}

// resolveRemoteState is the archive-flavoured wrapper around githubState.
func resolveRemoteState(ctx context.Context, gh remote.Runner, targets []*archiveTarget) (map[string]bool, error) {
	checks := make([]ghCheck, len(targets))
	for i, t := range targets {
		checks[i] = ghCheck{name: t.name, slug: t.ghSlug}
	}
	return githubState(ctx, gh, checks)
}

// archivePreflight runs every check before anything is moved. Pending-work
// findings are warnings (overridable with --force); everything else is a hard
// error that --force does not override.
func archivePreflight(runner git.Runner, targets []*archiveTarget, refs map[string][]spaceRef, alwaysSet map[string]bool) (warnings, hardErrs []string) {
	for _, t := range targets {
		if _, err := os.Stat(t.dest); err == nil {
			hardErrs = append(hardErrs, fmt.Sprintf("%s: archive destination already exists:\n  %s", t.name, tildify(t.dest)))
		}

		if rs := refs[filepath.Clean(t.clone)]; len(rs) > 0 {
			spaces := distinctSpaces(rs)
			label := "space"
			if len(spaces) > 1 {
				label = "spaces"
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%s is still referenced by %s %s.\nRemove it from the space first:", t.name, label, quotedNames(spaces))
			for _, cmd := range removeCommands(rs) {
				b.WriteString("\n  " + cmd)
			}
			hardErrs = append(hardErrs, b.String())
		}

		if alwaysSet[t.name] {
			hardErrs = append(hardErrs, fmt.Sprintf(
				"%s is listed in always.repos and is symlinked into every new space.\nRemove it from always.repos in your config first, then retry.", t.name))
		}

		hardErrs = append(hardErrs, worktreeProblems(runner, refs, t)...)
		warnings = append(warnings, pendingWorkWarnings(runner, t)...)
	}
	return warnings, hardErrs
}

// worktreeProblems reports linked worktrees that block archiving: live ones
// (their data would be orphaned) and stale ones (metadata that should be pruned).
func worktreeProblems(runner git.Runner, refs map[string][]spaceRef, t *archiveTarget) []string {
	wts, err := runner.WorktreeList(t.clone)
	if err != nil {
		return []string{fmt.Sprintf("%s: cannot list worktrees: %v", t.name, err)}
	}

	var live, stale []string
	for _, wt := range wts {
		if filepath.Clean(wt.Path) == filepath.Clean(t.clone) {
			continue
		}
		if _, err := os.Stat(wt.Path); err == nil {
			live = append(live, wt.Path)
		} else {
			stale = append(stale, wt.Path)
		}
	}

	var problems []string
	if len(live) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "%s has %d linked worktree(s), so it cannot be archived:\n  %s\nResolve them first, then retry:",
			t.name, len(live), strings.Join(live, "\n  "))
		cmds := worktreeRemovalCommands(refs, live)
		if len(cmds) == 0 {
			cmds = []string{"remove the worktree with:  git worktree remove <path>"}
		}
		for _, cmd := range cmds {
			b.WriteString("\n  " + cmd)
		}
		problems = append(problems, b.String())
	}
	if len(stale) > 0 && len(live) == 0 {
		problems = append(problems, fmt.Sprintf(
			"%s has %d stale worktree entry(ies) (their paths no longer exist), so it cannot be archived:\n  %s\nClean them up, then retry:\n  git -C %s worktree prune",
			t.name, len(stale), strings.Join(stale, "\n  "), t.clone))
	}
	return problems
}

// pendingWorkWarnings reports work that lives only in the clone. The unpushed
// check is skipped when there is no origin remote, where "not on any remote"
// would flag every commit.
func pendingWorkWarnings(runner git.Runner, t *archiveTarget) []string {
	var warnings []string
	if st, err := runner.Status(t.clone); err == nil && len(st.Files) > 0 {
		warnings = append(warnings, fmt.Sprintf("%s: has uncommitted changes", t.name))
	}
	unpushed, stashes, err := runner.PendingWork(t.clone)
	if err == nil {
		if unpushed > 0 && t.remoteURL != "" {
			warnings = append(warnings, fmt.Sprintf("%s: has %d commit(s) not on any remote", t.name, unpushed))
		}
		if stashes > 0 {
			warnings = append(warnings, fmt.Sprintf("%s: has %d stash(es)", t.name, stashes))
		}
	}
	return warnings
}

// archiveMoveStep renames one clone into the archive root, undoing the rename
// (and any directories it created) on rollback.
//
// A cross-filesystem move (EXDEV) is reported, never silently downgraded to a
// copy. That branch is untested — reproducing it needs two real filesystems,
// which the suite cannot create portably — and is accepted tech debt.
func archiveMoveStep(t *archiveTarget, archiveRoot string) saga.Step {
	return saga.Step{
		Name: fmt.Sprintf("move %s", t.name),
		Do: func(ctx context.Context) error {
			if err := os.MkdirAll(filepath.Dir(t.dest), 0o750); err != nil {
				return fmt.Errorf("create archive dir: %w", err)
			}
			if err := os.Rename(t.clone, t.dest); err != nil {
				removeEmptyParents(t.dest, archiveRoot)
				if errors.Is(err, syscall.EXDEV) {
					return fmt.Errorf("cannot move %s across filesystems; put archive.root_dir on the same filesystem as the repo", t.name)
				}
				return err
			}
			return nil
		},
		Undo: func(ctx context.Context) error {
			if err := os.Rename(t.dest, t.clone); err != nil {
				return err
			}
			removeEmptyParents(t.dest, archiveRoot)
			return nil
		},
	}
}

// archiveRecordStep appends the provenance records for all moved clones, and
// removes them again on rollback.
func archiveRecordStep(targets []*archiveTarget) saga.Step {
	records := make([]archive.Record, 0, len(targets))
	names := make([]string, 0, len(targets))
	now := time.Now().UTC()
	for _, t := range targets {
		records = append(records, archive.Record{
			Name:       t.name,
			Origin:     t.clone,
			Remote:     t.remoteURL,
			Host:       t.host,
			ArchivedAt: now,
		})
		names = append(names, t.name)
	}
	return saga.Step{
		Name: "record provenance",
		Do: func(ctx context.Context) error {
			return archive.Append(records...)
		},
		Undo: func(ctx context.Context) error {
			return archive.Remove(names...)
		},
	}
}

// archiveRemoteStep archives one repo upstream, and unarchives it again if a
// later step (or the saga's rollback) needs to undo the run. One step per repo
// keeps a partial failure compensable, mirroring archiveMoveStep.
func archiveRemoteStep(gh remote.Runner, t *archiveTarget) saga.Step {
	return saga.Step{
		Name: fmt.Sprintf("archive %s on GitHub", t.ghSlug),
		Do: func(ctx context.Context) error {
			return gh.Archive(ctx, remote.GitHubHost, t.ghSlug)
		},
		Undo: func(ctx context.Context) error {
			return gh.Unarchive(ctx, remote.GitHubHost, t.ghSlug)
		},
	}
}

// resolveArchiveRoot resolves the effective archive root, expands a leading ~,
// and rejects a root that sits inside a discovery root (where the moved clones
// would be rediscovered).
func resolveArchiveRoot(cfg *config.Config, override string) (string, error) {
	root := override
	if root == "" {
		root = cfg.Archive.RootDir
	}
	root = config.ExpandTilde(root)
	if root == "" {
		return "", errors.New("archive.root_dir is not set; set it in the config or pass --archive-dir")
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("archive.root_dir must be an absolute path, got %q", root)
	}
	root = filepath.Clean(root)

	for _, discoveryRoot := range cfg.DiscoveryRootDirs() {
		if pathWithin(root, discoveryRoot) {
			clean := filepath.Clean(discoveryRoot)
			sibling := filepath.Join(filepath.Dir(clean), filepath.Base(clean)+"-archived")
			return "", fmt.Errorf(
				"archive.root_dir (%s) is inside discovery root %s, so archived repos would be rediscovered;\nset archive.root_dir to a directory outside your discovery roots, e.g. %s",
				tildify(root), tildify(discoveryRoot), tildify(sibling))
		}
	}
	return root, nil
}

// spaceRef points at one repo entry within a space.
type spaceRef struct {
	space string
	repo  string
}

// spaceRefIndex maps cleaned paths (both main clone and worktree paths) to the
// space repo entries that reference them.
func spaceRefIndex() (map[string][]spaceRef, error) {
	spaces, err := state.List()
	if err != nil {
		return nil, err
	}
	index := make(map[string][]spaceRef)
	for _, sp := range spaces {
		for _, r := range sp.Repos {
			ref := spaceRef{space: sp.Name, repo: r.Name}
			for _, p := range []string{r.RepoPath, r.WorktreePath} {
				if p == "" {
					continue
				}
				key := filepath.Clean(p)
				index[key] = appendUniqueRef(index[key], ref)
			}
		}
	}
	return index, nil
}

func appendUniqueRef(refs []spaceRef, ref spaceRef) []spaceRef {
	if slices.Contains(refs, ref) {
		return refs
	}
	return append(refs, ref)
}

// alwaysRepoSet returns the canonical short names of the always.repos entries
// that name a discovered repo.
func alwaysRepoSet(cfg *config.Config, names []string) (map[string]bool, error) {
	set := make(map[string]bool, len(cfg.Always.Repos))
	for _, entry := range cfg.Always.Repos {
		canonical, ok, err := repoInSet(names, entry)
		if err != nil {
			return nil, err
		}
		if ok {
			set[canonical] = true
		}
	}
	return set, nil
}

func distinctSpaces(refs []spaceRef) []string {
	seen := make(map[string]bool)
	var out []string
	for _, r := range refs {
		if !seen[r.space] {
			seen[r.space] = true
			out = append(out, r.space)
		}
	}
	sort.Strings(out)
	return out
}

// removeCommands returns the deduplicated `wtg remove <space> <repo>` commands
// that would clear the given space references.
func removeCommands(refs []spaceRef) []string {
	seen := make(map[string]bool)
	var out []string
	for _, r := range refs {
		line := fmt.Sprintf("wtg remove %s %s", r.space, r.repo)
		if !seen[line] {
			seen[line] = true
			out = append(out, line)
		}
	}
	sort.Strings(out)
	return out
}

// worktreeRemovalCommands maps live worktree paths back to the spaces that own
// them, so the remediation can name the exact command to run.
func worktreeRemovalCommands(refs map[string][]spaceRef, worktrees []string) []string {
	var all []spaceRef
	for _, wt := range worktrees {
		all = append(all, refs[filepath.Clean(wt)]...)
	}
	return removeCommands(all)
}

func quotedNames(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = strconv.Quote(n)
	}
	return strings.Join(quoted, ", ")
}

// pathWithin reports whether path is dir or lives under it.
func pathWithin(path, dir string) bool {
	path = filepath.Clean(path)
	dir = filepath.Clean(dir)
	if path == dir {
		return true
	}
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}

// tildify renders an absolute path under the home directory using ~, for output.
func tildify(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + path[len(home):]
	}
	return path
}

func printArchiveResult(out io.Writer, targets []*archiveTarget, remoteMode bool, already map[string]bool) {
	tbl := ui.NewTableWriter(out)
	for _, t := range targets {
		tbl.Row(t.name, ui.SymOK+" moved to "+tildify(t.dest))
	}
	tbl.Flush()
	if remoteMode {
		printRemoteSummary(out, targets, already)
		return
	}
	printGitHubSuggestions(out, targets, "Upstream repos were not modified. To archive them on GitHub:")
}

func printArchivePlan(out io.Writer, targets []*archiveTarget, remoteMode bool) {
	tbl := ui.NewTableWriter(out)
	for _, t := range targets {
		tbl.Row(t.name, ui.SymLink+" would move to "+tildify(t.dest))
	}
	tbl.Flush()
	if remoteMode {
		printGitHubSuggestions(out, targets, "Upstream repos would be archived on GitHub:")
		return
	}
	printGitHubSuggestions(out, targets, "Upstream repos will not be modified. To archive them on GitHub:")
}

// printRemoteSummary reports what --remote did upstream: which repos it
// archived and which were already archived (idempotent no-ops).
func printRemoteSummary(out io.Writer, targets []*archiveTarget, already map[string]bool) {
	var archived, skipped []string
	for _, t := range targets {
		if already[t.name] {
			skipped = append(skipped, t.ghSlug)
		} else {
			archived = append(archived, t.ghSlug)
		}
	}
	printSlugs(out, "Archived on GitHub:", archived)
	printSlugs(out, "Already archived on GitHub:", skipped)
}

func printSlugs(out io.Writer, header string, slugs []string) {
	if len(slugs) == 0 {
		return
	}
	_, _ = fmt.Fprintf(out, "\n%s\n", ui.Muted.Render(header))
	for _, s := range slugs {
		_, _ = fmt.Fprintf(out, "  %s\n", s)
	}
}

// printGitHubSuggestions prints the `gh` commands for repos whose origin is on
// GitHub, or nothing when none apply.
func printGitHubSuggestions(out io.Writer, targets []*archiveTarget, header string) {
	var cmds []string
	for _, t := range targets {
		if t.ghOK {
			cmds = append(cmds, remote.ArchiveCommand(t.ghSlug))
		}
	}
	if len(cmds) == 0 {
		return
	}
	_, _ = fmt.Fprintf(out, "\n%s\n", ui.Muted.Render(header))
	for _, cmd := range cmds {
		_, _ = fmt.Fprintf(out, "  %s\n", cmd)
	}
}
