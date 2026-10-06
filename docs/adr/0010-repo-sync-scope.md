# ADR 0010: `repo sync` Scope

> Converted from a previous version of [`docs/DESIGN.md`](../DESIGN.md). The text is essentially unchanged aside from the framing, metadata, cross-references, and line wrapping.

- Status: Accepted
- Date: 2026-03-27
- Author: Geoff Amey

`repo sync` operates exclusively on the main clones in `discovery.root_dir` — it does NOT touch workspace worktrees. This is intentional: worktrees are on feature branches, not the default branch, and should be rebased/merged explicitly by the developer.

When `wtg new` runs, it branches from the current HEAD of the local default branch. Users should run `repo sync` first to ensure they're branching from the latest upstream.
