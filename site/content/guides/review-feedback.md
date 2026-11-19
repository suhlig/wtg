+++
title = "Review Feedback after the Workspace Is Gone"
description = "The pull requests are open and I cleaned up; now the reviewer asks for a change."
weight = 45

[extra]
nav_title = "Review Feedback"
+++

You opened the pull requests with `wtg land`, deleted the workspace to keep your disk tidy, and now a reviewer asks for a change. The workspace is gone, but the branch is on `origin`, and that is all you need.

<p class="note"><strong>Note:</strong> <code>gh</code> is simulated here; git is real. The pull request URL comes from a stand-in for the GitHub CLI.</p>

## 1. The Workspace Is Gone

`delete -d` removes the worktrees and deletes the branches whose work is not lost:

{% set id = "review-feedback" %}{% set step = 1 %}{% include "transcript.html" %}

The local branch was deleted because every commit on it also exists on `origin`.

## 2. Creating the Workspace Again

Use the same workspace name and repo. The branch does not exist locally any more, so wtg creates it from `origin/my-feature` and sets up tracking:

{% set id = "review-feedback" %}{% set step = 2 %}{% include "transcript.html" %}

The commit the reviewer saw is there:

{% set id = "review-feedback" %}{% set step = 3 %}{% include "transcript.html" %}

## 3. Pushing the Follow-up

Make the change and commit it. `push` sends it to the same branch, so the pull request updates:

{% set id = "review-feedback" %}{% set step = 4 %}{% include "transcript.html" %}

`land` is safe to run again; it finds the open pull request instead of creating a second one:

{% set id = "review-feedback" %}{% set step = 5 %}{% include "transcript.html" %}

### What you have now

The pull request has your follow-up commit, and nothing was recreated or lost. A workspace costs little to make and little to throw away, because the branch on `origin` is the thing that lasts.
