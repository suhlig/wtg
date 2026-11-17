# ADR 0026: Network timeout and visibility into slow repos

- Status: Accepted
- Date: 2026-11-17
- Author: Steffen Uhlig

## Context

`wtg repo sync --progress` once sat on a single repo for many minutes. Nothing was broken: `git fetch origin` was downloading a large pack over a slow link (about 200 KiB/s) and had hours of work left. wtg offered no way to see that. The progress bar said `(1 fetching)` without naming the repo, there was no upper bound on a fetch, and Ctrl-C ended the process without saying what it had been waiting for.

Every git call goes through `SystemRunner.run` ([ADR 0002](0002-git-abstraction-layer.md)), which used `exec.Command` with no deadline. Calls that contact a remote (`fetch`, `push`, `ls-remote`, `remote set-head`) can therefore wait for as long as the server and the link allow. [ADR 0023](0023-parallelism-policy.md) runs them in parallel, so one slow repo sets the finishing time of the whole command.

## Problem

Bound how long one remote git call may take, and make a slow or stuck repo identifiable while it runs and when the user gives up, in every command that waits on the network.

## Decisions

### D1. Network git and gh calls have a timeout, default 5 minutes

`git.network_timeout` (env `WTG_GIT_NETWORK_TIMEOUT`) is a Go duration; `0` disables it. It applies to one git command, not to a whole wtg run, so many repos do not eat into each other's budget. It covers git commands that contact a remote (`isNetwork` in `internal/git/system.go`) and every `gh` call (`internal/remote/gh.go`, which are all network calls); local git calls such as `status` are never timed out. The `gh` runner's limit is the same setting, applied per call, on top of the whole-phase `remoteTimeout` that `repo archive`/`unarchive` already had.

When it fires, the repo fails with `git fetch origin: timed out after 5m0s (raise git.network_timeout …)` and the other repos carry on. The message names the setting so the next step is obvious.

Five minutes is a ceiling for a call that has stopped being useful, not a target. A first fetch of a large repo can take longer on a slow link; users with such repos raise it or disable it.

### D2. A timeout stops git's whole process group

`git fetch` runs `git-remote-https`, `ssh` and `index-pack` as children. Killing only `git` leaves them running, holds the output pipes open, and leaves a `tmp_pack_*` file behind. Each git command therefore starts in its own process group (`internal/git/procgroup_unix.go`), and a timeout sends SIGTERM to the group, so git removes its temporary pack. `cmd.WaitDelay` bounds the wait if something ignores it. On non-Unix platforms only the git process is killed.

### D3. The runner knows what is running

`git.SystemRunner.Active()` and `remote.SystemRunner.Active()` list the commands in flight, with start time (and, for git, the directory). They are the source for everything below, so no command has to track its own repos; `internal/cmd/slow.go` reads them through one small interface. A runner that cannot report this (the test doubles) simply shows nothing. `wtg exec` has no runner, so it registers its user commands in a tracker of its own, read the same way.

### D4. A slow repo is named while it runs

A command is slow after 30 seconds (`slowThreshold` in `internal/cmd/slow.go`).

- With a progress bar (`repo sync --progress`, `rebase --progress`) the bar's suffix names the longest-running repo and counts the rest: `(1 fetching; slow: github.com/org/api 1m5s)`. The bar is redrawn every 5 seconds, so the duration moves even when no repo finishes.
- `exec --parallel` buffers every command's output until all are done, so its bar carries the same suffix: `[····] (slow: api 1m5s +1)`.
- Without a bar (`repo sync`, `repo fetch`, `rebase`, `push`, `land`, `delete`, `repo archive/unarchive --remote`, and the fetch in `new`/`add --read-only`) wtg prints `wtg: still waiting for: <repo> <duration>` to stderr every 30 seconds while something is slow. stdout, and so scripts that parse it, is unchanged.

### D5. Ctrl-C says what it was waiting for

On SIGINT or SIGTERM wtg prints the running git, gh and `exec` commands to stderr, stops them, and exits 130:

```
wtg: interrupted while waiting for:
  git -C /home/me/git/github.com/org/api fetch origin (42s)
  gh pr list --repo org/api (3s)
  command in org/api (36s)
```

Because of D2 the terminal no longer delivers Ctrl-C to git, so wtg stops the commands itself (`Interrupt` on each runner, `StopProcesses` for `exec`), with a short grace for git to remove its temporary pack. After the signal, an `exec` loop does not start the next repo's command.

### D6. `wtg exec` has an opt-in `--timeout`, and no default

`exec` runs commands the user chose, whose duration wtg cannot judge: a test suite may legitimately take an hour. A default would break such runs, so `--timeout <duration>` is off unless given. When it fires, that repo is reported `timed out after …`, the others carry on, and the command's process group is stopped, so a shell's children do not outlive it (where stdin is a terminal the command stays in wtg's group instead, so an interactive command is not stopped by SIGTTIN, and only the command itself is stopped).

## Consequences

- A hung or very slow remote costs at most the timeout per repo and is reported as that repo's failure instead of blocking the run.
- The user can tell, without a second terminal, which repo is slow and for how long.
- A `fetch` that was interrupted for time has transferred part of a pack and discards it; the next run starts that repo again from the beginning. For a repo that needs longer than the timeout the user must raise it.
- On Windows the timeout kills only the git process; its helpers may outlive it until they notice the closed pipe.

## Alternatives considered

- **Timeout only on idle (no bytes received).** Better matches "stuck" than "slow", but git exposes no byte counter to the caller, and `http.lowSpeedLimit`/`lowSpeedTime` only cover HTTP. A wall-clock limit is simple, uniform across transports, and good enough with a configurable value.
- **One timeout for the whole run.** Penalises the repos that happen to be last in a large fan-out.
- **Per-command `--timeout` flag for git and gh.** Not needed to solve the problem; the config key and environment variable cover scripted use. Only `exec`, which has no config default, gets one (D6).
- **A default timeout for `exec`.** Rejected, see D6.
- **Pass git's own `http.lowSpeedLimit` via `-c`.** Only HTTP, and would silently change what a user's own git config means.

## Implementation status

Implemented for every command that reaches the network through the git or gh runners (`repo sync`, `repo fetch`, `rebase`, `push`, `land`, `delete`, `repo archive/unarchive --remote`, and the fetch inside `new`/`add --read-only`), and for `wtg exec`.

- The slow notice is wired into each of those commands. `repo sync`, `rebase` and `exec --parallel` show it in the bar and redraw on a timer; the rest print a line to stderr.
- `repo archive` and `repo unarchive` now receive the shared gh runner from `main`, instead of building a private one, so they honour `git.network_timeout` and are covered by the Ctrl-C report.
- A gh call that times out or is interrupted is reported as that (`gh auth status for github.com: timed out after …`), never as "not authenticated".
- `internal/trace` gained `CommandContext`, `OwnProcessGroup` and `TerminateGroup`; the process-group helpers have a non-Unix fallback that kills only the process.

## Follow-ups

None.
