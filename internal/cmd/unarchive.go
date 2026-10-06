package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/urfave/cli/v3"

	"github.com/suhlig/wtg/internal/archive"
	"github.com/suhlig/wtg/internal/config"
	"github.com/suhlig/wtg/internal/remote"
	"github.com/suhlig/wtg/internal/saga"
	"github.com/suhlig/wtg/internal/ui"
)

// UnarchiveCommand returns the `wtg repo unarchive` subcommand.
func UnarchiveCommand() *cli.Command {
	return &cli.Command{
		Name:      "unarchive",
		Usage:     "restore archived repo clones to their original locations",
		ArgsUsage: "<repo>...",
		Description: `Moves each named repo clone out of archive.root_dir back to the
exact path it was archived from, and forgets its provenance record.

The local restore never touches a remote. Pass --remote to also unarchive the
repo on GitHub via the gh CLI; it requires the gh CLI (2.32+) and a github.com
origin, and is idempotent (a repo that is already unarchived upstream is left
as is). Use --dry-run to see the plan first.`,
		ShellComplete: completeArchivedRepos,
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would happen without moving anything"},
			&cli.BoolFlag{Name: "remote", Usage: "also unarchive each repo on GitHub via the gh CLI"},
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
			return RunRepoUnarchive(cfg, RepoUnarchiveArgs{
				Repos:      cmd.Args().Slice(),
				DryRun:     cmd.Bool("dry-run"),
				Remote:     cmd.Bool("remote"),
				ArchiveDir: cmd.String("archive-dir"),
			}, os.Stdout)
		},
	}
}

// RepoUnarchiveArgs holds the parsed arguments for RunRepoUnarchive.
type RepoUnarchiveArgs struct {
	Repos      []string // archived repo names to restore (at least one)
	DryRun     bool     // report the plan without moving anything
	Remote     bool     // also unarchive each repo upstream on GitHub via gh
	ArchiveDir string   // overrides cfg.Archive.RootDir when non-empty

	// GH is the gh client used when Remote is set. It is nil in normal use (the
	// system gh CLI is used); tests inject a fake here.
	GH remote.Runner
}

// unarchiveTarget is one archived repo queued for restoration.
type unarchiveTarget struct {
	name   string         // short name (the provenance record's name)
	record archive.Record // the provenance record
	source string         // archived clone path (archive.root_dir/<name>)
	dest   string         // recorded origin to restore to
	stop   string         // discovery root containing dest, for empty-dir cleanup
	ghSlug string         // "owner/repo" when the recorded origin is on github.com
	ghOK   bool           // whether a `gh repo unarchive` action applies
}

// RunRepoUnarchive restores one or more archived repo clones to the exact paths
// recorded when they were archived, and removes their provenance records. It
// never deletes anything. With args.Remote each repo is also unarchived
// upstream on GitHub, after an auth pre-flight and as part of the same saga, so
// a failure rolls the local restores back. See docs/adr/0013-repo-archive.md.
func RunRepoUnarchive(cfg *config.Config, args RepoUnarchiveArgs, out io.Writer) error {
	if len(args.Repos) == 0 {
		return errors.New("at least one repo is required")
	}

	archiveRoot, err := resolveArchiveRoot(cfg, args.ArchiveDir)
	if err != nil {
		return err
	}

	record, err := archive.Load()
	if err != nil {
		return err
	}
	if len(record.Entries) == 0 {
		return errors.New("no archived repos are recorded in " + tildify(archive.Path()))
	}

	names := make([]string, 0, len(record.Entries))
	byName := make(map[string]archive.Record, len(record.Entries))
	for _, e := range record.Entries {
		names = append(names, e.Name)
		byName[e.Name] = e
	}

	targets, err := resolveUnarchiveTargets(cfg, archiveRoot, names, byName, args.Repos)
	if err != nil {
		return err
	}

	hardErrs := unarchivePreflight(targets)
	if err := requireGitHubUnarchiveTargets(args.Remote, targets); err != nil {
		hardErrs = append(hardErrs, err.Error())
	}
	if len(hardErrs) > 0 {
		return errors.New(strings.Join(hardErrs, "\n\n"))
	}

	if args.DryRun {
		printUnarchivePlan(out, targets, args.Remote)
		return nil
	}

	ctx := context.Background()
	archived := map[string]bool{}
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
		checks := make([]ghCheck, len(targets))
		for i, t := range targets {
			checks[i] = ghCheck{name: t.name, slug: t.ghSlug}
		}
		if archived, err = githubState(ctx, gh, checks); err != nil {
			return err
		}
		args.GH = gh
	}

	steps := make([]saga.Step, 0, len(targets)*2+1)
	for _, t := range targets {
		steps = append(steps, unarchiveMoveStep(t, archiveRoot))
	}
	steps = append(steps, unarchiveRecordStep(targets))
	if args.Remote {
		for _, t := range targets {
			if archived[t.name] {
				steps = append(steps, unarchiveRemoteStep(args.GH, t))
			}
		}
	}

	if err := saga.Run(ctx, steps); err != nil {
		return err
	}

	printUnarchiveResult(out, targets, args.Remote, archived)
	return nil
}

// resolveUnarchiveTargets resolves the requested names against the recorded
// provenance entries and builds the restore targets, deriving the GitHub slug
// from each record's remote for the --remote step.
func resolveUnarchiveTargets(cfg *config.Config, archiveRoot string, names []string, byName map[string]archive.Record, inputs []string) ([]*unarchiveTarget, error) {
	roots := cfg.DiscoveryRootDirs()
	targets := make([]*unarchiveTarget, 0, len(inputs))
	seen := make(map[string]bool, len(inputs))
	for _, input := range inputs {
		canonical, ok, err := repoInSet(names, input)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("repo %q is not archived", input)
		}
		if seen[canonical] {
			continue
		}
		seen[canonical] = true

		rec := byName[canonical]
		slug, _, ghOK := remote.ParseGitHubRemote(rec.Remote)
		targets = append(targets, &unarchiveTarget{
			name:   canonical,
			record: rec,
			source: filepath.Join(archiveRoot, filepath.FromSlash(canonical)),
			dest:   filepath.Clean(rec.Origin),
			stop:   containingRoot(roots, rec.Origin),
			ghSlug: slug,
			ghOK:   ghOK,
		})
	}
	return targets, nil
}

// unarchivePreflight runs every check before anything is moved. Restoring is
// non-destructive, so every finding here is a hard error.
func unarchivePreflight(targets []*unarchiveTarget) []string {
	var hardErrs []string
	for _, t := range targets {
		if t.record.Origin == "" || !filepath.IsAbs(t.record.Origin) {
			hardErrs = append(hardErrs, fmt.Sprintf(
				"%s: recorded origin is not an absolute path (%q)", t.name, t.record.Origin))
			continue
		}

		info, err := os.Stat(t.source)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			hardErrs = append(hardErrs, fmt.Sprintf(
				"%s: archived clone not found at %s\nIf your archive root moved, pass --archive-dir",
				t.name, tildify(t.source)))
			continue
		case err != nil:
			hardErrs = append(hardErrs, fmt.Sprintf(
				"%s: cannot access archived clone at %s: %v", t.name, tildify(t.source), err))
			continue
		case !info.IsDir():
			hardErrs = append(hardErrs, fmt.Sprintf(
				"%s: archived path %s is not a directory", t.name, tildify(t.source)))
			continue
		}

		if _, err := os.Stat(t.dest); err == nil {
			hardErrs = append(hardErrs, fmt.Sprintf(
				"%s: origin path already exists:\n  %s", t.name, tildify(t.dest)))
		}
	}
	return hardErrs
}

// requireGitHubUnarchiveTargets rejects a --remote run when any requested repo
// cannot be unarchived on GitHub, so upstream restoration is never silently
// skipped. It makes no network calls, so it also runs under --dry-run.
func requireGitHubUnarchiveTargets(remoteMode bool, targets []*unarchiveTarget) error {
	if !remoteMode {
		return nil
	}
	var bad []string
	for _, t := range targets {
		if !t.ghOK {
			bad = append(bad, githubSkipReason(t.name, t.record.Remote, t.record.Host))
		}
	}
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf("--remote unarchives repos on GitHub, but some requested repos are not:\n%s\nRestore them without --remote, or unarchive them on their host manually", strings.Join(bad, "\n"))
}

// unarchiveMoveStep renames one archived clone back to its recorded origin,
// cleaning up the emptied archive directories, and reversing the rename on
// rollback. Its EXDEV branch is reported, not copied, and — like
// archiveMoveStep's — is untested and accepted tech debt.
func unarchiveMoveStep(t *unarchiveTarget, archiveRoot string) saga.Step {
	return saga.Step{
		Name: fmt.Sprintf("restore %s", t.name),
		Do: func(ctx context.Context) error {
			if err := os.MkdirAll(filepath.Dir(t.dest), 0o750); err != nil {
				return fmt.Errorf("create parent dir: %w", err)
			}
			if err := os.Rename(t.source, t.dest); err != nil {
				removeEmptyParents(t.dest, t.stop)
				if errors.Is(err, syscall.EXDEV) {
					return fmt.Errorf("cannot move %s across filesystems; put archive.root_dir on the same filesystem as the repo", t.name)
				}
				return err
			}
			removeEmptyParents(t.source, archiveRoot)
			return nil
		},
		Undo: func(ctx context.Context) error {
			if err := os.MkdirAll(filepath.Dir(t.source), 0o750); err != nil {
				return err
			}
			if err := os.Rename(t.dest, t.source); err != nil {
				return err
			}
			removeEmptyParents(t.dest, t.stop)
			return nil
		},
	}
}

// unarchiveRecordStep removes the provenance records for the restored clones,
// and re-appends them on rollback.
func unarchiveRecordStep(targets []*unarchiveTarget) saga.Step {
	records := make([]archive.Record, 0, len(targets))
	names := make([]string, 0, len(targets))
	for _, t := range targets {
		records = append(records, t.record)
		names = append(names, t.name)
	}
	return saga.Step{
		Name: "forget provenance",
		Do: func(ctx context.Context) error {
			return archive.Remove(names...)
		},
		Undo: func(ctx context.Context) error {
			return archive.Append(records...)
		},
	}
}

// unarchiveRemoteStep unarchives one repo upstream, and re-archives it again if
// a later step (or the saga's rollback) needs to undo the run. One step per repo
// keeps a partial failure compensable.
func unarchiveRemoteStep(gh remote.Runner, t *unarchiveTarget) saga.Step {
	return saga.Step{
		Name: fmt.Sprintf("unarchive %s on GitHub", t.ghSlug),
		Do: func(ctx context.Context) error {
			return gh.Unarchive(ctx, remote.GitHubHost, t.ghSlug)
		},
		Undo: func(ctx context.Context) error {
			return gh.Archive(ctx, remote.GitHubHost, t.ghSlug)
		},
	}
}

// containingRoot returns the longest discovery root that contains path, or ""
// when path is under none of them.
func containingRoot(roots []string, path string) string {
	best := ""
	for _, r := range roots {
		if pathWithin(path, r) && len(r) > len(best) {
			best = r
		}
	}
	return best
}

func printUnarchiveResult(out io.Writer, targets []*unarchiveTarget, remoteMode bool, archived map[string]bool) {
	tbl := ui.NewTableWriter(out)
	for _, t := range targets {
		tbl.Row(t.name, ui.SymOK+" restored to "+tildify(t.dest))
	}
	tbl.Flush()
	if remoteMode {
		printUnarchiveRemoteSummary(out, targets, archived)
		return
	}
	printUnarchiveSuggestions(out, targets, "Upstream repos were not modified. To unarchive them on GitHub:")
}

func printUnarchivePlan(out io.Writer, targets []*unarchiveTarget, remoteMode bool) {
	tbl := ui.NewTableWriter(out)
	for _, t := range targets {
		tbl.Row(t.name, ui.SymLink+" would restore to "+tildify(t.dest))
	}
	tbl.Flush()
	if remoteMode {
		printUnarchiveSuggestions(out, targets, "Upstream repos would be unarchived on GitHub:")
		return
	}
	printUnarchiveSuggestions(out, targets, "Upstream repos will not be modified. To unarchive them on GitHub:")
}

// printUnarchiveRemoteSummary reports what --remote did upstream: which repos it
// unarchived and which were already unarchived (idempotent no-ops).
func printUnarchiveRemoteSummary(out io.Writer, targets []*unarchiveTarget, archived map[string]bool) {
	var unarchived, skipped []string
	for _, t := range targets {
		if archived[t.name] {
			unarchived = append(unarchived, t.ghSlug)
		} else {
			skipped = append(skipped, t.ghSlug)
		}
	}
	printSlugs(out, "Unarchived on GitHub:", unarchived)
	printSlugs(out, "Already unarchived on GitHub:", skipped)
}

// printUnarchiveSuggestions prints the `gh repo unarchive` commands for repos
// whose recorded origin is on GitHub, or nothing when none apply.
func printUnarchiveSuggestions(out io.Writer, targets []*unarchiveTarget, header string) {
	var cmds []string
	for _, t := range targets {
		if t.ghOK {
			cmds = append(cmds, remote.UnarchiveCommand(t.ghSlug))
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
