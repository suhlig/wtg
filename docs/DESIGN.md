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
