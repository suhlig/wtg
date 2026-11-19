+++
title = "Why won't wtg delete my workspace?"
description = "Delete refuses when work could be lost, or when it cannot ask you."
weight = 30
+++

`wtg delete` prioritizes safety and will refuse or prompt for confirmation if:

1. There are **uncommitted changes** in any worktree.
2. There are **unpushed commits** that exist on no remote branch.
3. A local copy of an **`always.secrets`** file was edited and differs from the source clone.
4. It is running in a non-interactive script / pipeline without `--yes`.

Here the workspace has one commit that exists on no remote, and the command runs without a terminal:

{% set id = "refusals" %}{% set step = 2 %}{% include "transcript.html" %}

`--dry-run` shows what would be removed without asking or changing anything:

{% set id = "refusals" %}{% set step = 3 %}{% include "transcript.html" %}

To force-delete branches and ignore unmerged commits, use `wtg delete -D` (or `--yes` in scripts).

If the pull requests were squash-merged on GitHub, `wtg delete -d` recognises that; see [Cleaning Up after a Squash Merge](@/guides/delete-merged-pr.md).
