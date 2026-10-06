# ADR 0011: `wtg config`

> Converted from a previous version of [`docs/DESIGN.md`](../DESIGN.md). The text is essentially unchanged aside from the framing, metadata, cross-references, and line wrapping.

- Status: Accepted
- Date: 2026-06-21
- Author: Geoff Amey
- Contributors: Steffen Uhlig (updates 2026-09-23)

`wtg config` is the config command group. There is no interactive wizard: prompts can't offer shell completion and don't scale as the number of settings grows.

- `wtg config` (no subcommand) prints the resolved config file's raw contents.
- `wtg config init` writes a commented TOML template at `~/.config/wtg/config.toml`, with every setting shown commented out alongside its default. It refuses if the target exists (`--force` overwrites); `-o PATH` redirects output and `-o -` writes to stdout.
- `wtg config edit` opens the resolved config file in `$VISUAL` / `$EDITOR` (or fallbacks).
- `wtg config path` prints the resolved config path (the existing `config.toml`, else a legacy `config.yaml`, else the default `config.toml` path).

Config is TOML-primary. Both TOML and YAML load: the parser is chosen by file extension, so a pre-existing `config.yaml` keeps working without migration.
