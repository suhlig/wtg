# wtg

`wtg` manages feature branches that span multiple repos. When a feature touches several repos at once, `wtg` checks out a shared branch across all of them as [git worktrees](https://git-scm.com/docs/git-worktree) and wires them together with a `go.work` file — giving you an isolated, ready-to-build workspace per feature without cloning anything new.

> New to worktrees? The [GitKraken worktree guide](https://www.gitkraken.com/learn/git/git-worktree) is a good primer.

## Documentation

Full documentation, scenario guides, and reference material are published at **[suhlig.github.io/wtg](https://suhlig.github.io/wtg/)**:

- **[Guides](https://suhlig.github.io/wtg/guides/)**: Real walkthroughs covering setup, rebasing, landing, and read-only dependencies with output generated from the binary.
- **[CLI Reference](https://suhlig.github.io/wtg/reference/cli/)**: Exhaustive reference for all commands and flags.
- **[Configuration Reference](https://suhlig.github.io/wtg/reference/config/)**: Detailed config settings, discovery roots, and lifecycle hooks.
- **[FAQ & Troubleshooting](https://suhlig.github.io/wtg/faq/)**: Answers to common questions.

## Installation

```sh
go install github.com/suhlig/wtg@latest
```

### Shell completions and `wcd`

**bash** — add to `~/.bashrc`:
```bash
source <(wtg completion bash)
wcd() { cd "$(wtg path "$1")"; }
```

**zsh** — add to `~/.zshrc`:
```zsh
source <(wtg completion zsh)
wcd() { cd "$(wtg path "$1")"; }
```

**fish** — add to `~/.config/fish/conf.d/wtg.fish`:
```fish
if status is-interactive
  wtg completion fish | source
  function wcd
    cd (wtg path $argv[1])
  end
  complete -c wcd -f -a '(wtg path --generate-shell-completion 2>/dev/null)'
end
```

Completion offers repo and workspace names, and `.` wherever it is accepted: the repo or workspace you are in.

## Quick start

```sh
# Create a workspace for a new feature across three repos
wtg new my-feature api payments frontend

# Jump into the workspace
wcd my-feature

# Inspect status
wtg status

# Publish feature branch (opens PRs or merges)
wtg land

# Clean up
wtg delete my-feature --delete-branch
```

## Configuration

Run `wtg config init` to scaffold a commented config file at `~/.config/wtg/config.toml`:

```sh
wtg config init
```

A minimal config looks like:

```toml
[discovery]
root_dir = "~/repos"       # where wtg scans for git repos (or root_dirs = ["~/repos", "~/work/repos"])
max_depth = 2

[spaces]
root_dir = "~/spaces"      # where workspaces are created

[git]
branch_prefix = ""         # prepended to workspace names, e.g. "yourname/"
network_timeout = "5m"     # timeout for network git and gh calls
```

See the [Configuration Reference](https://suhlig.github.io/wtg/reference/config/) and [Always-included repos and hooks](https://suhlig.github.io/wtg/guides/always/) for complete options.

## Development

This fork renames the module path to `github.com/suhlig/wtg` so it installs via `go install github.com/suhlig/wtg@latest`. Enable [`git rerere`](https://git-scm.com/docs/git-rerere) once and Git replays the same resolution automatically on every future merge:

```sh
git config rerere.enabled true
```

### Documentation site

To update transcripts and preview the documentation site locally:

```sh
# Run acceptance tests to record transcripts, update CLI reference, and verify integrity
make docs

# Preview with Zola (serves on http://127.0.0.1:1111)
zola --root site serve
```
