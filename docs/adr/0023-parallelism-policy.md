# ADR 0023: Parallelism policy — run independent work concurrently

- Status: Proposed
- Date: 2026-11-01
- Author: Steffen Uhlig

## Context

wtg operates across multiple repos. Most of its per-repo work (git subprocesses,
network calls) is independent: the status of one repo tells nothing about the
status of another, and a fetch in one repo does not affect a fetch in another.

Where commands already exploit this, the user feels it. `wtg push` (introduced
with the first parallel fetch), `wtg repo sync`, `wtg repo fetch`, `wtg repo
status`, `wtg rebase`, `wtg exec --parallel`, and `wtg status` all fan out with
`errgroup` or `sync.WaitGroup`. Commands with ten repos finish in roughly the
time of the slowest one rather than the sum of all ten.

Not all commands follow this consistently, however. In particular, two
sequential loops stand out:

1. **`wtg delete` pre-flight** ([`internal/cmd/delete.go`](../../../internal/cmd/delete.go)): calls
   `runner.Status` once per repo to collect uncommitted-change and unpushed-commit
   warnings before asking for confirmation.
2. **`wtg remove` pre-flight** ([`internal/cmd/remove.go`](../../../internal/cmd/remove.go)): the
   same pattern, over the subset of repos being removed.

Both loops issue independent `git status` subprocesses that have no causal
relationship with each other and could be parallelised with the same
`errgroup`-and-indexed-results pattern used elsewhere.

There is no written rule about when to parallelise and when not to. New code
therefore has to rediscover the existing examples rather than following a stated
policy.

## Problem

State a policy so that:

- new operations default to parallel execution when they safely can, without
  requiring a case-by-case argument,
- existing sequential loops that could be parallel are recognised as
  inconsistencies, not deliberate choices, and
- the situations where sequential execution *is* the right choice are also
  explicit.

## Decisions

### D1. Default to parallel for independent per-repo work (decided)

Any operation that runs the same logic independently across multiple repos — no
output from one feeds into another, and no shared mutable state is written
during the loop — **must** use the established fan-out pattern:

- allocate a `results` slice indexed by repo position,
- spawn one goroutine per repo with `errgroup.Group` or `sync.WaitGroup`,
- write only to `results[i]` (no lock needed per slot because each goroutine
  owns its own index),
- wait for all goroutines, then render results in order.

The `errgroup` variant (`golang.org/x/sync/errgroup`) is preferred when the
goroutines could meaningfully return an error (even if in practice they always
return `nil` and encode errors into the result slot, as the existing code does).
`sync.WaitGroup` is fine when no error value is needed.

### D2. Sequential execution is required only when there is a real dependency (decided)

Sequential execution is justified when:

1. **Step N produces input for step N+1** within the same repo or across repos.
   Example: the saga steps in `wtg new` and `wtg add` are causally ordered
   (create branch → add worktree → copy secrets → write go.work → save state);
   they cannot be reordered, let alone parallelised.
2. **Steps share mutable data that is not goroutine-safe**, and the sharing
   cannot be removed without restructuring the logic. Example: `deleteOne`
   followed by `removeEmptyParents` in `wtg delete` and `wtg remove` — after
   removing a worktree, the shared intermediate directory (e.g. an `org/`
   prefix) is pruned. Two goroutines racing to prune the same parent would both
   call `os.Remove` on a directory that one of them already removed, producing a
   spurious error. The removal loop stays sequential.
3. **The operation is interactive**: it reads from stdin between steps. A
   confirmation prompt cannot be issued in parallel.

If none of these applies, the operation is a candidate for parallelism and
should be made parallel.

### D3. Pre-flight status checks in `delete` and `remove` must be parallelised (decided)

The sequential `runner.Status` loops in `RunSpaceDelete` and `RunSpaceRemove`
meet none of the conditions in D2:

- Each call is independent; the result for repo A is not read by the loop
  iteration for repo B.
- The writes go into a `warnings` slice that is appended from a single
  goroutine in the current code, but can trivially be converted to the indexed
  `results` pattern.
- The prompt follows *after* all status calls, so interactivity is not a
  constraint on the status collection itself.

Both loops must be replaced with the fan-out pattern from D1. The confirmation
prompt and the removal loop remain sequential.

### D4. Concurrency limit: none by default (decided)

All repos run at once. Workspaces have a handful of repos; the work is mostly
git subprocess overhead and, for fetch operations, network-bound. There is no
cap in the current implementation, and no problems have been observed.

Add a cap only if real workspaces demonstrate a need (for example SSH
connection-refused errors under heavy parallelism). If added, it should be a
shared constant or config key, not a per-command magic number.

## Consequences

- `wtg delete` and `wtg remove` collect their pre-flight warnings in parallel,
  making the confirmation prompt appear faster for large workspaces.
- New commands that iterate over repos have a clear default: parallel, unless
  one of D2's three conditions applies.
- The distinction between "sequential by design" (saga steps, removal + prune,
  interactive prompts) and "sequential by accident" (the pre-flight loops) is
  now explicit and checkable.

## Alternatives considered

- **Leave the pre-flight loops sequential.** Rejected: they are sequential by
  accident, and the pattern to fix them already exists in the same package.
- **A configurable parallelism flag for every command.** Rejected: `exec` has
  `--parallel` because its default is sequential (streaming output); commands
  that are parallel by default gain nothing from such a flag and it would just
  add noise.
- **Use a worker pool with a bounded channel.** Deferred to D4: not needed yet.
  Worth revisiting if SSH throttling or process-limit errors appear in practice.

## Decisions summary

| ID | Decision |
|----|----------|
| D1 | Independent per-repo work defaults to parallel: fan-out goroutines, indexed results slice, render in order |
| D2 | Sequential only when: step produces input for next step, steps share non-goroutine-safe state, or operation is interactive |
| D3 | Pre-flight status loops in `wtg delete` and `wtg remove` are parallelised (they meet none of D2's conditions) |
| D4 | No concurrency cap by default; add one only when real workspaces demonstrate a problem |
