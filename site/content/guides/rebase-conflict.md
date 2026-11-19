+++
title = "Resolving a Rebase Conflict"
description = "A rebase conflicts; what happened to my work, and how do I finish by hand?"
weight = 35
+++

You ran `wtg rebase`, and your branch and `main` both changed the same lines in `api`. This guide shows what wtg does about it, and how to finish the rebase yourself.

## 1. What wtg Does

`rebase` works on every repo in parallel. The repo that conflicts is put back the way it was; the others are not affected:

{% set id = "rebase-conflict" %}{% set step = 1 %}{% include "transcript.html" %}

Nothing is left half-done: the rebase was aborted, and uncommitted changes (`--autostash`) were restored. wtg exits non-zero so a script stops, and prints the exact command to retry by hand.

## 2. Retrying by Hand

Run that command in the repo. git stops at the conflict and leaves the worktree in the middle of the rebase:

{% set id = "rebase-conflict" %}{% set step = 2 %}{% include "transcript.html" %}

Edit the conflicted file, then `git add` it. Resolving a conflict is a decision about your code, so wtg leaves it to you.

## 3. Continuing

Once the file is resolved and added, `git rebase --continue` finishes:

{% set id = "rebase-conflict" %}{% set step = 3 %}{% include "transcript.html" %}

## 4. Back in wtg

wtg finds the branch already on top of `origin/main`:

{% set id = "rebase-conflict" %}{% set step = 4 %}{% include "transcript.html" %}

If the branch had been pushed before, the next `wtg push` is rejected until you overwrite the remote on purpose; see [Keeping Feature Branches Up to Date](@/guides/sync-and-rebase.md).

### What you have now

Every repo in the workspace is rebased on the latest `main`. Only the conflicting repo needed your hands, and no work was lost on the way.
