package cmd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/urfave/cli/v3"
	"golang.org/x/term"

	"github.com/suhlig/wtg/internal/config"
	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/state"
	"github.com/suhlig/wtg/internal/trace"
	"github.com/suhlig/wtg/internal/ui"
)

// DeleteCommand returns the `wtg delete` command.
func DeleteCommand(runner git.Runner, fsys ...trace.FS) *cli.Command {
	return &cli.Command{
		Name:      "delete",
		Usage:     "delete a workspace and optionally its branches",
		ArgsUsage: "[<workspace>]",
		Description: `Removes all worktrees in the workspace. By default, branches are left
untouched — use --delete-branch (-d) to delete merged branches or
--force-delete-branch (-D) to force-delete regardless of merge state.

Prompts for confirmation if any repo has uncommitted changes or commits that
exist on no remote. Pass --yes to answer the prompt in scripts; without --yes
and without a terminal, delete refuses. A declined prompt exits non-zero, so
"wtg land && wtg delete && ..." stops. -d deletes a branch when no commit
exists only on it.

A repo whose always.secrets copy differs from the main clone (or whose source
no longer exists) is kept, with its branch, and delete exits non-zero; resolve
the difference and run delete again.

The workspace root directory is removed if it is empty after worktrees are
cleaned up.

The workspace argument is optional when the current directory is inside a
known workspace — it will be inferred automatically.`,
		ShellComplete: completeSpaces,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "delete-branch",
				Aliases: []string{"d"},
				Usage:   "delete branch if fully merged into upstream",
			},
			&cli.BoolFlag{
				Name:    "force-delete-branch",
				Aliases: []string{"D"},
				Usage:   "force-delete branch regardless of merge state",
			},
			&cli.BoolFlag{
				Name:    "yes",
				Aliases: []string{"y"},
				Usage:   "answer the confirmation prompt with yes (required when stdin is not a terminal)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			spaceName, err := resolveSpaceArg(cmd.Args().First(), cmd.Args().Len() == 0)
			if err != nil {
				return err
			}
			cfg, err := config.Load(cmd.Root().String("config"))
			if err != nil {
				return err
			}
			return RunSpaceDelete(cfg, runner, SpaceDeleteArgs{
				Name:         spaceName,
				DeleteBranch: cmd.Bool("delete-branch"),
				ForceBranch:  cmd.Bool("force-delete-branch"),
				Yes:          cmd.Bool("yes"),
				NoTerminal:   !term.IsTerminal(int(os.Stdin.Fd())),
			}, os.Stdin, os.Stdout, fsys...)
		},
	}
}

// SpaceDeleteArgs holds the parsed arguments for RunSpaceDelete.
type SpaceDeleteArgs struct {
	Name         string
	DeleteBranch bool // -d: delete branch if no commit exists only on it
	ForceBranch  bool // -D: force-delete branch regardless of merge state
	Yes          bool // --yes: skip the confirmation prompt
	NoTerminal   bool // stdin is not a terminal: refuse to prompt unless Yes is set
}

// RunSpaceDelete removes all worktrees belonging to a space. If -d or -D is
// set, branches are also deleted. Prompts the user when uncommitted changes or
// unpushed commits are detected; only deletes state once all removals succeed.
func RunSpaceDelete(cfg *config.Config, runner git.Runner, args SpaceDeleteArgs, in io.Reader, out io.Writer, fsys ...trace.FS) error {
	var f trace.FS = trace.NewOsFS(nil)
	if len(fsys) > 0 && fsys[0] != nil {
		f = fsys[0]
	}
	sp, err := state.Load(args.Name)
	if err != nil {
		return fmt.Errorf("load space %q: %w", args.Name, err)
	}

	// Pre-flight: gather warnings about uncommitted or unpushed work in
	// parallel (independent per-repo git status calls — see ADR 0023 D3).
	// Symlink entries point to the shared main clone; their state is not
	// specific to this space, so they are excluded from pre-flight checks.
	type warnResult struct {
		msgs   []string
		secret []string // always.secrets files that differ from the main clone
	}
	warnResults := make([]warnResult, len(sp.Repos))
	var wg sync.WaitGroup
	for i, r := range sp.Repos {
		if r.Symlink {
			continue
		}
		wg.Add(1)
		go func(i int, r state.RepoEntry) {
			defer wg.Done()
			st, err := runner.Status(r.WorktreePath)
			if err != nil {
				return // worktree may have been externally deleted; skip
			}
			var msgs []string
			if len(st.Files) > 0 {
				msgs = append(msgs, fmt.Sprintf("%s: has uncommitted changes", r.Name))
			}
			msgs = append(msgs, unpushedWarnings(runner, r, sp.Branch, st.Branch)...)
			warnResults[i] = warnResult{msgs: msgs, secret: changedSecrets(cfg, r)}
		}(i, r)
	}
	wg.Wait()

	// A repo with a changed always.secrets copy is kept (ADR 0019 D11, change 5):
	// it is neither removed nor warned about, because nothing will be deleted.
	kept := make(map[string][]string)
	var warnings []string
	for i, wr := range warnResults {
		if len(wr.secret) > 0 {
			kept[sp.Repos[i].Name] = wr.secret
			continue
		}
		warnings = append(warnings, wr.msgs...)
	}

	needsForce := len(warnings) > 0
	if needsForce {
		for _, w := range warnings {
			_, _ = fmt.Fprintf(out, "  %s %s\n", ui.SymWarn, w)
		}
		switch {
		case trace.IsDryRun(f):
			_, _ = fmt.Fprintln(out, "would ask: Delete workspace anyway?")
		case args.Yes:
			// answered on the command line
		case args.NoTerminal:
			return errors.New("not deleting: stdin is not a terminal, so the confirmation prompt cannot be answered; pass --yes to delete anyway")
		default:
			ok, err := confirm(bufio.NewReader(in), out, "Delete workspace anyway?")
			if err != nil {
				return err
			}
			if !ok {
				return errors.New("aborted: nothing deleted")
			}
		}
	}

	// SymFail means the worktree could not be removed — state must be preserved
	// because the worktree still exists. SymWarn means the worktree was removed
	// but branch deletion failed — state cleanup still proceeds since the
	// worktree is gone and the user can delete the branch manually.
	hadError := false
	tbl := ui.NewTableWriter(out)
	var keptEntries []state.RepoEntry
	for _, r := range sp.Repos {
		if files, isKept := kept[r.Name]; isKept {
			keptEntries = append(keptEntries, r)
			for _, rel := range files {
				tbl.Row(r.Name, ui.Fail.Render(fmt.Sprintf("%s kept: %s differs from the main clone", ui.SymFail, rel)))
			}
			continue
		}
		sym, msg := deleteOne(runner, r, sp.Branch, args.DeleteBranch, args.ForceBranch, needsForce, f)
		tbl.Row(r.Name, sym+" "+msg)
		if sym == ui.SymFail {
			hadError = true
		} else {
			// The worktree is gone; prune the now-empty intermediate
			// directories (e.g. the org/group level of a nested repo name) so
			// the space root can be removed below.
			removeEmptyParents(r.WorktreePath, sp.Path, f)
		}
	}
	tbl.Flush()
	if hadError {
		return fmt.Errorf("some worktrees could not be removed; space %q not deleted from state", args.Name)
	}
	if len(keptEntries) > 0 {
		return keepSpace(sp, keptEntries, args.Name, out, f)
	}
	if isDry(runner, f) {
		_, _ = fmt.Fprintf(out, "Would delete workspace root: %s\n", sp.Path)
	} else {
		_, _ = fmt.Fprintf(out, "Deleting workspace root: %s\n", sp.Path)
	}

	// Warn if the user's working directory is inside the space root; the shell
	// will be left in a broken state after the directory is removed.
	if cwd, err := os.Getwd(); err == nil {
		if cwd == sp.Path || strings.HasPrefix(cwd, sp.Path+string(filepath.Separator)) {
			_, _ = fmt.Fprintf(out, "  %s your working directory is inside the space; cd elsewhere after deletion\n", ui.SymWarn)
		}
	}

	// Remove the go.work file that was written into the space root. Failure here
	// is reported but does not abort the delete — the state and worktrees are
	// already gone.
	if sp.GoWorkspace {
		goWorkPath := filepath.Join(sp.Path, "go.work")
		if err := f.Remove(goWorkPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			_, _ = fmt.Fprintf(out, "  %s could not remove go.work: %v\n", ui.SymWarn, err)
		}
		goWorkSumPath := filepath.Join(sp.Path, "go.work.sum")
		if err := f.Remove(goWorkSumPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			_, _ = fmt.Fprintf(out, "  %s could not remove go.work.sum: %v\n", ui.SymWarn, err)
		}
	}

	// Remove any always.files copies that were seeded into the space root.
	for _, fPath := range cfg.Always.Files {
		dst := filepath.Join(sp.Path, filepath.Base(fPath))
		if err := f.Remove(dst); err != nil && !errors.Is(err, fs.ErrNotExist) {
			_, _ = fmt.Fprintf(out, "  %s could not remove %s: %v\n", ui.SymWarn, filepath.Base(fPath), err)
		}
	}

	// Remove the space root directory. Only succeeds when empty; if the user
	// placed other files there, report a warning so they can clean up manually.
	if err := f.Remove(sp.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		_, _ = fmt.Fprintf(out, "  %s could not remove space directory %s: %v\n", ui.SymWarn, sp.Path, err)
	}

	if err := state.Delete(args.Name, f); err != nil {
		return err
	}
	runSpaceScript(cfg, "delete", sp, repoNames(sp), out, f)
	return nil
}

// unpushedWarnings reports commits that exist on no remote for the workspace
// branch and for whatever else is checked out in the worktree (ADR 0019 D11,
// change 2). Unlike "ahead of upstream", this is zero once a commit is on any
// remote, and non-zero for a branch that was never pushed.
func unpushedWarnings(runner git.Runner, r state.RepoEntry, spaceBranch, checkedOut string) []string {
	var msgs []string
	if n, err := runner.CommitsOnNoRemote(r.WorktreePath, "refs/heads/"+spaceBranch); err == nil && n > 0 {
		msgs = append(msgs, fmt.Sprintf("%s: has %d unpushed commit(s)", r.Name, n))
	}
	if checkedOut != "" && checkedOut != spaceBranch {
		if n, err := runner.CommitsOnNoRemote(r.WorktreePath, "refs/heads/"+checkedOut); err == nil && n > 0 {
			msgs = append(msgs, fmt.Sprintf("%s: has %d unpushed commit(s) on %s", r.Name, n, checkedOut))
		}
	}
	return msgs
}

// changedSecrets returns the always.secrets paths in r's worktree whose
// contents differ from the same path in the main clone, or whose source no
// longer exists (the worktree then holds the only copy). A copy missing from
// the worktree is not a change. Symlinked repos have no copies of their own.
func changedSecrets(cfg *config.Config, r state.RepoEntry) []string {
	if cfg == nil || r.Symlink {
		return nil
	}
	var changed []string
	for _, rel := range cfg.Always.Secrets {
		if validateSecretPath(rel) != nil {
			continue
		}
		cleaned := filepath.Clean(rel)
		copyData, err := os.ReadFile(filepath.Join(r.WorktreePath, cleaned)) // #nosec G304 -- path validated by validateSecretPath
		if err != nil {
			continue // nothing would be lost
		}
		srcData, err := os.ReadFile(filepath.Join(r.RepoPath, cleaned)) // #nosec G304 -- path validated by validateSecretPath
		if err != nil || !bytes.Equal(copyData, srcData) {
			changed = append(changed, cleaned)
		}
	}
	return changed
}

// keepSpace records that only the kept repos remain in the space, repairs
// go.work for them, and returns the error that makes a "wtg delete && ..."
// chain stop. Running delete again resumes with exactly these repos.
func keepSpace(sp *state.Space, keep []state.RepoEntry, name string, out io.Writer, f trace.FS) error {
	remaining := targetsFromState(&state.Space{Repos: keep})
	goWorkspace := false
	if sp.GoWorkspace {
		goWorkPath := filepath.Join(sp.Path, "go.work")
		hasGoMod, anyGoMod := detectGoMods(remaining)
		if anyGoMod {
			goWorkspace = true
			if err := writeGoWork(goWorkPath, sp.Path, remaining, hasGoMod, detectGoVersion(remaining, hasGoMod), f); err != nil {
				_, _ = fmt.Fprintf(out, "  %s could not update go.work: %v\n", ui.SymWarn, err)
			}
		} else if err := f.Remove(goWorkPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			_, _ = fmt.Fprintf(out, "  %s could not remove go.work: %v\n", ui.SymWarn, err)
		}
		if err := f.Remove(filepath.Join(sp.Path, "go.work.sum")); err != nil && !errors.Is(err, fs.ErrNotExist) {
			_, _ = fmt.Fprintf(out, "  %s could not remove go.work.sum: %v\n", ui.SymWarn, err)
		}
	}
	updated := *sp
	updated.Repos = keep
	updated.GoWorkspace = goWorkspace
	if err := state.Save(&updated, f); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	names := make([]string, len(keep))
	for i, r := range keep {
		names[i] = r.Name
	}
	return fmt.Errorf("space %q kept for: %s; copy the changed always.secrets files back (or discard them) and run delete again", name, strings.Join(names, ", "))
}
