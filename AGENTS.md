# wtg

- `README.md` — user-facing docs: install, usage, and configuration. Start here.
- `docs/` — longer-form docs. `DESIGN.md` indexes the architecture decision records in `docs/adr/`; feature guides (e.g. `always.md`) explain individual features in depth for users.

## Conventions

- Conventional commits (`feat:`, `fix:`, `docs:`, `test:`, `refactor:`)
- Each command implementation includes its unit tests in the same session/PR
- Ambiguous repo names → error with suggestions, not interactive prompt (keep scriptable)
- Update `CHANGELOG.md` as you add features: user-facing changes go under `## [Unreleased]` (Added/Changed/Fixed/Removed per Keep a Changelog). Skip dependency bumps, CI, and pure refactors.
- Collaborators never need wtg: see [ADR 0022](docs/adr/0022-no-tool-dependency.md). Nothing committed, pushed, or shared may require wtg to be installed or understood. Per-repo settings use `wtg.*` git config, not tracked files.
- This is a public project. Docs, ADRs, tests, examples, commit messages and changelog entries must not contain anything from a private or employer context: internal hostnames, org, repo, product or team names, real workspace names, or identifiers taken from private repos (commit hashes, pseudo-versions, tokens). When a real situation motivates a design, describe it with neutral placeholders (`example.com/org/lib`, `config-lib`, `my-feature`). History is public too; a leak means rewriting it, so catch it before committing.

## Releasing

Entries accrue under `## [Unreleased]` as features land (see above), so a release is just a stamp:

1. Rename `## [Unreleased]` → `## [X.Y.Z] - YYYY-MM-DD`, then add a fresh empty `## [Unreleased]` above it.
2. Update the compare links at the bottom: point `[Unreleased]` at `vX.Y.Z...HEAD` and add `[X.Y.Z]: ...compare/<prev>...vX.Y.Z`.
3. Commit (`chore(release): vX.Y.Z`), then `git tag vX.Y.Z` on that commit.

Pick X.Y.Z by what's in Unreleased: new features → minor bump, fixes only → patch.
