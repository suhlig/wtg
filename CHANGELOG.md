# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Scenario-based documentation static site powered by Zola, published on GitHub Pages, featuring executable scenario guides with outputs verified and generated directly from the real binary via a Ginkgo acceptance test suite (ADR 0027)
- `tools/gendocs` CLI documentation generator walking the command tree to generate exhaustive CLI, configuration, and behaviour references (ADR 0027)
- `git.network_timeout` (env `WTG_GIT_NETWORK_TIMEOUT`, default `5m`, `0` disables) bounds each git command that talks to a remote and each `gh` call. A call that exceeds it fails that repo with a message naming the setting, instead of blocking the whole run (ADR 0026)
- `wtg exec --timeout <duration>` stops the command in any repo that runs longer; that repo is reported as timed out and the others carry on. There is no default limit (ADR 0026)
- A repo that is slow to fetch is now named: `repo sync --progress` and `rebase --progress` show `(1 fetching; slow: <repo> 1m5s)`, and `repo sync`, `repo fetch`, `rebase`, `push` and `land` print `wtg: still waiting for: <repo> <duration>` to stderr after 30 seconds. `delete`, `repo archive --remote`, `repo unarchive --remote` and the fetch in `new`/`add --read-only` do the same, naming the `gh` call or repo, and `exec --parallel` names a slow repo in its progress line (ADR 0026)

### Changed

- Ctrl-C during a run now prints the git, `gh` and `exec` commands that were still running (repo and duration), stops them and their helper processes, and exits 130 (ADR 0026)
- `wtg repo archive --remote` and `wtg repo unarchive --remote` use the shared `gh` runner, so they honour the timeout; a `gh` call that times out is reported as such rather than as "not authenticated" (ADR 0026)

### Fixed

- A timed-out or interrupted `git fetch` no longer leaves `git-remote-https` and `index-pack` processes running, nor a partial `tmp_pack_*` file behind (ADR 0026)

## [1.10.0] - 2026-11-17

### Added

- Path-like arguments (`./...`, `../...`, `..`, `/...`, `~...`) are accepted wherever a repo is named, resolving via their path prefix to the canonical short name of the repo containing the target path (or main clone for worktrees) before name matching (ADR 0025)
- `wtg add --read-only [<workspace>] <repo>` adopts an existing member whose worktree is currently on a detached HEAD into a read-only member, recording its ref in state and checking out the target ref (ADR 0024 E11)
- `wtg new --read-only <repo>` (repeatable, with `--ref`) — create a workspace with read-only members in one step instead of `wtg new` followed by `wtg add --read-only`. The positional repos stay on the workspace branch, the named ones become detached worktrees at a pinned ref, and every ref is resolved before anything is created. At least one positional repo is still required, and a repo cannot be named both ways (ADR 0024)
- Shell completion offers `.` — the repo or workspace you are in — first, wherever the position accepts it; it is left out where `.` is rejected (the workspace name of `wtg new`, `wtg repo unarchive`) (ADR 0025)

### Changed

- Promoting a read-only member (`wtg add <repo>`) re-evaluates `go.work` membership based on `go.mod` in the branch/base commit rather than the stale pinned checkout on disk (ADR 0024)
- `wtg status` drift check evaluates minimum version selection (MVS) requirements across all workspace members' `go.mod` files on disk, comparing checkouts against the highest required semver version without network access (ADR 0024)
- `wtg add`, `remove` and `delete` edit an existing `go.work` instead of rewriting it: comments, `replace`, `toolchain` and `godebug` directives survive, only the `use` entries are reconciled with the workspace members, and the `go` directive is raised but never lowered. An unparseable `go.work` is still replaced (ADR 0008)

## [1.9.0] - 2026-11-15

### Added

- `wtg land --delete` — run `wtg delete` on the workspace once every repo landed without needing attention; branches are kept, and delete's own checks (uncommitted changes, commits on no remote, changed `always.secrets` copies) still apply
- `wtg land --delete-remote-branch` — delete `origin/<branch>` after a merge-landing, where the commits are on the base branch already; ignored for `pr` repos, and a no-op for a branch that was never pushed
- `wtg add --base <commit-ish>` — set the start point of a new branch, and record `origin/<branch>` as that repo's base branch for `rebase` and `land`
- `wtg add --read-only [--ref <ref>]` — add a repo as a member you consume rather than change: a detached worktree at a pinned ref, listed in `go.work` when it has a `go.mod`, with no feature branch. Without `--ref` the ref is the version the workspace's other members' `go.mod` files select (`GOWORK=off go list -m`, falling back to `origin/<default branch>`); a tag, commit or local branch pins, `origin/<branch>` is followed by `wtg rebase`. `wtg add <repo>` on a read-only member promotes it to a normal worktree (ADR 0024)
- `wtg status` compares, for every pair of `go.work` members, the version a consumer requires with what is checked out, and warns when they differ; modules a consumer `replace`s and versions that cannot be resolved locally are skipped, and the check never fails the command (ADR 0024 D6)

### Changed

- `wtg rebase` and `wtg land` target a repo's recorded base branch instead of its default branch when one exists; `wtg new --base origin/<branch>` (and the new `wtg add --base`) records it, while a tag, commit or local branch stays a plain start point (ADR 0018 D1, ADR 0019 D4)
- `wtg delete -d` and `wtg remove -d` delete the branch of a `MERGED` pull request whose head is the branch tip: a squash or rebase merge leaves no ancestry, so git alone would keep the branch forever. `gh` is consulted only when `-d` would otherwise refuse, a commit made after the merge keeps the branch, and any failure keeps it as before (ADR 0019 D12)
- `wtg rebase` advances a read-only member that follows `origin/<branch>`, reports a pinned one as `pinned at <ref>`, and skips a modified one instead of warning about a detached HEAD
- `wtg push` and `wtg land` skip read-only members, `wtg exec` includes them, and `wtg remove`/`wtg delete` remove their worktree without looking up or deleting a branch
- `.` is accepted wherever a repo or workspace is named — `wtg new`, `add`, `remove`, the `wtg repo` commands and the workspace commands — and resolves to the canonical short name of the repo containing the current directory, before name matching; `wtg new .` and `wtg repo unarchive .` are rejected because neither can mean what `.` says (ADR 0025)
- `wtg remove .` and `wtg repo status .` (and `fetch`, `sync`, `archive`) now work from a subdirectory and from a member worktree; previously `.` expanded to an absolute path that `remove` and `always.repos` could not match

## [1.8.0] - 2026-11-03

### Added

- `wtg land [<workspace>] [--strategy merge|pr] [--draft] [--no-link]` — publish every repo's branch: fast-forward the remote default branch (`merge`) or push and open a pull request (`pr`), with the choice read from `git config wtg.landStrategy`; PRs of one workspace link to each other in a marked block of their description, and the command exits non-zero when any repo needs attention (ADR 0019)
- `--summary` (global) — after the run, print the mutating git and `gh` commands and filesystem effects, grouped by repo, to stderr (ADR 0020)
- `--dry-run` (global) — print the plan without changing anything; supported by every command, never fetches, prompts or runs hooks (ADR 0021)
- `wtg delete --yes` — answer the confirmation prompt with yes
- `wtg add` accepts `.` as a repo argument, meaning the repo containing the current directory

### Changed

- `wtg delete` and `wtg remove` now warn about commits that exist on no remote instead of commits ahead of the upstream, so a never-pushed branch is protected and a merge-landed branch raises no false alarm; `delete -d` deletes a branch when no commit exists only on it
- `wtg delete` exits non-zero when it does not delete (declined prompt, or no terminal without `--yes`) and keeps a repo whose `always.secrets` copy was changed (ADR 0019 D11)
- `wtg --dry-run repo sync` and `wtg --dry-run rebase` say "as of the last fetch", because a dry run never fetches
- `.golangci.yml` now forbids direct filesystem mutation and process creation outside the `trace` package and the git/`gh` boundaries (ADR 0020 D7)
- `wtg delete` now infers the workspace from the current directory when `<workspace>` is omitted, like `push`, `rebase`, `exec`, `add` and `remove`; outside any workspace it still asks you to name one

## [1.7.0] - 2026-10-28

### Added

- `wtg rebase [<workspace>] [--onto <branch>] [--progress]` — fetch `origin` and rebase the workspace branch onto each repo's default branch (or `--onto`) in every repo in parallel, with `--autostash`; conflicts are aborted and the worktree restored, a stash that cannot be re-applied is reported and kept, and each rebased repo reports its old tip so it can be undone with `git reset --keep`

### Changed

- `wtg push` now explains a rejected push when the branch has diverged from its upstream (typically after a rebase) and shows the `git push --force-with-lease` command

### Fixed

- `wtg repo sync` no longer fails with "cannot determine default branch" when a repo's `origin/HEAD` is missing *and* its default branch was never fetched; when the `git remote set-head origin --auto` repair fails (`Not a valid ref: refs/remotes/origin/<branch>`), wtg now falls back to asking the remote directly with `git ls-remote --symref origin HEAD`
- `wtg repo sync` now reports the actual reason a repo's default branch could not be determined (e.g. an unreachable remote) instead of the misleading "is not a symbolic ref" from reading the cached `origin/HEAD`

## [1.6.0] - 2026-10-15

### Added

- `wtg repo unarchive <repo>...` — restore archived repo clones to the exact paths recorded when they were archived and forget their provenance; refuses if the origin is occupied or the clone is missing, `--dry-run` previews, `--archive-dir` overrides `archive.root_dir`, and `--remote` also unarchives each repo on GitHub via `gh` (same all-or-nothing saga as `--remote` on archive)
- `wtg repo archive --remote` — also archives each retired repo on GitHub via the `gh` CLI as part of the same all-or-nothing operation; it checks `gh` is installed and authenticated before moving anything, aborts unless every named repo has a github.com origin, treats an already-archived repo as a no-op, and rolls back the local moves if an upstream archival fails
- `wtg repo archive <repo>...` — retire main repo clones by moving them to `archive.root_dir` and recording their origin; refuses on uncommitted, unpushed, or stashed work unless `--force` is given, never touches a clone in use (worktrees, spaces, `always.repos`), and prints the `gh repo archive` command for GitHub remotes
- Repo names can be matched by a unique partial substring of any path segment (e.g. `infra` for `github.com/uhlig-it/infrastructure`) in `wtg new`, `add`, `remove`, and the `wtg repo` commands; when more than one repo matches, `wtg` errors and lists the candidates
- `wtg repo sync --progress` bar now shows a `~` slot while a fetch is in flight and appends `(N fetching)` to the bar label so it is always clear whether sync is still working; repos with no remote show `ø` instead of the ambiguous `·`

### Fixed

- `wtg repo sync` (and `repo status`) no longer fail with "cannot determine default branch" when a repo's cached `origin/HEAD` is missing or is a non-symbolic ref; wtg now asks the remote (`git remote set-head origin --auto`) and repairs the ref
- `wtg repo status`, `repo fetch`, and `repo sync` now resolve repo names like the workspace commands do — by exact short name, unique basename, or unique partial segment match — instead of only by exact relative path, so nested repos addressed by their basename (documented since 1.5.0) now work here too
- `wtg repo sync` no longer skips repos that only have untracked files; only tracked modifications now trigger the "uncommitted changes" warning, since untracked files do not block a fast-forward
- `wtg repo sync` (and any command that spawns git subprocesses) now sets `GIT_TERMINAL_PROMPT=0` and `GIT_SSH_COMMAND=ssh -o BatchMode=yes` so unknown SSH host keys and HTTPS credential prompts fail immediately instead of stalling indefinitely
- `wtg repo sync` and `wtg repo status` now display the root-level repo by its directory name instead of `.` when the discovery `root_dir` is itself a git repo

## [1.5.0] - 2026-10-04

### Added

- `wtg config edit` command to open the resolved config file in `$VISUAL` / `$EDITOR` (with fallback to `nano`, `vim`, `vi`)
- Repos can be addressed by their unique basename (e.g. `dspictl` for `github.com/suhlig/dspictl`) in `wtg new`, `add`, `remove`, and `wtg repo` commands; ambiguous basenames error with the list of matching repos

### Changed

- Module path renamed to `github.com/suhlig/wtg` so the fork installs via `go install github.com/suhlig/wtg@latest`
- Narrow the `wtg` skill trigger to workspace management and multi-repo operations, excluding ordinary work inside existing worktrees

### Fixed

- `wtg delete` and `wtg remove` now prune the empty org/group directories left behind by nested repo worktrees, so the space root is removed without a "directory not empty" warning

## [1.4.0] - 2026-07-22

### Added

- `always.secrets` config key — relative paths copied from each source repo into its worktree when present (see `docs/always.md`)
- Claude Code and Codex plugins sharing the same `wtg` and `wtg-setup` skills
- Agent-neutral workspace setup that supports `CLAUDE.md`, `AGENTS.md`, or both

## [1.3.1] - 2026-07-01

### Fixed

- `wtg exec` and `wtg push` now skip repos that are symlinks from `always.repos` instead of running commands against them and failing confusingly (#35)
- `wtg exec` now passes the parent's stdin through to the child command, so interactive commands (e.g. `git commit` opening an editor) work as expected

## [1.3.0] - 2026-06-21

### Added

- `wtg config` command group: `wtg config` prints the resolved config file, `wtg config init` scaffolds a commented TOML template (`--force`, `-o PATH`, `-o -`), `wtg config path` prints the resolved path
- TOML config support; the parser is selected by file extension, so existing `config.yaml` files keep loading
- `always.run` config key — an executable run after a space is created, changed, or deleted (see `docs/always.md`)

### Changed

- Config is now TOML-primary: the default path is `$XDG_CONFIG_HOME/wtg/config.toml`

### Removed

- `wtg init` interactive wizard, replaced by `wtg config init`

## [1.2.0] - 2026-05-19

### Added

- `always.repos` and `always.files` config keys — repos symlinked into and files copied into every new space

## [1.1.0] - 2026-05-18

### Changed

- `wtg status` always shows all spaces, with the current space sorted first
- `wtg new` requires at least one repo argument, and saves workspace state before the saga runs so cleanup covers a partial creation

### Fixed

- `wtg push` sets the upstream on first push and detects remote-only branches

## [1.0.0] - 2026-04-08

Initial public release.

### Added

- `wtg new` — create a workspace: one linked worktree per repo, all on the same branch, with an auto-generated `go.work` for Go modules
- `wtg delete` — remove all worktrees in a workspace; optionally delete branches with `-d` (merged only) or `-D` (force)
- `wtg add` / `wtg remove` — add or remove repos from an existing workspace; `go.work` is kept in sync
- `wtg status` — combined view of main repo clones and all workspaces; `--long` expands to per-file changes
- `wtg exec <workspace> -- <cmd>` — run a command in each worktree sequentially, continuing past failures
- `wtg push` — push all workspace branches to origin in parallel
- `wtg repo status` / `fetch` / `sync` — inspect and update main repo clones across the discovery root
- `wtg init` — interactive setup wizard
- Shell completions for bash, zsh, and fish; `wcd <workspace>` helper to `cd` into a workspace root
- Saga-based rollback: failures during `wtg new` or `wtg add` clean up any worktrees and branches created so far
- XDG Base Directory-compliant config (`$XDG_CONFIG_HOME/wtg/config.yaml`) and state (`$XDG_DATA_HOME/wtg/spaces/`)
- `--config` flag and `WTG_CONFIG` environment variable for config file override

[Unreleased]: https://github.com/suhlig/wtg/compare/v1.10.0...HEAD
[1.10.0]: https://github.com/suhlig/wtg/compare/v1.9.0...v1.10.0
[1.9.0]: https://github.com/suhlig/wtg/compare/v1.8.0...v1.9.0
[1.8.0]: https://github.com/suhlig/wtg/compare/v1.7.0...v1.8.0
[1.7.0]: https://github.com/suhlig/wtg/compare/v1.6.0...v1.7.0
[1.6.0]: https://github.com/suhlig/wtg/compare/v1.5.0...v1.6.0
[1.5.0]: https://github.com/suhlig/wtg/compare/v1.4.0...v1.5.0
[1.4.0]: https://github.com/suhlig/wtg/compare/v1.3.1...v1.4.0
[1.3.1]: https://github.com/suhlig/wtg/compare/v1.3.0...v1.3.1
[1.3.0]: https://github.com/suhlig/wtg/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/suhlig/wtg/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/suhlig/wtg/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/suhlig/wtg/releases/tag/v1.0.0
