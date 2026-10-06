# ADR 0007: Branch Strategy

> Converted from a previous version of [`docs/DESIGN.md`](../DESIGN.md). The text is essentially unchanged aside from the framing, metadata, cross-references, and line wrapping.

- Status: Accepted
- Date: 2026-03-27
- Author: Geoff Amey

- Default branch name: `<git.branch_prefix><space-name>` (prefix may be empty)
- `--branch` flag on `wtg new` overrides the generated name
- All repos in a space share the same branch name

## Branch conflict rules

Evaluated per-repo during `wtg new` and `wtg add`:

| Branch state in repo | Action |
|---|---|
| Does not exist | Create from HEAD of the repo's default branch |
| Exists, not checked out anywhere | Check out in the new worktree (do not reset) |
| Exists, already checked out in another worktree | **Error** — git does not allow two worktrees on the same branch |
| Exists in some repos, not others | Create where missing, use existing where present |

These checks are part of the pre-flight phase so that no worktrees are created before a conflict is detected.
