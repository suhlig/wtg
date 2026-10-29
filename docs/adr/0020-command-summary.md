# ADR 0020: Reporting what a run did with `--summary`

- Status: Proposed
- Date: 2026-10-29

## Context

wtg is a thin layer over system git ([ADR 0001](0001-library-choices.md)) and, for archiving and landing, over `gh` ([ADR 0013](0013-repo-archive.md), [ADR 0019](0019-workspace-land.md)). One wtg command expands into many git commands across many repos. By default wtg prints a short result table and nothing else, which is right for everyday use.

Experienced users sometimes want the opposite: an exact account of what happened, in the terms they already know (git commands), so they can check it, reproduce one step by hand, or understand a failure in one repo. Today the only way is `GIT_TRACE=1`, which is noisy, lists git's internal child processes, and says nothing about the things wtg does that are not git (writing `go.work`, saving state, creating symlinks, running the `always.run` hook) or about `gh`.

The companion question, previewing a run before it happens, is [ADR 0021](0021-dry-run.md). The two share one recording mechanism, which this ADR introduces.

## Problem

Let the user ask, per invocation, for a list of the commands and side effects that a wtg run executed, in order, copy-pasteable where possible, without changing what the command prints by default.

## Intent

```sh
wtg new my-feature api payments --summary
wtg --summary push                       # either position works
```

```
✓ api       worktree created
✓ payments  worktree created

Commands run:
  git -C ~/git/api       worktree add -b my-feature ~/ws/my-feature/api main
  git -C ~/git/payments  worktree add -b my-feature ~/ws/my-feature/payments main
  (wtg) write ~/ws/my-feature/go.work
  (wtg) save state my-feature
```

The normal output is unchanged. The summary is appended after it.

## Decisions

### D1. A global, per-invocation flag; no configuration (decided)

`--summary` is a flag on the root command. In urfave/cli v3 (the version in `go.mod`, 3.11.0) root flags are persistent, so `--summary` is accepted before or after the subcommand; this was verified with a small test program before writing this ADR.

There is deliberately no `[output] summary = true` setting. wtg prints minimal information by default, and a hidden default that changes output makes support and scripting harder. A user who wants it always can use a shell alias.

### D2. Record at the process boundary, not in the commands (decided)

All git commands go through one function, `SystemRunner.run`, and all `gh` commands through `ghOutput` (`internal/remote/gh.go`). A recorder at those two points sees exactly what was executed, so the summary cannot drift from the real behaviour, and no command has to remember to log anything.

- `main` creates one `trace.Recorder` and passes it to `git.New` and to the `gh` runner. It always records (the cost is one slice append per command) and prints only if `--summary` was given. This avoids constructing runners after flag parsing.
- Each entry holds the executable, the arguments as passed, the working repo (for git: the `-C` path), the exit code, and a sequence number.
- Environment variables are not recorded (see D6).

### D3. Mutating commands only; fail towards showing (decided)

The default summary lists only commands that can change something. Reads (`status`, `rev-parse`, and so on) would drown the three commands that matter.

A git command is **read-only** only if its verb (plus subcommand where needed) is on an explicit allowlist:

| Read-only (hidden) | Notes |
|---|---|
| `rev-parse`, `rev-list`, `merge-base`, `symbolic-ref` (read form) | |
| `status`, `stash list`, `worktree list`, `remote` (list), `remote get-url` | |

Everything else is shown, including anything new that a later change adds: **unknown means mutating**. Specific classifications:

- `fetch` is shown. It writes remote-tracking refs and uses the network.
- `remote set-head origin --auto`, which `DefaultBranch` runs as a repair, is shown even though it hides inside a method that looks like a read.
- `ls-remote` is shown. It is read-only, but it is a network call that the user would want to know about.
- `gh` commands: `pr create`, `pr edit`, `repo archive`, `repo unarchive` are shown; `pr list`, `pr view`, `repo view`, `auth status` are reads.

The same classification is reused by [ADR 0021](0021-dry-run.md), where the stakes are higher (it decides what is not executed). That is why the default is "mutating" and the allowlist is the only way out.

### D4. Effects that are not commands are reported as `(wtg)` lines through one helper (decided)

Many effects are not git or `gh` commands: writing `go.work`, copying `always.files` and `always.secrets`, creating and removing symlinks and directories, saving and deleting space state, and invoking the `always.run` hook. A summary that listed only git would be incomplete in a misleading way ("I see no deletion, yet my state file is gone").

These effects are wrapped in one helper:

```go
// Effect records desc and runs fn.
func (r *Recorder) Effect(desc string, fn func() error) error
```

- In this ADR `Effect` only records and runs. [ADR 0021](0021-dry-run.md) gives it a second behaviour (record and skip), which is why the helper is introduced now rather than as ad hoc log calls: converting a command once serves both flags.
- `desc` is written for a human and is stable text, for example `write ~/ws/my-feature/go.work`.
- The first conversion targets the effects found in `new`, `add`, `remove`, `delete` (the `os.Remove`, `os.WriteFile`, `os.Symlink`, `os.Rename`, `state.Save`, `state.Delete`, and `runSpaceScript` calls).
- `wtg exec` runs the user's own command, not git. It is recorded as given (`exec: <cmd>`), since that is what ran.

### D5. Output: stderr, after the run, also on failure, grouped by repo (decided)

- **Stderr.** The command's normal output on stdout stays parseable; scripts that read it are unaffected.
- **After the run.** Printing as each command runs would interleave with result tables and spinners, and with the per-repo goroutines of `push`, `rebase`, and `exec --parallel`.
- **Also when the command fails.** A failed run is when the summary matters most. The failing command is marked with its exit code: `git -C … push …   # exit 1`. (The hook for printing after an error needs checking against urfave/cli v3's `After` behaviour during implementation.)
- **Grouped by repo,** in the order repos first appeared, with sequence order preserved inside each group. `(wtg)` lines for the workspace go in a final group.
- **Copy-pasteable.** Arguments are shell-quoted when needed, and paths are printed in full so a line can be run as shown. Reads that were hidden (D3) are not a gap in this sense, since they do not change anything.

### D6. What is never recorded (decided)

Environment variables, command output, and the contents of files. wtg itself never puts credentials in arguments (it relies on git's and `gh`'s own auth), so arguments are safe to print as passed. For the hook, only the script path and event name are shown. This follows the security rule against logging sensitive data.

## Edge cases

| # | Situation | Handling |
|---|-----------|----------|
| E1 | Command makes no mutating calls (for example `wtg status`) | `Commands run: (none)` |
| E2 | A git call fails | The line is kept and marked with the exit code; later calls in the same group may be missing, which is the point |
| E3 | Parallel commands (`push`, `rebase`, `exec --parallel`) | Recorded concurrently (mutex), printed grouped by repo (D5) |
| E4 | A saga fails and rolls back | Rollback commands are listed too, after the failure, since they ran |
| E5 | `--summary` with `--dry-run` | The same renderer prints the plan under a different heading (ADR 0021) |
| E6 | Tests with the mock `git.Runner` | The mock does not shell out, so it records nothing; recording is tested against the real `SystemRunner` |
| E7 | `completion` or other commands that never reach a runner | Accepted, prints `(none)` |

## Implementation notes (not yet implemented)

- New package `internal/trace`: `Recorder` (mutex-guarded slice), `Entry`, `Classify` (D3), the `Effect` helper (D4), and a renderer (D5).
- `git.New()` and `remote.SystemRunner` accept an optional `*trace.Recorder`. With a nil recorder behaviour is unchanged, so existing tests do not need to change.
- Root flag and an `After` hook in `main.go` print the summary.
- Tests: `Classify` as a table over every verb used in `system.go` and `gh.go` (a new verb must be added deliberately or it shows as mutating); recording against a real repo from `internal/git/testhelper`; the renderer with golden text for grouping and failure; `Effect` for the converted commands.
- Docs: README section, `CHANGELOG.md` entry under *Unreleased → Added*, and the index row in [`DESIGN.md`](../DESIGN.md).

## Consequences

- Experienced users get an exact, ordered, per-repo account of a run, in git terms, without wtg printing more by default.
- New users can read it as documentation of what wtg is doing for them.
- A new place that mutates state without going through the runner or `Effect` is invisible to the summary. This is a convention that has to be maintained, and it is the same one ADR 0021 relies on.
- Entries are always collected, which has a negligible cost but means every command path now touches the recorder.

## Alternatives considered

- **A config setting that turns it on permanently.** Rejected in D1.
- **`GIT_TRACE=1`.** Exists already, but is noisy, internal, and blind to non-git effects and `gh`.
- **Print each command as it runs (`--verbose`).** Rejected in D5: interleaves with tables and parallel output. A live view could be added later as a separate flag.
- **Log every command, including reads.** Rejected in D3 as the default. A `--summary=all` variant is a possible follow-up.
- **Structured output (`--summary=json`).** Possible follow-up; the entry type already carries the fields.
- **Record inside each command.** Rejected in D2: easy to forget, and it drifts from what actually ran.

## Decisions summary

| ID | Decision |
|----|----------|
| D1 | Global per-invocation flag `--summary`; no config setting |
| D2 | Record at `SystemRunner.run` and `ghOutput` through a shared `trace.Recorder` |
| D3 | Show mutating commands only; read-only is an explicit allowlist, unknown means mutating; `fetch` counts as mutating |
| D4 | Non-command effects are `(wtg)` lines produced by one `Effect` helper |
| D5 | Printed to stderr after the run, also on failure, grouped by repo, copy-pasteable |
| D6 | No environment, output, or file contents are recorded |
