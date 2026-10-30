package main

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/urfave/cli/v3"

	"github.com/suhlig/wtg/internal/cmd"
	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/remote"
	"github.com/suhlig/wtg/internal/trace"
)

// dryRunCommands is the set of subcommand names that have been converted to
// honour --dry-run (D1 of ADR 0021). A command is safe to add here only when
// all its mutations go through the git runner, the gh runner, or the FS
// interface — never through direct os.* calls.
//
// Every command is converted. A new command must stay out of this map until all
// its mutations go through the git runner, the gh runner, the FS interface, or
// trace.RunProcess; the forbidigo rule in .golangci.yml enforces that.
var dryRunCommands = map[string]bool{
	"land":   true,
	"status": true,
	"path":   true,
	"new":    true,
	"delete": true,
	"add":    true,
	"remove": true,
	"exec":   true,
	"push":   true,
	"rebase": true,
	"repo":   true,
	"config": true,
}

func main() {
	// Allocate the recorder before building runners — runners hold a pointer to
	// it, so SetDryRun() called in Before will be seen by all of them at call time.
	rec := trace.NewRecorder(false)

	// Build all runners and FS once, sharing the single recorder.
	fs := trace.NewOsFS(rec)
	gitRunner := git.New(rec)
	ghRunner := remote.NewSystemRunner(rec)

	// rejected is set by Before when a --dry-run command is not yet converted.
	// After checks it to avoid printing the dry-run header after an early exit.
	var rejected bool

	app := &cli.Command{
		Name:                  "wtg",
		Usage:                 "manage multi-repo feature workflows using git worktrees and Go workspaces",
		Version:               buildVersion(),
		EnableShellCompletion: true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Sources: cli.EnvVars("WTG_CONFIG"),
				Usage:   "path to config file (default: $XDG_CONFIG_HOME/wtg/config.toml)",
			},
			&cli.BoolFlag{
				Name:  "summary",
				Usage: "print a list of git and filesystem commands that ran, to stderr after the run",
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "print the plan without executing anything (opt-in per command; see wtg <cmd> --help)",
			},
		},
		Before: func(ctx context.Context, c *cli.Command) (context.Context, error) {
			if c.Bool("dry-run") {
				// D1 (ADR 0021): refuse commands that have not been converted yet.
				subcmd := c.Args().First()
				if subcmd != "" && !dryRunCommands[subcmd] {
					rejected = true
					return ctx, fmt.Errorf(
						"--dry-run is not yet supported for %q\n\n"+
							"Supported commands: land, status, path, new, delete, add, remove, exec, push, rebase, repo, config\n\n"+
							"Run without --dry-run, or check the progress in docs/adr/0021-dry-run.md",
						subcmd,
					)
				}
				// Activate dry-run mode in the shared recorder so all runners
				// skip mutations from this point forward.
				rec.SetDryRun(true)
				// D3/D4/D5 (ADR 0021): the heading comes first, so the plan
				// below is not mistaken for something that happened.
				if subcmd != "" {
					_, _ = fmt.Fprintln(os.Stdout, "DRY RUN — nothing was changed; the plan is based on the last fetch")
					_, _ = fmt.Fprintln(os.Stdout)
				}
			}
			return ctx, nil
		},
		After: func(ctx context.Context, c *cli.Command) error {
			if rejected {
				return nil
			}
			dryRun := c.Bool("dry-run")
			summary := c.Bool("summary")
			if dryRun {
				// D5 (ADR 0021): the plan goes to stdout, after the result rows.
				rec.Print(os.Stdout, "Would run")
			} else if summary {
				// ADR 0020 D5: output to stderr after the run.
				rec.Print(os.Stderr, "Commands run")
			}
			return nil
		},
		Commands: []*cli.Command{
			cmd.ConfigCommand(fs),
			cmd.RepoCommand(gitRunner, fs),
			cmd.StatusCommand(gitRunner),
			cmd.NewCommand(gitRunner, fs),
			cmd.DeleteCommand(gitRunner, fs),
			cmd.AddCommand(gitRunner, fs),
			cmd.RemoveCommand(gitRunner, fs),
			cmd.ExecCommand(fs),
			cmd.PushCommand(gitRunner),
			cmd.RebaseCommand(gitRunner),
			cmd.LandCommand(gitRunner, ghRunner),
			cmd.PathCommand(),
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "wtg: %s\n", err)
		os.Exit(1)
	}
}

func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(unknown)"
	}
	v := info.Main.Version
	if v == "" || v == "(devel)" {
		return "(devel)"
	}
	return v
}
