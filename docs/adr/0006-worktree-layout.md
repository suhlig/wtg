# ADR 0006: Worktree Layout

> Converted from a previous version of [`docs/DESIGN.md`](../DESIGN.md). The text is essentially unchanged aside from the framing, metadata, cross-references, and line wrapping.

- Status: Accepted
- Date: 2026-03-27
- Author: Geoff Amey
- Contributors: Steffen Uhlig (updates 2026-08-21)

Workspace directories mirror the repo short name structure (relative path from `discovery.root_dir`). For non-nested repos the result is a flat directory. For nested repos the org-level directory is created automatically.

```
<space-root>/          # default: <spaces.root_dir>/<space-name>; overridable with --path
  <repo-short-name>/   # mirrors nesting from discovery.root_dir
  go.work
```

Examples:

```
# Non-nested repos (standard case) — flat workspace:
~/workspaces/myfeature/
  api/              ← repos/api
  frontend/         ← repos/frontend
  go.work

# Nested repos — org-level subdirectory created:
~/workspaces/myfeature/
  myorg/
    api/            ← repos/myorg/api
    frontend/       ← repos/myorg/frontend
  otherapg/
    api/            ← repos/otherapg/api
  go.work

# Mixed (common in practice):
~/workspaces/myfeature/
  infra/            ← repos/infra  (non-nested)
  myorg/
    api/            ← repos/myorg/api
    frontend/       ← repos/myorg/frontend
  go.work
```

This eliminates any collision risk: two repos with the same leaf name but different org paths produce distinct worktree directories, matching the short name that identified them.

When a worktree is removed (`wtg delete`, `wtg remove`, or a rollback), the now-empty org/group directories are pruned so the space root stays clean and removable.

The default space root is `<spaces.root_dir>/<space-name>`. The `--path` flag on `wtg new` overrides this for the specific space being created; `wtg add` inherits the path stored in the space state.
