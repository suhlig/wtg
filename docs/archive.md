# `archive`: retiring and restoring repo clones

`wtg repo archive` retires a main repo clone without deleting it: it moves the clone out of the discovery area into `archive.root_dir`, keeps it intact, and records where it came from. `wtg repo unarchive` reverses that, putting the clone back exactly where it was. Both are safe, batchable, and scriptable — there is no interactive prompt, so refusal is the default and flags are the override.

The problem it solves: discovery treats every clone under `discovery.root_dir` as live, so a finished or shelved project keeps showing up in `wtg repo status`/`sync`, in `wtg new` repo completion, and everywhere else. Deleting the clone is destructive and irreversible; archiving moves it aside instead, preserving unpushed commits, stashes, and untracked files.

## The mental model

Archiving is a **rename**, not a copy and not a delete:

- The whole clone directory moves, so nothing is lost and the operation is instant.
- Because it is a rename, `archive.root_dir` must be on the **same filesystem** as your repos. Across filesystems, `rename(2)` fails with `EXDEV` and `wtg` refuses rather than silently falling back to a slow, lossy copy.
- The archive root must be **outside every discovery root**, or the moved clone would be rediscovered as a live repo. The tempting `~/git/archived` is rejected when `~/git` is a discovery root; use a sibling such as `~/git-archived`.

The layout mirrors the short-name layout used everywhere else, so the archive is browsable and its inverse is obvious:

```
~/repos/github.com/something/foo          # before
~/repos-archived/github.com/something/foo # after
```

## Configuration

```toml
[archive]
root_dir = "~/repos-archived"   # where `wtg repo archive` moves retired clones
```

`archive.root_dir` defaults to `~/repos-archived` (parallel to the default discovery root `~/repos`). `~` is expanded. It must be absolute, outside every discovery root, and on the same filesystem as your repos. Pass `--archive-dir DIR` to override it for a single run.

## `wtg repo archive <repo>...`

```sh
wtg repo archive old-service spike-repo   # move the clones aside
wtg repo archive old-service --dry-run    # show the plan, change nothing
wtg repo archive old-service --force      # proceed despite pending work
wtg repo archive old-service --remote     # also archive it on GitHub
```

Repos are resolved by the same staged matching as the other commands: exact short name, then unique basename, then a unique substring of any path segment. An ambiguous name errors and lists the candidates; it never prompts.

### What it checks

Every requested repo is checked **before any filesystem change**. If any check fails, the whole invocation aborts and nothing moves (all-or-nothing).

Pending work is a **warning**: it aborts the run unless you pass `--force`. Archiving does not lose this work (it moves with the clone), but the warning keeps out-of-sight work from being forgotten — and, once a repo is archived upstream, that work cannot be pushed without unarchiving first.

| Finding | Severity |
|---|---|
| Uncommitted changes | warning (`--force` to proceed) |
| Commits on any local branch that exist on no remote | warning (`--force` to proceed) |
| Stashes | warning (`--force` to proceed) |

The "not on any remote" check runs `git rev-list --count --branches --not --remotes`, so it catches commits on branches other than the one currently checked out — which a plain status would miss. (It is skipped for a clone with no origin, where every commit would otherwise be flagged.)

Everything else is a **hard error** that `--force` does not override, with a message naming the fix:

| Finding | Why it blocks |
|---|---|
| Live linked worktrees | their data would be orphaned; the message names each path and the `wtg remove`/`wtg delete` that clears it |
| Stale worktree entries (registered but missing on disk) | refused, with a `git -C <clone> worktree prune` suggestion, rather than silently mutating |
| Referenced by a space's state | catches a space entry whose worktree was deleted out from under it |
| Listed in `always.repos` | it is symlinked into every new space |
| Destination already exists under `archive.root_dir` | would clobber an existing tree |

### Provenance

Each successful archive appends a record to `~/.local/share/wtg/archived.yaml` (i.e. `$XDG_DATA_HOME/wtg/archived.yaml`), alongside the space state. Keeping it in XDG data rather than inside the archive tree keeps cold storage clean and lets `unarchive` restore a clone to its **exact** origin even when discovery uses multiple roots.

```yaml
version: 1
entries:
  - name: github.com/something/foo
    origin: /Users/you/repos/github.com/something/foo
    remote: git@github.com:something/foo.git
    host: github.com
    archived_at: 2026-10-06T12:00:00Z
```

`name` is the short name, `origin` is the exact discovery path it was moved from, and `remote`/`host` are derived from the clone's `origin` remote (used by `--remote`). The schema is versioned by the top-level `version` field.

### Output

```
github.com/something/foo  ✓ moved to ~/repos-archived/github.com/something/foo
github.com/something/bar  ✓ moved to ~/repos-archived/github.com/something/bar

Upstream repos were not modified. To archive them on GitHub:
  gh repo archive something/foo --yes
  gh repo archive something/bar --yes
```

The `gh` block appears only when at least one repo's `origin` is on github.com; other forges are simply omitted in the local (no `--remote`) flow.

## `--remote`: archive upstream on GitHub

`wtg repo archive --remote` also archives each repo on GitHub, via the [`gh` CLI](https://cli.github.com), as part of the same all-or-nothing operation.

- **Requires `gh` 2.32 or newer**, installed and authenticated for github.com (a `GH_TOKEN`/`GITHUB_TOKEN` counts). `wtg` verifies both before moving anything.
- **Every named repo must have a github.com origin.** If any does not (GitLab, Bitbucket, a self-hosted host, or no origin at all), the whole command aborts rather than silently archiving only some of them. Other forges are not supported yet.
- **Idempotent.** `wtg` checks each repo's upstream archive state first; one that is already archived is a no-op and reported as such.
- **Remote changes roll back with the local ones.** The local moves and the upstream archival are one saga: if an upstream archival fails, the moved clones are restored and the provenance records are removed, so nothing is left half-archived.

```sh
wtg repo archive old-service --remote
# github.com/something/foo  ✓ moved to ~/repos-archived/github.com/something/foo
#
# Archived on GitHub:
#   something/foo
```

`gh` targets github.com explicitly (via `GH_HOST`), so a GitHub Enterprise default in your `gh` config cannot redirect an archival.

## `wtg repo unarchive <repo>...`

```sh
wtg repo unarchive old-service spike-repo   # restore the clones
wtg repo unarchive old-service --dry-run    # show the plan, change nothing
wtg repo unarchive old-service --remote     # also unarchive on GitHub
```

`unarchive` looks the repo up in `archived.yaml`, moves the clone from `<archive.root_dir>/<name>` back to its recorded `origin`, and removes the provenance record. It never deletes anything.

It **refuses** if the recorded origin path already exists, or if the archived clone is missing from `archive.root_dir` — the latter happens if you changed your archive root, in which case pass `--archive-dir DIR`. Restoration is non-destructive, so every finding is a hard error (there is no `--force`).

```
github.com/something/foo  ✓ restored to ~/repos/github.com/something/foo

Upstream repos were not modified. To unarchive them on GitHub:
  gh repo unarchive something/foo --yes
```

### `--remote`

`wtg repo unarchive --remote` also unarchives each repo on GitHub, under the same rules as `--remote` on archive: `gh` 2.32+ and github.com auth, a github.com origin for every named repo, idempotency (a repo that is already unarchived upstream is a no-op), and the same all-or-nothing saga — if an upstream unarchive fails, the restores are rolled back and the provenance records are restored.

```
github.com/something/foo  ✓ restored to ~/repos/github.com/something/foo

Unarchived on GitHub:
  something/foo
```

## Releasing an archived clone by hand

The archive tree is plain directories, and the provenance file is plain YAML, so you can always recover manually: move the directory back yourself and delete its entry from `~/.local/share/wtg/archived.yaml`. `unarchive` just does this for you, resolves the names, and reverses the upstream state.

## Putting it together

```sh
# Retire two finished repos, archiving them upstream too.
wtg repo archive old-service spike-repo --remote

# Later, bring one back, locally and upstream.
wtg repo unarchive old-service --remote
```

See [ADR 0013](adr/0013-repo-archive.md) for the decisions and trade-offs (why a move rather than a delete, why provenance lives in XDG state, why stale worktrees are refused rather than pruned).
