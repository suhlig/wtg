# ADR 0017: `always.run` is a best-effort post-operation hook

> Derived from the feature guide [`docs/always.md`](../always.md), which remains the user-facing reference for usage.

- Status: Accepted
- Date: 2026-06-21
- Author: Geoff Amey

## Context

Users want to wire a space into the rest of their environment on lifecycle events — trust a direnv file, open an editor, register the space with another tool, send a notification.

## Decision

A single executable in `always.run` is invoked directly (not through a shell) after a space operation has succeeded, for the events `create`, `add`, `remove`, and `delete`. It is best-effort: a non-zero exit prints a warning but does not fail the operation. Context is passed through `WTG_*` environment variables.

## Rationale

The operation has already completed, so the hook must not be able to fail or roll it back. Direct invocation avoids shell-quoting surprises and requires the script to be executable with a valid shebang, while a single config key plus event environment variables lets the script (or a `run-parts`-style dispatcher) branch on the event.

## Consequences

- The script must be executable and is not part of the saga rollback.
- Multiple independent hooks are supported by pointing `always.run` at a dispatcher that runs everything in a `hooks.d/` directory.

## Alternatives

- **Invoke through a shell.** Rejected: it adds quoting rules and hides the executable requirement.
- **Make the hook part of the transaction.** Rejected: rolling back an already-completed operation is the wrong model.
- **Several hook config keys.** Rejected: a dispatcher script is simpler and composable.
