# ADR 0009: `wtg delete` and Branch Safety

> Converted from a previous version of [`docs/DESIGN.md`](../DESIGN.md). The text is essentially unchanged aside from the framing, metadata, cross-references, and line wrapping.

- Status: Accepted
- Date: 2026-03-27
- Author: Geoff Amey

Following git's own `-d`/`-D` convention:

- No flag: remove worktrees only, leave branches
- `-d`: also delete branches if they are fully merged (wraps `git branch -d` semantics)
- `-D`: force-delete branches regardless of merge state

Before any destructive action, the tool checks:

1. Does any worktree have uncommitted changes (dirty working tree)?
2. Does any branch have commits not pushed to origin?

If either is true, a summary is printed and the user is prompted to confirm.

> **Amendment ([ADR 0019](0019-workspace-land.md) D11).** "Commits not pushed to origin" is now "commits on no remote ref" (`git rev-list --count <ref> --not --remotes`), checked for the workspace branch and for whatever else is checked out in each worktree. `-d` deletes a branch when that count is zero (using `git branch -D` internally, because `git branch -d` judges against the upstream); a declined prompt or a missing terminal without `--yes` exits non-zero; and a repo whose `always.secrets` copy changed is kept.
