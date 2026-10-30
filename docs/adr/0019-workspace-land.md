# ADR 0019: Landing a workspace with `wtg land`

- Status: Accepted
- Date: 2026-10-29
- Author: Steffen Uhlig

## Context

A workspace is one shared feature branch across several repos ([ADR 0007](0007-branch-strategy.md)). wtg covers the start of the life of that work (`new`, `add`) and the middle (`status`, `exec`, `rebase`, `push`). It has nothing for the end: getting the work into the default branch of each repo. Today that is manual and repeated per repo, either `git merge --ff-only` plus `git push` in the main clone, or `gh pr create` in each worktree.

Two lifecycles are tangled together in how the tool is used:

| | Workspace (directory) | Branch (per repo) |
|---|---|---|
| Created by | `wtg new` | `wtg new` (or already exists) |
| Purpose | A place to develop on this machine | The durable unit of work |
| Ends when | The developer is done *on this machine* | It is merged, directly or via a PR |
| Can outlive the other? | Yes, while waiting for a merge | Yes, after the workspace is deleted |

When work goes through a pull request, the branch must stay around for days while the workspace is no longer needed. When work is merged straight into the default branch, the two end together. The tool should treat both as the same end-of-development step and not force the two lifecycles to move in lockstep.

Existing behaviour that already supports this:

- `wtg delete` removes worktrees but leaves branches unless `-d`/`-D` is given ([ADR 0009](0009-delete-and-branch-safety.md)), and warns about unpushed commits before doing so.
- `wtg new` re-attaches a branch that already exists locally (not checked out elsewhere) or only on the remote ([ADR 0007](0007-branch-strategy.md)). A deleted workspace can therefore be recreated cheaply when review feedback arrives.
- `wtg rebase` ([ADR 0018](0018-workspace-rebase.md)) makes a branch fast-forwardable, and `wtg push` publishes it.

What is missing is the step in between: publish the work in the way the repo expects, which differs per repo (personal repos merge directly; shared repos need a pull request).

## Problem

Provide one command that, for every repo in the workspace that has work to land, either fast-forwards the default branch to the workspace branch or opens a pull request for it. After it succeeds, deleting the workspace must be safe and mean "I am done developing on this machine". Which route to take must be recordable per repo without adding files to the repo or requiring collaborators to use wtg, and the safe route must be the default.

## Intent

```sh
wtg land [<workspace>] [--strategy pr|merge] [--draft] [--no-link] [--dry-run]
```

- The workspace is inferred from the current directory, as in `push` and `rebase`.
- When the run produces two or more pull requests, each one is linked to its siblings (D7). `--no-link` turns that off.
- Per-repo work runs in parallel and is reported per repo in a table, like `wtg push`. The exit code is non-zero if any repo failed or was left needing attention (D11.1), so that `wtg land && wtg delete` stops.
- `land` never deletes anything. The usual sequence is `wtg rebase` (if needed), `wtg land`, then `wtg delete`.

## Decisions

### D1. Landing is separate from deleting (decided)

`land` publishes. `delete` releases the local workspace. Neither implies the other. The intended daily sequence is `wtg land && wtg delete`, run from inside the workspace (both infer it from the current directory; checked with the current binary).

- After `land`, nothing local has been removed, so a mistake costs nothing.
- `delete` is the "I am done here" signal. That only holds if `delete` recognises a landed workspace as safe, and if a chain stops when `land` did not finish or `delete` did not delete. The current `delete` does neither reliably; D11 lists what was measured and what has to change.
- The branch survives `delete` (no `-d`) for as long as the PR is open. When review feedback arrives, `wtg new <workspace> <repos…>` re-attaches it.
- Rejected: `land --delete`. A convenience that can be added later, but v1 keeps the two verbs composable, so a failed or partial land never deletes a workspace. That depends on the exit codes in D11.1.
- After `delete` the shell is still in the removed directory (`delete` already warns about this); the next command needs a `cd`.

### D2. One verb, two strategies (decided)

`wtg land` has two strategies, because they are two answers to the same question ("get this branch into the default branch"):

| Strategy | What it does | Provider |
|---|---|---|
| `merge` | Fast-forwards the remote default branch to the workspace branch (D5) | Any (git only) |
| `pr` | Pushes the branch and opens a pull request against the default branch (D6) | GitHub, via `gh` |

Rejected alternatives for the surface:

- **`wtg push --pr`.** Fits the PR route, but `merge` is not a push option, and the two strategies belong together.
- **Two commands (`wtg merge`, `wtg propose`).** Splits one decision across two verbs, and a workspace mixing both kinds of repos would need two invocations.
- **Names considered:** `ship` (git-town uses it for the merge route), `publish`, `finish` (git-flow's word, which also deletes the branch, the very coupling D1 avoids), `submit`. `land` was chosen because it says "arrive in the default branch" without suggesting cleanup, and works for both strategies.

### D3. Strategy comes from git config; the default is `pr` (decided)

Whether a repo may be merged directly is a property of the repo (and of who is working on it), not of the moment. It is recorded in **git's own configuration**, which is local to the user by construction, so there is no wtg-specific configuration, and nothing that must be checked in:

```sh
git config --local wtg.landStrategy merge       # this clone only (.git/config)
```

```gitconfig
# ~/.gitconfig: one rule for every repo whose remote matches
[includeIf "hasconfig:remote.*.url:git@github.com:suhlig/**"]
	path = ~/.config/git/wtg-personal.inc
[includeIf "hasconfig:remote.*.url:https://github.com/suhlig/**"]
	path = ~/.config/git/wtg-personal.inc

# ~/.config/git/wtg-personal.inc
[wtg]
	landStrategy = merge
```

For each repo wtg takes the first of:

1. `--strategy pr|merge` on the command line, for all repos in one run.
2. `git -C <main clone> config --get wtg.landStrategy`. Git resolves its own layers (repository, global, includes), so a per-clone setting beats a global rule.
3. `pr`.

Properties, checked against git 2.56 before writing this ADR:

- **Local by construction.** `.git/config` is not transferred by `git clone` (a clone returned no value), and `~/.gitconfig` and its includes never leave the machine. A repo cannot opt itself into direct pushes to its default branch, which a tracked settings file could. Nothing in the repo changes, so a collaborator who does not use wtg sees nothing.
- **Linked worktrees see it.** The value lives in the main clone's config, which every worktree of that clone shares, so reading it in the main clone or any worktree gives the same answer.
- **One rule can cover many repos.** `includeIf "hasconfig:remote.*.url:…"` matches on the remote URL, not on where the clone lives, so it also works for clones that are moved or re-created. A repo whose remote does not match gets no value and therefore `pr`. (`hasconfig:remote.*.url` needs git 2.36 or later; the version was not looked up, only the installed 2.56 was tested. An older git ignores the `includeIf` and falls back to `pr`, the safe side.) Ssh and https remotes are different URLs, so each form in use needs its own line.
- **It says where the choice came from.** `git config --show-origin --get wtg.landStrategy` names the file. `--dry-run` (D10) prints it, for example `merge (from ~/.config/git/wtg-personal.inc)`, and a surprising result can be traced without wtg.
- **Fails safe.** An unset key exits 1 and is treated as "not set". An unknown value is an error before any git or `gh` call (E14).
- **Anyone can use it without wtg.** The setting is an ordinary git key. Reading it, or ignoring it, needs nothing from wtg ([ADR 0022](0022-no-tool-dependency.md)).

Other rules:

- With no setting every repo uses `pr`. wtg never writes to a default branch unless git config says so for that repo or the user passes `--strategy merge`.
- No interactive prompt. The safety is the configuration plus `--dry-run` (D10), which keeps the command scriptable (see `AGENTS.md`).
- Rejected: a `[land]` section with glob rules in wtg's own `config.toml`. It would add a store only wtg reads, plus a second pattern-matching feature next to the one git already has.
- Rejected: a tracked file in the repo (for example `.wtg.toml`). It would put wtg-specific files into every collaborator's repo and let a repo grant itself direct-push rights.
- Rejected: deriving the strategy from the remote or org without any recorded choice. It encodes one person's policy in the tool and guesses wrong silently. (The `includeIf` rule above is a recorded choice that happens to be keyed on the remote.)

### D4. Which repos are landed (decided)

After fetching `origin`, a repo is landed only if its workspace branch has commits that `origin/<default>` does not (`git rev-list --count origin/<default>..<branch> > 0`). The default branch is resolved per repo with `Runner.DefaultBranch`, as in `rebase` (ADR 0018 D1).

- A repo with no commits of its own reports `ø nothing to land`. That is the "repos that have commits since main" selection.
- Uncommitted changes are not part of the branch. They produce a warning (`⚠ uncommitted changes are not landed`), not a failure, because landing commits is valid while other edits are in flight. `wtg delete` still prompts for them later.
- Symlink repos (`always.repos`) are skipped, as in `push` and `rebase`.

### D5. `merge` strategy: push the branch tip to the default branch (decided)

wtg runs `git push origin <branch>:<default>` from the worktree. The remote accepts it only as a fast-forward, so no merge commit is ever created and no history is rewritten.

- This does not touch the main clone's working tree. A plain `git merge --ff-only` there would need the default branch checked out and clean, and would fail on a dirty main clone for reasons unrelated to the landing. ADR 0010 promises the main clones are not modified except by `repo sync`; this keeps that promise.
- If the remote rejects it as non-fast-forward, the row says `! not a fast-forward — run wtg rebase, then retry`. Branch protection rules that reject the push are reported with git's message plus a hint to use the `pr` strategy for this repo.
- After a successful push, wtg makes a best-effort fast-forward of the main clone's local default branch (the `FastForward` used by `repo sync`). Without that, `wtg delete -d` would report the branch as "not fully merged", because `BranchMerged` compares against the main clone's `HEAD`. If the main clone is dirty or on another branch, the update is skipped with a `⚠` and `repo sync` does it later.
- Rejected: `git merge --ff-only` in the main clone (above). Rejected: merge commits or squash locally; they rewrite what the user reviewed.

### D6. `pr` strategy: push, then find-or-create the PR (decided)

For each repo to land:

1. Push the branch with `--set-upstream` (the existing `Runner.Push`). A diverged branch gets the same `--force-with-lease` reminder as `wtg push` (ADR 0018 D5). `land` never forces.
2. Look for an existing open PR with this head branch (`gh pr list --head <branch> --json number,url`). If found, report its URL and create nothing (its sibling block is still refreshed, D7). This makes `land` idempotent and safe to re-run.
3. Otherwise create it: `gh pr create --base <default> --head <branch> --fill`, plus `--draft` if requested. `--fill` takes the title and body from the commits, so wtg invents no text.

Details:

- wtg shells out to `gh`, as [ADR 0013](0013-repo-archive.md) does for archiving. That reuses the user's authentication, keeps wtg free of HTTP clients, and mirrors the "use system git" choice.
- `GH_HOST` is pinned to the remote's host for every call (as in `internal/remote/gh.go`). `remote.ParseGitHubRemote` currently accepts only `github.com`. For `land`, a GitHub Enterprise host is accepted when `gh auth status --hostname <host>` succeeds. Non-GitHub remotes fail with `! pr strategy needs a GitHub remote` after the push has succeeded, so the work is still published.
- If the push works and PR creation fails, the row says `⚠ pushed, PR not created: …`. A retry skips the push (nothing to push) and creates the PR.
- Once every `pr` repo has a PR (created now or found), the PRs are linked to each other in a second pass (D7).

### D7. wtg opens PRs, links them to each other, and then stops (decided)

PR handling is in scope up to "the PRs exist and know about each other". wtg does not review, track, merge, enable auto-merge, or poll CI. `gh` and the web UI do those well, and each of them would pull in a forge's API and its concepts.

Cross-linking is the one thing wtg adds on top, because it is the part no forge can do: only wtg knows that these PRs, in different repos, are one feature. A reviewer of `api#12` should see at once that `payments#7` and `frontend#31` belong to it, and in which order they might need to land.

**When.** After the per-repo phase has finished, once every `pr` repo has a PR URL, whether it was created now or found (D6 step 2). Linking needs a barrier, because each body must name PRs that do not exist until the others are created. The fan-out stays parallel; only this pass waits. It runs only when there are at least two PRs, and it is skipped with `--no-link`.

**What.** A block at the end of each PR body, listing every *other* PR of the run:

```markdown
<!-- wtg:siblings:start -->
**Part of workspace `my-feature`.** These pull requests belong together:

- suhlig/payments#7
- suhlig/frontend#31
<!-- wtg:siblings:end -->
```

- The reference form `owner/repo#N` is used, not a bare URL. GitHub renders it as a link with the PR's title and status, and also shows a cross-reference on the sibling PR. It works for GitHub Enterprise within the same instance.
- The workspace name tells a reader where the grouping comes from. It is also the default branch name, so it is stable.

**How (idempotent, never destructive).**

1. Read the current body (`gh pr view --json body`).
2. If it contains the `wtg:siblings` markers, replace what is between them. Otherwise append the block, preceded by a blank line.
3. Write it back (`gh pr edit --body`) only if the result differs.

Everything outside the markers is left byte-for-byte as the author wrote it, so edits to the description are preserved and a re-run converges on the same text. The markers are HTML comments, invisible when rendered.

**Failure.** Linking is best-effort. If it fails for a PR (permissions, network), the row for that repo says `⚠ PR created, siblings not linked: …` and the exit code is non-zero, but nothing is rolled back. A re-run finds the existing PRs (E8) and tries again.

**Limits, stated up front.**

- Read-modify-write on a body has a small race with someone editing the same description at that moment. The window is a few hundred milliseconds, and the markers confine the change to wtg's own block.
- PRs across different orgs link fine, but a reader without access to a private sibling sees plain text, not a title.
- Repos landed with `merge` have no PR and are not listed. If every other repo uses `merge`, there is nothing to link.
- If a sibling PR is later closed or merged, the block is not updated. It is a snapshot from the last `land`, and GitHub shows each linked PR's current state anyway.

### D8. No cross-repo transaction (decided)

As with `rebase` and `push` ([ADR 0018](0018-workspace-rebase.md) D3), repos are independent: each lands or fails alone, and failures do not undo successes. An atomic multi-repo landing is not possible with plain git remotes.

Consequence to document: when repos depend on each other (for example `api` before `frontend`), the `merge` strategy gives no ordering guarantee between repos. Such workspaces should use `pr`, where the merge order is decided by the reviewers.

### D9. Branch cleanup is not part of `land` (decided)

`land` leaves branches alone, local and remote. Known limit that this ADR records but does not solve: after a PR is merged by GitHub with *squash* or *rebase*, the feature branch is not an ancestor of the default branch, so `wtg delete -d` refuses it (`BranchMerged` is an ancestry check). The user then needs `-D`. A reliable fix needs the PR state (follow-up).

### D10. `--dry-run` prints the plan (decided)

`land` mutates remotes, and the `merge` strategy writes to a default branch. `--dry-run` runs the checks against the last fetched state and prints for every repo what it would do (`would push to origin/main`, `would open PR`, `PR #12 exists`), without pushing, fetching, or calling `gh pr create`. It is the global flag of [ADR 0021](0021-dry-run.md), which decides how a preview is produced (mutating calls skipped at the git and `gh` boundaries, no fetch, no prompts); `land` supports it from the start.

### D11. `land && delete` must be safe end to end (changes `delete`; decided)

D1 promises that `wtg land && wtg delete` covers the whole end of the workflow. This was checked against the current binary (git 2.56, throwaway repositories with a bare `origin`, a main clone, and one workspace). It does not hold yet:

| # | Case | What `delete` does today | Verdict |
|---|------|--------------------------|---------|
| 1 | Branch never pushed; plain `delete` | No warning. Worktree removed, branch kept | Data safe, because the branch survives |
| 2 | Branch never pushed; `delete -D` | No warning. Branch deleted; the commit is only in the reflog | **Data-loss gap** (exists today, independent of `land`) |
| 3 | PR flow: `push -u`, then `delete` or `delete -d` | No warning. With `-d` the local branch goes, the remote branch stays, and `wtg new` re-attaches it with its commit | Works |
| 4 | Merge-landing, branch never pushed | No warning, branch kept. `-d` is refused while the main clone's local default branch is stale (D5 fast-forwards it) | Works with D5 |
| 5 | Branch pushed earlier (`-u`), more commits, then merge-landed | Warns "1 unpushed commit(s)" although the commit is on `origin/main`. `-d` is refused: "not yet merged to `origin/feat`, even though it is merged to HEAD" | **False alarm** that blocks the chain and `-d` |
| 6 | Same, but the branch was pushed after its last commit | Clean; `-d` deletes | Works |
| 7 | Prompt declined, or stdin is not a terminal | Prompt printed, nothing deleted, **exit 0** | **A chain continues as if the workspace were gone** |
| 8 | Worktree left on another branch with unpushed commits (`land` skips it, E3) | No warning. The branch survives in the main clone | Silent; `land` has to report it |
| 9 | `always.secrets` copies in the worktree (for example `.env.local`), edited since they were copied | Removed with the worktree, no warning, because `git status` does not list ignored files | **Data-loss gap** (exists today); fixed by change 5 |
| 10 | Stash in the worktree | Survives: `refs/stash` is shared with the main clone | Works |
| 11 | Untracked file | Warns and prompts | Works |

The cause of cases 2, 5 and 8 is one check: `delete` counts commits ahead of the branch's *upstream* (`RepoStatus.Ahead`). With no upstream the count is zero, and after a merge-landing the upstream is stale. The right question is whether a commit exists *nowhere else*. `git rev-list --count <ref> --not --remotes` answers it and was measured on the same repositories: 1 for a never-pushed branch, 1 for "pushed earlier plus a newer commit", and 0 once `origin/main` or `origin/<branch>` contains the tip, without a fetch (a push updates the remote-tracking ref itself).

**Required changes** (all in this ADR's scope, because without them D1 is not true):

1. **Exit codes of `land`.** Non-zero if any repo failed, was skipped with `⚠` (wrong branch E3, not a fast-forward E5, `gh` missing E9, PR not created, siblings not linked E20), as `rebase` does for "failed or needing attention" (ADR 0018). `ø nothing to land` is not a failure. This is what makes `&&` stop (case 8).
2. **`delete` asks "does any commit exist only here?"** It counts commits on no remote ref for the workspace branch and for whatever is checked out in each worktree, replacing the Ahead-versus-upstream count. This fixes cases 2, 5 and 8. The warning text stays ("has N unpushed commit(s)").
3. **`delete` exits non-zero when it does not delete.** A declined prompt or end-of-input exits non-zero with `aborted: nothing deleted` (case 7), so `land && delete && …` cannot proceed on the assumption that the workspace is gone.
4. **`delete --yes`** skips the confirmation prompt (Q2). Without `--yes` and without a terminal, `delete` does not read an answer from a pipe: it refuses with a hint to pass `--yes`, and exits non-zero (change 3). `--yes` answers only the prompt; it does not override change 5.
5. **`delete` keeps a repo whose `always.secrets` copy was changed** (Q3), as described next.

**Decided**

- **Q1. `-d` means "no commit exists only on this branch".** `delete -d` deletes the branch when the check of change 2 finds no commit that is on no remote ref, using `git branch -D` internally, because `git branch -d` judges against the upstream and gives the false alarm of case 5. A never-pushed branch is still refused, and `-D` still forces. This amends [ADR 0009](0009-delete-and-branch-safety.md); the amendment note is added there when the change is implemented. It also resolves the squash-merge limit of D9 for as long as `origin/<branch>` exists.
- **Q2. `--yes` is added** (change 4).
- **Q3. Changed `always.secrets` copies keep their repo.** Only files from the `always.secrets` list can be compared, because they are the only ignored files wtg created and therefore the only ones with a known origin. Other ignored files the user made are out of scope; that is stated in the README, not detected.

**Secrets comparison (change 5).**

- For each non-symlink repo, for each `always.secrets` entry that exists in the worktree, `delete` compares the worktree file byte for byte with the same relative path in the main clone (the source `secretCopySteps` copied it from).
- The copy counts as **changed**, and the repo is kept, when the contents differ or when the source no longer exists (the worktree holds the only copy). A copy missing from the worktree is not a change: nothing would be lost.
- The comparison is read-only and runs in the pre-flight phase, in parallel with the status checks (ADR 0023 D3).
- A changed repo is **not removed and its branch is not deleted**, even with `-D` and `--yes`. All other repos are processed as usual: this is the "do not stop the other repos" rule.
- The repo is listed with `✗ kept: <path> differs from the main clone` (one line per file), and `delete` exits non-zero at the end. The space stays in wtg's state with **only the kept repos** listed, so running `delete` again after the user has copied the change back, or removed the file, resumes with exactly those repos.
- The user resolves it with ordinary tools: `diff` the two files, copy the worktree file over the source (or discard it), run `wtg delete` again. Nothing here needs wtg ([ADR 0022](0022-no-tool-dependency.md)): the files are left where they are.
- Rationale for failing instead of warning: the data is not recoverable from git (ignored, so not in the reflog), a prompt cannot be answered in a chain, and the user explicitly asked for a hard stop on exactly this case.

## Edge cases

| # | Situation | Handling |
|---|-----------|----------|
| E1 | Repo is a symlink (`always.repos`) | `⚠ skipped — symlink (always.repos)` |
| E2 | Branch has no commits beyond `origin/<default>` | `ø nothing to land` (D4) |
| E3 | Worktree is not on the workspace branch (detached or switched by hand) | `⚠ skipped — on <branch>, expected <space branch>`, as in `rebase` E7 |
| E4 | Worktree has uncommitted changes | Landing proceeds, with `⚠ uncommitted changes are not landed` |
| E5 | `merge`: default branch moved on, not a fast-forward | `! not a fast-forward — run wtg rebase` (D5) |
| E6 | `merge`: remote rejects the push (branch protection) | `! rejected: <git message>`; hint to use `pr` for this repo |
| E7 | `merge`: main clone dirty or not on the default branch | Landing succeeds; the local default branch is not updated; `⚠` with a hint to run `wtg repo sync` |
| E8 | `pr`: PR for this head branch already exists | Report URL; no new PR; its sibling block is still refreshed (D6, D7) |
| E9 | `pr`: `gh` missing or not authenticated for the host | Pre-flight failure for every `pr` repo, before any push, with the `gh auth login --hostname <host>` hint |
| E10 | `pr`: remote is not GitHub | `! pr strategy needs a GitHub remote`; branch is already pushed |
| E11 | Branch already pushed and rebased since | Same diverged-branch reminder as `wtg push`; `land` never forces |
| E12 | Fetch fails or no remote | `! fetch failed: …` / `ø`, as in `rebase` E8, E9 |
| E13 | Branch was already merged (squash) upstream | Not detected in v1; it shows as commits to land. A PR would be empty or conflicting. Follow-up. |
| E14 | `wtg.landStrategy` has a value other than `pr` or `merge` | Error naming the file it came from (`--show-origin`), before any git or `gh` call |
| E15 | Workspace mixes `merge` and `pr` repos | Each repo uses its own strategy; the table shows which |
| E16 | Re-running `land` after a partial failure | Idempotent: landed repos report `up to date` or the existing PR; failed repos are retried |
| E17 | Workspace has no non-symlink repos | Table shows only skipped rows; exit 0 |
| E18 | Only one PR results from the run | No link block is written (D7) |
| E19 | PR body already has a `wtg:siblings` block | The block is replaced in place; the rest of the body is untouched (D7) |
| E20 | Linking one PR fails | `⚠ PR created, siblings not linked: …`; other PRs are still linked; exit non-zero; no rollback (D7) |
| E21 | `--no-link`, or `--dry-run` | `--no-link`: no body is read or written. `--dry-run`: bodies are not written, and the plan says `would link N sibling PRs` (ADR 0021 D3 supplies placeholder PR numbers) |
| E22 | A `pr` repo failed before getting a PR | The block lists only the PRs that exist. A re-run links the rest, and replaces the block (E19) |

## Implementation notes (not yet implemented)

- `git.Runner` additions: `CommitsAhead(repoPath, base, branch string) (int, error)` and `PushTo(repoPath, branch, remoteBranch string) error` (refspec push, no force). `Push` and `FastForward` already exist. The test runner in `internal/cmd/testrunner_test.go` needs matching stubs.
- `remote.Runner` additions: `FindPR(ctx, host, ownerRepo, head string) (pr PR, found bool, err error)`, `CreatePR(ctx, host, ownerRepo, head, base string, draft bool) (PR, error)`, `PRBody(ctx, host, ownerRepo string, number int) (string, error)` and `SetPRBody(ctx, host, ownerRepo string, number int, body string) error`. `PR` carries `Number` and `URL`. Host acceptance (D6) is a small change next to `ParseGitHubRemote`.
- Sibling block (D7): a pure function `withSiblingBlock(body, workspace string, siblings []PRRef) string` that inserts or replaces the marked block. It is the part with the most edge cases (no markers, markers present, empty body, CRLF line endings, a stray start marker without an end), so it is unit-tested on its own with a table of inputs, including idempotence (applying it twice equals applying it once).
- Strategy lookup: a `Runner` method, for example `ConfigGet(repoPath, key string) (value, origin string, found bool, err error)`, wrapping `git config --show-origin --get`. No new section in wtg's `config.toml` and nothing for `wtg config init` to scaffold. The README documents the two git config snippets of D3.
- Command code: `internal/cmd/land.go` with `LandCommand(runner, ghRunner)` and `RunSpaceLand(...)`, reusing `opResult`, `ui.NewTableWriter`, `resolveSpaceArg`, and the `push` diverged-branch hint. Registered next to `PushCommand`.
- Tests ship with the implementation (`AGENTS.md`): mock-runner tests for E1–E22 and strategy selection (command line beats repo config beats default; unset; unknown value; origin reported), with real-git cases for a per-clone value and an `includeIf "hasconfig:remote.*.url"` rule, plus real-git scenarios via `internal/git/testhelper` for a fast-forward landing, a rejected non-fast-forward, and a landing followed by `delete -d`. `gh` is faked through the `remote.Runner` interface, including a fake that records the bodies written, to assert that the second pass runs only after all PRs exist.
- `delete` changes (D11): a `Runner` method such as `CommitsOnNoRemote(repoPath, ref string) (int, error)` (`git rev-list --count <ref> --not --remotes`) used by `RunSpaceDelete` in place of `RepoStatus.Ahead`; a non-zero exit when the prompt is declined; `delete --yes` and the refusal without a terminal; a pre-flight secrets comparison (reusing the path validation and root check of `secretCopySteps`) that moves a repo from the removal loop to a `kept` result and rewrites the space state to the kept repos; `delete -d` using the same count for the branch decision; `land` returns an error when any row is `!` or `⚠`. ADR 0009 gets an amendment note when this is implemented.
- Tests for D11 use real repositories (`internal/git/testhelper`, a bare origin) and run `land` followed by `delete` for each case in the table: PR flow, merge-landing with and without an earlier upstream, never-pushed (with `-D`), a worktree on another branch, a declined prompt (exit code), no terminal without `--yes`, a stale local default branch, and secrets: unchanged copy (deleted), edited copy (kept, exit non-zero, other repos deleted, state lists only the kept repo, second run succeeds after the copy was reconciled), source missing, copy missing, and a symlinked repo.
- Docs: README section for `wtg land` and the `wtg.landStrategy` git config key, `CHANGELOG.md` entry under *Unreleased → Added*, and the index row in [`DESIGN.md`](../DESIGN.md) (added with this ADR).

## Without wtg ([ADR 0022](0022-no-tool-dependency.md))

- **What others see:** ordinary pushes of an ordinary branch, an ordinary fast-forward of the default branch, or an ordinary PR whose title and body come from the commits (`--fill`). The only wtg-specific text is the sibling block in PR descriptions (D7), which is plain Markdown, can be edited or deleted by anyone, and is switched off with `--no-link`. ADR 0022 D5 records it as the one deliberate exception.
- **What stays local:** the strategy choice, in git config (D3), not in any tracked file.
- **Reproducible by hand:** every step is a git or `gh` command: `git push origin <branch>:<default>`, `git push --set-upstream`, `gh pr create --fill`, `gh pr edit`.

## Consequences

- The end of the workflow becomes one command, and the answer to "may I merge this directly?" lives in the user's git configuration instead of in the developer's head, and no collaborator has to use wtg or see any trace of it.
- A workspace can be deleted as soon as its branches are pushed or PRs are open. The branch lives on until it is merged, and `wtg new` brings it back.
- wtg's first dependence on a forge for something other than archiving. It is limited to `gh pr list`, `gh pr create`, and reading and editing PR bodies, and the `merge` strategy stays provider-agnostic.
- Reviewers of any one PR see the rest of the feature from there, which is the main value of landing a workspace instead of N unrelated repos.
- wtg now writes into text that humans also edit. The markers, the replace-only-between-markers rule, and the "write only if different" check (D7) keep that contained.
- No atomicity across repos (D8). Dependent repos must use PRs.
- The squash-merge branch-cleanup gap (D9) becomes more visible, because PR-based landing is now the default route.

## Alternatives considered

- **Document a `wtg exec` recipe only.** Rejected: it cannot select repos with commits, find an existing PR, or report per repo.
- **Make PR handling a separate tool (`gh` only).** Rejected: opening N PRs for one feature and keeping them consistent is exactly the multi-repo chore wtg exists for. The boundary in D7 keeps this from growing into a forge client.
- **`git merge --ff-only` in the main clone.** Rejected in D5.
- **`land --delete` / auto-delete after landing.** Rejected in D1.
- **Prompt before merging into a default branch.** Rejected in D3: it breaks scripting, and git config plus `--dry-run` give the same protection.
- **Infer the strategy from the remote or org, with no recorded choice.** Rejected in D3.
- **A `[land]` section in wtg's own config, or a tracked per-repo file.** Rejected in D3: git config is local by construction and already has per-repo and per-remote layering.
- **Record PR URLs in the space state.** Rejected: state is deleted with the workspace, and the PR can be found again through `gh` (D6 step 2), so it would only add data that can go stale.

## Follow-ups

- Use PR state (`gh pr view --json state`) so `wtg delete -d` and a possible `wtg repo prune` can clean up branches of squash- or rebase-merged PRs (D9, E13).
- Optional `land --delete`, and optional deletion of the remote branch after a merge-landing.
- Per-repo recorded base branch (ADR 0018 D1) would let `land` target a non-default base.

## Decisions summary

| ID | Decision |
|----|----------|
| D1 | `land` publishes; `delete` releases the workspace. Neither implies the other |
| D2 | One verb `wtg land` with strategies `merge` and `pr` |
| D3 | Strategy from git config `wtg.landStrategy` (per clone or via `includeIf hasconfig:remote.*.url`); default `pr`; `--strategy` overrides; no wtg config section, no tracked files, no prompts |
| D4 | Land only repos whose branch has commits beyond `origin/<default>`; uncommitted changes warn |
| D5 | `merge` = `git push origin <branch>:<default>` (fast-forward only), then best-effort fast-forward of the main clone |
| D6 | `pr` = push, find existing PR, else `gh pr create --fill`; idempotent; `GH_HOST` pinned |
| D7 | wtg opens PRs and links siblings to each other via a marked, idempotent block in each body (second pass, `--no-link` to skip); no review, merge, or CI tracking |
| D8 | No cross-repo transaction; dependent repos should use `pr` |
| D9 | `land` does not clean up branches; squash-merge cleanup is a known gap |
| D10 | `--dry-run` prints the plan from the last fetched state, without changing or fetching anything (ADR 0021) |
| D11 | `land && delete` made safe: `land` exits non-zero on any failure or warning; `delete` counts commits on no remote ref, `-d` means "no commit only here" (amends ADR 0009), `--yes` skips the prompt, it exits non-zero when it does not delete, and it keeps (and fails for) any repo whose `always.secrets` copy differs from its source |
