+++
title = "Landing Changes and Cleaning Up"
description = "I am done; how do I get it into main, and which strategy?"
weight = 40
+++

Your feature is finished and tested. This guide explains how to preview and land your changes using the `merge` or `pr` strategy, and safely clean up the workspace.

## Choosing a Landing Strategy

| Strategy | When to use | Action taken |
|---|---|---|
| **`pr`** (default) | Shared or protected branches, or when pull requests need review | Pushes branch and opens a PR on GitHub; links sibling PRs |
| **`merge`** | Repositories you push to directly with fast-forward permissions | Fast-forwards remote `origin/main` to your branch tip |

Set per-repository strategies using git config `git config wtg.landStrategy merge` or pass `--strategy <merge|pr>` on the command line.

## 1. Previewing with `--dry-run`

Preview the landing plan without making any remote mutations:

{% set id = "land-and-delete" %}{% set step = 1 %}{% include "transcript.html" %}

## 2. Landing the Workspace

Publish the feature branch to `main`:

{% set id = "land-and-delete" %}{% set step = 2 %}{% include "transcript.html" %}

## 3. Deleting the Workspace

Once changes are landed, delete the workspace and remove the feature branches:

{% set id = "land-and-delete" %}{% set step = 3 %}{% include "transcript.html" %}

`wtg delete` checks for uncommitted changes, unpushed commits, and modified secrets before removing anything.

### What you have now

Your feature changes are safely integrated upstream and your workspace directories and worktrees have been cleaned up.
