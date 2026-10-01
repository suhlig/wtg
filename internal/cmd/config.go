package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/shlex"
	"github.com/urfave/cli/v3"

	"github.com/geoffamey/wtg/internal/config"
)

// configTemplate is the commented config scaffold written by `wtg config init`.
// Every parameter is shown commented out with its default; commented means the
// built-in default applies, so future default changes still reach users.
const configTemplate = `# wtg configuration. Every setting is shown commented out with its default, or an
# example value where the default is empty. Uncomment a line and edit it to override.

[discovery]
# Directory wtg scans to find your git repos. Anything matching under here can be
# pulled into a space by its short name. (default: ~/repos)
# root_dir = "~/repos"

# Additional directories to scan for repos. Use this instead of (or together
# with) root_dir when your clones live in multiple top-level directories.
# All configured roots are searched; repos are identified by their path
# relative to whichever root they were found under.
# root_dirs = ["~/repos", "~/work/repos"]

# How many directory levels below each root dir to descend while scanning.
# Raise this if your clones are nested under org or group subdirectories.
# (default: 2)
# max_depth = 2

[spaces]
# Directory where new spaces are created, one subdirectory per space.
# (default: ~/spaces)
# root_dir = "~/spaces"

[git]
# Prefix prepended to a space name to form its branch name, so space "login" with
# prefix "alice/" branches as "alice/login". Leave empty to branch on the space
# name alone. (default: none)
# branch_prefix = ""

[always]
# Repos symlinked into every new space without getting their own worktree. Use it
# for shared docs or tooling you want on hand but never branch. Default is none;
# example:
# repos = ["docs"]

# Files copied into the root of every new space. Good for editor configs, direnv
# files, or a CLAUDE.md you want present in every workspace. Default is none;
# example:
# files = ["~/.config/wtg/CLAUDE.md"]

# Relative paths looked up in each source repo and copied into that repo's
# worktree when present (e.g. local .env files). Missing files are skipped.
# See docs/always.md. Default is none; example:
# secrets = [".env", "config/local.env"]

# Executable run after a space is created, changed, or deleted. It receives the
# event type and the space path through environment variables. See docs/always.md.
# Default is none; example:
# run = "~/.config/wtg/on-event.sh"
`

// ConfigCommand returns the `wtg config` command group. With no subcommand it
// prints the resolved config file's raw contents.
func ConfigCommand() *cli.Command {
	return &cli.Command{
		Name:  "config",
		Usage: "show and manage the wtg config file",
		Description: `With no subcommand, prints the resolved config file's raw contents
(or a note if none exists). Use the subcommands to scaffold a config
file, edit it in an editor, or print its resolved path.`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runConfigPrint(config.ResolvePath(cmd.String("config")), os.Stdout, os.Stderr)
		},
		Commands: []*cli.Command{
			{
				Name:  "init",
				Usage: "write a commented config template",
				Description: `Writes a commented TOML config template. Refuses if the target
already exists; pass --force to overwrite. -o sets the output path;
-o - writes to stdout (and never refuses).`,
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "force", Usage: "overwrite an existing config file"},
					&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "output path, or - for stdout"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					// Default to the canonical config.toml, not ResolvePath: the latter
					// prefers an existing legacy config.yaml, and writing the TOML
					// template into a .yaml file would corrupt it. An explicit --config
					// or -o still wins.
					out := cmd.String("output")
					if out == "" {
						out = cmd.String("config")
					}
					if out == "" {
						out = config.DefaultPath()
					}
					return runConfigInit(out, cmd.Bool("force"), os.Stdout)
				},
			},
			{
				Name:  "edit",
				Usage: "open the resolved config file in $EDITOR",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runConfigEdit(config.ResolvePath(cmd.String("config")))
				},
			},
			{
				Name:  "path",
				Usage: "print the resolved config file path",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					_, _ = fmt.Fprintln(os.Stdout, config.ResolvePath(cmd.String("config")))
					return nil
				},
			},
		},
	}
}

// runConfigPrint writes the raw contents of the file at path to out, or a note to
// note if it does not exist.
func runConfigPrint(path string, out, note io.Writer) error {
	data, err := os.ReadFile(path) // #nosec G304 -- path is user-supplied config file location
	if errors.Is(err, fs.ErrNotExist) {
		_, _ = fmt.Fprintf(note, "no config file at %s (run `wtg config init` to create one)\n", path)
		return nil
	}
	if err != nil {
		return err
	}
	_, err = out.Write(data)
	return err
}

// runConfigInit writes the config template. With path "-" it writes to stdout and
// never refuses. Otherwise it refuses if the target exists unless force is set.
func runConfigInit(path string, force bool, out io.Writer) error {
	if path == "-" {
		_, err := io.WriteString(out, configTemplate)
		return err
	}
	// The template is TOML, so refuse to write it anywhere Load would parse as
	// something else (a .yaml target would silently become a broken config).
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".toml" {
		return fmt.Errorf("config init writes a TOML template; output path must end in .toml (got %s)", path)
	}
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("config already exists at %s (use --force to overwrite)", path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil { // #nosec G301 -- user config dir
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(configTemplate), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	_, _ = fmt.Fprintf(out, "Config written to %s\n", path)
	// A sibling config.yaml still loads, but config.toml now takes precedence;
	// warn so its settings don't silently appear to vanish.
	if legacy := filepath.Join(filepath.Dir(path), "config.yaml"); legacy != path {
		if _, err := os.Stat(legacy); err == nil {
			_, _ = fmt.Fprintf(out, "Note: %s still exists but is now shadowed by config.toml.\n", legacy)
		}
	}
	return nil
}

// runConfigEdit launches the user's preferred editor to edit the config file at path.
func runConfigEdit(path string) error {
	editorCmd := resolveEditor()
	if len(editorCmd) == 0 {
		return errors.New("no editor found (set $VISUAL or $EDITOR)")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil { // #nosec G301 -- user config dir
		return fmt.Errorf("create config dir: %w", err)
	}

	args := append(editorCmd[1:], path)
	c := exec.Command(editorCmd[0], args...) // #nosec G204 -- user-configured editor
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("run editor %s: %w", editorCmd[0], err)
	}
	return nil
}

// resolveEditor looks up the editor command from $VISUAL, $EDITOR, or standard fallback binaries.
func resolveEditor() []string {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		val := strings.TrimSpace(os.Getenv(env))
		if val != "" {
			parts, err := shlex.Split(val)
			if err == nil && len(parts) > 0 {
				return parts
			}
		}
	}

	for _, fallback := range []string{"nano", "vim", "vi"} {
		if p, err := exec.LookPath(fallback); err == nil && p != "" {
			return []string{fallback}
		}
	}

	return nil
}
