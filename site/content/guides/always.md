+++
title = "Always-Included Repos, Files, and Hooks"
description = "Configure shared repos, files, secrets, and lifecycle hooks for every new space."
weight = 80

[extra]
nav_title = "Always Repos & Hooks"
+++

# `always`: shared repos, files, secrets, and event hooks

The `always` section of your config lets you say "every space should have these
repos, these files, these secrets, and run this script" once, instead of
repeating it on every `wtg new`. It has four independent keys:

```yaml
always:
  repos: [shared-tooling, protos]        # symlinked into every new space
  files: [~/.config/wtg/CLAUDE.md]       # copied into every new space root
  secrets: [.env, config/local.env]      # copied into each worktree when present
  run: ~/.config/wtg/on-event            # executable run on lifecycle events
```

All four are optional and can be used on their own. `~/` is expanded in
`files` entries and in `run` (not in `secrets`, which are repo-relative).

## `always.repos`

Repos you almost always want available in a space but rarely want to put on the
feature branch: a shared tooling repo, a protobuf/schema repo, design docs, a
scratch area. Listing them in `always.repos` adds each one to every new space as
a **symlink to your main clone**, not as a worktree.

```yaml
always:
  repos: [shared-tooling]
```

A new space then has the worktree for `api` next to a symlink to `shared-tooling`, plus `go.work` (see [See it run](#see-it-run)):

```
my-feature/
  api/             ← worktree on branch <prefix>my-feature
  shared-tooling/  → symlink to ~/repos/shared-tooling
  go.work
```

What the symlink means in practice:

- It points at the main clone, so it stays on whatever branch that clone is on.
  There is no per-space worktree and no feature branch for it.
- It is excluded from `go.work` (it is not a feature-branch checkout), and from
  the dirty/unpushed safety checks during `remove`/`delete`.
- It is shared: edits show up across every space that symlinks the same repo,
  because they are all the same working copy.

If a particular feature *does* need to touch a shared repo on the branch, you
have two ways to get a real worktree for it:

- Name it explicitly on `wtg new` (`wtg new my-feature api shared-tooling`) — an
  explicit repo always wins over the symlink.
- Upgrade it later with `wtg add my-feature shared-tooling` — this replaces the
  symlink with a worktree on the space's branch.

`wtg remove` does the inverse: removing a repo that is in `always.repos` restores the symlink rather than dropping the repo from the space. Removing a repo that is *currently a symlink* leaves it out (you have opted it out for that space).

### How this differs from a read-only member

`always.repos` and `wtg add --read-only` both avoid a feature branch, but they answer different questions:

| | `always.repos` symlink | `--read-only` member |
|---|---|---|
| What is there | the main clone itself | a detached worktree of its own |
| Version | whatever the main clone has today | the ref you pinned or follow |
| In `go.work` | no | yes |
| Shared across spaces | yes, one working copy | no, one checkout per space |
| `wtg repo sync` / your edits | change every space at once | change this space only |

Use `always.repos` for something every space should simply have — tooling, schemas, notes. Use `--read-only` for a dependency the build reads, because only a member in `go.work` takes part in the build, and only a worktree can be pinned (see [ADR 0024](../../../docs/adr/0024-read-only-workspace-members.md)).

A repo can move between the two: `wtg add --read-only <repo>` replaces the symlink of an `always.repos` entry with a pinned worktree, and `wtg remove` puts the symlink back if the repo is still configured in `always.repos`.

## `always.files`

Files seeded into the root of every new space. They are **copied**, not
symlinked, so per-space edits stay local and never write back to the source.

```yaml
always:
  files:
    - ~/.config/wtg/CLAUDE.md      # an AI assistant context file
    - ~/.config/wtg/.envrc         # direnv config
```

A new space then has the copies in its root:

```
my-feature/
  api/
  CLAUDE.md        ← copy of ~/.config/wtg/CLAUDE.md
  .envrc           ← copy of ~/.config/wtg/.envrc
  go.work
```

Notes:

- The copy is placed at `<space-root>/<basename>`, so only the file name matters
  at the destination — `~/templates/wtg/CLAUDE.md` lands as `CLAUDE.md`.
- These copies are removed when you `wtg delete` the space.
- Good fits: a `CLAUDE.md`/`AGENTS.md` template, `.envrc`, `.editorconfig`, a
  scratch `TODO.md`, a `justfile`. Anything you want present and editable per
  space.

## `always.secrets`

Relative paths looked up in each **source repo** and copied into that repo's
**worktree** when the file exists. Use this for local files that live in your
main clones but are not on the feature branch — `.env`, machine-local config,
and similar.

```toml
[always]
secrets = [".env", "config/local.env"]
```

If `~/repos/api` has a `.env` and `~/repos/docs` does not, a new space with both gets:

```
my-feature/
  api/.env         ← copy of ~/repos/api/.env
  docs/            ← no .env (missing in source; skipped)
```

Notes:

- Paths are relative to the source repo root and preserved under the worktree
  (no absolute paths, no `..`).
- Missing files are skipped per repo; directories are a hard error.
- Existing destination files are overwritten.
- Symlinked `always.repos` are skipped (the symlink already points at the main
  clone). Upgrading a symlink to a worktree with `wtg add` does apply secrets.
- Unlike `always.files` (global absolute paths → space root), secrets are
  repo-relative and land inside each worktree.

## `always.run`

A hook script that `wtg` runs after a space operation completes. Use it to wire
spaces into the rest of your environment: trust a direnv file, open an editor,
register the space with another tool, send yourself a notification.

```yaml
always:
  run: ~/.config/wtg/on-event
```

The script is invoked **directly** (not through a shell), so it must be
executable (`chmod +x`) and have a valid shebang.

### When it runs

| Event | Trigger |
|---|---|
| `create` | `wtg new` completed |
| `add` | `wtg add` completed |
| `remove` | `wtg remove` completed |
| `delete` | `wtg delete` completed |

It runs **after** the operation has already succeeded, and it is best-effort: if
the script exits non-zero, `wtg` prints a warning but the operation still
succeeds (your space is already created/changed). It is not part of the
operation's rollback.

### What it receives

Context is passed via environment variables:

| Variable | Value |
|---|---|
| `WTG_SPACE_NAME` | space name, e.g. `my-feature` |
| `WTG_SPACE_ROOT` | absolute path to the space directory |
| `WTG_SPACE_BRANCH` | branch name, e.g. `alice/my-feature` |
| `WTG_SPACE_EVENT` | `create`, `add`, `remove`, or `delete` |
| `WTG_REPOS` | newline-separated short names of all repos in the space |
| `WTG_REPO_PATHS` | newline-separated absolute paths, aligned with `WTG_REPOS` |
| `WTG_EVENT_REPOS` | newline-separated repos this event affected (added/removed) |

For `create`, `WTG_EVENT_REPOS` is every repo in the new space. For `add`/`remove`
it is just the repos added or removed. For `delete`, `WTG_REPOS` and
`WTG_EVENT_REPOS` reflect the repos that were in the space before it was deleted
(the space no longer exists afterwards).

### Example: dispatch on event

```bash
#!/usr/bin/env bash
# ~/.config/wtg/on-event

case "$WTG_SPACE_EVENT" in
  create)
    # Trust the .envrc that always.files dropped in, and open the space.
    command -v direnv >/dev/null && (cd "$WTG_SPACE_ROOT" && direnv allow)
    code "$WTG_SPACE_ROOT"
    ;;
  add)
    echo "Added to $WTG_SPACE_NAME: $WTG_EVENT_REPOS" >&2
    ;;
  delete)
    osascript -e "display notification \"deleted $WTG_SPACE_NAME\" with title \"wtg\""
    ;;
esac
```

### Example: act on each repo

```bash
#!/usr/bin/env bash
# Run a setup step in every repo of a freshly created space.
[ "$WTG_SPACE_EVENT" = create ] || exit 0

while IFS= read -r path; do
  [ -f "$path/package.json" ] && (cd "$path" && pnpm install)
done <<< "$WTG_REPO_PATHS"
```

### Example: a hooks directory

`always.run` is a single script, but the `WTG_*` variables are exported, so any
program it runs inherits them. That makes a `run-parts`-style dispatcher a few
lines: point `always.run` at this, and drop independent hooks into `hooks.d/`.

```bash
#!/usr/bin/env bash
# always.run → runs every executable in hooks.d, sorted by name.
hooks="${WTG_HOOKS_DIR:-$HOME/.config/wtg/hooks.d}"
[ -d "$hooks" ] || exit 0
for h in "$hooks"/*; do
  [ -f "$h" ] && [ -x "$h" ] || continue   # skip dirs, non-executables, backup files
  "$h" || echo "wtg hook failed: $h (exit $?)" >&2
done
```

Each hook in `hooks.d` sees the full environment and does its own
`case "$WTG_SPACE_EVENT"` branching. Name them `10-direnv`, `20-editor`, etc. to
control order. The `|| echo` keeps it best-effort, so one failing hook does not
stop the rest — matching how wtg treats the top-level script.

### Example: one tmux window per repo

wtg itself does not use tmux, but a hook can give every space its own tmux
session with one window per repo, and keep the windows in step as repos come and
go. This is only an example for people who use tmux; adapt the window names and
layout to taste.

```bash
#!/usr/bin/env bash
# One tmux session per space, one window per repo.
session="$WTG_SPACE_NAME"

case "$WTG_SPACE_EVENT" in
  create)
    tmux new-session -d -s "$session" -c "$WTG_SPACE_ROOT" -n root
    while IFS= read -r path; do
      tmux new-window -t "=$session:" -c "$path" -n "$(basename "$path")"
    done <<< "$WTG_REPO_PATHS"
    tmux select-window -t "=$session:root"
    ;;
  add)
    tmux has-session -t "=$session" 2>/dev/null || exit 0
    existing=$(tmux list-windows -t "=$session" -F '#{window_name}')
    # WTG_REPOS and WTG_REPO_PATHS are aligned, so paste pairs each name with its path.
    paste -d '\t' <(echo "$WTG_REPOS") <(echo "$WTG_REPO_PATHS") |
      while IFS=$'\t' read -r repo path; do
        grep -qxF -- "$repo" <<< "$WTG_EVENT_REPOS" || continue   # not added by this event
        name=$(basename "$path")
        grep -qxF -- "$name" <<< "$existing" && continue          # already has a window
        tmux new-window -t "=$session:" -c "$path" -n "$name"
      done
    ;;
  remove)
    tmux has-session -t "=$session" 2>/dev/null || exit 0
    while IFS= read -r repo; do
      tmux kill-window -t "=$session:=$(basename "$repo")" 2>/dev/null
    done <<< "$WTG_EVENT_REPOS"
    ;;
  delete)
    tmux kill-session -t "=$session" 2>/dev/null
    ;;
esac
```

Windows are named after the last path component of the repo, so a repo called
`myorg/frontend` gets a window called `frontend`. The `add` and `remove` branches
do nothing if the session does not exist, for example because it was closed by hand.

## See it run

The config below sets all four keys. `api` has a local `.env` that is not on any branch; `docs` has none. The hook is a one-line script that prints the event and the space name.

```toml
[always]
repos = ["shared-tooling"]
files = ["~/.config/wtg/CLAUDE.md"]
secrets = [".env"]
run = "~/.config/wtg/on-event"
```

Create a workspace with two repos. `shared-tooling` comes along as a symlink, and the hook runs once the space exists:

{% set id = "always" %}{% set step = 1 %}{% include "transcript.html" %}

The agent file was copied into the space root, and `shared-tooling` is a symlink next to the worktrees:

{% set id = "always" %}{% set step = 2 %}{% include "transcript.html" %}

The secret was copied into the worktree of the repo that has it, and skipped for the one that does not:

{% set id = "always" %}{% set step = 3 %}{% include "transcript.html" %}

{% set id = "always" %}{% set step = 4 %}{% include "transcript.html" %}

Deleting the workspace removes the copies and the symlink, and the hook runs again with the `delete` event:

{% set id = "always" %}{% set step = 5 %}{% include "transcript.html" %}

## Putting it together

```toml
[discovery]
root_dir = "~/repos"

[spaces]
root_dir = "~/spaces"

[git]
branch_prefix = "alice/"

[always]
repos = ["shared-tooling"]
files = ["~/.config/wtg/CLAUDE.md", "~/.config/wtg/.envrc"]
run = "~/.config/wtg/on-event"
```

With this config, `wtg new my-feature api` creates the `api` worktree, symlinks
`shared-tooling`, copies `CLAUDE.md` and `.envrc` into the space root, and then
runs `on-event` with `WTG_SPACE_EVENT=create`.

Scaffold a config file with these settings (commented out) using `wtg config init`.
