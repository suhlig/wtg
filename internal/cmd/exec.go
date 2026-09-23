package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/urfave/cli/v3"
	"golang.org/x/term"

	"github.com/geoffamey/wtg/internal/state"
	"github.com/geoffamey/wtg/internal/ui"
)

// execTermWidthFn returns the current terminal column count, or 0 if unknown.
// It is a variable so tests can override it.
var execTermWidthFn = func() int {
	w, _, _ := term.GetSize(int(os.Stdout.Fd()))
	return w
}

// ExecCommand returns the `wtg exec` command.
func ExecCommand() *cli.Command {
	return &cli.Command{
		Name:      "exec",
		Usage:     "run a command in each repo of a workspace",
		ArgsUsage: "[<workspace>] -- <cmd> [<args>...]",
		Description: `Runs a command in each repo's worktree, streaming output as it goes.
A header line identifies each repo. Execution continues even if a command
fails — all repos are attempted and failures are reported at the end.

With --parallel, all commands run concurrently. A progress indicator shows
live status. Output for each repo is buffered and printed serially in repo
order once all commands complete.

The workspace argument is optional when the current directory is inside a
known workspace — it will be inferred automatically. Use -- to separate the
workspace name from the command:

   wtg exec myfeature -- git status
   wtg exec myfeature -- go test ./...
   wtg exec -- git status            # workspace inferred from CWD`,
		ShellComplete: completeSpaceAtFirst,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "parallel",
				Usage: "run the command in all repos concurrently",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			spaceName, execArgs, err := resolveExecArgs(cmd.Args().Slice())
			if err != nil {
				return err
			}
			return RunSpaceExec(spaceName, execArgs, cmd.Bool("parallel"), os.Stdout)
		},
	}
}

// resolveSpaceArg returns a workspace name for commands that take an optional
// single workspace argument. When omitted (empty=="") it infers the workspace
// from the current working directory. Returns an error when the CWD is not
// inside any known workspace.
func resolveSpaceArg(arg string, omitted bool) (string, error) {
	if !omitted {
		return arg, nil
	}
	spaces, err := state.List()
	if err != nil {
		return "", fmt.Errorf("list spaces: %w", err)
	}
	sp := spaceContainingCWD(spaces)
	if sp == nil {
		return "", fmt.Errorf("no workspace found for current directory; specify one explicitly")
	}
	return sp.Name, nil
}

// resolveSpaceAndRepos separates the optional workspace name from a list of
// repo names (used by `wtg add` and `wtg remove`). If the first argument names
// a known space it is used as the workspace and the remainder are repo names;
// otherwise the workspace is inferred from the current working directory and
// all arguments are treated as repo names. Returns an error when no repos are
// provided after resolution, or when CWD inference fails.
func resolveSpaceAndRepos(args []string) (spaceName string, repos []string, err error) {
	if len(args) == 0 {
		return "", nil, fmt.Errorf("no repos specified")
	}

	// Try treating the first argument as an explicit workspace name.
	if _, loadErr := state.Load(args[0]); loadErr == nil {
		if len(args) < 2 {
			return "", nil, fmt.Errorf("no repos specified")
		}
		return args[0], args[1:], nil
	}

	// First arg is not a known space — infer workspace from CWD.
	name, err := resolveSpaceArg("", true)
	if err != nil {
		return "", nil, err
	}
	return name, args, nil
}

// resolveExecArgs separates the optional workspace name from the command
// arguments. If the first argument names a known space it is used; otherwise
// the workspace is inferred from the current working directory. An error is
// returned when neither succeeds or when no command arguments remain.
func resolveExecArgs(args []string) (spaceName string, execArgs []string, err error) {
	if len(args) == 0 {
		return "", nil, fmt.Errorf("usage: wtg exec [<workspace>] -- <cmd> [<args>...]")
	}

	// Try treating the first argument as an explicit workspace name.
	if _, loadErr := state.Load(args[0]); loadErr == nil {
		if len(args) < 2 {
			return "", nil, fmt.Errorf("usage: wtg exec [<workspace>] -- <cmd> [<args>...]")
		}
		return args[0], args[1:], nil
	}

	// First arg is not a known space — infer workspace from CWD.
	name, err := resolveSpaceArg("", true)
	if err != nil {
		return "", nil, err
	}
	return name, args, nil
}

// RunSpaceExec runs a command in each worktree of the named space. When
// parallel is false it runs sequentially, streaming output as it goes. When
// parallel is true all commands run concurrently; a progress indicator
// (matching the sync --progress style) shows live status, and each repo's
// complete buffered output is printed serially in repo order once all commands
// have finished. In both modes execution continues past failures and all
// failed repos are reported at the end.
func RunSpaceExec(spaceName string, args []string, parallel bool, out io.Writer) error {
	sp, err := state.Load(spaceName)
	if err != nil {
		return fmt.Errorf("load space %q: %w", spaceName, err)
	}

	if !parallel {
		return runSpaceExecSerial(sp, args, out)
	}
	return runSpaceExecParallel(sp, args, out)
}

func runSpaceExecSerial(sp *state.Space, args []string, out io.Writer) error {
	var failed []string
	for i, r := range sp.Repos {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "%s\n", ui.SectionHeader(r.Name))
		if r.Symlink {
			fmt.Fprintf(out, "%s\n", ui.Warn.Render(ui.SymWarn+" skipped — symlink (always.repos)"))
			continue
		}
		cmd := exec.Command(args[0], args[1:]...) //nolint:gosec
		cmd.Dir = r.WorktreePath
		cmd.Stdin = os.Stdin
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Run(); err != nil {
			failed = append(failed, r.Name)
			fmt.Fprintf(out, "%s\n", ui.Fail.Render(ui.SymFail+" failed"))
		} else {
			fmt.Fprintf(out, "%s\n", ui.OK.Render(ui.SymOK+" ok"))
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("command failed in: %s", strings.Join(failed, ", "))
	}
	return nil
}

// execResult holds the buffered output and outcome for one parallel exec.
type execResult struct {
	name    string
	output  []byte
	failed  bool
	skipped bool
}

func runSpaceExecParallel(sp *state.Space, args []string, out io.Writer) error {
	repos := sp.Repos
	results := make([]execResult, len(repos))

	// syms holds the current display symbol for each repo slot (progress bar).
	syms := make([]string, len(repos))
	for i := range syms {
		syms[i] = "·"
	}

	termWidth := execTermWidthFn()

	printProgress := func() {
		var b strings.Builder
		if termWidth > 0 {
			extraLines := (len(syms) + 2) / termWidth
			for range extraLines {
				b.WriteString("\x1b[A") // cursor up one line
			}
		}
		b.WriteRune('\r')
		b.WriteRune('[')
		for _, s := range syms {
			b.WriteString(s)
		}
		b.WriteRune(']')
		fmt.Fprint(out, b.String())
	}

	var mu sync.Mutex
	if len(repos) > 0 {
		mu.Lock()
		printProgress()
		mu.Unlock()
	}

	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := execResult{name: r.Name}
			if r.Symlink {
				res.skipped = true
				mu.Lock()
				syms[i] = ui.Warn.Render(ui.SymWarn)
				printProgress()
				mu.Unlock()
				results[i] = res
				return
			}
			var buf bytes.Buffer
			cmd := exec.Command(args[0], args[1:]...) //nolint:gosec
			cmd.Dir = r.WorktreePath
			cmd.Stdin = os.Stdin
			cmd.Stdout = &buf
			cmd.Stderr = &buf
			if err := cmd.Run(); err != nil {
				res.failed = true
			}
			res.output = buf.Bytes()
			mu.Lock()
			if res.failed {
				syms[i] = ui.Fail.Render(ui.SymFail)
			} else {
				syms[i] = ui.OK.Render(ui.SymOK)
			}
			printProgress()
			mu.Unlock()
			results[i] = res
		}()
	}
	wg.Wait()

	// End progress line.
	fmt.Fprintln(out)

	// Print each repo's output serially in repo order.
	var failed []string
	for i, res := range results {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "%s\n", ui.SectionHeader(res.name))
		if res.skipped {
			fmt.Fprintf(out, "%s\n", ui.Warn.Render(ui.SymWarn+" skipped — symlink (always.repos)"))
			continue
		}
		out.Write(res.output) //nolint:errcheck
		if res.failed {
			failed = append(failed, res.name)
			fmt.Fprintf(out, "%s\n", ui.Fail.Render(ui.SymFail+" failed"))
		} else {
			fmt.Fprintf(out, "%s\n", ui.OK.Render(ui.SymOK+" ok"))
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("command failed in: %s", strings.Join(failed, ", "))
	}
	return nil
}
