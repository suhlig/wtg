package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
	"golang.org/x/sync/errgroup"

	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/state"
	"github.com/suhlig/wtg/internal/ui"
)

// PushCommand returns the `wtg push` command.
func PushCommand(runner git.Runner) *cli.Command {
	return &cli.Command{
		Name:      "push",
		Usage:     "push all branches in a workspace to origin",
		ArgsUsage: "[<workspace>]",
		Description: `Pushes the workspace's branch from each repo's worktree to origin in
parallel. Repos that fail are reported individually; others are not affected.

The workspace argument is optional when the current directory is inside a
known workspace — it will be inferred automatically.`,
		ShellComplete: completeSpaces,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			spaceName, err := resolveSpaceArg(cmd.Args().First(), cmd.Args().Len() == 0)
			if err != nil {
				return err
			}
			return RunSpacePush(runner, spaceName, os.Stdout)
		},
	}
}

// divergedHint returns a reminder to append to a failed push when the branch
// has diverged from its upstream (typically after a rebase), or "" otherwise.
// It only annotates a failure and never changes what gets pushed; any error
// while checking yields "" so the original push error stands alone.
// See docs/adr/0018-workspace-rebase.md (D5).
func divergedHint(runner git.Runner, worktree, branch string) string {
	st, err := runner.Status(worktree)
	if err != nil || st.Upstream == "" {
		return ""
	}
	isAncestor, err := runner.IsAncestor(worktree, st.Upstream, "HEAD")
	if err != nil || isAncestor {
		return ""
	}
	return fmt.Sprintf("rejected — branch has diverged from %s (typically after a rebase).\n"+
		"  To overwrite the remote: git -C %s push --force-with-lease origin %s",
		st.Upstream, worktree, branch)
}

// RunSpacePush pushes each repo's branch to origin in parallel.
func RunSpacePush(runner git.Runner, spaceName string, out io.Writer) error {
	sp, err := state.Load(spaceName)
	if err != nil {
		return fmt.Errorf("load space %q: %w", spaceName, err)
	}

	results := make([]opResult, len(sp.Repos))

	var g errgroup.Group
	for i, r := range sp.Repos {
		if r.Symlink {
			results[i] = opResult{r.Name, ui.SymWarn, "skipped — symlink (always.repos)"}
			continue
		}
		g.Go(func() error {
			if err := runner.Push(r.WorktreePath, sp.Branch); err != nil {
				msg := err.Error()
				if hint := divergedHint(runner, r.WorktreePath, sp.Branch); hint != "" {
					msg += "\n" + hint
				}
				results[i] = opResult{r.Name, ui.SymFail, msg}
			} else {
				results[i] = opResult{r.Name, ui.SymOK, "pushed " + sp.Branch}
			}
			return nil
		})
	}
	_ = g.Wait() // goroutines always return nil; outcomes are written to results[i]

	tbl := ui.NewTableWriter(out)
	var failed []string
	for _, r := range results {
		tbl.Row(r.name, r.render())
		if r.sym == ui.SymFail {
			failed = append(failed, r.name)
		}
	}
	tbl.Flush()
	if len(failed) > 0 {
		return fmt.Errorf("push failed in: %s", strings.Join(failed, ", "))
	}
	return nil
}
