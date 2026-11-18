+++
title = "CLI Reference"
description = "Exhaustive command and flag reference for the wtg CLI."
weight = 10
+++

# CLI Reference

manage multi-repo feature workflows using git worktrees and Go workspaces

## Global Flags

The following flags apply to `wtg` and can be passed with any command:

| Flag | Alias | Default | Description |
|------|-------|---------|-------------|
| `--config` |  |  | path to config file (default: $XDG_CONFIG_HOME/wtg/config.toml) (env: `"WTG_CONFIG"`) |
| `--dry-run` |  |  | print the plan without executing anything (opt-in per command; see wtg <cmd> --help) |
| `--summary` |  |  | print a list of git and filesystem commands that ran, to stderr after the run |


## Commands

## `wtg config`

**show and manage the wtg config file**

```sh
wtg config
```

With no subcommand, prints the resolved config file's raw contents
(or a note if none exists). Use the subcommands to scaffold a config
file, edit it in an editor, or print its resolved path.

### Subcommands

### `wtg config init`

**write a commented config template**

```sh
wtg config init [flags]
```

Writes a commented TOML config template. Refuses if the target
already exists; pass --force to overwrite. -o sets the output path;
-o - writes to stdout (and never refuses).

#### Flags

| Flag | Alias | Default | Description |
|------|-------|---------|-------------|
| `--force` |  |  | overwrite an existing config file |
| `--output` | `-o` |  | output path, or - for stdout |

### `wtg config edit`

**open the resolved config file in $EDITOR**

```sh
wtg config edit
```

### `wtg config path`

**print the resolved config file path**

```sh
wtg config path
```

## `wtg repo`

**manage repositories**

```sh
wtg repo
```

Operates on the main repo clones (not workspace worktrees). Use these
commands to inspect and update the clones that wtg discovers under
discovery.root_dir.

### Subcommands

### `wtg repo status`

**show status of main repo clones**

```sh
wtg repo status [flags] [<repo>...]
```

Shows branch, dirty status, and ahead/behind counts for each discovered
repo clone. Without arguments, all repos are shown. Pass repo names to
filter the output. Use --long (-l) to also show each repo's remote URL
and local path. A repo argument of "." means the repo containing the
current directory.

#### Flags

| Flag | Alias | Default | Description |
|------|-------|---------|-------------|
| `--long` | `-l` |  | also show remote URL and local path |

### `wtg repo fetch`

**fetch from origin for all repos (no fast-forward)**

```sh
wtg repo fetch [<repo>...]
```

Downloads new commits from origin for each repo without updating any
local branches (equivalent to git fetch origin). Runs in parallel.
A repo argument of "." means the repo containing the current directory.

### `wtg repo sync`

**fetch and fast-forward repos to origin's default branch**

```sh
wtg repo sync [flags] [<repo>...]
```

Fetches origin and fast-forwards each repo's default branch (main, master,
etc.) if it has no uncommitted changes. Repos with uncommitted changes or a
diverged branch are skipped with a warning. Runs in parallel.
Use --progress to watch per-repo progress; ~ means a fetch is in flight.
A repo argument of "." means the repo containing the current directory.

#### Flags

| Flag | Alias | Default | Description |
|------|-------|---------|-------------|
| `--progress` |  |  | show live per-repo progress indicators |

### `wtg repo archive`

**retire main repo clones by moving them to an archive directory**

```sh
wtg repo archive [flags] <repo>...
```

Moves each named main repo clone out of the discovery area into
archive.root_dir, keeping the clone intact, and records where it came from.
Nothing is deleted, and without --remote no remote is touched.
A repo argument of "." means the repo containing the current directory.

Refuses if a clone has uncommitted changes, commits not on any remote, or
stashes unless --force is given. A clone with live (or stale) worktrees, one
referenced by a space, or one listed in always.repos is never archived; --force
does not override these.

When a repo's origin is on github.com, the exact gh command to archive it
upstream is printed at the end. Pass --remote to run that command instead,
archiving each repo upstream as part of the same all-or-nothing operation; it
requires the gh CLI (2.32+) and a github.com origin, and is idempotent (a repo
that is already archived upstream is left as is). Use --dry-run to see the plan
first.

#### Flags

| Flag | Alias | Default | Description |
|------|-------|---------|-------------|
| `--archive-dir` |  |  | override archive.root_dir for this run |
| `--force` |  |  | proceed despite uncommitted, unpushed, or stashed work |
| `--remote` |  |  | also archive each repo on GitHub via the gh CLI |

### `wtg repo unarchive`

**restore archived repo clones to their original locations**

```sh
wtg repo unarchive [flags] <repo>...
```

Moves each named repo clone out of archive.root_dir back to the
exact path it was archived from, and forgets its provenance record.

The local restore never touches a remote. Pass --remote to also unarchive the
repo on GitHub via the gh CLI; it requires the gh CLI (2.32+) and a github.com
origin, and is idempotent (a repo that is already unarchived upstream is left
as is). Use --dry-run to see the plan first.

#### Flags

| Flag | Alias | Default | Description |
|------|-------|---------|-------------|
| `--archive-dir` |  |  | override archive.root_dir for this run |
| `--remote` |  |  | also unarchive each repo on GitHub via the gh CLI |

## `wtg status`

**show status of repos and workspaces**

```sh
wtg status [flags] [<workspace>...]
```

Without arguments, shows a combined view: main repo clone status at the top,
then per-repo status for every workspace. The workspace containing the current
directory is shown first.

Pass one or more workspace names to show those workspaces explicitly.
Use --long (-l) to expand each dirty repo with its individual file changes.
A workspace argument of "." means the workspace containing the current
directory.

### Flags

| Flag | Alias | Default | Description |
|------|-------|---------|-------------|
| `--long` | `-l` |  | show individual modified files per repo |

## `wtg new`

**create a workspace**

```sh
wtg new [flags] <workspace> [<repo>...]
```

Creates a workspace: a directory containing one linked git worktree per
repo, all on the same branch. A go.work file is written automatically when
any of the included repos have a go.mod.

At least one repo must be specified. If the branch already exists in a repo
it is checked out as-is — no reset or rebase is performed.
If the branch already exists in a repo it is checked out as-is — no reset or rebase is performed.

--read-only <repo> adds a repo as a member that is consumed, not changed: a
detached worktree at a pinned ref, with no branch of its own (see wtg add).
Repeat the flag for several repos; the positional repos stay ordinary
members, so at least one positional repo is still required. Without --ref the
ref is the version the positional repos' go.mod files select, else
origin/<default branch>. --ref applies to every --read-only repo.

A repo argument of "." means the repo containing the current directory; it is
accepted as a --read-only value too.

Any always.repos are symlinked into the workspace, any always.files are copied
into its root, always.secrets are seeded into each worktree when present in
the source repo, and any always.run hook fires once the workspace is created.

### Flags

| Flag | Alias | Default | Description |
|------|-------|---------|-------------|
| `--base` |  |  | commit-ish to branch from (default: HEAD); origin/<branch> is also recorded as the base branch for rebase and land |
| `--branch` |  |  | branch name (default: <git.branch_prefix><workspace>) |
| `--path` |  |  | workspace root path (default: <spaces.root_dir>/<workspace>) |
| `--read-only` |  |  | add <repo> as a read-only member: a detached worktree at a pinned ref (repeatable) |
| `--ref` |  |  | ref the read-only members are pinned to or follow (requires --read-only) |

## `wtg delete`

**delete a workspace and optionally its branches**

```sh
wtg delete [flags] [<workspace>]
```

Removes all worktrees in the workspace. By default, branches are left
untouched — use --delete-branch (-d) to delete merged branches or
--force-delete-branch (-D) to force-delete regardless of merge state.
A read-only member has no branch of its own, so -d and -D do not apply to it.

Prompts for confirmation if any repo has uncommitted changes or commits that
exist on no remote. Pass --yes to answer the prompt in scripts; without --yes
and without a terminal, delete refuses. A declined prompt exits non-zero, so
"wtg land && wtg delete && ..." stops. -d deletes a branch when no commit
exists only on it; a merged pull request for the branch counts as upstream
too, so a squash- or rebase-merged branch can be deleted with -d.

A repo whose always.secrets copy differs from the main clone (or whose source
no longer exists) is kept, with its branch, and delete exits non-zero; resolve
the difference and run delete again.

The workspace root directory is removed if it is empty after worktrees are
cleaned up.

The workspace argument is optional when the current directory is inside a
known workspace — it will be inferred automatically. Pass "." to name the
workspace containing the current directory.

### Flags

| Flag | Alias | Default | Description |
|------|-------|---------|-------------|
| `--delete-branch` | `-d` |  | delete branch if fully merged into upstream |
| `--force-delete-branch` | `-D` |  | force-delete branch regardless of merge state |
| `--yes` | `-y` |  | answer the confirmation prompt with yes (required when stdin is not a terminal) |

## `wtg add`

**add repos to a workspace**

```sh
wtg add [flags] [<workspace>] <repo>...
```

Creates a new worktree for each specified repo inside an existing workspace,
checking out the workspace's branch. Updates go.work automatically if the
workspace already has one.

If the branch already exists in a repo (locally or on the remote) it is
checked out as-is — no reset or rebase is performed.

--base names the commit-ish a new branch is created from. When it is
origin/<branch>, that branch is recorded as the repo's base, so wtg rebase
and wtg land target it instead of the repo's default branch.

Paths in always.secrets are copied from each source repo into its new
worktree when present.

With --read-only, a repo is added as a member that is consumed, not changed:
a detached worktree at a pinned ref, listed in go.work if it has a go.mod,
and invisible to push and land. Without --ref the ref is the version the other
members' go.mod files select, else origin/<default branch>. A plain
wtg add without --read-only on a read-only member promotes it to an ordinary
worktree.

A repo argument of "." means the repo containing the current directory.

The workspace argument is optional when the current directory is inside a
known workspace — it will be inferred automatically.

### Flags

| Flag | Alias | Default | Description |
|------|-------|---------|-------------|
| `--base` |  |  | commit-ish to create the branch from (default: HEAD); origin/<branch> is also recorded as the base branch for rebase and land |
| `--read-only` |  |  | add repos as read-only members: a detached worktree at a pinned ref |
| `--ref` |  |  | ref a read-only member is pinned to or follows (requires --read-only) |

## `wtg remove`

**remove repos from a workspace**

```sh
wtg remove [flags] [<workspace>] <repo>...
```

Removes the specified repos' worktrees from a workspace and updates go.work.
By default, branches are left untouched — use --delete-branch (-d) or
--force-delete-branch (-D) to also delete them.

Prompts for confirmation if any repo has uncommitted changes or unpushed
commits. To remove all repos at once, use wtg delete instead. -d deletes a
branch whose work is upstream, including a merged pull request, which
git's own ancestry check cannot see after a squash or rebase merge.

A repo argument of "." means the repo containing the current directory.

The workspace argument is optional when the current directory is inside a
known workspace — it will be inferred automatically.

### Flags

| Flag | Alias | Default | Description |
|------|-------|---------|-------------|
| `--delete-branch` | `-d` |  | delete branch if fully merged into upstream |
| `--force-delete-branch` | `-D` |  | force-delete branch regardless of merge state |

## `wtg exec`

**run a command in each repo of a workspace**

```sh
wtg exec [flags] [<workspace>] -- <cmd> [<args>...]
```

Runs a command in each repo's worktree, streaming output as it goes.
A header line identifies each repo. Execution continues even if a command
fails — all repos are attempted and failures are reported at the end.

With --parallel, all commands run concurrently. A progress indicator shows
live status. Output for each repo is buffered and printed serially in repo
order once all commands complete. If a command in one repo runs long, the
progress line names it.

Use --timeout to stop a command that runs longer than a given time in any repo
(a Go duration such as 90s or 10m). That repo is reported as timed out and the
others carry on. There is no timeout by default: exec runs your own commands,
and only you know how long they should take.

The workspace argument is optional when the current directory is inside a
known workspace — it will be inferred automatically. Use -- to separate the
workspace name from the command:

   wtg exec myfeature -- git status
   wtg exec myfeature -- go test ./...
   wtg exec -- git status            # workspace inferred from CWD

### Flags

| Flag | Alias | Default | Description |
|------|-------|---------|-------------|
| `--parallel` |  |  | run the command in all repos concurrently |
| `--timeout` |  |  | stop the command in a repo after this long, e.g. 90s (default: no limit) |

## `wtg push`

**push all branches in a workspace to origin**

```sh
wtg push [<workspace>]
```

Pushes the workspace's branch from each repo's worktree to origin in
parallel. Repos that fail are reported individually; others are not affected.

The workspace argument is optional when the current directory is inside a
known workspace — it will be inferred automatically. Pass "." to name the
workspace containing the current directory.

## `wtg rebase`

**rebase all branches in a workspace onto their base branch**

```sh
wtg rebase [flags] [<workspace>]
```

Fetches origin and rebases the workspace's branch onto origin/<base branch> in
every repo's worktree, in parallel, with --autostash so that uncommitted
changes are carried along. The base is the branch recorded when the workspace
was created (wtg new, or wtg add --base origin/<branch>), else the repo's
default branch; use --onto to name one branch for all repos.

A conflict aborts that repo's rebase and restores the worktree (including
uncommitted changes); the retry command is printed. Other repos are not
affected. Nothing is ever pushed.

The workspace argument is optional when the current directory is inside a
known workspace — it will be inferred automatically.
Use --progress to watch per-repo progress; ~ means a repo is busy.
Pass "." to name the workspace containing the current directory.

A read-only member is not rebased: one that follows origin/<branch> is
advanced, a pinned one is left alone, and a modified one is skipped.

### Flags

| Flag | Alias | Default | Description |
|------|-------|---------|-------------|
| `--onto` |  |  | rebase onto origin/<branch> instead of each repo's base branch |
| `--progress` |  |  | show live per-repo progress indicators |

## `wtg land`

**publish each repo's branch as a merge or pull request**

```sh
wtg land [flags] [<workspace>]
```

For every repo in the workspace that has commits beyond origin/<base>,
either fast-forwards the remote base branch (strategy "merge") or
pushes the branch and creates a pull request (strategy "pr"). The base is the
branch recorded at new/add time (--base origin/<branch>), else the repo's
default branch.

Strategy is read from git config wtg.landStrategy (per-clone or via an
includeIf rule in ~/.gitconfig). The command-line flag overrides it.
When no setting is found, "pr" is used.

After all PRs are created, each one is updated with a sibling block
listing the other PRs in the workspace (use --no-link to suppress).

--delete runs wtg delete on the workspace when nothing needs attention,
leaving every branch in place. --delete-remote-branch removes origin/<branch>
after a merge-landing, where the commits are on the base branch already.

The workspace argument is optional when run from inside a workspace.

### Flags

| Flag | Alias | Default | Description |
|------|-------|---------|-------------|
| `--delete` |  |  | delete the workspace once every repo landed (branches are kept) |
| `--delete-remote-branch` |  |  | after a merge-landing, delete origin/<branch> (merge strategy only) |
| `--draft` |  |  | create pull requests as drafts (pr strategy only) |
| `--no-link` |  |  | skip linking sibling pull requests |
| `--strategy` |  |  | override land strategy for all repos ("merge" or "pr") |

## `wtg path`

**print the root path of a workspace**

```sh
wtg path <workspace>
```

Prints the absolute root path of a workspace to stdout. This backs the
wcd shell helper, which uses it to cd into a workspace by name.

