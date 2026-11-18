+++
title = "Repository Sync State & Progress Indicators"
description = "Understanding wtg repo sync progress symbols and state transitions."
weight = 100
+++

# `wtg repo sync` — progress symbols and state diagram

## Progress bar symbols

Run `wtg repo sync --progress` to watch each repo's slot in real time.
Each slot cycles through these symbols:

| Symbol | Meaning |
|--------|---------|
| `·`    | Waiting to start |
| `~`    | Fetch in flight |
| `ø`    | Silently omitted — no remote configured |
| `✓`    | Done — already up to date |
| `↑`    | Done — fast-forwarded to origin |
| `⚠`    | Skipped — wrong branch or uncommitted changes (purely local check, flips fast) |
| `!`    | Error — fetch, fast-forward, or status call failed |

The bar also shows a `(N fetching)` suffix after the closing `]` with the
count of in-flight network calls, so it is always clear whether sync is
still working or just waiting on the last few stragglers.

When a fetch has run for more than 30 seconds the suffix names the slowest repo
and how long it has been going, e.g. `(2 fetching; slow: github.com/org/api 1m5s +1)`
(`+1` means one more is slow too). Without `--progress`, a
`wtg: still waiting for: …` line is printed to stderr instead. A fetch is stopped
after `git.network_timeout` (default 5 minutes) and the repo shows `!`; Ctrl-C
prints the git commands that were still running.

`⚠` slots flip noticeably faster than the others because the branch and
dirty checks are local and run before any network call.

## State transition diagram

```mermaid
stateDiagram-v2
    [*] --> · : slot initialised
    · --> ~ : goroutine starts
    ~ --> ø : no remotes (omitted from table)
    ~ --> ⚠ : skipped — wrong branch
    ~ --> ⚠ : skipped — uncommitted changes
    ~ --> ✓ : fetched, already up to date
    ~ --> ↑ : fetched, fast-forwarded
    ~ --> ! : any error (fetch / ff / status / default branch)
    ø --> [*] : omitted
    ⚠ --> [*]
    ✓ --> [*]
    ↑ --> [*]
    ! --> [*]
```
