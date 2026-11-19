+++
title = "wtg — WorkTrees for Go"
template = "index.html"
+++

# wtg

`wtg` manages feature branches that span multiple repositories. When a feature touches several repos at once, `wtg` checks out a shared branch across all of them as linked [git worktrees](https://git-scm.com/docs/git-worktree) and wires them together with a `go.work` file &mdash; giving you an isolated, ready-to-build workspace per feature without cloning anything new.

## Documentation Structure

- **[Guides](guides/)**: Step-by-step walkthroughs for common situations, backed by verified transcripts directly from the real binary.
- **[CLI Reference](reference/cli/)**: Exhaustive documentation for every command and flag.
- **[Configuration Reference](reference/config/)**: Format and default settings for `config.toml`.
- **[FAQ &amp; Troubleshooting](faq/)**: Answers to common questions and edge cases.

## Quick Installation

```sh
go install github.com/suhlig/wtg@latest
```

Follow the [First Steps Guide](guides/first-steps/) to set up shell completion, configuration, and start managing workspaces.
