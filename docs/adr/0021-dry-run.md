# ADR 0021: Previewing a run with `--dry-run`

- Status: Proposed
- Date: 2026-10-29

## Context

A single wtg command expands into many git commands across many repos, and some of them are hard to undo by hand (`delete`, `land`, `repo archive`). New users have no way to see what a command will do before they run it, and no way to learn which git commands wtg stands for.

Two commands already have their own `--dry-run` (`repo archive` and `repo unarchive`, [ADR 0013](0013-repo-archive.md), decision 9), each implemented locally in its `Run…` function. [ADR 0019](0019-workspace-land.md) D10 plans a third for `land`. Each is a separate, hand-written second code path, which can drift from the real one.

[ADR 0020](0020-command-summary.md) introduces a recorder at the two process boundaries (git and `gh`) and an `Effect` helper for everything that is not a command. This ADR builds on those: a dry run is the same recording with the mutating calls skipped.

## Problem

Let the user ask, for any wtg command, to print the commands and effects it *would* run, without changing anything, so that a new user can see what would happen. A preview must never mutate state by accident.

## Intent

```sh
wtg new my-feature api payments --dry-run
wtg --dry-run delete
```

```
DRY RUN — nothing was changed

Would run:
  git -C ~/git/api       worktree add -b my-feature ~/ws/my-feature/api main
  git -C ~/git/payments  worktree add -b my-feature ~/ws/my-feature/payments main
  (wtg) write ~/ws/my-feature/go.work
  (wtg) save state my-feature
```

## Decisions

### D1. A global, per-invocation flag that fails closed (decided)

`--dry-run` is a root flag, accepted before or after the subcommand (urfave/cli v3 flags are persistent by default; verified for `--summary` in ADR 0020).

It is **opt-in per command**. A command declares that it supports dry-run once all its mutations go through the runners or `Effect`. Until then, `wtg <cmd> --dry-run` stops with `--dry-run is not supported for <cmd> yet` and runs nothing. A dry run that silently mutated would be worse than having no dry run, and `delete`, `new`, `add`, and `remove` today call `os.Remove`, `os.Symlink`, `state.Save`, and `state.Delete` directly.

`repo archive` and `repo unarchive` keep their behaviour, and their local flags are replaced by the global one. Their tests (`…DryRunMakesNoChanges`, `…RemoteDryRunMakesNoGHCalls`) are the acceptance criteria for that move.

### D2. Skip mutations at the boundaries, not in the commands (decided)

A dry-run flag is passed to the same places that record (ADR 0020 D2, D4):

- **Git runner.** For each call, the classification from ADR 0020 D3 decides. Read-only calls run for real, because the plan depends on real state. Mutating calls are recorded as "would run" and return success without executing.
- **`gh` runner.** The same, with `pr list`, `pr view`, `repo view`, and `auth status` as reads.
- **`Effect`.** Records the description and does not call `fn`.

The commands themselves are the real code paths. They run top to bottom with the mutations turned off, so the preview and the real run cannot drift. This replaces the alternative of describing each saga step separately (see Alternatives).

Pre-flight checks are reads and run for real. A dry run therefore fails the way the real run would: an unknown repo, a branch checked out elsewhere, a non-GitHub remote with `--remote` (the existing archive behaviour).

The classification is the safety property: **unknown means mutating**, so a new git verb is skipped under `--dry-run` until someone deliberately lists it as read-only.

### D3. A plan is based on current state, not a prediction (decided)

After a skipped mutation, later reads still see the old state. The output says so and handles the cases where it matters:

- **Results that do not exist yet.** A skipped `gh pr create` returns a placeholder PR (`#?`) so the code after it can continue. `land` can then show `would link 2 sibling PRs` ([ADR 0019](0019-workspace-land.md) D7) instead of failing.
- **Steps that depend on an earlier result.** A command that cannot know the outcome (will the rebase conflict? will the push be rejected?) prints the step as planned and, where useful, notes the dependency. It never claims a result.
- **Heading.** The output starts with `DRY RUN — nothing was changed`, and states that it is based on the last `fetch`.

This is the same limit as `make -n`, and the output is explicit about it.

### D4. Never fetch, never prompt (decided)

- **No `git fetch`.** It is classified as mutating (ADR 0020 D3): it writes remote-tracking refs and uses the network. A preview that touches the repo, even harmlessly, is not a preview. For `rebase` and `land` this means the plan uses the last fetched state, and the heading says so. (This changes [ADR 0019](0019-workspace-land.md) D10, which had the dry run fetch; that ADR is updated accordingly.) `ls-remote` is likewise skipped.
- **No prompts.** `delete` warns about uncommitted and unpushed work and asks for confirmation. Under `--dry-run` the warnings are printed, the prompt is replaced by a line `would ask: Delete workspace anyway?`, and the plan continues as if the answer were yes.
- **No hooks.** The `always.run` script is shown as `(wtg) run hook <path> <event>` and not executed. It is arbitrary user code.

### D5. Output goes to stdout; result tables say "would" (decided)

The plan is the point of a dry run, so it goes to stdout, and the exit code is 0 when the plan could be made. The normal per-repo result rows ("worktree created") would claim success that did not happen. Under `--dry-run` they use a plan symbol and plan wording (`→ would create worktree`), and the "Would run" list from the recorder follows.

The renderer is the one used by `--summary` (ADR 0020 D5), with the heading "Would run" instead of "Commands run". `--summary` together with `--dry-run` is accepted and equivalent to `--dry-run`.

### D6. `wtg exec` shows the command and does not run it (decided)

`exec` runs a command the user supplied, which wtg cannot classify. Under `--dry-run` it prints the working directory for every repo and the command to run there, and executes nothing.

## Edge cases

| # | Situation | Handling |
|---|-----------|----------|
| E1 | Command not yet converted | `--dry-run is not supported for <cmd> yet`; nothing runs (D1) |
| E2 | Pre-flight fails | Same error as a real run, exit non-zero (D2) |
| E3 | Mutating call returns data later steps use (PR number, new commit) | Placeholder value; dependent steps shown as planned (D3) |
| E4 | Rebase or push that might fail | Shown as planned, no outcome claimed (D3) |
| E5 | `delete` finds uncommitted or unpushed work | Warnings printed; prompt replaced by `would ask` (D4) |
| E6 | `always.run` hook configured | Shown, not run (D4) |
| E7 | Remote state is stale because no fetch was done | Heading says the plan is based on the last fetch (D4) |
| E8 | A git verb is not on the read-only allowlist | Treated as mutating and skipped (D2) |
| E9 | Parallel commands (`push`, `rebase`, `exec --parallel`) | Recorded per repo and printed grouped, as in ADR 0020 E3 |
| E10 | Saga would fail halfway | Cannot fail from a skipped step; failures come from real reads only, so rollback does not occur |
| E11 | Code mutates state directly, bypassing the runner and `Effect` | Not caught by the mechanism. This is why commands opt in (D1) and why a conversion must grep for `os.`/`state.` mutations |
| E12 | `--dry-run` on `status`, `path`, `config` | Accepted and a no-op: they mutate nothing (`config init`/`edit` are not converted, so they reject it) |

## Implementation notes (not yet implemented)

- Depends on [ADR 0020](0020-command-summary.md): the recorder, `Classify`, `Effect`, and the renderer land first. This ADR adds a `DryRun` mode to the recorder and runners.
- `git.Runner`/`SystemRunner`: in dry-run mode, a mutating call is recorded and returns without running. The mock runner used by command tests is unaffected.
- A per-command opt-in, for example a field on the command's constructor or a `DryRunnable` marker checked in the root `Before` hook, which produces E1.
- Convert in this order: `delete` (the original motivation and the highest risk), `new`, `add`, `remove`, `push`, `rebase`, `exec`, `repo archive`, `repo unarchive` (replacing their local flags). `land` ships with `--dry-run` from the start.
- Tests: for each converted command, a real-repo scenario asserting that no ref, file, symlink, or state file changed (compare a snapshot of the main clone, the worktree list, and the data directory before and after), and that the output lists the expected commands. Plus a test that an unconverted command rejects the flag.
- Docs: README (a "Previewing" section), `CHANGELOG.md` entry, and index rows in [`DESIGN.md`](../DESIGN.md).

## Consequences

- New users can see what a command will do, in git terms, before it does it. Experienced users get a cheap safety check for `delete` and `land`.
- The preview runs the real code, so it stays accurate as commands change, and `--summary` and `--dry-run` share one renderer.
- Every command has to be converted before it supports the flag. Until then it refuses, which is slower to roll out but safe.
- Mutations that bypass the runners and `Effect` are invisible to both flags (E11). Reviewers need to hold that line.
- The preview is only as current as the last `fetch` (D4). That is an accepted trade-off for never touching the repos.

## Alternatives considered

- **Describe each saga step separately (`Step.Describe()`).** Rejected: a second description of every step, written by hand, drifts from `Do`. It also covers only saga-based commands.
- **Run for real in a copy** of the repos or a temporary worktree. Rejected: slow, large, and still runs hooks and network calls.
- **Fetch first so the plan is more accurate.** Rejected in D4: a preview must not touch the repos. Users who want current state can run `wtg repo fetch` first.
- **Per-command local flags, as `repo archive` does today.** Rejected: N hand-written second code paths. Existing ones are folded into the global flag.
- **Best-effort dry-run for all commands from day one.** Rejected in D1: the first unconverted mutation would silently happen.
- **Interactive "proceed?" after the plan.** Rejected: breaks scripting (see `AGENTS.md`). The plan, then re-running without the flag, is the intended flow.

## Decisions summary

| ID | Decision |
|----|----------|
| D1 | Global `--dry-run`, opt-in per command; unconverted commands refuse |
| D2 | Skip mutating calls at the git, `gh`, and `Effect` boundaries; reads run for real; unknown means mutating |
| D3 | The plan is based on current state; placeholders for results that do not exist; no outcomes claimed |
| D4 | No fetch, no prompts, no hooks |
| D5 | Plan to stdout, exit 0; result rows say "would"; same renderer as `--summary` |
| D6 | `exec` shows the command per repo and does not run it |
