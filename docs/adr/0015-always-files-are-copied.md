# ADR 0015: `always.files` are copied, not symlinked

> Derived from the feature guide [`docs/always.md`](../always.md), which remains the user-facing reference for usage.

- Status: Accepted
- Date: 2026-06-21
- Author: Geoff Amey

## Context

Templates such as `CLAUDE.md`, `.envrc`, or `.editorconfig` are wanted in the root of every new space and are expected to be edited per space.

## Decision

Files listed in `always.files` are copied into the space root at `<space-root>/<basename>`.

## Rationale

A copy keeps per-space edits local: editing the seeded file never writes back to the source template.

## Consequences

- Only the file name matters at the destination (`~/templates/wtg/CLAUDE.md` lands as `CLAUDE.md`).
- The copies are removed when the space is deleted, and later edits to the source do not propagate to existing spaces.

## Alternatives

- **Symlinking.** Rejected: per-space edits would mutate the shared source file.
