+++
title = "Keeping Feature Branches Up to Date"
description = "I am mid-feature; how do I pick up changes in main?"
weight = 30

[extra]
nav_title = "Sync & Rebase"
+++

While developing your feature across multiple repositories, other team members merged new changes into `main`. This guide shows how to pull upstream changes into your main clones and rebase your workspace branches concurrently.

## 1. Syncing Main Clones

Fetch and fast-forward each repository's default branch on your machine:

{% set id = "sync-and-rebase" %}{% set step = 1 %}{% include "transcript.html" %}

`repo sync` only modifies your main repo clones, skipping any repositories with uncommitted changes.

## 2. Rebasing Workspace Branches

Rebase your workspace's feature branches onto `origin/main` across all repositories in parallel:

{% set id = "sync-and-rebase" %}{% set step = 2 %}{% include "transcript.html" %}

`rebase` automatically uses `--autostash`, so uncommitted edits are stashed and restored automatically.

## 3. Pushing the Rebased Branch

When you rebase, Git rewrites commit hashes. If you had previously pushed the feature branch, git will reject a non-fast-forward push and `wtg push` suggests the exact `--force-with-lease` invocation:

{% set id = "sync-and-rebase" %}{% set step = 3 %}{% include "transcript.html" %}

`wtg` never force-pushes on its own, ensuring you never accidentally overwrite remote commits.

### What you have now

Your workspace feature branches are rebased directly on top of the latest `main` across all repositories.
