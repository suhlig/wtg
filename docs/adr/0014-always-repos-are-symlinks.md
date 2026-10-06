# ADR 0014: `always.repos` are symlinks, not worktrees

> Derived from the feature guide [`docs/always.md`](../always.md), which remains the user-facing reference for usage.

- Status: Accepted
- Date: 2026-06-21
- Author: Geoff Amey

## Context

Some repos are wanted in nearly every space but rarely modified on the feature branch — shared tooling, protobuf/schema repos, design docs, a scratch area.

## Decision

Repos listed in `always.repos` are added to every new space as a symlink to the main clone, never as a worktree.

## Rationale

A symlink keeps a single shared working copy: the repo stays on whatever branch the main clone is on, needs no per-space feature branch, is excluded from `go.work` (it is not a feature-branch checkout), and is skipped by the dirty/unpushed safety checks in `remove`/`delete`.

## Consequences

- Edits are shared across every space that symlinks the repo, because they are all the same working copy.
- To work on the branch in a shared repo, name it explicitly on `wtg new` (an explicit repo wins over the symlink) or upgrade it later with `wtg add`, which replaces the symlink with a worktree. `wtg remove` restores the symlink.

## Alternatives

- **A worktree per space.** Rejected: it forces a feature branch nobody uses and duplicates a checkout that is meant to be shared.
- **Copying the repo.** Rejected: diverging copies are worse than one shared working tree.
