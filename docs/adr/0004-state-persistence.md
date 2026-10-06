# ADR 0004: State Persistence

> Converted from a previous version of [`docs/DESIGN.md`](../DESIGN.md). The text is essentially unchanged aside from the framing, metadata, cross-references, and line wrapping.

- Status: Accepted
- Date: 2026-03-27
- Author: Geoff Amey

Space metadata is stored in `~/.local/share/wtg/spaces/<name>.yaml` (XDG data directory).

**Why not inside the workspace directory?** If the workspace directory is deleted externally (e.g. `rm -rf ~/workspaces/myfeature`), the space would disappear from the tool's awareness entirely. Storing state separately allows the tool to detect and report missing worktrees rather than silently losing track.

**Why a state file at all?** Git worktrees record per-repo state (path, branch, HEAD), but git has no concept of a "space" — grouping worktrees across multiple repos is entirely this tool's abstraction. The state file is the only place that knows which repos belong to which space. It is kept intentionally minimal.

## Space state schema

The Go type (`state.Space` in `internal/state/state.go`) is authoritative. A written file looks like:

```yaml
name: myfeature
path: /Users/geoff/workspaces/myfeature   # absolute, tilde-expanded
branch: geoff/myfeature                   # branch shared across all repos in this space
created_at: 2026-03-27T10:00:00Z
repos:
  - name: myorg/api                       # short name (relative to discovery.root_dir)
    repo_path: /Users/geoff/repos/myorg/api
    worktree_path: /Users/geoff/workspaces/myfeature/myorg/api
    symlink: true                         # present for always.repos entries: a symlink to the main clone, not a worktree
go_workspace: true                        # whether a go.work was generated
```

## State validation and auto-heal

State files are hints, not ground truth. Every command that reads space state validates against the actual filesystem and git state before acting.

| What state says | What reality shows | Action |
|---|---|---|
| Worktree exists | Path ✓, `git worktree list` ✓ | Healthy — proceed |
| Worktree exists | Path ✗, stale git entry | Run `git worktree prune`; warn user that data is gone |
| Worktree exists | Path ✓, not in git | Attempt `git worktree repair`; if git < 2.29, error with manual instructions |
| Worktree exists | Path ✗, not in git | Remove from state, warn user |

If a worktree directory is deleted externally, the data inside it is gone. There is nothing to restore. "Repair" in this context means cleaning up stale references, not recovering data.

**`git worktree repair`** (added in git 2.29) is attempted automatically in the relevant case. If git is too old, the error output from git will include the unknown-subcommand message; `wtg` catches this and prints manual recovery instructions instead.
