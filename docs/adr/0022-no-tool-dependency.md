# ADR 0022: wtg is never a dependency of collaborators

- Status: Proposed
- Date: 2026-10-29
- Author: Steffen Uhlig

## Context

wtg is a personal tool for working across repos. The people who share those repos do not have to use it, and some will not. A teammate who clones a repo, opens a pull request that a wtg user created, or checks out a branch a wtg user pushed must be able to do all of that with plain git and the forge's own tools, and must never be able to tell, from anything shared, that wtg was involved.

Recent proposals have each touched this line:

- [ADR 0019](0019-workspace-land.md) writes a block into pull request descriptions (D7) and needs a per-repo choice between merging and opening a PR (D3). An early draft of D3 put that choice in a file that would be checked in.
- [ADR 0020](0020-command-summary.md) and [ADR 0021](0021-dry-run.md) add `--summary` and `--dry-run`, which expose what wtg does.
- `always.secrets` ([ADR 0016](0016-always-secrets-are-repo-relative-copies.md)) copies files into worktrees.

No earlier ADR states the rule, and it is cheaper to write down once than to rediscover in each review.

## Measured footprint today

Checked against the current binary, using a throwaway repository with the data and config directories redirected:

| What | Result |
|---|---|
| `wtg new feat api`, then compare the main clone before and after | Added: branch `feat` and a worktree entry. Nothing else. `.git/config`, `.git/hooks`, `.git/info/exclude`, and tracked files were all unchanged. |
| Everything wtg keeps for itself | `~/.local/share/wtg/spaces/feat.yaml` (and `archived.yaml` for archived repos). Nothing in the repository. |
| Files created in the workspace directory | `go.work` and the worktrees. All outside the repositories. |
| `wtg delete feat -d`, then compare the main clone with the state before `new` | Identical. The workspace directory and the state file are gone. |
| Delete a workspace directory by hand, without wtg | `git worktree list` shows it as `prunable`; `git worktree prune` removes the entry. The branch stays. |

The invariant therefore already holds. This ADR keeps it holding.

## Problem

State a rule that every wtg feature can be checked against, say how it is verified, and list the places where the rule is bent on purpose.

## Decisions

### D1. The rule (decided)

1. **Shared artefacts are plain.** Anything that leaves the machine or lives in a repository (commits, branches, tags, tracked files, pull request text, remote refs) uses only formats and names that a person without wtg can read and use. wtg adds no file, ref, note, or format that has to be parsed or kept.
2. **wtg-only data is user-local.** Data that exists only for wtg lives in places that do not travel with a clone: the XDG data and config directories, the repository's `.git/config`, and the user's global git configuration. Never in tracked files, pushed refs, or git notes.
3. **Everything wtg does is expressible as git or `gh` commands.** There is no operation that only wtg can perform on a repo. [ADR 0020](0020-command-summary.md)'s `--summary` prints that expression, which also serves as the proof.
4. **Removing wtg leaves ordinary repositories.** Branches are ordinary branches, worktrees are ordinary linked worktrees, and losing wtg's state files costs convenience only (D3), never data.

### D2. Where each wtg feature stands (decided)

| Feature | Shared artefact | wtg-only data | Verdict |
|---|---|---|---|
| `new`, `add`, `remove`, `delete` | Branch names (`git.branch_prefix` + workspace name), pushed like any branch | Space state in XDG | Complies |
| `go.work`, `always.files`, `always.repos` | None. They live in the workspace directory, outside the repos | None | Complies |
| `always.secrets` | None (copies stay on the machine) | None | Complies. Open point in D4 |
| `push` | Ordinary pushes of ordinary branches | None | Complies |
| `rebase` | Rewritten branch, as `git rebase` would | None | Complies |
| `land` (`merge`) | A fast-forward push of a branch to the default branch | `wtg.landStrategy` in git config (D1.2) | Complies |
| `land` (`pr`) | An ordinary PR, title and body from commits | `wtg.landStrategy` in git config | Complies |
| `land` sibling links | A block in each PR description | None | **Deliberate exception**, D5 |
| `--summary`, `--dry-run` | None | None | Complies |
| `repo archive` | None. A directory move; `--remote` runs `gh repo archive` | `archived.yaml` in XDG | Complies |

### D3. State loss is recoverable from git (decided)

The state file is an index that makes wtg fast and its commands short. It is not the source of truth: git knows every worktree (`git worktree list`), every branch, and every path. If the state is lost, nothing is stranded. The user can carry on with plain git, and a future `wtg` command could rebuild the index from `git worktree list` (not proposed here). A feature that would make the state file the only record of something that matters (for example, the only place a PR URL or a base branch is kept) is declined or has to store that information in git as well. This is why [ADR 0019](0019-workspace-land.md) does not record PR URLs in the state.

### D4. Decisions for wtg-only settings: git config, not a new store (decided)

When wtg needs a per-repo setting that collaborators should not see, it uses a key under the `wtg.` section of git's own configuration:

- `git config --local wtg.<key> <value>` for one clone (`.git/config`, not transferred by `git clone`).
- `[includeIf "hasconfig:remote.*.url:…"]` in the user's global configuration for a group of repos.

This was tested before writing this ADR (see [ADR 0019](0019-workspace-land.md) D3 for the details). It needs no wtg-specific file and no checked-in file, and a repo cannot grant itself behaviour through it. Settings that apply to the user as a whole (`discovery.root_dir`, `always.*`) stay in wtg's own `config.toml`, which is user-local already.

**Open point, not decided here:** copies made by `always.secrets` are untracked files in a worktree. wtg writes no ignore file (verified: the code never touches `.gitignore` or `.git/info/exclude`), so if a repo does not already ignore them they show as untracked and could be committed by accident, by the user, not by a collaborator. Adding the paths to the clone's `.git/info/exclude` would satisfy the rule (local, not shared) and remove the risk. It changes a file under `.git`, so it should be a deliberate, separate decision.

### D5. The one deliberate exception: the sibling-PR block (decided)

[ADR 0019](0019-workspace-land.md) D7 writes a block into each PR description to link the PRs of one workspace. It is the only place where wtg writes into something collaborators read. It is kept because it is the main thing wtg can add that a forge cannot, and because it passes the substance of D1:

- It is plain Markdown that renders as a heading and a list of PR references. A reader needs nothing to understand it, and nothing breaks if they edit or delete it.
- Its two HTML-comment markers are invisible when rendered. They exist only so wtg can update its own block, and the PR is complete without them.
- It can be turned off per run with `--no-link`.
- It never changes the title, the commits, or anything outside the markers.

If this later turns out to be unwelcome in a team, the remedy is to make linking opt-in (`--link`). That would be a change to ADR 0019, not a reversal of this one.

### D6. Checking the rule (decided)

- **Every ADR that proposes a feature carries a short "Without wtg" note** saying which of D1's four points it touches and what a non-user sees. This ADR adds the check, and [ADR 0019](0019-workspace-land.md) already follows it.
- **A no-footprint test.** For each command that creates or removes workspaces, a test with a real repository (`internal/git/testhelper`) snapshots the main clone before and after: refs, worktree list, `.git/config`, hooks, `info/` and tracked files. It asserts that the only differences are the expected branch and worktree entries, and that `new` followed by `delete -d` restores the starting state exactly. The manual check in the table above becomes this test.
- **No wtg-specific text in shared output** is asserted for `land`: the commit messages pushed, and the PR body outside the sibling block, are exactly what git and `gh --fill` would produce.

## Edge cases

| # | Situation | Handling |
|---|-----------|----------|
| E1 | A collaborator checks out a wtg user's branch | Nothing to do. It is a normal branch |
| E2 | A collaborator edits the PR description | The sibling block may be edited or removed (D5); the next `land` by the wtg user restores or replaces it |
| E3 | The wtg user's state file is deleted | Git still lists every worktree and branch (D3). `wtg` commands no longer find the workspace by name until it is rebuilt |
| E4 | A repo is cloned fresh on another machine | No wtg setting comes along (D4). The global `includeIf` rule applies there only if the user copies it |
| E5 | The user uninstalls wtg | Worktrees and branches remain valid; `git worktree prune` cleans up stale entries |
| E6 | A teammate has a repo with the same remote and no wtg | Unaffected. `wtg.landStrategy` exists only in the user's own git configuration |

## Implementation notes (not yet implemented)

- The no-footprint test (D6) in `internal/cmd`, using `internal/git/testhelper`.
- `AGENTS.md` gains one line under Conventions: "Collaborators never need wtg: see ADR 0022."
- Index row in [`DESIGN.md`](../DESIGN.md). No code change is required for the rule itself, since today's behaviour already complies.

## Consequences

- A feature that cannot be expressed as git or `gh` commands, or that needs a shared file, is declined or redesigned, which narrows what wtg can offer.
- Settings are easy to inspect and override with tools everyone has (`git config --show-origin`).
- The sibling-PR block is the single accepted visible trace, and it is documented and switchable.
- A user who loses wtg's state, or stops using wtg, can continue with plain git.

## Alternatives considered

- **A tracked per-repo file for wtg settings (for example `.wtg.toml`).** Rejected: it puts a wtg-specific file in every collaborator's checkout, and lets a repo grant itself behaviour such as direct pushes to the default branch.
- **Git notes or custom refs to store workspace metadata.** Rejected: they travel with `git push` unless carefully excluded, and are invisible to most people.
- **Store everything in wtg's own files and accept the dependency.** Rejected: the whole point of keeping to git is that collaborators and the user's future self are not locked in.
- **No sibling-PR block, to be fully clean.** Rejected in D5: it would remove the one cross-repo feature a forge cannot provide. The block is plain text, switchable, and harmless without wtg.

## Decisions summary

| ID | Decision |
|----|----------|
| D1 | Four rules: shared artefacts are plain; wtg-only data is user-local; everything is expressible as git/`gh` commands; removing wtg leaves ordinary repos |
| D2 | Audit table of every feature; all comply except the sibling-PR block |
| D3 | State files are an index; git is the source of truth; loss is recoverable |
| D4 | Per-repo wtg-only settings use git config (`wtg.*`, `includeIf`), not a new store or tracked file; `always.secrets` ignore-file question left open |
| D5 | Sibling-PR block is the one deliberate exception: plain Markdown, switchable with `--no-link`, harmless without wtg |
| D6 | Each feature ADR carries a "Without wtg" note; a no-footprint test snapshots a repo before and after `new` and `delete -d` |
