# ADR 0013: Retiring repos with `wtg repo archive`

- Status: Proposed
- Date: 2026-10-06

## Context

wtg discovers a repo by scanning `discovery.root_dir` (or `root_dirs`) for directories containing a `.git` entry, and treats every match as live: `wtg repo status`/`sync` operate on them, `wtg new`/`add` branch worktrees off them, and they appear in shell completion. There is no way to retire a repo. A finished or shelved project keeps showing up in discovery, and the only way to stop that is to delete the clone — which is destructive (it can destroy unpushed commits, stashes, and untracked files) and irreversible.

The common real-world sequence is: a feature lands or a project is put on ice, the local clone is no longer needed for active work but should be kept for reference, and the upstream GitHub repo should be marked archived (read-only). Today this is done by hand — check each clone for unsaved work, move it somewhere out of the discovery path, and archive it on GitHub — repeatedly across several repos, with no safety net.

## Problem

Provide a safe, batchable, scriptable way to retire one or more repos: move each main clone out of the active discovery area into an archive area (intact and restorable), and — optionally — mark the upstream GitHub repo as archived.

## Decisions

1. **Command surface: `wtg repo archive <repo>...`.** Archiving is a main-clone lifecycle operation, so it belongs in the `repo` command group alongside `repo sync` and `repo status`, not at the top level. It accepts multiple repos in one invocation, and an optional `--archive-dir` per-run override mirrors `wtg new --path`.

2. **The local move is the core action and is provider-agnostic.** Archiving renames the main clone from its discovery path to `<archive.root_dir>/<repo-short-name>`, mirroring the nested short-name layout used everywhere else. The clone is kept intact; nothing is deleted. A same-filesystem rename is assumed (instant and lossless).

3. **Remote archival is opt-in and never the default.** v1 does not call any hosting API; it prints the exact `gh` command to archive each repo on GitHub at the end of a successful run, so the user stays in control of the remote change. v2 adds `--remote` to execute it. This keeps wtg provider-agnostic (it still shells out to `git`, and would shell out to `gh`) and keeps an irreversible server-side change out of a routine local cleanup.

4. **Pre-flight the whole batch; refuse rather than partially apply.** All repos are checked before any filesystem change. If any requested repo is unusable, the entire invocation aborts, matching the pre-flight-then-saga pattern of `wtg new`.

5. **"Pending work" warnings are advisory, not data-loss guards.** Because archiving moves rather than deletes, uncommitted changes, unpushed commits, and stashes all travel with the clone and remain usable. The warning exists to prevent out-of-sight work from being forgotten, and to note that once the remote is archived (v2) the work can no longer be pushed without unarchiving first. wtg therefore flags all three categories: dirty files (`Status`), commits on any local branch that exist on no remote (`git rev-list --count --branches --not --remotes`), and stashed changes (`git stash list`). Without `--force` the run aborts; there is no interactive prompt, keeping the command scriptable.

6. **`--force` overrides pending-work warnings only.** A clone that has live linked worktrees, is referenced by a space's state, or is listed in `always.repos` cannot be archived even with `--force`; the command explains why and prints the commands that would make it succeed. A clone with stale (registered but missing-on-disk) worktree entries is likewise refused, with a suggestion to run `git worktree prune`, rather than silently pruning.

7. **The archive root must be outside every discovery root.** Discovery would otherwise re-find the moved clone as a live repo. This is validated at run time and rejected with guidance. Consequently the tempting example `~/git/archived/...` is *not* allowed when `~/git` is a discovery root; the archive root must be a sibling such as `~/git-archived`.

8. **Record provenance in wtg's XDG data directory.** Each archive records the repo's short name, original path, origin remote, host, and timestamp in `~/.local/share/wtg/archived.yaml`, alongside the existing space state. This keeps the archive tree pristine (no wtg-specific files mixed into cold storage) and is enough for a future `wtg repo unarchive` (v3) to restore the clone to its exact discovery root without guessing among multiple roots.

9. **`--dry-run` prints the full plan without changing anything.** Archiving is a user-visible, hard-to-undo-by-hand operation across many repos; a preview is cheap insurance.

## Consequences

- Retired repos drop out of discovery, `repo status`/`sync`, and completion automatically, because they no longer live under a discovery root.
- The local clone and its work are preserved and restorable, in contrast to deletion.
- wtg gains a hosting-provider concern, but only as an optional, shell-out-to-`gh` step; the core command remains pure filesystem plus git.
- New surface area must be added, documented, and tested: a config key (`archive.root_dir`), an XDG data file (`archived.yaml`), and a new `git.Runner` method for the pending-work checks (unpushed-across-branches and stashes).
- Users whose layout puts `archived/` inside a discovery root must move it out; this is a deliberate rejection of the tempting-but-wrong default.

## Alternatives considered

- **Top-level `wtg archive`.** Rejected: it would be the only main-clone command outside `wtg repo`, splitting a coherent group.
- **Delete instead of move.** Rejected: destructive and irreversible; the whole point is to keep the clone.
- **Archive on GitHub by default.** Rejected: it makes a routine local cleanup mutate shared remote state, and ties a provider-agnostic tool to GitHub.
- **Use `go-github` or a raw REST client now.** Deferred: v1 needs no HTTP. Shelling out to `gh` (v2) reuses the user's existing auth and GitHub Enterprise configuration, and mirrors the deliberate "use system git, not go-git" choice.
- **Store provenance in a manifest inside the archive tree.** Rejected in favour of the XDG data file for consistency with how spaces are stored and to keep cold storage clean. If the archive tree is expected to move between machines, the self-describing manifest is the better choice and this decision should be revisited.
- **Fold archiving into `repo sync`.** Rejected: unrelated intent and a different safety model; a separate verb is clearer.
- **Silently prune stale worktree entries.** Rejected: it is a hidden mutation; refuse-and-inform is more predictable.

## Follow-ups

- v2 (done): `--remote` executes the `gh` command, with an auth pre-flight and idempotent archive.
- v3: `wtg repo unarchive` moves the clone back to its recorded origin and unarchives upstream.
