# ADR 0003: Transactions / Saga Pattern

> Converted from a previous version of [`docs/DESIGN.md`](../DESIGN.md). The text is essentially unchanged aside from the framing, metadata, cross-references, and line wrapping.

- Status: Accepted
- Date: 2026-03-27
- Author: Geoff Amey

Commands that mutate state (`wtg new`, `wtg add`, `wtg delete`) follow a pre-flight + saga rollback pattern.

## Pre-flight checks

Before any mutation, verify:

1. All repos exist on disk and are valid git repos
2. Target worktree paths don't already exist (or are empty)
3. No space with this name already exists in the state file
4. Branch state in each repo (see [ADR 0007](0007-branch-strategy.md))

If any pre-flight check fails, the command errors immediately with no side effects.

## Rollback on mid-operation failure

Each mutating step registers a compensating action — a `Do` and an `Undo` (`internal/saga/saga.go`):

```go
type Step struct {
    Name string
    Do   func(ctx context.Context) error
    Undo func(ctx context.Context) error // called on rollback; errors are logged, not fatal
}
```

`saga.Run` executes steps in order; if one fails, the already-completed steps are unwound in reverse by calling their `Undo`. No external saga library is used — the pattern is a few dozen lines and carries no dependency risk. Existing Go saga libraries have low adoption and awkward APIs.

Compensation failures (e.g. `git worktree remove` failing mid-rollback) are logged and reported to the user but do not prevent the remaining rollback steps from running.
