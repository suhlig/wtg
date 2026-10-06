# ADR 0008: go.work Generation

> Converted from a previous version of [`docs/DESIGN.md`](../DESIGN.md). The text is essentially unchanged aside from the framing, metadata, cross-references, and line wrapping.

- Status: Accepted
- Date: 2026-03-27
- Author: Geoff Amey

When `wtg new` or `wtg add` runs, `golang.org/x/mod/modfile` is used to read and write `go.work` files — no shelling out to the `go` binary required.

- Each repo with a `go.mod` at its root gets a `use` directive
- Repos without `go.mod` are silently skipped
- If no repos have `go.mod`, no `go.work` is created
- `go.work` is written at `<space-root>/go.work`
- Only `use` directives are emitted; no `replace` directives
- Paths in `use` directives are relative to the workspace root and mirror the repo nesting:

```
# ~/workspaces/myfeature/go.work
go 1.24

use (
  ./infra             # repos/infra
  ./myorg/api         # repos/myorg/api
  ./myorg/frontend    # repos/myorg/frontend
)
```
