# ADR 0005: Repository Discovery

> Converted from a previous version of [`docs/DESIGN.md`](../DESIGN.md). The text is essentially unchanged aside from the framing, metadata, cross-references, and line wrapping.

- Status: Accepted
- Date: 2026-03-27
- Author: Geoff Amey
- Contributors: Steffen Uhlig (updates 2026-08-21, 2026-10-06)

Discovery scans `discovery.root_dir` for directories containing a `.git` entry up to `discovery.max_depth` levels deep. No cache is maintained — the scan is fast at depth 2 and caching introduces staleness problems.

A repo's **short name** is its path relative to `discovery.root_dir`, using `/` as the separator. This naturally disambiguates repos with the same directory name under different parent directories:

```
~/repos/api/.git          → short name: api
~/repos/foo/api/.git      → short name: foo/api
~/repos/bar/api/.git      → short name: bar/api
```

In the standard (non-nested) case the short name is just the directory name, so there is no added complexity for the common workflow. When used in commands like `wtg new`, users can specify `foo/api` to be explicit or `api` if it is unambiguous.

Name resolution is staged, most specific first (`repoInSet` in `internal/cmd/workspace.go`): an exact short name wins outright, then a unique exact basename, then a unique substring match against any path segment — so `infra` finds `foo/infrastructure` and `foo` finds it via the org segment. Each stage is only consulted when the previous one is inconclusive, so typing a repo's exact name never becomes ambiguous merely because another repo contains it as a substring. When a stage yields more than one candidate, `wtg` errors and lists them rather than prompting, keeping commands scriptable (see the convention in `AGENTS.md`).

Repo listings also include the remote `origin` URL to make repos easy to identify:

```
api          https://github.com/myorg/api.git        /Users/geoff/repos/api
foo/api      https://github.com/foo/api.git           /Users/geoff/repos/foo/api
bar/api      https://github.com/bar/api.git           /Users/geoff/repos/bar/api
```
