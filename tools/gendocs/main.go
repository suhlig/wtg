package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/suhlig/wtg/internal/cmd"
)

func main() {
	outDir := "site/content/reference"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", outDir, err)
		os.Exit(1)
	}

	app := buildApp()

	if err := generateCLIReference(app, filepath.Join(outDir, "cli.md")); err != nil {
		fmt.Fprintf(os.Stderr, "generate cli.md: %v\n", err)
		os.Exit(1)
	}

	if err := generateConfigReference(filepath.Join(outDir, "config.md")); err != nil {
		fmt.Fprintf(os.Stderr, "generate config.md: %v\n", err)
		os.Exit(1)
	}

	if err := generateReadOnlyReference(filepath.Join(outDir, "read-only.md")); err != nil {
		fmt.Fprintf(os.Stderr, "generate read-only.md: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Generated reference documentation in", outDir)
}

func buildApp() *cli.Command {
	return &cli.Command{
		Name:                  "wtg",
		Usage:                 "manage multi-repo feature workflows using git worktrees and Go workspaces",
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
		Commands: []*cli.Command{
			cmd.ConfigCommand(),
			cmd.RepoCommand(nil, nil),
			cmd.StatusCommand(nil),
			cmd.NewCommand(nil),
			cmd.DeleteCommand(nil, nil),
			cmd.AddCommand(nil),
			cmd.RemoveCommand(nil, nil),
			cmd.ExecCommand(),
			cmd.PushCommand(nil),
			cmd.RebaseCommand(nil),
			cmd.LandCommand(nil, nil),
			cmd.PathCommand(),
		},
	}
}

func generateCLIReference(app *cli.Command, outPath string) error {
	var b bytes.Buffer

	b.WriteString(`+++
title = "CLI Reference"
description = "Exhaustive command and flag reference for the wtg CLI."
weight = 10
+++

# CLI Reference

` + app.Usage + `

## Global Flags

The following flags apply to ` + "`wtg`" + ` and can be passed with any command:

`)

	renderFlagsTable(&b, app.Flags)

	b.WriteString("\n## Commands\n\n")

	// Sort commands by name or keep order
	for _, c := range app.Commands {
		renderCommand(&b, "wtg", c, 2)
	}

	return os.WriteFile(outPath, b.Bytes(), 0644)
}

func renderCommand(b *bytes.Buffer, parent string, c *cli.Command, level int) {
	fullName := parent + " " + c.Name
	heading := strings.Repeat("#", level)

	fmt.Fprintf(b, "%s `%s`\n\n", heading, fullName)
	if c.Usage != "" {
		fmt.Fprintf(b, "**%s**\n\n", c.Usage)
	}

	// Synopsis
	synopsis := fullName
	if len(c.Flags) > 0 {
		synopsis += " [flags]"
	}
	if c.ArgsUsage != "" {
		synopsis += " " + c.ArgsUsage
	}
	fmt.Fprintf(b, "```sh\n%s\n```\n\n", synopsis)

	if c.Description != "" {
		fmt.Fprintf(b, "%s\n\n", strings.TrimSpace(c.Description))
	}

	if len(c.Flags) > 0 {
		fmt.Fprintf(b, "%s# Flags\n\n", heading)
		renderFlagsTable(b, c.Flags)
	}

	if len(c.Commands) > 0 {
		fmt.Fprintf(b, "%s# Subcommands\n\n", heading)
		for _, sub := range c.Commands {
			renderCommand(b, fullName, sub, level+1)
		}
	}
}

type flagInfo struct {
	Name        string
	Aliases     string
	Type        string
	Default     string
	EnvVar      string
	Description string
}

func renderFlagsTable(b *bytes.Buffer, flags []cli.Flag) {
	if len(flags) == 0 {
		return
	}

	var infos []flagInfo
	for _, f := range flags {
		info := extractFlagInfo(f)
		infos = append(infos, info)
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})

	b.WriteString("| Flag | Alias | Default | Description |\n")
	b.WriteString("|------|-------|---------|-------------|\n")
	for _, info := range infos {
		flagCol := "`--" + info.Name + "`"
		aliasCol := ""
		if info.Aliases != "" {
			aliasCol = "`-" + info.Aliases + "`"
		}
		defaultCol := info.Default
		if defaultCol != "" {
			defaultCol = "`" + defaultCol + "`"
		}
		descCol := info.Description
		if info.EnvVar != "" {
			descCol += " (env: `" + info.EnvVar + "`)"
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s |\n", flagCol, aliasCol, defaultCol, descCol)
	}
	b.WriteString("\n")
}

func extractFlagInfo(f cli.Flag) flagInfo {
	var info flagInfo
	names := f.Names()
	if len(names) > 0 {
		info.Name = names[0]
		if len(names) > 1 {
			info.Aliases = strings.Join(names[1:], ", -")
		}
	}

	switch v := f.(type) {
	case *cli.BoolFlag:
		info.Type = "bool"
		info.Description = v.Usage
		if v.Value {
			info.Default = "true"
		}
	case *cli.StringFlag:
		info.Type = "string"
		info.Description = v.Usage
		info.Default = v.Value
		if len(v.Sources.Chain) > 0 {
			info.EnvVar = formatSources(v.Sources)
		}
	case *cli.StringSliceFlag:
		info.Type = "string slice"
		info.Description = v.Usage
		if len(v.Value) > 0 {
			info.Default = strings.Join(v.Value, ", ")
		}
	case *cli.IntFlag:
		info.Type = "int"
		info.Description = v.Usage
		if v.Value != 0 {
			info.Default = fmt.Sprintf("%d", v.Value)
		}
	case *cli.DurationFlag:
		info.Type = "duration"
		info.Description = v.Usage
		if v.Value != 0 {
			info.Default = v.Value.String()
		}
	default:
		info.Description = f.String()
	}

	return info
}

func formatSources(sources cli.ValueSourceChain) string {
	var envs []string
	for _, s := range sources.Chain {
		if strings.HasPrefix(s.String(), "environment variable ") {
			envs = append(envs, strings.TrimPrefix(s.String(), "environment variable "))
		} else {
			envs = append(envs, s.String())
		}
	}
	return strings.Join(envs, ", ")
}

func generateConfigReference(outPath string) error {
	scaffold := cmd.ConfigTemplate()

	content := fmt.Sprintf(`+++
title = "Configuration Reference"
description = "Configuration file format, settings, and environment variables."
weight = 20
+++

# Configuration Reference

wtg reads its configuration from a TOML file. By default, it looks for:

1. `+"`$WTG_CONFIG`"+` (if set)
2. `+"`--config <path>`"+` (if passed)
3. `+"`$XDG_CONFIG_HOME/wtg/config.toml`"+` (or `+"`~/.config/wtg/config.toml`"+`)
4. Legacy `+"`~/.config/wtg/config.yaml`"+` (for backwards compatibility)

Run `+"`wtg config init`"+` to generate a fresh configuration file with all defaults documented in place.

## Scaffold Template

The following template is generated by `+"`wtg config init -o -`"+`:

`+"```toml\n%s```"+`
`, scaffold)

	return os.WriteFile(outPath, []byte(content), 0644)
}

func generateReadOnlyReference(outPath string) error {
	content := `+++
title = "Read-Only Workspace Members"
description = "Behaviour and reference for read-only workspace members."
weight = 30
+++

# Read-Only Members

A feature touches two or three repos and merely *uses* a few more: a library you built against, a docs repo, a code generator. ` + "`wtg add --read-only`" + ` adds such a repo as a **detached worktree at a ref you choose**. It is a real directory, so you can build, search and run in it, and it is listed in ` + "`go.work`" + ` if it has a ` + "`go.mod`" + ` — but it has no feature branch, and ` + "`push`" + `, ` + "`land`" + ` and the branch safety checks in ` + "`remove`" + `/` + "`delete`" + ` ignore it.

## Ref Resolution

| Invocation | Ref Selected |
|---|---|
| ` + "`--read-only`" + ` | the version the other members' ` + "`go.mod`" + ` files select (` + "`go list -m`" + `), else ` + "`origin/<default branch>`" + ` |
| ` + "`--read-only --ref v2.0.0`" + ` | a tag, pinned |
| ` + "`--read-only --ref 1a2b3c4`" + ` | a commit, pinned |
| ` + "`--read-only --ref origin/main`" + ` | a branch, followed by ` + "`wtg rebase`" + ` |

` + "`--ref`" + ` applies to every repo named in that invocation (every ` + "`--read-only`" + ` repo, for ` + "`wtg new`" + `); add repos that need different refs in separate invocations. A ref that does not resolve fails before anything is created. Without ` + "`--ref`" + `, consumers that require different versions are an error that lists each one, because ` + "`go.work`" + ` overrides ` + "`go.mod`" + `: the member contributes exactly one version to the build and it has to be the right one.

## Command Behaviour with Read-Only Members

| Command | Behaviour |
|---|---|
| ` + "`status`" + ` | shows ` + "`◇ read-only @ <ref>`" + `; a modified worktree is flagged |
| ` + "`rebase`" + ` | advances it when it follows ` + "`origin/<branch>`" + `, leaves a pinned one alone, never touches a modified one |
| ` + "`push`" + `, ` + "`land`" + ` | skipped, with no failure |
| ` + "`exec`" + ` | included — it is a real worktree of its own |
| ` + "`remove`" + `, ` + "`delete`" + ` | removes the worktree; ` + "`-d`" + `/` + "`-D`" + ` have no branch to delete |
| ` + "`add <repo>`" + ` | promotes it to a normal worktree on the workspace's branch |

## Promotion and Re-pinning

"Read-only" is a statement of intent, not file permissions: ` + "`wtg`" + ` reports a modification but does not prevent one. To change a read-only member, promote it first. To re-pin one, ` + "`wtg remove`" + ` it and add it again with a different ` + "`--ref`" + `.

` + "`wtg status`" + ` also compares, for every pair of ` + "`go.work`" + ` members, the version a consumer requires with what is checked out, and prints a line when they differ:

` + "```" + `
⚠ config-lib is 12 commits behind v2.0.0, which api requires
` + "```" + `
`

	return os.WriteFile(outPath, []byte(content), 0644)
}
