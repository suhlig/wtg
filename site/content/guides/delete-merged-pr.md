+++
title = "Cleaning Up after a Squash Merge"
description = "My pull requests were squash-merged on GitHub; why can wtg delete the branches?"
weight = 47
+++

Your reviewer merged the pull requests with GitHub's squash button, and GitHub deleted the branches on `origin`. This guide shows why `wtg delete -d` still removes your local branches, and when it would not.

<p class="note"><strong>Note:</strong> <code>gh</code> is simulated here; git is real. The two pull requests were opened with <code>wtg land</code>, and then squash-merged and marked merged outside wtg.</p>

## Why git alone would hesitate

A squash merge writes a new commit on `main`. Your feature commits are not ancestors of it, and with the branch gone from `origin`, they exist on no remote. To git, and to wtg's safety check, they look like unpushed work.

## 1. Deleting the Workspace

`-d` deletes a branch only when no commit exists on it alone. Before refusing, wtg asks `gh` about the branch's pull request. A merged pull request whose head is exactly the branch tip means the work is upstream, and the row names it:

{% set id = "delete-merged-pr" %}{% set step = 1 %}{% include "transcript.html" %}

## 2. Nothing Left Behind

The workspace is gone, and so is the branch in the main clone:

{% set id = "delete-merged-pr" %}{% set step = 2 %}{% include "transcript.html" %}

## When wtg keeps the branch

The lookup never makes `delete` less safe. The branch is kept, as `-d` would without it, when:

- `gh` is missing, not authenticated, or the lookup fails;
- the pull request was closed without being merged;
- you committed to the branch after the merge, so its tip is not what was merged.

In those cases `wtg delete -D` is the way out once you have looked at what would be lost.

### What you have now

The workspace and its branches are gone from your machine, and the merged work is on `main`.
