# ADR 0002: Git Abstraction Layer

> Converted from a previous version of [`docs/DESIGN.md`](../DESIGN.md). The text is essentially unchanged aside from the framing, metadata, cross-references, and line wrapping.

- Status: Accepted
- Date: 2026-03-27
- Author: Geoff Amey
- Contributors: Steffen Uhlig (updates 2026-10-04, 2026-10-05)

All git operations go through a single `Runner` interface — the authoritative definition lives in `internal/git/runner.go`, so it is not reproduced here. The production implementation shells out to system git; tests use a mock or a real git repo created with `internal/git/testhelper`.

The interface groups operations into:

- **Worktrees** — add, remove, list, repair
- **Branches** — local and remote existence, delete, merged-check
- **Status** — working-tree, upstream, and ahead/behind state
- **Sync** — fetch, fast-forward, push, rebase, default-branch lookup
- **Info** — remote URL

`git worktree repair` (git ≥ 2.29) is surfaced through `ErrRepairUnsupported` on older git, so callers can fall back to printing manual recovery instructions.

## Scripting-safe git output formats

No free-text git output is parsed. All parsing uses structured, version-stable formats:

| Operation | Command | Format |
|-----------|---------|--------|
| Worktree list | `git worktree list --porcelain` | Record-per-worktree, blank-line separated |
| Working tree status + branch + ahead/behind | `git status --porcelain=v2 --branch` | `# branch.*` headers + XY-coded file lines |
| Branch existence | `git rev-parse --verify refs/heads/<branch>` | Exit code only |
| Remote URL | `git remote get-url origin` | Single line |
| Default branch | `git symbolic-ref refs/remotes/origin/HEAD` | `refs/remotes/origin/<branch>` |
| Default branch (repair) | `git remote set-head origin --auto` | Human output; the ref is re-read via the row above |

`origin/HEAD` is a local cache that git populates on clone, so it can be missing or left as a non-symbolic commit ref (e.g. in repos not created by `git clone`, or after the remote's default branch changed). `DefaultBranch` reads it when present and otherwise repairs it with `git remote set-head origin --auto` before reading again — the only case where a normally-local lookup contacts the remote. `repo status` treats a failure as "no default branch" (branch shown muted).

## Testing strategy

Unit tests mock the `Runner` interface directly — no git binary required.

Integration tests use a helper (`internal/git/testhelper`) that:

1. Creates a real git repo in `t.TempDir()`
2. Seeds it with commits, branches, and worktrees as needed
3. Returns a production `Runner` pointed at it

This gives the same coverage benefit as go-git's in-memory repos while testing against real git output formats.
