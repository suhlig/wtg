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

[archive]
root_dir = "~/repos-archived"  # where `wtg repo archive` moves retired clones
```

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

### `wtg new <workspace> <repo>...`

Create a workspace. At least one repo must be specified. For each repo, `wtg`
creates or checks out a branch named `<branch_prefix><workspace>` as a linked
worktree. A `go.work` file is written automatically for repos that have a
`go.mod`. Paths listed in `always.secrets` are copied from each source repo into
its worktree when present (see [docs/always.md](docs/always.md)).

```sh
wtg new my-feature api payments frontend
wtg new my-feature api --branch yourname/main  # check out an existing branch
```

Branch behaviour per repo:

| Branch state | Action |
|---|---|
| Does not exist | Created from the repo's default branch |
| Exists, not checked out | Checked out in the new worktree |
| Exists, already checked out | Error |

### `wtg delete <workspace>`

Delete a workspace and remove its worktrees. Prompts for confirmation if any
repo has uncommitted changes or unpushed commits.

```sh
wtg delete my-feature            # remove worktrees, keep branches
wtg delete my-feature -d         # also delete branches if merged
wtg delete my-feature -D         # force-delete branches
```

### `wtg add [<workspace>] <repo>...`

Add repos to an existing workspace. Creates worktrees on the workspace's branch
and updates `go.work`. When run from inside a workspace directory, the workspace
argument can be omitted.

```sh
wtg add my-feature infra logging
wtg add infra logging             # workspace inferred from CWD
```

### `wtg remove [<workspace>] <repo>...`

Remove repos from a workspace. Prompts if there are uncommitted changes or
unpushed commits. Use `wtg delete` to remove the whole workspace. When run from
inside a workspace directory, the workspace argument can be omitted.

```sh
wtg remove my-feature logging
wtg remove my-feature logging -d  # also delete the branch
wtg remove logging                # workspace inferred from CWD
```

### `wtg push [<workspace>]`

Push the workspace's branch from each repo's worktree to origin in parallel.
When run from inside a workspace directory, the workspace argument can be
omitted.

```sh
wtg push my-feature
wtg push                          # workspace inferred from CWD
```

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

### `wtg exec [<workspace>] [--parallel] -- <cmd> [<args>...]`

Run a command in each repo's worktree sequentially, streaming output as it goes. With `--parallel`, commands run in all repos concurrently with a live progress indicator, buffering per-repo output until complete.

Execution continues even if a command fails — all repos are attempted and failures are reported at the end. When run from inside a workspace directory, the workspace argument can be omitted.

```sh
wtg exec my-feature -- git status
wtg exec my-feature --parallel -- go test ./...
wtg exec -- git status            # workspace inferred from CWD
```

## Repo commands

These operate on your main repo clones, not workspace worktrees. Useful for keeping clones up to date before starting a new feature.

### `wtg repo sync [<repo>...]`

Fetch and fast-forward each repo's default branch. Repos with local changes are skipped with a warning.

```sh
wtg repo sync               # sync all repos
wtg repo sync api payments  # sync specific repos
```

### `wtg repo status [<repo>...]`

Show branch, dirty status, and ahead/behind counts for each main repo clone.

```sh
wtg repo status
wtg repo status --long  # also show remote URL and local path
```

### `wtg repo archive <repo>...`

Retire one or more main repo clones: each is moved out of the discovery area into `archive.root_dir`, kept intact, and recorded (with its original path) under `~/.local/share/wtg/archived.yaml`. Nothing is deleted and, without `--remote`, no remote is touched.

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
