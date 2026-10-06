# ADR 0016: `always.secrets` are repo-relative copies

> Derived from the feature guide [`docs/always.md`](../always.md), which remains the user-facing reference for usage.

- Status: Accepted
- Date: 2026-07-22
- Author: Jason Dale

## Context

Local-only files such as `.env` or machine-local config live in the main clones but are deliberately not on the feature branch.

## Decision

Paths in `always.secrets` are treated as relative to each source repo and copied into that repo's worktree when the file exists. Missing files are skipped per repo; a directory is a hard error; an existing destination file is overwritten.

## Rationale

Secrets stay out of git while remaining available where the code expects them, and the repo-relative form matches how the files are laid out in the clone.

## Consequences

- Unlike `always.files` (absolute paths → space root), secrets land inside each worktree.
- Symlinked `always.repos` entries are skipped (the symlink already points at the main clone); upgrading a symlink to a worktree with `wtg add` does apply secrets.

## Alternatives

- **Absolute paths.** Rejected: not portable across machines, and better served by `always.files`.
- **Symlinking secrets.** Rejected: it couples the worktree to the main clone and defeats the isolation.
