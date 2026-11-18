+++
title = "First Steps after Installation"
description = "I just installed wtg; what are the first steps?"
weight = 10
+++

You have just installed `wtg` on your machine. This guide walks you through setting up your configuration file, inspecting discovered repositories, and enabling shell completion.

## 1. Scaffolding Configuration

`wtg` looks for its configuration file at `~/.config/wtg/config.toml`. Run the initialization command to create a commented configuration template:

{% set id = "first-steps" %}{% set step = 1 %}{% include "transcript.html" %}

## 2. Locating the Configuration File

You can verify the path to the resolved configuration file at any time:

{% set id = "first-steps" %}{% set step = 2 %}{% include "transcript.html" %}

## 3. Checking Discovered Repositories

By default, `wtg` scans `~/repos` (or configured discovery roots) for existing git clones. Inspect the current status of all discovered clones:

{% set id = "first-steps" %}{% set step = 3 %}{% include "transcript.html" %}

## 4. Shell Integration and `wcd`

Generate shell completions for your shell (e.g. zsh):

{% set id = "first-steps" %}{% set step = 4 %}{% include "transcript.html" %}

To jump directly into workspace directories with `wcd`, add the shell helper to your `.zshrc` or `.bashrc`:

```bash
source <(wtg completion zsh)
wcd() { cd "$(wtg path "$1")"; }
```

### What you have now

You have a valid configuration file, verified that `wtg` discovers your main git clones, and set up shell integration so you can create workspaces and jump into them instantly.
