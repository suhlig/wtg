package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/urfave/cli/v3"

	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/state"
	"github.com/suhlig/wtg/internal/ui"
)

// RebaseCommand returns the `wtg rebase` command.
func RebaseCommand(runner git.Runner) *cli.Command {
	return &cli.Command{
		Name:      "rebase",
		Usage:     "rebase all branches in a workspace onto their base branch",
		ArgsUsage: "[<workspace>]",
		Description: `Fetches origin and rebases the workspace's branch onto origin/<default
branch> in every repo's worktree, in parallel, with --autostash so that
uncommitted changes are carried along. The default branch is resolved per
repo; use --onto to name one branch for all repos.

A conflict aborts that repo's rebase and restores the worktree (including
uncommitted changes); the retry command is printed. Other repos are not
affected. Nothing is ever pushed.

The workspace argument is optional when the current directory is inside a
known workspace — it will be inferred automatically.
Use --progress to watch per-repo progress; ~ means a repo is busy.`,
		ShellComplete: completeSpaces,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "onto",
				Usage: "rebase onto origin/<branch> instead of each repo's default branch",
			},
			&cli.BoolFlag{
				Name:  "progress",
				Usage: "show live per-repo progress indicators",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			spaceName, err := resolveSpaceArg(cmd.Args().First(), cmd.Args().Len() == 0)
			if err != nil {
				return err
			}
			return RunSpaceRebase(runner, spaceName, cmd.String("onto"), cmd.Bool("progress"), os.Stdout)
		},
	}
}

// rebaseResult is the outcome of rebasing one repo.
type rebaseResult struct {
	opResult
	attention bool     // the repo needs the user's attention (non-zero exit)
	hints     []string // extra lines printed below the table
}

// rebasePhase tracks what a repo's goroutine is currently doing.
type rebasePhase int

const (
	phaseFetching rebasePhase = iota + 1
	phaseRebasing
)

// RunSpaceRebase fetches origin and rebases each repo's workspace branch onto
// its base branch in parallel. See docs/adr/0018-workspace-rebase.md.
func RunSpaceRebase(runner git.Runner, spaceName, onto string, progress bool, out io.Writer) error {
	sp, err := state.Load(spaceName)
	if err != nil {
		return fmt.Errorf("load space %q: %w", spaceName, err)
	}

	results := make([]rebaseResult, len(sp.Repos))

	syms := make([]string, len(sp.Repos))
	for i := range syms {
		syms[i] = "·"
	}
	var fetching, rebasing atomic.Int32
	termWidth := termWidthFn()

	printProgress := func() {
		var parts []string
		if n := fetching.Load(); n > 0 {
			parts = append(parts, fmt.Sprintf("%d fetching", n))
		}
		if n := rebasing.Load(); n > 0 {
			parts = append(parts, fmt.Sprintf("%d rebasing", n))
		}
		suffix := ""
		if len(parts) > 0 {
			suffix = " (" + strings.Join(parts, ", ") + ")"
		}
		var b strings.Builder
		if termWidth > 0 {
			for range (len(syms) + 2 + len(suffix)) / termWidth {
				b.WriteString("\x1b[A")
			}
		}
		b.WriteString("\r[")
		for _, s := range syms {
			b.WriteString(s)
		}
		b.WriteString("]")
		b.WriteString(suffix)
		b.WriteString("\x1b[K")
		_, _ = fmt.Fprint(out, b.String())
	}

	var mu sync.Mutex
	update := func(i int, sym string) {
		if !progress {
			return
		}
		mu.Lock()
		syms[i] = sym
		printProgress()
		mu.Unlock()
	}
	// setPhase moves repo i between the fetching/rebasing counters.
	setPhase := func(i int, from, to rebasePhase) {
		if !progress {
			return
		}
		adjust := func(p rebasePhase, d int32) {
			switch p {
			case phaseFetching:
				fetching.Add(d)
			case phaseRebasing:
				rebasing.Add(d)
			}
		}
		mu.Lock()
		adjust(from, -1)
		adjust(to, 1)
		if to != 0 {
			syms[i] = ui.SymBusy
		}
		printProgress()
		mu.Unlock()
	}

	if progress && len(sp.Repos) > 0 {
		mu.Lock()
		printProgress()
		mu.Unlock()
	}

	var wg sync.WaitGroup
	for i, r := range sp.Repos {
		wg.Go(func() {
			if r.Symlink {
				results[i] = rebaseResult{opResult: opResult{r.Name, ui.SymWarn, "skipped — symlink (always.repos)"}}
				update(i, results[i].renderSym())
				return
			}
			res, omitted := rebaseOne(runner, sp.Branch, onto, r, func(from, to rebasePhase) { setPhase(i, from, to) })
			if omitted {
				update(i, ui.SymSkipped)
				return
			}
			results[i] = res
			update(i, res.renderSym())
		})
	}
	wg.Wait()

	if progress {
		_, _ = fmt.Fprintln(out)
	}

	tbl := ui.NewTableWriter(out)
	var problems []string
	for _, r := range results {
		if r.name == "" {
			continue
		}
		tbl.Row(r.name, r.render())
		if r.sym == ui.SymFail || r.attention {
			problems = append(problems, r.name)
		}
	}
	tbl.Flush()
	for _, r := range results {
		for _, h := range r.hints {
			_, _ = fmt.Fprintf(out, "  %s: %s\n", r.name, h)
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("rebase needs attention in: %s", strings.Join(problems, ", "))
	}
	return nil
}

// rebaseOne fetches and rebases a single repo. omitted is true when the repo
// has no remote and must not appear in the table.
func rebaseOne(runner git.Runner, branch, onto string, r state.RepoEntry, phase func(from, to rebasePhase)) (res rebaseResult, omitted bool) {
	fail := func(format string, a ...any) rebaseResult {
		return rebaseResult{opResult: opResult{r.Name, ui.SymFail, fmt.Sprintf(format, a...)}}
	}
	skip := func(format string, a ...any) rebaseResult {
		return rebaseResult{
			opResult:  opResult{r.Name, ui.SymWarn, "skipped — " + fmt.Sprintf(format, a...)},
			attention: true,
		}
	}

	if none, _ := hasNoRemotes(r.WorktreePath, runner); none {
		return rebaseResult{}, true
	}

	phase(0, phaseFetching)
	err := runner.Fetch(r.WorktreePath)
	if err != nil {
		phase(phaseFetching, 0)
		return fail("fetch failed: %v", err), false
	}
	phase(phaseFetching, phaseRebasing)
	defer phase(phaseRebasing, 0)

	base := onto
	if base == "" {
		base, err = runner.DefaultBranch(r.WorktreePath)
		if err != nil {
			return fail("%v", err), false
		}
	}
	ref := "origin/" + base

	st, err := runner.Status(r.WorktreePath)
	if err != nil {
		return fail("error: %v", err), false
	}
	if st.Branch != branch {
		on := st.Branch
		if on == "" {
			on = "detached HEAD"
		}
		return skip("on %s, expected %s", on, branch), false
	}
	if hasUnmerged(st.Files) {
		return skip("unresolved conflicts in the worktree"), false
	}
	op, err := runner.InProgress(r.WorktreePath)
	if err != nil {
		return fail("error: %v", err), false
	}
	switch op {
	case "":
	case "rebase":
		return skip("rebase in progress; run git rebase --continue or --abort"), false
	default:
		return skip("%s in progress", op), false
	}

	upToDate, err := runner.IsAncestor(r.WorktreePath, ref, "HEAD")
	if err != nil {
		return fail("cannot compare with %s: %v", ref, err), false
	}
	if upToDate {
		return rebaseResult{opResult: opResult{r.Name, ui.SymOK, "up to date"}}, false
	}

	old, err := runner.ShortRev(r.WorktreePath, "HEAD")
	if err != nil {
		return fail("error: %v", err), false
	}

	retry := fmt.Sprintf("git -C %s rebase --autostash %s", r.WorktreePath, ref)
	rebased := fmt.Sprintf("rebased (was %s)", old)

	err = runner.Rebase(r.WorktreePath, ref)
	switch {
	case err == nil:
		return rebaseResult{opResult: opResult{r.Name, ui.SymUp, rebased}}, false

	case errors.Is(err, git.ErrRebaseConflict):
		if abortErr := runner.RebaseAbort(r.WorktreePath); abortErr != nil {
			return rebaseResult{
				opResult: opResult{r.Name, ui.SymFail, "conflict — could not abort the rebase"},
				hints:    []string{fmt.Sprintf("%v; run git -C %s rebase --abort", abortErr, r.WorktreePath)},
			}, false
		}
		return rebaseResult{
			opResult: opResult{r.Name, ui.SymFail, "conflict — rebase aborted, your uncommitted changes were restored"},
			hints:    []string{"to resolve by hand: " + retry},
		}, false

	case errors.Is(err, git.ErrAutostashConflict):
		return rebaseResult{
			opResult:  opResult{r.Name, ui.SymWarn, rebased + ", but your uncommitted changes conflict with the new base"},
			attention: true,
			hints: []string{
				fmt.Sprintf("the branch is rebased; conflict markers are in %s and your changes are kept in the stash (git stash list)", r.WorktreePath),
				"resolve and git add, then git stash drop; or git checkout -- . && git stash pop to retry later",
			},
		}, false

	case errors.Is(err, git.ErrUntrackedBlocked):
		return rebaseResult{
			opResult: opResult{r.Name, ui.SymFail, "blocked by untracked files"},
			hints:    []string{"untracked files are not stashed; move or delete them and retry (worktree unchanged): " + retry},
		}, false

	default:
		return fail("%v", err), false
	}
}

// hasUnmerged reports whether any file is in an unmerged (conflicted) state.
func hasUnmerged(files []git.FileStatus) bool {
	for _, f := range files {
		x, y := f.Index, f.Worktree
		if x == 'U' || y == 'U' || (x == 'A' && y == 'A') || (x == 'D' && y == 'D') {
			return true
		}
	}
	return false
}
