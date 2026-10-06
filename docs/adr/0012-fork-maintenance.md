# ADR 0012: Fork Maintenance

> Converted from a previous version of [`docs/DESIGN.md`](../DESIGN.md). The text is essentially unchanged aside from the framing, metadata, cross-references, and line wrapping.

- Status: Accepted
- Date: 2026-10-04
- Author: Steffen Uhlig

This fork (`github.com/suhlig/wtg`) renames the module path from upstream's `github.com/geoffamey/wtg` so it installs directly with `go install github.com/suhlig/wtg@latest`. Go requires a module's declared path to match the path it is installed from, and a `replace` directive cannot bridge the gap: `replace` only applies in the main module and is ignored by `go install pkg@version`, and the `internal/` package rule blocks aliasing the old path to the new one (a package under `github.com/geoffamey/wtg/internal/...` is not importable from `github.com/suhlig/wtg`).

The rename touches every internal import line, so merging upstream changes conflicts on import blocks. Enable `git rerere` once and Git replays the same resolution automatically on every future merge:

```sh
git config rerere.enabled true
```

Conflicts are limited to hunks that touch import blocks; upstream changes elsewhere in a file merge cleanly.
