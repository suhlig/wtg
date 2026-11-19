+++
title = "Carrying on without wtg"
description = "I stopped using wtg, or lost its state; is my work still there?"
weight = 110
+++

wtg is a convenience, not a home for your work. Everything it creates is plain git: ordinary branches and linked worktrees. Nothing is stored in your repositories that only wtg can read, and your collaborators never need it. This guide shows that by losing wtg's state on purpose and carrying on with git alone.

Here a workspace `my-feature` holds one commit in `api`, and wtg's data directory has been deleted.

## 1. wtg No Longer Knows the Workspace

wtg keeps a small index of your workspaces outside the repositories. With the index gone, the workspace is unknown to it:

{% set id = "without-wtg" %}{% set step = 1 %}{% include "transcript.html" %}

## 2. git Still Knows Everything

The worktree is registered in the main clone, and git lists it:

{% set id = "without-wtg" %}{% set step = 2 %}{% include "transcript.html" %}

The commit is on an ordinary branch:

{% set id = "without-wtg" %}{% set step = 3 %}{% include "transcript.html" %}

## 3. Finishing with git

Push the branch the usual way. The remote sees a normal branch, as it would from anyone:

{% set id = "without-wtg" %}{% set step = 4 %}{% include "transcript.html" %}

Remove the worktree and the branch when you are done:

{% set id = "without-wtg" %}{% set step = 5 %}{% include "transcript.html" %}

{% set id = "without-wtg" %}{% set step = 6 %}{% include "transcript.html" %}

If you delete a workspace directory by hand instead, `git worktree prune` removes the stale entry (see the [FAQ](@/faq/_index.md)). The branch stays.

### What you have now

Your work is on a branch in your repository and on `origin`. Losing wtg's state cost you the convenience of its commands, nothing more.
