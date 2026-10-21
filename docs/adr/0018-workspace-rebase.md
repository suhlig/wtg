# ADR 0018: Rebasing a workspace with `wtg rebase`

- Status: Accepted
- Date: 2026-10-20

## Context

A workspace is one shared feature branch across several repos ([ADR 0007](0007-branch-strategy.md)). The branch is cut from the default branch of each repo when the workspace is created. While the feature is in progress, the default branches move on, and the feature branches need to be brought up to date, in every repo.

wtg offers nothing for this. [ADR 0010](0010-repo-sync-scope.md) deliberately keeps `repo sync` away from worktrees and leaves rebasing to the developer. The only route today is `wtg exec <ws> -- git rebase …`. It works, but it needs a shell one-liner to find each repo's default branch, has no rebase-specific reporting, and gives no hint about what to do after a conflict.

The runner already has `Rebase(repoPath, onto string)`, but nothing calls it, and it cannot abort, detect an in-progress rebase, or tell "already up to date" from "rebased".

## Problem

Provide one command that brings every repo's workspace branch up to date with that repo's base branch, as fast as possible (all repos in parallel), with predictable results when some repos succeed and others do not. Worktrees are frequently dirty, so dirty worktrees must be handled rather than skipped.

## Intent

```sh
wtg rebase [<workspace>] [--onto <branch>] [--progress]
```

- Fetch `origin` and rebase the workspace branch onto the repo's default branch (or `--onto`), with `--autostash`, in every non-symlink repo of the workspace.
- Per-repo work runs in parallel; each repo's goroutine does fetch, then checks, then rebase, with no barrier between phases, so a slow fetch in one repo never delays the others.
- Results are reported per repo in a table, like `wtg push`. The exit code is non-zero if any repo failed or was left needing attention.
- `--progress` shows the live per-repo bar used by `repo sync --progress` and `exec --parallel` (`~` busy, `✓`, `↑`, `⚠`, `!`). It drives wtg's own bar only; it is **not** forwarded to git (see D4).

### Workspace inference

The workspace argument is optional and is inferred from the current directory. This reuses `resolveSpaceArg` and `spaceContainingCWD`, exactly as `push`, `status`, and `exec` do. Outside any workspace, the command errors with "specify one explicitly". Shell completion uses `completeSpaces`.

## Decisions

### D1. Rebase onto `origin/<default branch>`, per repo

wtg does not record which branch a workspace branch was based on (git does not store it either), so the base is derived: `origin/<default>` of each repo, using `Runner.DefaultBranch`. The default can differ per repo (`main`, `master`, `develop`), so it is resolved per repo.

- The fetch makes `origin/<default>` current. The main clone's local default branch is never touched, which keeps the ADR 0010 promise. No prior `repo sync` is needed, and a stale local `main` cannot cause a stale rebase.
- Rejected: rebase onto the local default branch. It silently depends on `repo sync` having been run.

**Non-default bases (decided).** v1 supports the default branch only, plus `--onto <branch>` to override it with one branch name for all repos (`origin/<branch>` is used). A repo where that ref does not exist fails individually. Recording a per-repo base in the state file (`RepoEntry.Base`, written by `new`/`add`) is a possible follow-up. Rejected: heuristic detection from the nearest merge-base, because it can silently pick the wrong branch.

### D2. Conflicts: abort automatically (decided)

A conflict stops `git rebase` halfway, and in a parallel run no one can resolve it interactively. On a conflict, wtg runs `git rebase --abort`. The worktree is left exactly as it was before, including its uncommitted changes (`--abort` re-applies the autostash; see D6). The table says `! conflict` and prints the retry command, `git -C <worktree> rebase --autostash origin/<default>`, so the user can resolve it by hand. A worktree is never left half-rebased, which would confuse `wtg status` and `wtg push`.

### D3. No saga for this command

[ADR 0003](0003-transactions-saga-pattern.md) uses pre-flight plus rollback for `new`/`add`/`delete`. `rebase` does not follow it: repos are independent, and each is all-or-nothing on its own (success, or abort and restore on conflict). Undoing successful repos because another conflicted would discard the work that went right. This matches `push` and `exec`, which also continue past failures and report at the end.

To keep recovery possible, each successful repo's message includes the old tip, `rebased (was a1b2c3d)`. Undo it with `git reset --keep a1b2c3d`, **not** `--hard`: `--keep` refuses rather than discarding uncommitted changes made since. The reflog also has it.

### D4. `--progress` is for wtg's bar only (decided)

`git rebase` has no progress option. `git fetch --progress` exists, but wtg captures git's stderr in `SystemRunner.run`, so nothing from it would reach the terminal, and letting git write to the terminal from N goroutines would corrupt wtg's bar. The flag therefore only drives wtg's own per-repo bar, as `repo sync --progress` does today, and is not forwarded to git. The bar label shows `(N fetching, M rebasing)`.

### D5. `rebase` never pushes; `wtg push` warns about diverged branches (decided)

A rebase rewrites history. If the branch was already pushed, `wtg push` (which runs `git push --set-upstream origin <branch>` without force) is rejected for each rebased repo. `rebase` does not push or force anything; pushing a rewritten shared branch is the user's call.

**Can we detect at push time that the branch was rebased?** Not with certainty, but we can detect the thing that matters: the branch has *diverged* from its upstream. A rebased branch has a different history than `origin/<branch>`, so the upstream tip is no longer an ancestor of `HEAD`. Options considered:

1. **Divergence check at push time (chosen).** If the push fails, `wtg push` checks `git merge-base --is-ancestor <upstream> HEAD`, where `<upstream>` comes from `Status().Upstream`. When the upstream tip is not an ancestor, the failure message is extended with a reminder instead of just git's raw rejection:

   ```
   ! rejected — branch has diverged from origin/<branch> (typically after a rebase).
     To overwrite the remote: git -C <worktree> push --force-with-lease origin <branch>
   ```

   - It works no matter how the history was rewritten (`wtg rebase`, manual `git rebase`, `commit --amend`, `reset`).
   - It needs no new state and no parsing of git's English error text.
   - Limit: it cannot say *who* diverged the branch. A colleague pushing to the same branch produces the same state, and force-pushing would then discard their work. The reminder therefore says `--force-with-lease` (which refuses if the remote moved since the last fetch) and says "diverged", not "you rebased".
   - `wtg rebase` fetches first, so the remote-tracking ref is fresh at that point.
2. Record `rebased_at` in the space state when `wtg rebase` rewrites a repo, and clear it on a successful push. *Rejected:* exact for wtg's own rebases, but misses rebases done by hand, and adds state that can go stale.
3. Parse the branch reflog for `rebase (finish)` after the last push. *Rejected:* local only, expires, and fragile.

Decision: option 1. It only annotates a failure; it never changes what gets pushed. The push is still attempted first, because a skipped push on a stale tracking ref could hide a push that would have succeeded. A `wtg push --force-with-lease` flag is a separate follow-up; `--force` is never offered.

Alternatives for what `rebase` itself does about pushing:

1. **`rebase` never pushes (chosen).** It may print a hint, but pushing a rewritten shared branch is the user's call.
2. **`rebase --push`, force-pushing with `--force-with-lease`.** *Rejected:* convenient, but makes a destructive remote operation a side effect of a local one.

### D6. Dirty worktrees: `--autostash` from the start (decided)

Worktree branches are often dirty, so skipping them would make the command unhelpful. wtg runs `git rebase --autostash`: git stashes uncommitted changes, rebases, and re-applies them.

What can go wrong, and what wtg adds on top of git's own message (wtg prints git's message as well; the added lines cover only what git does not say):

| Situation | What happens in git | What wtg reports |
|-----------|---------------------|------------------|
| Rebase conflicts | `--abort` restores the original HEAD and re-applies the stash | `! conflict — rebase aborted, your uncommitted changes were restored`, plus the retry command (D2) |
| Rebase succeeds, but re-applying the stash conflicts | The rebase is complete. Conflict markers are in the files, and git keeps the changes in `refs/stash` ("Your changes are safe in the stash") | `⚠ rebased, but your uncommitted changes conflict with the new base`. Added: the branch **is** rebased (so the usual `rebased (was a1b2c3d)` applies), the markers are in the worktree, the changes are still in the stash, and the exit code is non-zero. Next steps: resolve and `git add`, then `git stash drop`; or `git checkout -- . && git stash pop` to retry later. |
| Stash cannot be created (for example unmerged paths from an earlier conflicted merge) | git refuses before rebasing | Treated as a pre-check skip (E2), with the message `⚠ skipped — unresolved conflicts in the worktree` |
| Untracked files collide with files the new base adds | Autostash does not stash untracked files, so git aborts the rebase with "untracked working tree files would be overwritten" | `! blocked by untracked files`. Added: these are not stashed; move or delete them and retry. The worktree is unchanged. |
| Stash left behind | `refs/stash` is shared by the main clone and all of its worktrees | The "stash kept" message names the repo so the user knows where to run `git stash list`. `wtg repo archive` already counts stashes as pending work. |

Autostash can leave conflict markers in uncommitted work, which is the risk that made it opt-in in the first version of this ADR. It is accepted here because the stash is always kept on failure, so nothing is lost; the worst case is a manual `git stash pop`. (Needs verification during implementation: the exit code `git rebase --autostash` returns in the stash-apply-conflict case. The code must not rely on exit status alone, and should check for a kept stash or markers.)

### D7. Concurrency limit

All repos run at once, as `push` does. Workspaces have a handful of repos, and the work is mostly network-bound fetches. There is no cap in v1. Add one only if real workspaces show a problem (for example SSH connection throttling).

## Edge cases

| # | Situation | Handling |
|---|-----------|----------|
| E1 | Repo is a symlink (`always.repos`) | `⚠ skipped — symlink (always.repos)`, as in `push`. It points at the main clone, which is never rebased. |
| E2 | Worktree has unmerged paths, or a merge/cherry-pick/revert is in progress | `⚠ skipped — <reason>`. Autostash cannot stash unmerged paths. Plain uncommitted changes are **not** a reason to skip (D6). |
| E3 | Already up to date (`origin/<default>` is an ancestor of `HEAD`) | `✓ up to date`. Checked with `git merge-base --is-ancestor`, so no rebase is attempted, no stash is made, and no history is touched. |
| E4 | Branch has no commits of its own and is behind | `git rebase` fast-forwards. Reported as `↑ rebased`. |
| E5 | Rebase conflicts | Per D2: abort, uncommitted changes restored, `! conflict`, retry command, exit non-zero. |
| E6 | A rebase is already in progress in the worktree (left over from earlier) | `⚠ skipped — rebase in progress; run git rebase --continue or --abort`. Detected via `git rev-parse --git-path rebase-merge` / `rebase-apply`. Never start a second rebase on top of one. |
| E7 | Worktree is not on the workspace branch (detached HEAD, or switched by hand) | `⚠ skipped — on <branch>, expected <space branch>`. Compare `RepoStatus.Branch` with `Space.Branch`. Rebasing whatever happens to be checked out would be dangerous. |
| E8 | No remote configured | `ø`, omitted from the table as in `repo sync`. There is nothing to fetch or rebase onto. |
| E9 | Fetch fails (offline, auth) | `! fetch failed: …` for that repo only. No rebase is attempted onto a stale ref. `GIT_TERMINAL_PROMPT=0` and SSH `BatchMode` already prevent hangs. |
| E10 | Default branch cannot be determined | `! cannot determine default branch: …` for that repo only (existing `DefaultBranch` fallbacks apply). |
| E11 | `--onto <branch>` does not exist in some repos | Fails for those repos only (D1). |
| E12 | Branch was squash-merged upstream already | A plain rebase replays the commits and usually conflicts or yields empty commits. Not detected in v1; the conflict is reported normally. A possible follow-up is a warning using `BranchMerged`. |
| E13 | Merge commits on the feature branch | The default `git rebase` linearises them. v1 does not pass `--rebase-merges`; a user who needs it uses `wtg exec`. |
| E14 | Pushed branch is now diverged from its upstream | `rebase` does not push. `wtg push` explains and shows the `--force-with-lease` command (D5). |
| E15 | Another process holds a lock on the repo | Git's own lock error is reported for that repo (`!`); others continue. |
| E16 | User git config (`rebase.autoStash`, `rebase.autoSquash`, `rebase.updateRefs`, hooks) | Honoured, since system git is used (ADR 0001). The command is non-interactive: wtg does not pass `-i`. |
| E17 | Workspace has no non-symlink repos | Table shows only the skipped rows; exit 0. |
| E18 | Repos have different default branches | No cross-repo coupling: each repo's base is resolved independently (D1). |
| E19 | Untracked files that would be overwritten by the new base | Blocked, unchanged worktree, explained (D6). |
| E20 | Stash-apply conflict after a successful rebase | `⚠`, branch is rebased, changes kept in the stash, exit non-zero (D6). |

## Implementation notes (for later; nothing is implemented yet)

- Runner additions: `Rebase` gains `--autostash` and returns enough information to tell a conflict, a stash-apply conflict, and an untracked-file block from other failures (sentinel errors such as `ErrRebaseConflict`, `ErrAutostashConflict`); `RebaseAbort`; `IsAncestor`; `RebaseInProgress`. The test runner in `internal/cmd/testrunner_test.go` needs matching stubs.
- `wtg push`: after a failed `Push`, call `Status` and `IsAncestor` on the upstream to decide whether to append the diverged-branch reminder (D5).
- Command code: `internal/cmd/rebase.go` with `RebaseCommand(runner)` and `RunSpaceRebase(...)`, reusing `opResult`, `ui.NewTableWriter`, and the shared progress rendering. Registered next to `PushCommand`.
- Tests ship with the implementation (AGENTS.md): unit tests with the mock runner for E1–E11, plus real-git scenarios using `internal/git/testhelper` for a clean rebase, a clean rebase of a dirty tree, a conflict (aborted, changes restored), and a stash-apply conflict. Also a `push` test for the divergence reminder.
- Docs: README section for `wtg rebase`, `CHANGELOG.md` entry under *Unreleased → Added* (and *Changed* for the `push` reminder), and the index row in [`DESIGN.md`](../DESIGN.md) (already added).

## Consequences

- Updating a whole workspace to the latest default branches becomes one command, instead of a per-repo shell one-liner, and dirty worktrees are handled.
- A failed or conflicting repo never leaves a half-rebased worktree, and no other repo is affected.
- The main clones remain untouched, consistent with ADR 0010.
- A rebased, already-pushed branch needs a force push, which wtg does not yet offer; `wtg push` at least explains the situation and the exact command (D5).
- The default-branch assumption (D1) is a known limitation for repos that branch off something else, until a recorded base exists.
- A stash-apply conflict leaves conflict markers in a worktree (with the changes also kept in the stash). This is the one case where a worktree is not restored automatically.

## Alternatives considered

- **Document the `wtg exec` recipe only.** Rejected: it cannot detect up-to-date repos, abort conflicts, explain autostash failures, or report cleanly.
- **`wtg repo rebase`.** Rejected: that group acts on main clones, and this acts on workspace worktrees.
- **Merge instead of rebase (`--merge`).** Out of scope; the request was a rebase. It could be added as a flag later.
- **Rebase onto the local default branch after `repo sync`.** Rejected in D1.
- **All-or-nothing across repos.** Rejected in D3.
- **Skip dirty worktrees instead of autostash.** Rejected in D6: it is the common case for this user.
- **`rebase --push` / force-pushing from `rebase`.** Rejected in D5.
- **Forward `--progress` to `git fetch`.** Rejected in D4: invisible, since git's stderr is captured.

## Decisions summary

| ID | Decision |
|----|----------|
| D1 | Default branch per repo, plus `--onto`; per-repo recorded base later |
| D2 | Auto-abort on conflict, restore uncommitted changes, print retry command |
| D3 | No cross-repo rollback; report the old tip, undo with `git reset --keep` |
| D4 | `--progress` drives wtg's bar only; not forwarded to git |
| D5 | `rebase` never pushes; `wtg push` appends a diverged-branch reminder with the `--force-with-lease` command on failure |
| D6 | `--autostash` always; wtg adds explanations only where git's message is insufficient |
