# ADR 0024: Read-only workspace members — pinned, in `go.work`, never branched

- Status: Proposed
- Date: 2026-11-02
- Author: Steffen Uhlig

## Context

A workspace is one shared feature branch across several repos ([ADR 0007](0007-branch-strategy.md)). In practice, a feature touches two or three repos and merely *uses* a handful more: a library the code builds against, a docs repo, a code generator. Today wtg knows two kinds of member:

| Kind | Checkout | In `go.work` | Branch |
|---|---|---|---|
| Worktree | linked worktree of the main clone | yes ([ADR 0008](0008-go-work-generation.md)) | the workspace branch |
| Symlink (`always.repos`, [ADR 0014](0014-always-repos-are-symlinks.md)) | the main clone itself | **no** | whatever the main clone has |

There is nothing in between. A repo that is only consumed has to be either a worktree, which forces a feature branch nobody uses, or a symlink, which cannot be part of the Go workspace.

The first option is what happens in practice, and the result is a third, unmodelled state: worktrees that sit on a detached HEAD. In the `my-feature` workspace on 2026-11-02, three of ten members were detached (`config-lib`, `codegen`, `controller-a`). The consequences were:

- `wtg rebase` reports each of them as `⚠ skipped — on detached HEAD, expected my-feature` on every run and exits non-zero ([ADR 0018](0018-workspace-rebase.md), E7). The warning cannot be acted on, so it trains the user to ignore the exit code.
- Nothing ever updates them. `config-lib` was checked out at `v2.14.0`'s predecessor `v2.12.1`, two minor versions behind the `cli/go.mod` pin, and neither `wtg status` nor `wtg rebase` gave any signal.
- That mattered because `go.work` **overrides** `go.mod`. A member in the Go workspace is built from its checkout, whatever the consuming module pins. A stale checkout therefore silently changes what is built and tested locally, and CI (which has no `go.work`) builds something else. The same mechanism hid that the `api` checkout was 14 commits past its last tag: the code compiled locally and could not compile in CI.

Whether the stale `config-lib` caused the two failing integration tests seen that day was not established (a stale test cache is an equally plausible cause). The structural point stands either way: wtg lets a member influence the build without anyone having decided which version it should be.

### Git facts this design rests on

Checked in a throwaway repository with system git:

| Question | Result |
|---|---|
| Add a second worktree on a branch that another worktree has checked out | Refused: `'main' is already used by worktree at …` |
| Add a detached worktree at that branch's tip, or at a tag | Works. `git branch --show-current` is empty. |
| Read a module's `go.mod` at a tag without checking it out | `git show <tag>:go.mod` works. |

The first row is why a read-only member of a repo that is also checked out on its default branch in the main clone cannot simply be "a worktree on `main`". It has to be detached.

## Problem

Let a workspace contain repos that are consumed, not changed: present in `go.work`, at a version the user chose, without a feature branch, and without making `rebase`, `push`, `land` and `delete` treat them as work in progress. Let such a repo become an ordinary member when it turns out it needs a change. And make a member whose version differs from what its consumers require visible.

## Intent

```sh
wtg add --read-only [--ref <ref>] [<workspace>] <repo>...   # new: pinned, branch-less member
wtg add [<workspace>] <repo>...                              # existing: on a read-only member, promotes it
```

- A read-only member is a **detached worktree** at a resolved commit. It is a real directory, so it can be built, searched and run, and it is listed in `go.work` if it has a `go.mod`.
- It has no feature branch and is invisible to `push` and `land`.
- `wtg rebase` moves it forward when it follows a remote branch, and leaves it alone when it is pinned to a tag or commit.
- `wtg status` shows what it is pinned to and warns when a consumer in the workspace requires a different version (D6).

## Decisions

### D1. A third member kind: a detached worktree (decided)

Neither existing kind fits:

- **Not a symlink.** A symlink is the main clone ([ADR 0014](0014-always-repos-are-symlinks.md)): it cannot be pinned, it moves whenever the main clone is switched, and every workspace sees the same state. The exclusion of symlinks from `go.work` is deliberate there and stays unchanged; `always.repos` keeps its meaning.
- **Not a branch worktree.** It would need a feature branch that nobody commits to, and ADR 0007's rules (create, check out, error) would apply to a branch that should not exist.
- **Detached worktree.** It is the only form git allows for a second checkout of a ref the main clone may already have checked out (see the facts above), and it carries no branch to create, push or delete.

### D2. Recorded in state: `read_only` and `ref` (decided)

`state.RepoEntry` gains two optional fields, next to `symlink`:

```yaml
repos:
  - name: github.com/example/config-lib
    repo_path: /Users/me/git/github.com/example/config-lib
    worktree_path: /Users/me/workspace/ws/github.com/example/config-lib
    read_only: true            # omitted when false
    ref: v2.14.0               # the ref as the user gave it, or origin/<default branch>
```

- Both use `omitempty`, so existing state files load unchanged and no migration is needed.
- `ref` records the user's *intent* (`v2.14.0`, `origin/main`), not the resolved commit; the commit is always available from the worktree's `HEAD`.

### D3. Choosing the ref (decided)

```sh
wtg add --read-only config-lib                      # the version a consumer in the workspace requires (D8), else origin/<default branch>
wtg add --read-only --ref v2.14.0 config-lib        # pinned to a tag
wtg add --read-only --ref 1a2b3c4 config-lib        # pinned to a commit
wtg add --read-only --ref origin/main config-lib    # follows a branch
```

- Without `--ref`, the ref is derived from the consumers' `go.mod` files (D8). If no member of the workspace requires the module, it is `origin/<default branch>` (resolved per repo with `Runner.DefaultBranch`, as in [ADR 0018](0018-workspace-rebase.md) D1), after a fetch: the "follow main" behaviour.
- `--ref` applies to every repo named in that invocation. Repos that need different refs are added in separate invocations. A per-repo `repo@ref` syntax is rejected: `@` already has a meaning in remote URLs (`internal/remote`), and the repo-name resolution of [ADR 0005](0005-repository-discovery.md) should not grow a second syntax for one flag.
- `--ref` without `--read-only` is an error.
- A ref that does not resolve in a repo fails the pre-flight for the whole invocation, before anything is created ([ADR 0003](0003-transactions-saga-pattern.md)).
- A *local* branch name is accepted but is resolved to its current commit and is then fixed: only refs under `origin/` follow a moving target (D4).
- Deriving the default from the consuming `go.mod` is D8.

### D4. `wtg rebase` advances a following member and leaves a pinned one (decided)

Today a detached member is skipped with a warning that always fires ([ADR 0018](0018-workspace-rebase.md), E7). For read-only members that rule is replaced:

| Member | `wtg rebase` does |
|---|---|
| `ref` is `origin/<branch>` | After the fetch the command already performs, `git checkout --detach <new tip>` if it moved. Reported as `↑ advanced (was a1b2c3d)` or `✓ up to date`. |
| `ref` is a tag, commit or local branch | Nothing. Reported as `✓ pinned at <ref>`. |
| Worktree has uncommitted changes | `⚠ skipped — modified; a read-only member must stay clean (wtg add promotes it)`. Nothing is stashed or discarded. |

Advancing never rebases and never uses `--autostash`: there are no commits of ours to replay, and a dirty read-only worktree is a sign it should have been promoted.

### D5. Promotion is `wtg add`, as for symlinks (decided)

`wtg add <repo>` (no `--read-only`) on a read-only member replaces the detached worktree with a worktree on the workspace branch. It mirrors the symlink upgrade of [ADR 0014](0014-always-repos-are-symlinks.md) and goes through the same saga step order: remove the old checkout, create the worktree, secret copy, `go.work`, state. [ADR 0007](0007-branch-strategy.md)'s branch rules apply unchanged because the normal creation path is reused.

- The read-only worktree must be clean. A modified one is refused in pre-flight with the file list and the hint to commit or stash in the main clone first. Switching the detached worktree to a new branch in place (`git switch -c`) would keep the changes, but it cannot honour ADR 0007's "branch exists → check it out, do not reset" rule and would need its own branch logic. Rejected for v1.
- The saga's undo re-creates the detached worktree at the recorded `ref`.
- There is no direct demotion. `wtg remove` followed by `wtg add --read-only` does it, and `remove` already refuses to drop unpushed work.
- Adding a repo that is already a read-write member with `--read-only` is an error that names `wtg remove`.

### D6. Drift check in `wtg status` (decided; independent of D1–D5)

A member in `go.work` overrides what other members' `go.mod` files pin. `wtg status` therefore compares, for every pair of `go.work` members, the version a consumer requires with what is checked out, and prints a line under the table when they differ. This applies to **all** members, read-write ones included; it is the part of this ADR that would have caught the incident in Context on its own.

Algorithm, per consumer `go.mod` (parsed with `golang.org/x/mod/modfile`, already a dependency):

1. For each `require` of a module whose path equals another member's `module` directive, take the required version.
2. Skip it if the consumer has a `replace` for that module. The user decided; the check must not second-guess an explicit override.
3. Resolve the version in the member's repo: a tagged version via `refs/tags/<version>`, a pseudo-version via its 12-character commit suffix. If neither resolves, say nothing: "cannot verify" must never become a false alarm.
4. Compare with the member's `HEAD` using `git rev-list --left-right --count <version-commit>...HEAD`:

| Relation of checkout to the required version | Output |
|---|---|
| Same commit | nothing |
| Behind or diverged | `⚠ config-lib is 41 commits behind v2.14.0, which cli requires` |
| Ahead | muted note: `api is 19 commits ahead of v0.9.5 (cli requires v0.9.5; this will not build without go.work until a tag is cut)` |

"Ahead" is the intended state for a repo being edited, so it is a note, not a warning. It still tells the user that the branch cannot build elsewhere yet, which is the second half of the incident. `wtg status` never fails because of drift; warnings are information.

Steps 1 and 2 are what D8 gets from the Go toolchain instead of reimplementing; D6 should use the same helper (`gomod.RequiredVersion`, below) so that `status` and `add` can never disagree about what "required" means. Step 3 stays on plain git, because `status` must not need the network or a populated module cache (E9).

### D7. Behaviour of the other commands (decided)

| Command | Read-only member |
|---|---|
| `status` | Shows `◇ read-only @ <ref>` instead of a branch; a modified worktree is flagged `⚠`. Subject to D6. |
| `push` | `⚠ skipped — read-only member`, no failure, same as for symlinks. |
| `land` | `⚠ skipped — read-only member`. It is excluded from the PR-linking block ([ADR 0019](0019-workspace-land.md)). |
| `exec` | Included. Unlike a symlink it is a worktree of its own, so running a build or a search there is unsurprising. |
| `remove` | Removes the worktree. If the repo is in `always.repos`, the symlink is restored, as for any removed worktree. The unpushed-commit check is skipped (see below). |
| `delete` | Removes the worktree. `-d` and `-D` do not apply: no branch is looked up or deleted ([ADR 0009](0009-delete-and-branch-safety.md)). A modified worktree still produces the usual uncommitted-changes warning. |
| `new` | Unchanged in v1: every named repo is a read-write member. See Follow-ups. |
| `go.work` | Written for read-only members with a `go.mod`, like any worktree. Read-only members without one (docs, generated assets) are still members, just not listed. |

**This needs code changes, not just a new skip.** Both commands assume every non-symlink member has the workspace branch:

- `unpushedWarnings` ([`delete.go`](../../internal/cmd/delete.go)) runs `CommitsOnNoRemote` on `refs/heads/<space branch>`. For a read-only member that ref does not exist, so the call would error or report nothing useful. It must be skipped for read-only entries.
- `deleteOne` ([`workspace.go`](../../internal/cmd/workspace.go)) calls `BranchDelete(<space branch>)` after removing the worktree when `-d` or `-D` is given. In a repo that has no such branch this ends as `⚠ branch not deleted`, a false warning, and in a repo that happens to have a *same-named* branch (for example one left over from an earlier workspace) it could delete it. Read-only entries must return right after `WorktreeRemove`, the way symlink entries return right after unlinking.

The second case is the one to test: it is the only place where this feature could remove something the user did not ask to remove.

"Read-only" is a statement of intent, not file permissions: wtg does not `chmod` anything. A modification is detected and reported, not prevented.

### D8. The default ref comes from `go list`, not from a parser of our own (decided)

What a consumer requires is not what its `go.mod` literally says: minimum version selection, `replace` directives, `/vN` module paths, pseudo-versions and `vendor/` all play a part. The Go toolchain already implements all of it, so wtg asks it instead of reimplementing it.

Given the repo being added and the other members of the workspace:

1. Read the repo's module path from its `go.mod` at the ref being considered (`git show origin/<default>:go.mod`, then `modfile.ModulePath`). The path includes any `/vN` suffix (`github.com/example/config-lib/v2`). This is why no module-to-repo mapping is needed: the repo says which module it provides.
2. For each other member that has a `go.mod`, run in that member's directory:

   ```sh
   GOWORK=off GOFLAGS=-mod=readonly go list -m -f '{{.Version}}{{if .Replace}} replaced{{end}}' <module>
   ```

   - `GOWORK=off` is essential. In workspace mode a `use`d module is reported as the main module with no version.
   - `-mod=readonly` is essential too, for two reasons: it works in repos with a `vendor/` directory for modules that are in the build list but not vendored (the default fails there with `can't resolve module using the vendor directory`; verified for `controller-b` in `api`), and, unlike `-mod=mod`, it never touches `go.mod` or `go.sum`. During the investigation for this ADR `-mod=mod` left `go.sum` modified in four repos.
   - It returns the *selected* version, so multiple requirements and indirect requirements are already resolved. A module the consumer does not depend on fails with `not a known dependency`; that member simply does not vote.
   - A consumer with a `replace` for the module is skipped: the user has decided there, and the replacement is not a version of this repo (same rule as D6, step 2).
3. Turn the version into a ref:
   - A pseudo-version (`module.IsPseudoVersion`) becomes its commit, `module.PseudoVersionRev`.
   - Anything else is a tag with that name.
   - Both are resolved with plain git in the repo. The default is fetched first, as for every other ref, so a tag cut since the last fetch is found. A version that does not resolve is a pre-flight error that names the consumer and the version, not a silent fallback: the user asked wtg to match CI and it cannot.
4. Combine: all consumers agree, use it. Consumers disagree, fail pre-flight, list each `consumer → version`, and ask for `--ref`. Nobody requires the module, fall back to `origin/<default branch>`.

The result is recorded in state as an ordinary pinned `ref` (a tag or a commit). Nothing records that it was derived, so D4 needs no new case.

Verified on 2026-11-02 against the `my-feature` workspace with Go 1.27.3:

| Probe | Result |
|---|---|
| `GOWORK=off go list -m -f '{{.Version}}' github.com/example/config-lib/v2` in `cli` | `v2.14.0`, the same as `api` selects |
| Same, but with the path without `/v2` | `not a known dependency`, so the `/vN` suffix must come from the repo's own `module` line |
| Pseudo-version module (`shared-lib`) | `v0.0.0-20260101120000-0123456789ab`, handled by `module.PseudoVersionRev` |
| `go list -m` in `api` (has `vendor/`) for `controller-b`, which is in the build list but not vendored | Fails without `-mod=readonly`; with it, `v0.4.649`. For vendored modules (`config-lib/v2` in `api`; `controller-b` in the other controller repos and `controller-a`) both modes give the same version. The tree stays clean in all cases. |
| `go list -m -json <module>@v2.14.0` | `Origin.Hash` is `1a2b3c4…`, identical to the `config-lib` checkout's `HEAD`. Useful to cross-check, but needs the module cache or network, so wtg resolves with git instead. |

Consequences for wtg:

- **First use of the `go` binary.** wtg shells out to `git` and `gh` today (`internal/git`, `internal/remote`), so this is the same pattern, not a new kind of dependency. [ADR 0022](0022-no-tool-dependency.md) is about collaborators never needing wtg and is not affected: `go` is only used on the user's own machine, and only when `--ref` is omitted. If `go` is not on `PATH`, `wtg add --read-only` falls back to `origin/<default branch>` and prints one line saying why.
- It goes through the `Runner` abstraction like git does (new `GoRunner` or an extension), so `--dry-run`, `--summary` and the mock for tests apply ([ADR 0020](0020-command-summary.md), [ADR 0021](0021-dry-run.md)).
- Needs the module cache (or the network) in the *consumer*, since `go list -m` loads the build list. For a consumer whose dependencies were never downloaded this fails; it counts as "does not vote" with a warning, never as an error.
- Subdirectory modules (a `go.mod` below the repo root, whose tags are prefixed `sub/v1.2.3`) are not supported in v1: the module-path check in step 1 only looks at the root `go.mod`, and such a repo gets the fallback.

There is no spec to wait for: the behaviour above is the documented behaviour of `go list -m` and of `golang.org/x/mod/module`, both of which are stable and already in use by the toolchain. What was missing was knowing which flags make it safe, and that is what the table records.

## Edge cases

| # | Situation | Handling |
|---|---|---|
| E1 | `--ref` does not exist in one of several repos | Pre-flight error naming the repo; nothing is created. |
| E2 | Repo is a symlink in the space (`always.repos`) | `--read-only` replaces the symlink with a pinned worktree, the same upgrade path as in ADR 0014. The repo is now in `go.work`. |
| E3 | Repo has no `go.mod` | Read-only member without a `go.work` entry. The detached-HEAD warning on `rebase` goes away. |
| E4 | Main clone has the default branch checked out | Not a problem: the member is detached (see the facts above). |
| E5 | Fetch fails during `rebase` | `! fetch failed` for that repo only; the member stays where it is. |
| E6 | The followed branch was force-pushed upstream | `git checkout --detach` to the new tip is unconditional; nothing is replayed, so no conflict is possible. |
| E7 | Worktree has a rebase, merge or bisect in progress | `⚠ skipped — <reason>`, like ADR 0018 E2/E6. |
| E8 | Drift check: consumer has a `replace` for the member | Skipped (D6, step 2). |
| E9 | Drift check: tag not present locally | Silently skipped. `wtg status` does not fetch. |
| E10 | Drift check: several consumers require different versions | One line per consumer. |
| E11 | Existing detached member created before this ADR (state has no `read_only`) | Unchanged behaviour: still reported as unexpected. To adopt it, `wtg remove` and `wtg add --read-only`; a clean detached worktree has nothing to lose. |
| E12 | `--dry-run` / `--summary` | The new git operations go through the `Runner`, so ADR 0020 and ADR 0021 cover them without special handling. |
| E13 | Concurrent advance across several members | Per-repo goroutines, as in ADR 0018 and [ADR 0023](0023-parallelism-policy.md). |
| E14 | D8: `go` not on `PATH` | Fall back to `origin/<default branch>`, one explanatory line. |
| E15 | D8: consumers require different versions | Pre-flight error listing each; `--ref` resolves it. |
| E16 | D8: a consumer's build list cannot be loaded (modules not downloaded, offline) | That consumer does not vote; warning. |
| E17 | D8: required version does not resolve in the repo (tag missing after fetch, unreachable commit) | Pre-flight error naming consumer and version. |
| E18 | D8: the only consumer is the repo being edited in this workspace (read-write member that requires an older tag of the module being added) | It votes like any other; its requirement is what CI would build. |

## Implementation notes (proposed)

Two independently shippable steps. The first is useful on its own.

1. **Drift check (D6).** `internal/cmd/status.go` plus a small `internal/gomod` package:
   - `RequiredVersion(dir, module) (version string, replaced bool, err error)`: the `go list -m` call of D8 (with `GOWORK=off`, `-mod=readonly`).
   - `ModulePath(goModBytes)`: `modfile.ModulePath`.
   - `RefForVersion(version)`: pseudo-version → commit, else tag.

   New `Runner` methods: `RevListCount(path, a, b)` and `ResolveRef(path, ref)`. No state change, no new flags.
2. **Read-only members (D1–D5, D7).**
   - `state.RepoEntry`: `ReadOnly bool` (`read_only,omitempty`) and `Ref string` (`ref,omitempty`).
   - `Runner`: `WorktreeAddDetached(repoPath, worktreePath, ref)` and `CheckoutDetached(worktreePath, ref)`.
   - `internal/cmd/add.go`: `--read-only` and `--ref`, and the promotion branch next to the existing symlink upgrade. `internal/cmd/rebase.go`: replace the E7 skip for read-only members by D4. `push.go` and `land.go`: add `ReadOnly` next to the `Symlink` skips. `status.go`: render the new kind. `delete.go` and `workspace.go` (`deleteOne`): skip the branch logic for read-only entries (D7).
   - `detectGoMods` needs no change: it excludes `t.symlink` only.
3. Tests with the implementation (AGENTS.md): unit tests with the mock runner for E1–E18, and real-git scenarios via `internal/git/testhelper`: add pinned, add following, advance after an upstream commit, promote, refuse promotion when modified, `delete -D` leaving a same-named branch in a read-only member's repo untouched, and the drift cases (behind, ahead, replaced, unresolvable). `gomod` needs a test that runs the real `go` on a tiny fixture module (a consumer requiring a `/v2` module and a pseudo-version, with a `vendor/` directory) and asserts the fixture's `go.sum` is unchanged afterwards; skip when `go` is absent.
4. Docs: README section for `--read-only`, a note in [`docs/always.md`](../always.md) on how this differs from `always.repos`, a `CHANGELOG.md` entry under *Unreleased → Added*, and the index row in [`DESIGN.md`](../DESIGN.md) (already added).

## Without wtg ([ADR 0022](0022-no-tool-dependency.md))

- `read_only` and `ref` live only in the user-local state file. Nothing is written into any repository, committed or pushed.
- A read-only member is an ordinary git worktree. Without wtg, `git worktree list` shows it as a detached worktree, and `git worktree remove` removes it. The main clone gains no branch, config or hook.
- A collaborator cannot tell that a read-only member existed.
- The `go list` calls of D8 are read-only (`-mod=readonly`) and write nothing into any repository.

## Consequences

- Consumed repos stop being false alarms in `wtg rebase`, whose exit code becomes meaningful again.
- The version a member contributes to the build is chosen (a pinned ref) or at least tracked (a followed branch), instead of being whatever was last checked out.
- Drift between `go.work` and the `go.mod` pins becomes visible in the place the user already looks (`wtg status`), including the case where a branch has to be tagged before CI can build it.
- The cost is a third member kind that every per-repo command must handle. The `Symlink` checks are scattered across `add`, `delete`, `exec`, `land`, `push`, `rebase`, `remove`, `status` and `workspace` (17 sites in nine files at the time of writing), and the new kind has to be added to the same places. A small predicate on `RepoEntry` (for example `IsBranchless()`) would keep those checks from drifting; whether to introduce it is an implementation detail.
- "Read-only" is advisory. A user can edit a read-only member; wtg reports it but does not stop it.
- A followed member makes a workspace less reproducible, since two runs on different days see different code. Pinning is the answer, and it is one flag away.

## Alternatives considered

- **Include `always.repos` symlinks in `go.work`.** Rejected: the symlink is the main clone, so every workspace shares one mutable checkout whose branch changes when the main clone is switched. That is the drift problem in its worst form, and it contradicts the reason ADR 0014 excludes symlinks.
- **Keep detached worktrees as an unmodelled convention.** This is the status quo, and what produced the permanent `rebase` warning and the silent staleness.
- **Trim `go.work` by hand to the repos being edited.** Works until the next `wtg add` or `wtg remove`. [ADR 0008](0008-go-work-generation.md) states that only `use` directives are emitted, and `writeGoWork` rebuilds the file from scratch. Verified in a sandbox with `wtg add`: a leading comment and a `replace` directive were both gone afterwards, and the `go` directive was recomputed. `wtg remove` goes through the same `writeGoWork` and additionally deletes `go.work.sum` (by code reading; the sandbox run stopped at the unpushed-commit prompt). See Follow-ups.
- **Only the drift check (D6), no new member kind.** It is step 1 and is worth shipping alone, but it does not remove the `rebase` noise and gives the user no way to say "this one is not mine to change".
- **Parse the consumer's `go.mod` ourselves to derive the ref.** Would avoid the `go` dependency, but means reimplementing minimum version selection, `replace`, `/vN` and vendoring, and would be subtly wrong in each. Rejected in favour of D8.
- **Use `go list -m -json <module>@<version>` (`Origin.Hash`) to turn the version into a commit.** Exact and also handles pseudo-versions, but needs the module cache or network and can fail with `unknown revision` offline. Git, which already has the repo, does the same without either.
- **Promote in place with `git switch -c`.** Rejected for v1 (D5): it bypasses ADR 0007's branch rules.
- **Enforce read-only with file permissions.** Rejected: it breaks builds that write into the tree, and makes `git worktree remove` fail.

## Follow-ups

- `wtg new --read-only <repo>` (repeatable), so a mixed workspace can be created in one step.
- Preserve non-`use` directives (`replace`, `toolchain`, comments) when regenerating `go.work`, by editing the parsed file (`modfile.ParseWork`) instead of rewriting it. This is useful with or without this ADR.
- An `adopt` path for existing detached members (E11).

## Decisions summary

| ID | Decision |
|----|----------|
| D1 | A third member kind: a detached worktree, neither a symlink nor a branch worktree |
| D2 | State gains `read_only` and `ref` (both `omitempty`), recording the user's intent |
| D3 | `--read-only [--ref <ref>]` on `wtg add`; default ref comes from D8, else `origin/<default branch>`; `--ref` applies to the whole invocation |
| D4 | `wtg rebase` advances a member that follows `origin/<branch>`, leaves a pinned one, and never touches a modified one |
| D5 | Promotion is `wtg add` without the flag, mirroring ADR 0014; the worktree must be clean; no in-place branch switch |
| D6 | `wtg status` reports where a `go.work` member differs from what another member's `go.mod` requires; skips `replace`d modules and anything it cannot verify |
| D7 | `push` and `land` skip, `exec` includes, `remove` and `delete` just remove the worktree; "read-only" is advisory |
| D8 | The default ref is what the consumers' Go toolchain selects (`GOWORK=off go list -m` with `-mod=readonly`), mapped to a tag or commit with git; no `go.mod` parser of our own; falls back when `go` is absent |
