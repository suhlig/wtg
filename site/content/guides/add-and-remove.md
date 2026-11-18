+++
title = "Adding and Removing Repositories"
description = "The feature needs a third repo, or one is no longer needed."
weight = 50
+++

During the course of feature development, you may discover that your work needs an additional repository, or that a repository added initially is no longer required.

## 1. Adding a Repository

Add an additional repository (such as `payments`) to an active workspace:

{% set id = "add-and-remove" %}{% set step = 1 %}{% include "transcript.html" %}

`add` checks out a worktree on the workspace's branch and updates `go.work` automatically.

## 2. Removing a Repository

Remove a repository from the workspace:

{% set id = "add-and-remove" %}{% set step = 2 %}{% include "transcript.html" %}

`remove` verifies that there are no uncommitted changes or unpushed commits before removing the worktree.
