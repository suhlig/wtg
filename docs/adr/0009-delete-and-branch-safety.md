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
