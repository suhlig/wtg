+++
title = "Branching from develop, or Continuing a Branch"
description = "The work starts from develop, or a colleague's branch already exists."
weight = 25
+++

By default a new workspace branches from the repo's current `HEAD`, on a new branch named after the workspace. Two flags change that: `--base` picks where the branch starts, and `--branch` names a branch that may already exist.

## 1. Starting from develop

Your team integrates on `develop`, not `main`. Name it as the base:

{% set id = "base-and-branch" %}{% set step = 1 %}{% include "transcript.html" %}

The branch starts with what `develop` has:

{% set id = "base-and-branch" %}{% set step = 2 %}{% include "transcript.html" %}

When the base is `origin/<branch>`, wtg also remembers it. `rebase` and `land` then use `develop` instead of the default branch, so the feature lands where it started:

{% set id = "base-and-branch" %}{% set step = 3 %}{% include "transcript.html" %}

Any other commit-ish (a tag or a commit) is a valid start point too. Only a branch of `origin` is remembered, because only a branch can be rebased onto and landed into.

## 2. Continuing an existing branch

A colleague pushed `colleague/fix-login` and you want to build on it or review it in a workspace. Give the branch name with `--branch`:

{% set id = "base-and-branch" %}{% set step = 4 %}{% include "transcript.html" %}

Because the branch exists on `origin`, it is checked out as it is, with its history; wtg does not reset or rebase it:

{% set id = "base-and-branch" %}{% set step = 5 %}{% include "transcript.html" %}

Without `--branch`, the name is `git.branch_prefix` followed by the workspace name.

### What you have now

A workspace that starts from the branch you chose, and a recorded base so `rebase` and `land` agree with it.
