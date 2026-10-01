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
```

YAML is still accepted: a file ending in `.yaml`/`.yml` loads via its extension, so an existing `config.yaml` keeps working.

`discovery.root_dir` (or `discovery.root_dirs` for multiple search directories) should contain your regular repo clones, each sitting on their default branch (`main`, `master`, etc.) and otherwise left untouched. `wtg` creates worktrees alongside them — it never modifies the main clones.

Repos are addressed by their slash-separated path relative to their discovery root directory, e.g. `github.com/suhlig/rustomato`. A repo nested under org or group directories can also be addressed by its basename (`rustomato`) as long as no other discovered repo shares that basename; otherwise `wtg` errors and lists the matching repos, and you use the full path to disambiguate.

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
