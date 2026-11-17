package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/urfave/cli/v3"
	"golang.org/x/term"

	"github.com/suhlig/wtg/internal/config"
	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/state"
	"github.com/suhlig/wtg/internal/trace"
	"github.com/suhlig/wtg/internal/ui"
)

// execTermWidthFn returns the current terminal column count, or 0 if unknown.
// It is a variable so tests can override it.
var execTermWidthFn = func() int {
	w, _, _ := term.GetSize(int(os.Stdout.Fd()))
	return w
}

// ExecCommand returns the `wtg exec` command.
func ExecCommand(fsys ...trace.FS) *cli.Command {
	return &cli.Command{
		Name:      "exec",
		Usage:     "run a command in each repo of a workspace",
		ArgsUsage: "[<workspace>] -- <cmd> [<args>...]",
		Description: `Runs a command in each repo's worktree, streaming output as it goes.
A header line identifies each repo. Execution continues even if a command
fails — all repos are attempted and failures are reported at the end.

With --parallel, all commands run concurrently. A progress indicator shows
live status. Output for each repo is buffered and printed serially in repo
order once all commands complete. If a command in one repo runs long, the
progress line names it.

Use --timeout to stop a command that runs longer than a given time in any repo
(a Go duration such as 90s or 10m). That repo is reported as timed out and the
others carry on. There is no timeout by default: exec runs your own commands,
and only you know how long they should take.

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
			&cli.DurationFlag{
				Name:  "timeout",
				Usage: "stop the command in a repo after this long, e.g. 90s (default: no limit)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			spaceName, execArgs, err := resolveExecArgs(cmd.Args().Slice())
			if err != nil {
				return err
			}
			return RunSpaceExecTimeout(spaceName, execArgs, cmd.Bool("parallel"), cmd.Duration("timeout"), os.Stdout, fsys...)
		},
	}
}

// resolveSpaceArg returns a workspace name for commands that take an optional
// single workspace argument. When omitted, or when arg is ".", it infers the
// workspace from the current working directory. Returns an error when the CWD is
// not inside any known workspace.
func resolveSpaceArg(arg string, omitted bool) (string, error) {
	if !omitted && !isDotArg(arg) {
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
//
// A repo argument of "." is replaced with the canonical short name of the repo
// containing the current directory (ADR 0025). In this position "." always
// denotes a repo, never the workspace: the first positional "." means the
// current workspace would make `wtg remove .` from a member worktree mean
// "remove nothing" (ADR 0025 D7).
func resolveSpaceAndRepos(runner git.Runner, cfg *config.Config, args []string) (spaceName string, repos []string, err error) {
	if len(args) == 0 {
		return "", nil, fmt.Errorf("no repos specified")
	}

	// Try treating the first argument as an explicit workspace name. A path-like
	// argument never names a workspace, so it skips this stage.
	if !isPathArg(args[0]) {
		if _, loadErr := state.Load(args[0]); loadErr == nil {
			if len(args) < 2 {
				return "", nil, fmt.Errorf("no repos specified")
			}
			repos, err := resolveRepoArgs(runner, cfg, args[1:])
			return args[0], repos, err
		}
	}

	// First arg is not a known space — infer workspace from CWD.
	name, err := resolveSpaceArg("", true)
	if err != nil {
		return "", nil, err
	}
	repos, err = resolveRepoArgs(runner, cfg, args)
	return name, repos, err
}

// resolveExecArgs separates the optional workspace name from the command
// arguments. If the first argument names a known space it is used; otherwise
// the workspace is inferred from the current working directory. An error is
// returned when neither succeeds or when no command arguments remain.
func resolveExecArgs(args []string) (spaceName string, execArgs []string, err error) {
	if len(args) == 0 {
		return "", nil, fmt.Errorf("usage: wtg exec [<workspace>] -- <cmd> [<args>...]")
	}

	// An explicit "." names the workspace containing the current directory, so
	// `wtg exec . -- <cmd>` is the explicit form of `wtg exec -- <cmd>`.
	if isDotArg(args[0]) {
		if len(args) < 2 {
			return "", nil, fmt.Errorf("usage: wtg exec [<workspace>] -- <cmd> [<args>...]")
		}
		name, err := resolveSpaceArg("", true)
		if err != nil {
			return "", nil, err
		}
		return name, args[1:], nil
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
func RunSpaceExec(spaceName string, args []string, parallel bool, out io.Writer, fsys ...trace.FS) error {
	return RunSpaceExecTimeout(spaceName, args, parallel, 0, out, fsys...)
}

// execKillGrace is how long a command that timed out gets to exit after being
// interrupted before it is killed.
const execKillGrace = 5 * time.Second

// execProc is one user command, started in dir. With a timeout it is stopped
// when the time is up. Where stdin is not a terminal it runs in a process group
// of its own and the whole group is stopped, so a shell's children do not
// outlive it. On a terminal it stays in wtg's group: a command in the
// background group that reads the terminal is stopped by SIGTTIN, and an
// interactive command must keep working. Then only the command itself is
// stopped, and its children are cut off from the output after a grace period.
// Without a timeout it is always in wtg's group, where the terminal's Ctrl-C
// reaches it, as before; wtg also interrupts it itself, for a signal that was
// sent to wtg alone.
type execProc struct {
	*exec.Cmd
	ctx     context.Context
	timeout time.Duration
	stop    func() // releases the timer and the tracker entry
}

// startExec builds the command for the repo called name. The command is not
// started; run it with trace.RunProcess and call stop when done.
func startExec(name, dir string, args []string, timeout time.Duration) *execProc {
	// The cancel func ends the command on timeout and when wtg is interrupted
	// (StopProcesses), including by a signal sent to wtg alone.
	ctx, cancel := context.WithCancel(context.Background())
	if timeout > 0 {
		var cancelTimeout context.CancelFunc
		ctx, cancelTimeout = context.WithTimeout(ctx, timeout)
		inner := cancel
		cancel = func() { cancelTimeout(); inner() }
	}
	cmd := trace.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = execKillGrace
	if timeout > 0 && !stdinIsTerminal() {
		// Stop the whole group, so a shell's children do not outlive it.
		trace.OwnProcessGroup(cmd)
		cmd.Cancel = func() error { return trace.TerminateGroup(cmd) }
	}
	end := execProcs.begin(name, cancel)
	return &execProc{Cmd: cmd, ctx: ctx, timeout: timeout, stop: func() { end(); cancel() }}
}

// stdinIsTerminal reports whether wtg's stdin is a terminal. A variable so
// tests can override it.
var stdinIsTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// timedOut reports whether the command was stopped for running too long.
func (p *execProc) timedOut() bool {
	return p.timeout > 0 && errors.Is(p.ctx.Err(), context.DeadlineExceeded)
}

// timeoutMsg is the status line shown for a command that was stopped.
func timeoutMsg(d time.Duration) string { return "timed out after " + d.String() }

// RunSpaceExecTimeout is RunSpaceExec with a limit on how long the command may
// run in each repo. A timeout of zero or less means no limit. A command that
// exceeds it is interrupted, and killed if it does not exit; the repo is
// reported as timed out and the others carry on. The command and the processes
// it started are stopped together.
func RunSpaceExecTimeout(spaceName string, args []string, parallel bool, timeout time.Duration, out io.Writer, fsys ...trace.FS) error {
	var f trace.FS = trace.NewOsFS(nil)
	if len(fsys) > 0 && fsys[0] != nil {
		f = fsys[0]
	}
	sp, err := state.Load(spaceName)
	if err != nil {
		return fmt.Errorf("load space %q: %w", spaceName, err)
	}

	if !parallel {
		return runSpaceExecSerial(sp, args, timeout, out, f)
	}
	return runSpaceExecParallel(sp, args, timeout, out, f)
}

func runSpaceExecSerial(sp *state.Space, args []string, timeout time.Duration, out io.Writer, f trace.FS) error {
	var failed []string
	isDry := trace.IsDryRun(f)
	for i, r := range sp.Repos {
		if i > 0 {
			_, _ = fmt.Fprintln(out)
		}
		_, _ = fmt.Fprintf(out, "%s\n", ui.SectionHeader(r.Name))
		if r.Symlink {
			_, _ = fmt.Fprintf(out, "%s\n", ui.Warn.Render(ui.SymWarn+" skipped — symlink (always.repos)"))
			continue
		}
		if isDry {
			_ = trace.RunProcess(f, r.WorktreePath, trace.FmtArgs(args), nil)
			_, _ = fmt.Fprintf(out, "%s\n", ui.OK.Render("→ would run in "+r.WorktreePath+": "+trace.FmtArgs(args)))
			continue
		}
		p := startExec(r.Name, r.WorktreePath, args, timeout)
		p.Stdout = out
		p.Stderr = out
		err := trace.RunProcess(f, r.WorktreePath, trace.FmtArgs(args), func() error { return p.Run() })
		p.stop()
		if err != nil {
			failed = append(failed, r.Name)
			msg := "failed"
			if p.timedOut() {
				msg = timeoutMsg(timeout)
			}
			_, _ = fmt.Fprintf(out, "%s\n", ui.Fail.Render(ui.SymFail+" "+msg))
		} else {
			_, _ = fmt.Fprintf(out, "%s\n", ui.OK.Render(ui.SymOK+" ok"))
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
	timeout time.Duration // non-zero when the command was stopped for running too long
}

func runSpaceExecParallel(sp *state.Space, args []string, timeout time.Duration, out io.Writer, f trace.FS) error {
	repos := sp.Repos
	results := make([]execResult, len(repos))
	isDry := trace.IsDryRun(f)

	// syms holds the current display symbol for each repo slot (progress bar).
	syms := make([]string, len(repos))
	for i := range syms {
		syms[i] = "·"
	}

	termWidth := execTermWidthFn()

	printProgress := func() {
		// Output is buffered until every command is done, so name a repo that
		// is taking long here, or the bar would sit still without saying why.
		suffix := ""
		if note := slowBarNote(execProcs, nil); note != "" {
			suffix = " (" + note + ")"
		}
		var b strings.Builder
		if termWidth > 0 {
			extraLines := (len(syms) + 2 + len(suffix)) / termWidth
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
		b.WriteString(suffix)
		b.WriteString("\x1b[K") // erase to end of line so a shrinking suffix leaves no ghost chars
		_, _ = fmt.Fprint(out, b.String())
	}

	var mu sync.Mutex
	if len(repos) > 0 {
		mu.Lock()
		printProgress()
		mu.Unlock()
	}
	// Redraw on a timer so the slow note appears and its duration ticks while
	// every command is still running.
	stopSlow := watchSlow(execProcs, slowInterval, func() {
		mu.Lock()
		printProgress()
		mu.Unlock()
	})

	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Go(func() {
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
			if isDry {
				_ = trace.RunProcess(f, r.WorktreePath, trace.FmtArgs(args), nil)
				res.output = []byte("→ would run in " + r.WorktreePath + ": " + trace.FmtArgs(args) + "\n")
				mu.Lock()
				syms[i] = ui.OK.Render(ui.SymOK)
				printProgress()
				mu.Unlock()
				results[i] = res
				return
			}
			var buf bytes.Buffer
			p := startExec(r.Name, r.WorktreePath, args, timeout)
			p.Stdout = &buf
			p.Stderr = &buf
			if err := trace.RunProcess(f, r.WorktreePath, trace.FmtArgs(args), func() error { return p.Run() }); err != nil {
				res.failed = true
				if p.timedOut() {
					res.timeout = timeout
				}
			}
			p.stop()
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
		})
	}
	wg.Wait()
	stopSlow() // before the final newline, or a redraw would land after it

	// End progress line.
	_, _ = fmt.Fprintln(out)

	// Print each repo's output serially in repo order.
	var failed []string
	for i, res := range results {
		if i > 0 {
			_, _ = fmt.Fprintln(out)
		}
		_, _ = fmt.Fprintf(out, "%s\n", ui.SectionHeader(res.name))
		if res.skipped {
			_, _ = fmt.Fprintf(out, "%s\n", ui.Warn.Render(ui.SymWarn+" skipped — symlink (always.repos)"))
			continue
		}
		if isDry {
			_, _ = out.Write(res.output)
			continue
		}
		_, _ = out.Write(res.output)
		if res.failed {
			failed = append(failed, res.name)
			msg := "failed"
			if res.timeout > 0 {
				msg = timeoutMsg(res.timeout)
			}
			_, _ = fmt.Fprintf(out, "%s\n", ui.Fail.Render(ui.SymFail+" "+msg))
		} else {
			_, _ = fmt.Fprintf(out, "%s\n", ui.OK.Render(ui.SymOK+" ok"))
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("command failed in: %s", strings.Join(failed, ", "))
	}
	return nil
}
