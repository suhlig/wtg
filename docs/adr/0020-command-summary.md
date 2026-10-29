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

### D4. Every filesystem mutation goes through one typed interface, and shows up as a `(wtg)` line (decided)

Many effects are not git or `gh` commands: writing `go.work`, copying `always.files` and `always.secrets`, creating and removing symlinks and directories, saving and deleting space state, moving a clone to the archive. A summary that listed only git would be incomplete in a misleading way ("I see no deletion, yet my state file is gone").

All such mutations go through a small interface in `internal/trace`, passed in the way the git runner is:

```go
// FS is the only way wtg code changes the filesystem.
type FS interface {
	WriteFile(path string, data []byte, perm fs.FileMode) error
	MkdirAll(path string, perm fs.FileMode) error
	Remove(path string) error
	RemoveAll(path string) error
	Rename(oldPath, newPath string) error
	Symlink(target, link string) error
}
```

- The production implementation records the operation and then performs it with the `os` package. It does **not** shell out to `cp`, `rm`, `ln` or `mv` (see Alternatives): Go's file operations keep atomicity, permissions, portability, and error values such as `fs.ErrNotExist`, which the code checks with `errors.Is`, and they avoid building shell arguments from paths.
- The description is **derived from the operation and its arguments** (`write ~/ws/my-feature/go.work`, `remove …`, `symlink <link> -> <target>`, `rename <old> -> <new>`, `mkdir …`). Authors cannot write a misleading one, and there is no free-text label to keep in sync.
- A third behaviour is added by [ADR 0021](0021-dry-run.md): record and skip. A fourth is for tests: an in-memory implementation, so command tests no longer need real temp directories for mutations.
- Space state is not special. `state.Save` and `state.Delete` take an `FS`, so a state write appears as `write ~/.local/share/wtg/spaces/my-feature.yaml`. Reads (`os.ReadFile`, `os.Stat`, `os.ReadDir`) stay direct; they change nothing.
- Programs that wtg starts on the user's behalf (`wtg exec`, the `always.run` hook, `$EDITOR` for `config edit`) are not git and cannot be classified. A wrapper in `internal/trace` constructs and runs them, records the program and arguments as `(wtg) run: <program> <args>`, and is the only place besides the git and `gh` boundaries that may start a process (D7). The exact shape of the wrapper is an implementation detail.

### D5. Output: stderr, after the run, also on failure, grouped by repo (decided)

- **Stderr.** The command's normal output on stdout stays parseable; scripts that read it are unaffected.
- **After the run.** Printing as each command runs would interleave with result tables and spinners, and with the per-repo goroutines of `push`, `rebase`, and `exec --parallel`.
- **Also when the command fails.** A failed run is when the summary matters most. The failing command is marked with its exit code: `git -C … push …   # exit 1`. (The hook for printing after an error needs checking against urfave/cli v3's `After` behaviour during implementation.)
- **Grouped by repo,** in the order repos first appeared, with sequence order preserved inside each group. `(wtg)` lines for the workspace go in a final group.
- **Copy-pasteable.** Arguments are shell-quoted when needed, and paths are printed in full so a line can be run as shown. Reads that were hidden (D3) are not a gap in this sense, since they do not change anything.

### D6. What is never recorded (decided)

Environment variables, command output, and the contents of files. wtg itself never puts credentials in arguments (it relies on git's and `gh`'s own auth), so arguments are safe to print as passed. For the hook, only the script path and event name are shown. This follows the security rule against logging sensitive data.

### D7. Bypassing the interface fails CI (decided)

An interface is only a convention until something enforces it. The project already runs `golangci-lint` in CI (`.github/workflows/ci.yml`) without a config file. A `.golangci.yml` adds the [`forbidigo`](https://golangci-lint.run/usage/linters/#forbidigo) linter with `analyze-types: true` (so aliased imports are caught) and forbids, in non-test code:

- `os.WriteFile`, `os.Mkdir`, `os.MkdirAll`, `os.Remove`, `os.RemoveAll`, `os.Rename`, `os.Symlink`, `os.Create`, `os.OpenFile`, `os.Chmod`, `os.Truncate`
- `exec.Command`, `exec.CommandContext`

Allowed locations (path exclusions): `internal/trace/` (the implementation), `internal/git/system.go` and `internal/remote/gh.go` (the two process boundaries of D2), `internal/git/testhelper/`, and `_test.go` files.

- The rest of the default linter set is kept (`linters.default: standard`), so the new file adds a rule and does not change the existing ones.
- **Measured before writing this ADR.** A trial run of exactly this rule over the repository found 52 sites in 15 files. Of these, 6 are intended boundaries (git, `gh`, `exec`, the hook, the editor) and 3 are in the test helper. The remaining 43 are real filesystem mutations to convert: 14 in `workspace.go`, 4 each in `unarchive.go`, `remove.go` and `delete.go`, 3 each in `config.go`, `state.go`, `secrets.go`, `cmd/archive.go` and `internal/archive/archive.go`, and 2 in `add.go`.
- The rule can be adopted gradually: until a file is converted it carries a `//nolint:forbidigo // TODO ADR 0020` marker, or is excluded by path. The exclusion list shrinks to the allowed locations above as conversion finishes, and the marker count is the progress measure.
- **Limits.** `forbidigo` matches calls, so it will not catch the same effect reached through another package (`syscall`, a third-party library, a method on an open `*os.File`). `os.Create` and `os.OpenFile` are on the list for that reason, and review still has to look at new imports. This closes the common accident (calling `os.Remove` out of habit), and does not make bypassing impossible.

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
| E8 | The user's `$EDITOR`, `exec` command, or hook script itself changes files | Not visible and not claimed: the summary says what wtg started (`(wtg) run: …`), not what that program did |
| E9 | A file write fails halfway | The `(wtg)` line is kept and marked with the error, as for a failed git call (E2) |
| E10 | Code outside the allowed locations calls `os.Remove` or `exec.Command` | `forbidigo` fails the CI lint job (D7) |

## Implementation notes (not yet implemented)

- New package `internal/trace`: `Recorder` (mutex-guarded slice), `Entry`, `Classify` (D3), the `FS` interface with its production, in-memory, and (ADR 0021) skipping implementations (D4), the process wrapper (D4), and a renderer (D5).
- `git.New()` and `remote.SystemRunner` accept an optional `*trace.Recorder`. With a nil recorder behaviour is unchanged, so existing tests do not need to change.
- Commands receive an `FS` the way they receive a `git.Runner` today (a constructor argument). `state.Save` and `state.Delete` gain an `FS` parameter. Tests that currently write to `t.TempDir()` keep working with the production `FS`.
- `.golangci.yml` with `forbidigo` (D7). Land it first, with the `//nolint` markers on the existing 43 sites, so the count can only go down.
- Convert in the order of ADR 0021 (`delete` first), one command per change, removing its markers.
- Root flag and an `After` hook in `main.go` print the summary.
- Tests: `Classify` as a table over every verb used in `system.go` and `gh.go` (a new verb must be added deliberately or it shows as mutating); recording against a real repo from `internal/git/testhelper`; the renderer with golden text for grouping and failure; the production `FS` records exactly what it performs; for each converted command, a test that its mutations appear in the summary.
- Docs: README section, `CHANGELOG.md` entry under *Unreleased → Added*, and the index row in [`DESIGN.md`](../DESIGN.md).

## Consequences

- Experienced users get an exact, ordered, per-repo account of a run, in git terms, without wtg printing more by default.
- New users can read it as documentation of what wtg is doing for them.
- Mutations that go through the git runner, the `gh` runner, or `FS` are all visible, and CI rejects the common ways of bypassing them (D7). The remaining gap is code that mutates through another package, which review has to catch (D7, limits). ADR 0021 relies on the same guarantee.
- About 43 call sites in 10 files have to be converted before the lint exclusions can be removed. It is mostly mechanical, and it is staged by the `//nolint` markers.
- Commands gain an `FS` dependency, which makes them easier to test (an in-memory `FS`) at the cost of one more constructor argument.
- Entries are always collected, which has a negligible cost but means every command path now touches the recorder.

## Alternatives considered

- **A config setting that turns it on permanently.** Rejected in D1.
- **`GIT_TRACE=1`.** Exists already, but is noisy, internal, and blind to non-git effects and `gh`.
- **Print each command as it runs (`--verbose`).** Rejected in D5: interleaves with tables and parallel output. A live view could be added later as a separate flag.
- **Log every command, including reads.** Rejected in D3 as the default. A `--summary=all` variant is a possible follow-up.
- **Structured output (`--summary=json`).** Possible follow-up; the entry type already carries the fields.
- **Record inside each command.** Rejected in D2: easy to forget, and it drifts from what actually ran.
- **Route every mutation through command-line programs (`cp`, `rm`, `ln -s`, `mv`, `tee`)** so that one process boundary sees everything. Rejected in D4: it would make every effect visible, but at the price of quoting and portability bugs (BSD and GNU flags, Windows), the loss of atomic writes, permissions, and `fs.ErrNotExist`, and shell arguments built from paths. The summary would also show `rm` and `cp` where `write go.work` is clearer to a reader. The same visibility comes from a typed Go interface without any of that.
- **A free-text `Effect(desc, fn)` wrapper.** Considered in an earlier draft of this ADR. Rejected in D4: the description could disagree with what `fn` does, and nothing stopped a command from skipping the wrapper.
- **Enforce by review only.** Rejected in D7: the failure mode (one forgotten `os.Remove`) is silent and shows up only when a dry run mutates something.

## Decisions summary

| ID | Decision |
|----|----------|
| D1 | Global per-invocation flag `--summary`; no config setting |
| D2 | Record at `SystemRunner.run` and `ghOutput` through a shared `trace.Recorder` |
| D3 | Show mutating commands only; read-only is an explicit allowlist, unknown means mutating; `fetch` counts as mutating |
| D4 | All filesystem mutations go through a typed `FS` interface (Go file operations, not shell commands); descriptions are derived from the operation; user-started programs go through a recording wrapper |
| D5 | Printed to stderr after the run, also on failure, grouped by repo, copy-pasteable |
| D6 | No environment, output, or file contents are recorded |
| D7 | `forbidigo` in CI forbids direct `os` mutations and `exec.Command` outside `internal/trace` and the two process boundaries |
