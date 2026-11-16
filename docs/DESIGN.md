# Design Notes

Decisions and rationale for contributors. The code is the source of truth for signatures, schemas, and exact behaviour; this document indexes the architecture decision records (in [`adr/`](adr/)) and links to the relevant files rather than copying their contents (which only goes stale). User-facing usage lives in `README.md`.

## Architecture Decision Records

Each decision below has its own record in [`adr/`](adr/), carrying the original context, rationale, and alternatives. The records were retroactively numbered for the decisions that predate the ADR practice; new decisions continue from there.

| ADR | Decision |
|-----|----------|
| [0001](adr/0001-library-choices.md) | Library choices, including using system `git` instead of go-git |
| [0002](adr/0002-git-abstraction-layer.md) | A single git `Runner` abstraction over system git |
| [0003](adr/0003-transactions-saga-pattern.md) | Pre-flight plus saga rollback for mutating commands |
| [0004](adr/0004-state-persistence.md) | Space state persisted outside the workspace |
| [0005](adr/0005-repository-discovery.md) | Repository discovery, short names, and staged name resolution |
| [0006](adr/0006-worktree-layout.md) | Worktree layout mirrors the repo short name |
| [0007](adr/0007-branch-strategy.md) | One shared branch per space, with branch conflict rules |
| [0008](adr/0008-go-work-generation.md) | Generate `go.work` with `golang.org/x/mod/modfile` |
| [0009](adr/0009-delete-and-branch-safety.md) | Branch safety in `wtg delete` |
| [0010](adr/0010-repo-sync-scope.md) | `repo sync` operates only on main clones |
| [0011](adr/0011-config-command.md) | `wtg config` is a non-interactive command group |
| [0012](adr/0012-fork-maintenance.md) | Fork maintenance and module path |
| [0013](adr/0013-repo-archive.md) | Retiring repos with `wtg repo archive` |
| [0014](adr/0014-always-repos-are-symlinks.md) | `always.repos` are symlinks to the main clone, not worktrees |
| [0015](adr/0015-always-files-are-copied.md) | `always.files` are copied, not symlinked |
| [0016](adr/0016-always-secrets-are-repo-relative-copies.md) | `always.secrets` are repo-relative copies into each worktree |
| [0017](adr/0017-always-run-best-effort-hook.md) | `always.run` is a best-effort post-operation hook |
| [0018](adr/0018-workspace-rebase.md) | `wtg rebase` brings a workspace's branches up to date in parallel |
| [0019](adr/0019-workspace-land.md) | `wtg land` publishes a workspace by fast-forward or pull request, separate from `wtg delete` |
| [0020](adr/0020-command-summary.md) | `--summary` lists the mutating commands and effects a run executed |
| [0021](adr/0021-dry-run.md) | `--dry-run` previews a run by skipping mutations at the git, `gh`, and filesystem boundaries |
| [0022](adr/0022-no-tool-dependency.md) | Collaborators never need wtg: shared artefacts stay plain, wtg-only data stays user-local |
| [0023](adr/0023-parallelism-policy.md) | Independent per-repo work runs in parallel; sequential only for real dependencies |
| [0024](adr/0024-read-only-workspace-members.md) | Read-only workspace members: pinned detached worktrees in `go.work`, never branched; drift check in `wtg status` |
| [0025](adr/0025-dot-repo-arguments.md) | `.` names the repo or workspace containing the current directory, resolved to a canonical short name before name matching |

## Open work

Work a decision deliberately left out, collected in one place instead of buried in the records. Each item links to the ADR that explains it; **the ADR is authoritative** — when an item is picked up, reshaped, or dropped, change the record first, then this list. This mirrors the `## Follow-ups` sections: ADR 0013's two follow-ups (v2 and v3) and ADR 0019's three are done, and no other record has one.

### [ADR 0024](adr/0024-read-only-workspace-members.md) — read-only workspace members

- `wtg new --read-only <repo>`, repeatable, so a mixed workspace is created in one step instead of `new` followed by `add --read-only`.
- An `adopt` path for detached worktrees that were created before read-only members existed (E11); today it is `wtg remove` followed by `wtg add --read-only`, which loses nothing because the worktree is clean.
- Re-evaluate `go.work` membership on promotion: it is decided from the pinned checkout, before the branch checkout exists, so a branch that adds or drops a `go.mod` relative to the pin needs a manual `go work use`/`go work edit` (or a `remove` + `add` cycle).
- Teach the drift check the selected version rather than the literal `require`, while staying offline. Minimum version selection can pick a newer version than a consumer's own `go.mod` states, which today's check under-reports; running `go list -m` in `wtg status` would fix that at the cost of a network round-trip per consumer, which E9 rules out.

### [ADR 0025](adr/0025-dot-repo-arguments.md) — `.` arguments

- Path-like arguments (`./api`, `../api`, absolute paths) stay unsupported and are treated as names; supporting them needs a marker or its own decision, because a canonical short name already contains slashes (D8).
- Offer `.` in shell completion; `completeRepos` and `completeSpaceThenRepos` list names only.

