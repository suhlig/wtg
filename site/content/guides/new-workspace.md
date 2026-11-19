+++
title = "Creating a New Workspace"
description = "I want a new workspace with two projects; where is my stuff?"
weight = 20

[extra]
nav_title = "New Workspace"
+++

You are starting work on a new feature that spans multiple repositories (such as `api` and `web`). This guide explains how to create the workspace, where the files live on disk, and how `go.work` connects them.

## 1. Creating the Workspace

Create a new workspace named `my-feature` containing linked worktrees for `api` and `web`:

{% set id = "new-workspace" %}{% set step = 1 %}{% include "transcript.html" %}

`wtg` creates a linked worktree for each repository on the feature branch, and generates a `go.work` file in the workspace root.

## 2. Navigating to the Workspace

Find the absolute path to your new workspace:

{% set id = "new-workspace" %}{% set step = 2 %}{% include "transcript.html" %}

If you configured the `wcd` helper, you can simply type `wcd my-feature` in your shell to jump directly there.

## 3. Checking Workspace Status

Inspect the status of the workspace, its branches, and whether working trees are clean:

{% set id = "new-workspace" %}{% set step = 3 %}{% include "transcript.html" %}

## 4. Automatic `go.work` Generation

Inside the workspace root, `wtg` maintains a `go.work` file listing all Go modules in the workspace:

{% set id = "new-workspace" %}{% set step = 4 %}{% include "transcript.html" %}

### What you have now

A complete workspace ready for editing and building across both repositories with standard Go tooling (`go build`, `go test ./...`), with zero duplicate git objects cloned.
