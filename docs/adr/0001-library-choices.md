# ADR 0001: Library Choices

> Converted from a previous version of [`docs/DESIGN.md`](../DESIGN.md). The text is essentially unchanged aside from the framing, metadata, cross-references, and line wrapping.

- Status: Accepted
- Date: 2026-03-27
- Author: Geoff Amey

Versions are pinned in `go.mod`; only the choice and rationale live here.

| Concern | Library | Rationale |
|---------|---------|-----------|
| CLI framework | `urfave/cli` | Lightweight, idiomatic, good subcommand support |
| Configuration | `knadh/koanf` | Multi-source (file, env, flags), clean layering |
| Git operations | System `git` via `os/exec` | See below |
| `go.work` files | `golang.org/x/mod/modfile` | Official parser/writer, no shelling out needed |
| Terminal UI / colors | `lipgloss` | Composable styling, no TUI required |
| Confirmation prompts | hand-rolled (`internal/cmd/prompt.go`) | A yes/no reader over stdin; no prompt library needed |

## Why system `git` instead of go-git

The initial design targeted `go-git/go-git/v6` (pre-release) to avoid a dependency on the host system's git version. After a spike (see `spikes/go-git-worktree-support.md`), we concluded the tradeoffs favor shelling out to system git:

**go-git v6 costs:**

- Pre-release, unstable API (the `x/plumbing/worktree` package)
- Name character restriction (`^[a-zA-Z0-9\-]+$`) requiring a custom worktree layer
- Does not respect the host's credential helpers, SSH agents, or proxy config
- In-memory implementations help testing, but a clean interface (see below) achieves the same

**System git benefits:**

- All worktree subcommands needed (`add`, `list`, `remove`, `prune`, `move`) are available in git 2.5–2.18. Ubuntu 20.04 LTS ships git 2.25.1 — well within range
- `--porcelain` and `--porcelain=v2` output formats are stable across versions and locales; no fragile text parsing required
- Respects the user's existing git configuration (SSH, proxies, credential helpers)
- Battle-tested edge case handling
- `golang.org/x/mod/modfile` handles `go.work` in pure Go; no `go` binary shelling needed

**Minimum supported git version:** 2.25.1 (Ubuntu 20.04 LTS default). All features used are available in this version. `git worktree repair` (added in 2.29) is attempted opportunistically — see the State Validation section in [ADR 0004](0004-state-persistence.md).
