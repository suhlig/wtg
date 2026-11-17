# ADR 0025: Resolving `.` in repo and workspace arguments

- Status: Accepted
- Date: 2026-11-15
- Author: Steffen Uhlig

## Context

`wtg` addresses repos by short name: the path relative to a discovery root, with `/` separators ([ADR 0005](0005-repository-discovery.md)). Users type those names often enough that the shell already makes the obvious shortcut available: `.`. The repo a user wants is usually the one they are standing in, and typing its short name means remembering the host and org segments first.

`wtg add` already accepts `.` through `expandDots` in `internal/cmd/exec.go`, which replaces it with `os.Getwd()` and lets `buildTargets` match the absolute path. That shortcut does not extend to the other commands, and even in `add` it works only when the current directory is exactly a main clone root under a discovery root. The same shortcut is also dangerous to model as a name: `.` is a substring of almost every segment of a short name (`github.com`, `gist.github.com`, `notes.md`), so `repoInSet`'s partial-match stage treats it as matching most repos and reports an ambiguity that names half the discovery tree, or, with one dotted repo, silently resolves to it.

The commands divide into three argument shapes: repo-only (`new`, `repo ...`), repo-plus-optional-workspace (`add`, `remove`), and workspace-only (`path`, `status`, `delete`, `push`, `land`, `rebase`, `exec`). `.` needs one definition that works for all three without making the optional workspace ambiguous.

## Problem

Make `.` mean "the repo (or workspace) I am in" wherever a command takes a repo or workspace argument, without weakening short-name matching, without breaking `always.repos` symlink upgrades, and without depending on the accidental identity of a filesystem path.

## Decisions

### D1. `.` is a reserved argument, resolved before name matching

`.` is never passed to `repoInSet`. It is recognised before the staged name resolution of [ADR 0005](0005-repository-discovery.md) and replaced by the canonical short name it denotes. The same reservation applies in every command that accepts repo arguments.

This is a correctness requirement, not a convenience: the substring stage compares each slash-separated segment of a short name, and `.` occurs in host segments (`github.com`, `gist.github.com`) and in file-name segments such as `notes.md`. Letting `.` reach that stage produces the ambiguity this ADR is meant to remove, and can resolve spuriously when only one discovered repo contains a dot.

`repo unarchive` has no coherent meaning for `.` (an archived clone is not in discovery), so it rejects the argument rather than treating it as a name.

### D2. `.` means the repo containing the current directory

Resolution asks git for the worktree root containing the current directory (`git rev-parse --show-toplevel`, through the git `Runner` of [ADR 0002](0002-git-abstraction-layer.md)). Git already handles the cases a hand-written parent walk gets wrong: running from a subdirectory, a linked worktree, a submodule, or a repository reached through `GIT_DIR`.

If the current directory is not inside a repository, the command fails with a message that says so; it never falls back to using the directory itself as a repo name.

### D3. A worktree resolves to its main clone

A linked worktree is a second checkout of a repo, not a separate repo. Resolution maps a worktree root to its main clone through the git `Runner`'s `WorktreeList`; the first entry is the main worktree. Space-scoped commands (`add`, `remove`) may instead identify the member by matching the worktree path against the loaded space's `WorktreePath`, which ends at the same canonical name.

This gives both useful behaviours with one rule: `wtg remove .` from inside a member worktree removes that member, and `wtg new other-space .` from the same place adds the repo behind it.

### D4. Resolution yields a canonical short name, not a path

Every consumer of repo arguments is name-based: `buildTargets` and `resolveAlwaysRepos` for `new`, `RunSpaceRemove` for state matching, `resolveRepoPaths` for the `repo` commands, and the archive and unarchive resolvers. Resolving `.` to a name keeps all of them on one key.

This is also what fixes `always.repos`: `resolveAlwaysRepos` builds its "already named explicitly" set from the raw arguments, so an absolute path does not match a configured repo name. A repo that is both named by `.` and configured in `always.repos` must be created as a worktree and must not also be symlinked into the same path. Names make that true by construction.

The absolute-path branch in `buildTargets` is an implementation detail of the current `expandDots` approach. Once no caller produces an absolute path, it can be removed; the CLI does not document absolute paths as repo arguments.

### D5. Compare physical paths

A logical `$PWD` can differ from the path discovery recorded: a discovery root may be reached through a symlink, and on macOS `/tmp` is `/private/tmp`. Before deciding whether the repo found in D2 is managed, canonicalize both sides with `filepath.EvalSymlinks` or compare them with `os.SameFile`. Do not compare raw strings.

### D6. Scope

| Argument position | `.` means |
|---|---|
| `new <workspace> .` | the repo containing the current directory |
| `add [<workspace>] .` | the repo containing the current directory |
| `remove [<workspace>] .` | the repo containing the current directory |
| `repo status/fetch/sync/archive .` | the repo containing the current directory |
| `path/status/delete/push/land/rebase/exec .` | the workspace containing the current directory |
| `repo unarchive .` | rejected |

For a workspace-only command, `.` is equivalent to omitting the argument, which already infers the workspace from the current directory. `wtg exec . -- <cmd>` is the explicit form of `wtg exec -- <cmd>`.

### D7. In `add` and `remove`, `.` is always a repo, never the workspace

These commands take an optional workspace followed by repos, so a position alone cannot distinguish the two. The tempting rule "the first positional `.` means the current workspace" is rejected: it would make the most useful invocation, `wtg remove .` from inside a member worktree, mean "remove nothing from the current workspace". `.` therefore always denotes a repo in `add` and `remove`; the workspace comes from an explicit name or from the existing current-directory inference.

`.` cannot be a literal workspace name. In `wtg new` the workspace argument is a name, so `wtg new . <repos>` is rejected: a workspace name becomes part of a branch name and a state filename, and `.` is neither. Commands that resolve a workspace from context (`path`, `status`, `delete`, `push`, `land`, `rebase`, `exec`) accept `.` as the current workspace, as D6 says.

### D8. `.` is not a new path syntax

Only the exact token `.` is special. `./api`, `../api`, and absolute paths are not documented repo arguments and continue to be treated as names, which will normally not match.

A canonical short name already contains slashes (`github.com/example/api`), so treating every slash-containing argument as a filesystem path would break existing usage. `.` is safe to reserve because discovery skips hidden directories, so no repo can be named `.`. General path support, if wanted, needs a marker or its own decision.

### D9. Errors

- No repository contains the current directory: say so and ask for an explicit repo name.
- The repo is not under any discovery root: say which path was found and which roots were searched, and suggest adding a root or naming a repo explicitly. Do not silently accept an unmanaged repo: the worktree layout and archive provenance assume short names ([ADR 0006](0006-worktree-layout.md)).
- The current directory is a workspace root or a group directory inside one, not a repo: same "not inside a repository" error.
- `.` for `repo unarchive`: rejected with the reason above.
- `.` as a workspace name: rejected with the reason above.

## Consequences

- `wtg new <workspace> .` works from a main clone, a subdirectory, or a worktree, and adds the repo behind the current directory.
- `wtg add .` works from a subdirectory and from a worktree, and upgrades an `always.repos` symlink exactly like `wtg add <name>` ([ADR 0014](0014-always-repos-are-symlinks.md)).
- `wtg remove .` starts working from a member worktree. Today it expands `.` to an absolute path and then fails to match any state name, even though `expandDots` runs on this path.
- `wtg repo status .`, `repo fetch .`, `repo sync .`, and `repo archive .` address the main clone, including from inside its worktree.
- `wtg path .`, `wtg status .`, `wtg delete .`, `wtg push .`, `wtg land .`, `wtg rebase .`, and `wtg exec . -- <cmd>` address the current workspace.
- `wtg new . <repos>` fails early with a clear error instead of creating state under the spaces root and failing on an invalid branch name.
- No state schema or configuration change. The git calls added in D2 and D3 go through the git `Runner` ([ADR 0002](0002-git-abstraction-layer.md)).
- Tests: repo root, subdirectory, worktree, symlinked root, `always.repos` member, repo outside discovery, `new .`, `remove .`, and the workspace-position commands.
- Documentation: README, command help, and a `CHANGELOG.md` entry under `## [Unreleased]`.

## Alternatives considered

- **Keep expanding `.` to an absolute path.** Rejected: every name-based consumer needs a second key, and `remove` and `always.repos` do not have one. It also makes correctness depend on raw path equality.
- **Let `.` be a repo name.** Rejected: partial matching makes it match most repos, which is the failure this ADR fixes.
- **Treat path-like arguments as paths.** Rejected for now: collides with slash-separated short names such as `github.com/example/api`.
- **First-position `.` means the current workspace in `add`/`remove`.** Rejected: breaks `wtg remove .`.
- **Accept repos outside discovery roots.** Rejected: breaks the short-name layout ([ADR 0006](0006-worktree-layout.md)) and the archive and provenance assumptions. The error should be actionable instead.

## Implementation status

Implemented. `internal/cmd/dot.go` holds the resolution: `resolveRepoArgs` replaces every `.` with a canonical short name before any name matching, `currentRepoName` asks git for the worktree root (`Runner.Toplevel`) and maps a linked worktree to its main clone through `WorktreeList`, and `samePath` compares physical paths with `os.SameFile`.

- Repo positions: `new`, `add`, `remove`, and `repo status`/`fetch`/`sync`/`archive` (which resolve through `resolveRepoPaths`).
- Workspace positions: `path`, `status`, `delete`, `push`, `land`, `rebase` and `exec` via `resolveSpaceArg`/`resolveExecArgs`.
- Rejections: `wtg new .` (`rejectDotAsWorkspace`) and `wtg repo unarchive .`.
- `expandDots`, and the absolute-path branch in `buildTargets` that served it, are gone: no caller produces a path any more.

Tests cover the repo root, a subdirectory, a linked worktree, a symlinked discovery root, a repo outside every root, `always.repos` (a repo named by `.` becomes a worktree, not a symlink), `new .`, `remove .`, and the workspace-position commands, including the rejections.

Implemented later, from this record's follow-ups:

- **Shell completion offers `.`** (`internal/cmd/complete.go`) wherever the position accepts it, listed before the names. The rule is the table in D6/D7, applied per completer:
  - `completeRepos` (`repo status`/`fetch`/`sync`/`archive`, the repo positions of `new`, `add`): `.` first, when at least one repo is discovered. With no discovered repo `.` could not resolve, so it is not offered.
  - `completeSpaces` (`path`, `status`, `delete`, `push`, `land`, `rebase`, `exec`, and position 0 of `add`/`remove`): `.` first, when at least one workspace exists. In the workspace commands it is the current workspace; in `add` and `remove` position 0 is the one place a repo can stand where a workspace name also can, and D7 says `.` is a repo there, so offering it is correct.
  - `completeSpaceMembers` (`remove`, position 1+): `.` first, when the workspace has members.
  - Not offered: position 0 of `new` (a workspace *name*, where `.` is rejected, D7) and `repo unarchive` (`completeArchivedRepos`; D1 rejects it).
  - `.` is just one more candidate line: the bash, zsh and fish scripts generated by the CLI library pass whatever the handler prints through (fish via `-xa '(wtg … --generate-shell-completion)'`, zsh via `_describe`), so none of them changed.
  - Tests: `internal/cmd/complete_test.go` runs each completer as a `ShellComplete` handler and asserts the output, including the absent cases.

## Follow-ups

- Path-like arguments (`./api`, `../api`, absolute paths) remain unsupported and are still treated as names (D8). Supporting them needs a marker or its own decision, because a canonical short name already contains slashes.
