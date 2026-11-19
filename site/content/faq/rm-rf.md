+++
title = "What if I delete a workspace directory by hand?"
description = "Git still lists the worktrees; `git worktree prune` cleans up."
weight = 70
+++

If a workspace directory is removed manually without `wtg delete`, Git's administrative metadata still knows about the registered worktrees. Run:

```sh
git worktree prune
```

in your main clones to clean up stale metadata. The branches stay.
