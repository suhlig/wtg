+++
title = "Read-Only Workspace Members"
description = "Behaviour and reference for read-only workspace members."
weight = 30
+++

# Read-Only Members

A feature touches two or three repos and merely *uses* a few more: a library you built against, a docs repo, a code generator. `wtg add --read-only` adds such a repo as a **detached worktree at a ref you choose**. It is a real directory, so you can build, search and run in it, and it is listed in `go.work` if it has a `go.mod` — but it has no feature branch, and `push`, `land` and the branch safety checks in `remove`/`delete` ignore it.

## Ref Resolution

| Invocation | Ref Selected |
|---|---|
| `--read-only` | the version the other members' `go.mod` files select (`go list -m`), else `origin/<default branch>` |
| `--read-only --ref v2.0.0` | a tag, pinned |
| `--read-only --ref 1a2b3c4` | a commit, pinned |
| `--read-only --ref origin/main` | a branch, followed by `wtg rebase` |

`--ref` applies to every repo named in that invocation (every `--read-only` repo, for `wtg new`); add repos that need different refs in separate invocations. A ref that does not resolve fails before anything is created. Without `--ref`, consumers that require different versions are an error that lists each one, because `go.work` overrides `go.mod`: the member contributes exactly one version to the build and it has to be the right one.

## Command Behaviour with Read-Only Members

| Command | Behaviour |
|---|---|
| `status` | shows `◇ read-only @ <ref>`; a modified worktree is flagged |
| `rebase` | advances it when it follows `origin/<branch>`, leaves a pinned one alone, never touches a modified one |
| `push`, `land` | skipped, with no failure |
| `exec` | included — it is a real worktree of its own |
| `remove`, `delete` | removes the worktree; `-d`/`-D` have no branch to delete |
| `add <repo>` | promotes it to a normal worktree on the workspace's branch |

## Promotion and Re-pinning

"Read-only" is a statement of intent, not file permissions: `wtg` reports a modification but does not prevent one. To change a read-only member, promote it first. To re-pin one, `wtg remove` it and add it again with a different `--ref`.

`wtg status` also compares, for every pair of `go.work` members, the version a consumer requires with what is checked out, and prints a line when they differ:

```
⚠ config-lib is 12 commits behind v2.0.0, which api requires
```
