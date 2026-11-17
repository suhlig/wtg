# wtg

`wtg` manages feature branches that span multiple repos. When a feature touches several repos at once, `wtg` checks out a shared branch across all of them as [git worktrees](https://git-scm.com/docs/git-worktree) and wires them together with a `go.work` file — giving you an isolated, ready-to-build workspace per feature without cloning anything new.

> New to worktrees? The [GitKraken worktree guide](https://www.gitkraken.com/learn/git/git-worktree) is a good primer.

## Installation

For using this fork:

```sh
go install github.com/suhlig/wtg@latest
```

### Shell completions

**bash** — add to `~/.bashrc`:
```bash
source <(wtg completion bash)
```

**zsh** — add to `~/.zshrc`:
```zsh
source <(wtg completion zsh)
```

**fish** — add to `~/.config/fish/conf.d/wtg.fish`:
```fish
if status is-interactive
  wtg completion fish | source
end
```

Completion offers repo and workspace names, and `.` (see [Workspace commands](#workspace-commands)) wherever it is accepted: the repo or workspace you are in. It is listed first.

## Configuration

Run `wtg config init` to scaffold a commented config file at `~/.config/wtg/config.toml`:

```sh
wtg config init
```

It writes every setting commented out with its default; uncomment and edit the lines you want to override. `wtg config` prints the resolved file, `wtg config edit` opens it in `$EDITOR`, and `wtg config path` prints its path. A minimal config looks like:

```toml
[discovery]
root_dir = "~/repos"       # where wtg scans for git repos (or use root_dirs = ["~/repos", "~/work/repos"])
max_depth = 2

[spaces]
root_dir = "~/workspaces"  # where workspaces are created

[git]
branch_prefix = ""         # prepended to workspace names, e.g. "yourname/"
network_timeout = "5m"     # longest one fetch/push may take; "0" disables (see below)

[archive]
root_dir = "~/repos-archived"  # where `wtg repo archive` moves retired clones
```

`network_timeout` (env `WTG_GIT_NETWORK_TIMEOUT`) bounds each git command that talks to a remote, and each `gh` call (`land --strategy pr`, `delete -d`'s PR lookup, `repo archive --remote`). A repo that exceeds it is reported as failed (`timed out after 5m0s`) and the others carry on. A first fetch of a very large repo over a slow link can need more; raise it or set it to `0`. While a fetch or push runs longer than 30 seconds, wtg names the repo it is waiting on, and Ctrl-C prints which git commands were still running. See [ADR 0026](docs/adr/0026-network-timeout-and-slow-repo-visibility.md).

YAML is still accepted: a file ending in `.yaml`/`.yml` loads via its extension, so an existing `config.yaml` keeps working.

`discovery.root_dir` (or `discovery.root_dirs` for multiple search directories) should contain your regular repo clones, each sitting on their default branch (`main`, `master`, etc.) and otherwise left untouched. `wtg` creates worktrees alongside them — it never modifies the main clones.

Repos are addressed by their slash-separated path relative to their discovery root directory, e.g. `github.com/suhlig/rustomato`. A repo nested under org or group directories can also be addressed by its basename (`rustomato`) as long as no other discovered repo shares that basename; otherwise `wtg` errors and lists the matching repos, and you use the full path to disambiguate. A unique partial match on any path segment works too — `infra` selects `github.com/suhlig/infrastructure`, and `suhlig` selects a repo under that org — but when more than one repo matches, `wtg` lists all candidates and you disambiguate with a longer name or the full path.

Override with `--config <path>` or the `WTG_CONFIG` environment variable.

### Always-included repos, files, and hooks

The optional `always` section applies the same setup to every new space:

```toml
[always]
repos = ["shared-tooling"]          # symlinked into every new space
files = ["~/.config/wtg/CLAUDE.md"] # copied into every new space root
secrets = [".env"]                  # copied into each worktree when present in the source repo
run = "~/.config/wtg/on-event"      # executable run after create/add/remove/delete
```

`always.repos` symlinks shared repos in (no feature-branch worktree),
`always.files` seeds template files into the space root, `always.secrets`
copies listed local files from each source repo into its worktree when present,
and `always.run` invokes a hook script on space lifecycle events with the space
context in `WTG_*` environment variables. See [docs/always.md](docs/always.md)
for the full behaviour, the event/variable reference, and examples.

## Quick start

```sh
# Create a workspace for a new feature across three repos
wtg new my-feature api payments frontend

# Jump in
wcd my-feature

# ... do your work, then clean up
wtg delete my-feature --delete-branch
```

## Workspace commands

`.` names the repo or workspace you are in, and path-like arguments (`./...`, `../...`, `/...`, `~/...`) name the repo containing the target path. Every command that takes a repo or a workspace argument accepts them: `wtg new other-space .` or `wtg add ../other-repo` adds the repo behind the path, `wtg remove .` removes it from the current workspace, and `wtg status .` shows the workspace containing it. They work from a subdirectory and from a linked worktree, and are resolved before name matching, so they never collide with short names. In `add` and `remove`, `.` and paths always mean a repo, never the workspace.

### `wtg new <workspace> <repo>... [--read-only <repo>]...`

Create a workspace. At least one repo must be specified. For each repo, `wtg`
creates or checks out a branch named `<branch_prefix><workspace>` as a linked
worktree. A `go.work` file is written automatically for repos that have a
`go.mod`. Paths listed in `always.secrets` are copied from each source repo into
its worktree when present (see [docs/always.md](docs/always.md)).

```sh
wtg new my-feature api payments frontend
wtg new my-feature api --branch yourname/main  # check out an existing branch
wtg new my-feature api --base origin/develop   # branch from develop, and stay on it
wtg new my-feature api --read-only config-lib --read-only codegen   # api is changed; the others are consumed
```

`--base <commit-ish>` cuts a new branch from something other than `HEAD`. When it names a remote-tracking branch (`--base origin/develop`), that branch is also recorded as the repo's base: later `wtg rebase` and `wtg land` target it instead of the repo's default branch. A tag, a commit or a local branch is only a start point and records nothing.

`--read-only <repo>` (repeatable) creates a mixed workspace in one step: the named repos become [read-only members](#read-only-members) — detached worktrees at a pinned ref, with no feature branch — while the positional repos get the workspace branch as usual. `--ref <ref>` applies to every `--read-only` repo and, as with `add`, a ref that does not resolve fails before anything is created. Without `--ref`, the ref is the version the positional repos' `go.mod` files select, else `origin/<default branch>`. At least one positional repo is still required, and a repo cannot be both. `.` works as a `--read-only` value too.

Branch behaviour per repo:

| Branch state | Action |
|---|---|
| Does not exist | Created from the repo's default branch |
| Exists, not checked out | Checked out in the new worktree |
| Exists, already checked out | Error |

### `wtg delete [<workspace>]`

Delete a workspace and remove its worktrees. Prompts for confirmation if any
repo has uncommitted changes or commits that exist on no remote (so a branch
that was merge-landed with `wtg land` does not trigger a false alarm).

```sh
wtg delete my-feature            # remove worktrees, keep branches
wtg delete my-feature -d         # also delete branches when no commit exists only on them
wtg delete my-feature -D         # force-delete branches
wtg delete my-feature --yes      # answer the prompt with yes (scripts)
wtg delete                       # workspace inferred from CWD
```

`delete` exits non-zero whenever it does not delete, so `wtg land && wtg delete`
stops: a declined prompt prints `aborted: nothing deleted`, and without a
terminal and without `--yes` it refuses instead of reading an answer from a pipe.

`-d` also recognises a merged pull request. A squash or rebase merge puts the work into the base branch without making the feature branch an ancestor of it, so git alone would keep the branch forever. When `gh` is available, `delete -d` (and `remove -d`) asks GitHub and deletes the branch of a `MERGED` pull request, reporting `(PR #12 merged)` — but only when the PR's head commit is the branch tip, so a commit made after the merge keeps the branch. If `gh` is missing, not authenticated, or the remote is not GitHub, the branch is kept exactly as before.

A repo whose `always.secrets` copy differs from the main clone, or whose source
no longer exists, is **kept** (with its branch), the other repos are deleted as
usual, and `delete` exits non-zero. Copy the change back (or discard it) and run
`wtg delete` again; it resumes with exactly the kept repos. Other ignored files
you created yourself are not detected.

### `wtg add [<workspace>] <repo>... [--read-only [--ref <ref>]]`

Add repos to an existing workspace. Creates worktrees on the workspace's branch and updates `go.work`. When run from inside a workspace directory, the workspace argument can be omitted.

```sh
wtg add my-feature infra logging
wtg add infra logging             # workspace inferred from CWD
wtg add .                         # the repo behind the current directory
```

With `--read-only`, the repo is added as a member you *consume* rather than change: a detached worktree at a pinned ref, listed in `go.work` if it has a `go.mod`, with no feature branch of its own. See [Read-only members](#read-only-members). To create a workspace with read-only members from the start, use `wtg new --read-only <repo>`.

```sh
wtg add --read-only config-lib                # what the workspace's consumers require
wtg add --read-only --ref v2.0.0 config-lib   # pin a tag
wtg add --read-only --ref origin/main lib     # follow a branch
```

`--base <commit-ish>` sets the start point of a new branch, and records `origin/<branch>` as that repo's base exactly as `wtg new --base` does. Use it to give one repo of a workspace a base other than its default branch.

### `wtg remove [<workspace>] <repo>...`

Remove repos from a workspace. Prompts if there are uncommitted changes or
unpushed commits. Use `wtg delete` to remove the whole workspace. When run from
inside a workspace directory, the workspace argument can be omitted.

```sh
wtg remove my-feature logging
wtg remove my-feature logging -d  # also delete the branch
wtg remove logging                # workspace inferred from CWD
```

### Read-only members

A feature touches two or three repos and merely *uses* a few more: a library you built against, a docs repo, a code generator. `wtg add --read-only` adds such a repo as a **detached worktree at a ref you choose**. It is a real directory, so you can build, search and run in it, and it is listed in `go.work` if it has a `go.mod` — but it has no feature branch, and `push`, `land` and the branch safety checks in `remove`/`delete` ignore it.

```sh
wtg add --read-only config-lib                 # the version your consumers require
wtg status
```

```
my-feature  ~/workspaces/my-feature
  api          [geoff/my-feature]    ✓ clean
  config-lib   ◇ read-only @ v2.0.0  ✓ clean
```

Where the ref comes from:

| Invocation | Ref |
|---|---|
| `--read-only` | the version the other members' `go.mod` files select (`go list -m`), else `origin/<default branch>` |
| `--read-only --ref v2.0.0` | a tag, pinned |
| `--read-only --ref 1a2b3c4` | a commit, pinned |
| `--read-only --ref origin/main` | a branch, followed by `wtg rebase` |

`--ref` applies to every repo named in that invocation (every `--read-only` repo, for `wtg new`); add repos that need different refs in separate invocations. A ref that does not resolve fails before anything is created. Without `--ref`, consumers that require different versions are an error that lists each one, because `go.work` overrides `go.mod`: the member contributes exactly one version to the build and it has to be the right one.

What the commands do with a read-only member:

| Command | Behaviour |
|---|---|
| `status` | shows `◇ read-only @ <ref>`; a modified worktree is flagged |
| `rebase` | advances it when it follows `origin/<branch>`, leaves a pinned one alone, never touches a modified one |
| `push`, `land` | skipped, with no failure |
| `exec` | included — it is a real worktree of its own |
| `remove`, `delete` | removes the worktree; `-d`/`-D` have no branch to delete |
| `add <repo>` | promotes it to a normal worktree on the workspace's branch |

```sh
wtg add config-lib       # promote: needs a clean worktree
wtg rebase               # "advanced" for followed refs, "pinned at v2.0.0" otherwise
```

"Read-only" is a statement of intent, not file permissions: `wtg` reports a modification but does not prevent one. To change a read-only member, promote it first. To re-pin one, `wtg remove` it and add it again with a different `--ref`.

`wtg status` also compares, for every pair of `go.work` members, the version a consumer requires with what is checked out, and prints a line when they differ:

```
⚠ config-lib is 12 commits behind v2.0.0, which api requires
```

That warning means the local build is not what CI builds, since `go.work` overrides `go.mod`. A checkout that is *ahead* of the required version is a muted note instead — that is the normal state of a repo you are editing, but it cannot build without `go.work` until a tag is cut. Modules a consumer `replace`s are not checked, and a version that cannot be resolved locally stays silent rather than raising a false alarm. The check never fails `wtg status` and never fetches.

See [ADR 0024](docs/adr/0024-read-only-workspace-members.md).

### `wtg push [<workspace>]`

Push the workspace's branch from each repo's worktree to origin in parallel.
When run from inside a workspace directory, the workspace argument can be
omitted.

```sh
wtg push my-feature
wtg push                          # workspace inferred from CWD
```

If a push is rejected because the branch has diverged from its upstream
(typically after `wtg rebase`), the output says so and shows the
`git push --force-with-lease` command. wtg never force-pushes.

### `wtg land [<workspace>] [--strategy merge|pr] [--draft] [--no-link] [--delete] [--delete-remote-branch]`

Publish every repo's branch that has commits beyond `origin/<base>`, in parallel, where the base is the branch recorded at `wtg new`/`wtg add` time or the repo's default branch. With the `pr` strategy (the default) the branch is pushed and a pull request is opened against the base (an existing one is reused); with `merge` the remote base branch is fast-forwarded to the branch. `land` never forces and never deletes unless asked, so `wtg land && wtg delete` is the end of the workflow: `land` exits non-zero if any repo failed or needs attention (wrong branch, not a fast-forward, `gh` missing, siblings not linked).

The strategy is a per-repo choice kept in git config, not in a tracked file:

```sh
git -C ~/git/api config --local wtg.landStrategy merge      # one clone
```

```gitconfig
# ~/.gitconfig: every repo whose remote matches
[includeIf "hasconfig:remote.*.url:git@github.com:myorg/**"]
    path = ~/.gitconfig-myorg-merge
```

`--strategy` overrides the setting for one run. With the `pr` strategy, each
pull request description gets a marked block that links the other PRs of the
workspace; `--no-link` suppresses it. `--dry-run` shows the plan, including
`would link N sibling PRs`.

`--delete` runs `wtg delete` on the workspace once every repo landed without needing attention; branches are kept, and delete's own checks still apply, so a worktree with unlanded changes still prompts (or refuses in a script). `--delete-remote-branch` deletes `origin/<branch>` after a merge-landing, where the commits are on the base branch already; a branch that was never pushed is a no-op, and `pr` repos keep their head branch.

### `wtg rebase [<workspace>] [--onto <branch>] [--progress]`

Fetch `origin` and rebase the workspace's branch onto `origin/<base branch>` in every repo's worktree, in parallel, with `--autostash` so uncommitted changes come along. The base is the branch recorded when the workspace was created (`wtg new`/`wtg add --base origin/<branch>`), else the repo's default branch; `--onto` names one branch for all repos. When run from inside a workspace directory, the workspace argument can be omitted.

```sh
wtg rebase my-feature
wtg rebase                        # workspace inferred from CWD
wtg rebase --onto develop         # rebase onto origin/develop in every repo
wtg rebase --progress             # live per-repo progress bar
```

- Repos that are already up to date are reported as `✓ up to date`; rebased
  repos as `↑ rebased (was a1b2c3d)`. Undo with `git reset --keep a1b2c3d`.
- A conflict aborts that repo's rebase and restores the worktree, including
  uncommitted changes; the command to retry by hand is printed.
- If re-applying the stash conflicts after a successful rebase, the branch is
  rebased, conflict markers are in the worktree, and the changes stay in the
  stash.
- Repos on another branch, with unresolved conflicts, or with a rebase, merge,
  cherry-pick or revert in progress are skipped.
- A read-only member is not rebased: one that follows `origin/<branch>` is advanced (`↑ advanced (was a1b2c3d)`), a pinned one is reported as `✓ pinned at v2.0.0`, and a modified one is skipped with a warning.
- The exit code is non-zero if any repo failed or needs attention. Nothing is
  pushed.

See [ADR 0018](docs/adr/0018-workspace-rebase.md) for the design.

### `wtg status [<workspace>...]`

Show workspace status. Without arguments, shows all workspaces — the one
containing the current directory is listed first. Pass `--long` / `-l` to
expand file-level changes per repo.

```sh
wtg status
wtg status my-feature
wtg status my-feature --long
```

```
my-feature  ~/workspaces/my-feature
  api       [geoff/my-feature]  ✓ clean      ↑2
  payments  [geoff/my-feature]  ! 2 modified
  frontend  [geoff/my-feature]  ✓ clean
```

Members that are consumed rather than changed show the ref they are pinned to instead of a branch, and a warning is printed when a `go.work` member's checkout differs from what another member requires:

```
  config-lib  ◇ read-only @ v2.0.0  ✓ clean
⚠ config-lib is 12 commits behind v2.0.0, which api requires
```

See [Read-only members](#read-only-members).

### `wtg exec [<workspace>] [--parallel] [--timeout <duration>] -- <cmd> [<args>...]`

Run a command in each repo's worktree sequentially, streaming output as it goes. With `--parallel`, commands run in all repos concurrently with a live progress indicator, buffering per-repo output until complete.

Execution continues even if a command fails — all repos are attempted and failures are reported at the end. When run from inside a workspace directory, the workspace argument can be omitted.

```sh
wtg exec my-feature -- git status
wtg exec my-feature --parallel -- go test ./...
wtg exec -- git status            # workspace inferred from CWD
wtg exec --timeout 10m my-feature --parallel -- go test ./...
```

`--timeout` stops the command in any repo that runs longer than the given Go duration (`90s`, `10m`); that repo is reported as `timed out after …` and the others carry on. There is no limit by default, because `exec` runs your own commands. With `--parallel`, the progress line names a repo whose command has run for more than 30 seconds, e.g. `[····] (slow: api 1m5s +1)`. Ctrl-C lists the commands still running.

## Previewing and reporting

Two global flags work with any command, before or after the subcommand.

```sh
wtg --dry-run new my-feature api payments   # print the plan; change nothing
wtg push --summary                          # run, then list what was executed
```

`--dry-run` runs the real code with every mutation turned off and prints what
it would do: result rows say `→ would …`, followed by the git, `gh` and
filesystem commands under "Would run". Reads still run, so the plan is based on
current state and on the last `fetch`; a dry run never fetches, never prompts
(`would ask: …`), and never runs the `always.run` hook or an `exec` command.
Its exit code is 0 when a plan could be made.

`--summary` runs the command normally and, afterwards, prints the mutating git
and `gh` commands and filesystem effects (`(wtg) write …`) to stderr, grouped
by repo. Failed commands are marked with their exit code.

## Repo commands

These operate on your main repo clones, not workspace worktrees. Useful for keeping clones up to date before starting a new feature.

### `wtg repo sync [<repo>...]`

Fetch and fast-forward each repo's default branch. Repos with uncommitted changes are skipped with a warning.

```sh
wtg repo sync               # sync all repos
wtg repo sync api payments  # sync specific repos
wtg repo sync --progress    # watch per-repo progress live
```

See [docs/repo-sync.md](docs/repo-sync.md) for the progress-bar symbol legend and state diagram.

### `wtg repo status [<repo>...]`

Show branch, dirty status, and ahead/behind counts for each main repo clone.

```sh
wtg repo status
wtg repo status --long  # also show remote URL and local path
```

### `wtg repo archive <repo>...`

Retire one or more main repo clones: each is moved out of the discovery area into `archive.root_dir`, kept intact, and recorded (with its original path) under `~/.local/share/wtg/archived.yaml`. Nothing is deleted and, without `--remote`, no remote is touched. See [docs/archive.md](docs/archive.md) for the full behaviour, the provenance file, and examples.

```sh
wtg repo archive old-service spike-repo
wtg repo archive old-service --dry-run
wtg repo archive old-service --remote
```

Archiving refuses if a clone has uncommitted changes, commits not on any remote, or stashes; pass `--force` to proceed anyway. It also refuses, with no override, when a clone has live or stale worktrees, is still referenced by a space, or is listed in `always.repos` — the messages name the exact command that clears the block.

`archive.root_dir` must be on the same filesystem as your repos and outside every discovery root, or the moved clones would be rediscovered.

Pass `--remote` to also archive each repo on GitHub, as part of the same all-or-nothing operation. It requires the [`gh` CLI](https://cli.github.com) (2.32 or newer) with an authenticated github.com account, and every named repo must have a github.com origin — if any does not, the whole command aborts rather than silently skipping it. It is idempotent: a repo that is already archived upstream is left as is. If an upstream archival fails, the local moves are rolled back and nothing is left archived.

Without `--remote`, when a repo's `origin` is on github.com, the exact `gh` command to archive it upstream is printed at the end instead:

```
Upstream repos were not modified. To archive them on GitHub:
  gh repo archive something/foo --yes
```

### `wtg repo unarchive <repo>...`

Restore one or more archived repo clones: each is moved out of `archive.root_dir` back to the exact path recorded when it was archived, and its provenance record is removed. Nothing is deleted.

```sh
wtg repo unarchive old-service spike-repo
wtg repo unarchive old-service --dry-run
wtg repo unarchive old-service --remote
```

`unarchive` refuses if the recorded origin path already exists, or if the archived clone is missing from `archive.root_dir`; pass `--archive-dir` if you moved your archive root.

Pass `--remote` to also unarchive each repo on GitHub, under the same rules as `--remote` on archive: it requires the `gh` CLI (2.32 or newer) with an authenticated github.com account, every named repo must have a github.com origin, it is idempotent for a repo that is already unarchived upstream, and if an upstream unarchive fails the local restores are rolled back. Without `--remote`, the exact `gh` command is printed instead:

```
Upstream repos were not modified. To unarchive them on GitHub:
  gh repo unarchive something/foo --yes
```

## Development

This fork renames the module path to `github.com/suhlig/wtg` so it installs via `go install github.com/suhlig/wtg@latest`. Upstream declares `github.com/geoffamey/wtg`, so merging upstream changes conflicts on the internal import lines. Enable [`git rerere`](https://git-scm.com/docs/git-rerere) once and Git replays the same resolution automatically on every future merge:

```sh
git config rerere.enabled true
```

Conflicts are limited to hunks that touch import blocks; changes elsewhere in a file merge cleanly.
