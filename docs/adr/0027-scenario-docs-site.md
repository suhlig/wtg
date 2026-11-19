# ADR 0027: Scenario-based documentation, built from the real binary, published as a static site

- Status: Accepted
- Date: 2026-11-18
- Author: Steffen Uhlig

## Context

wtg's documentation today is one long `README.md` (about 450 lines: install, configuration, and an exhaustive per-command reference) plus three feature guides in `docs/` (`always.md`, `archive.md`, `repo-sync.md`) and the architecture decision records in `docs/adr/`. It answers "what does this flag do?" well and "what do I do now?" poorly. A new user has to assemble the workflow from the command list, and nothing explains the choices that matter at the end of a feature, such as `land` with `merge` or `pr`.

Several things are wanted at once:

- **Scenario-based guides**: each starts with a context ("I just installed wtg", "I am in the middle of a feature and main moved") and walks through the commands, with the output wtg prints.
- **Examples that cannot go stale**: the commands and output shown on the page must come from the real `wtg` binary of the release being documented, not from hand-typed text.
- **An FAQ**, and an **exhaustive reference** as a separate kind of page, so that the README can shrink.
- **A published static site** (GitHub Pages, built with Zola).
- **ADRs stay readable on `main`**, as plain files in the repository, because they drive LLM agents working on the code.

### What was measured before writing this

All of this was checked against the current tree, with throwaway repositories (bare origins, clones, a temporary `HOME` and `XDG_*`):

| Question | Result |
|---|---|
| Is the output of the built binary reproducible? | Yes. With `GIT_AUTHOR_*` / `GIT_COMMITTER_*` name, email and date pinned, two complete runs of `new`, `rebase`, `push`, `status` were byte-identical, including the abbreviated commit hash in `↑ rebased (was 23a7164)`. The only run-specific text was the temp directory, replaced by `~`. |
| Does wtg colour output when piped? | Yes. `status` printed ANSI escapes through `| cat`, and `NO_COLOR=1` and `TERM=dumb` changed nothing. Styles are in `internal/ui/ui.go` (lipgloss). wtg does not honour `NO_COLOR` today. |
| Which commands can be captured end to end with git only? | `new`, `add`, `remove`, `status`, `path`, `rebase`, `push`, `exec`, `repo sync`, `repo status`, `repo archive`, `land --strategy merge`, `land --dry-run`, `delete --yes -d`, `--dry-run`, `--summary`. Verified for `new`, `status`, `path`, `rebase`, `push`, `land` (merge, and `--dry-run`) and `delete`. |
| What cannot be captured? | Anything that needs a terminal (the `delete` confirmation prompt, `--progress` bars), and `land --strategy pr`, which needs `gh` and a GitHub remote. |
| Do the existing tests already produce this output? | No. `internal/cmd/footprint_test.go` and similar call `RunSpaceNew`, `RunSpaceDelete`, … in-process. That skips `main.go`: argument parsing, config loading, the `DRY RUN` header, `--summary`, and colour. They also use `t.TempDir()` paths and unpinned dates. The mock-runner tests (`testrunner_test.go`) say nothing about real output. |
| Does urfave/cli v3 (v3.11.0) generate docs? | Not in the main module. It is a separate module, `github.com/urfave/cli-docs/v3` (v3.2.0, released 2025-10-22), with `ToMarkdown` and `ToMan`. A spike on a copy of the repo ran, but its Markdown for `land` and `new` contained the one-line usage and the flags only: **`Description` and `ArgsUsage` were not rendered**, and the module is more than six months old (it adds `go-md2man` and `blackfriday`). |
| How much code would moving to cobra touch? | 20 Go files import `urfave/cli` (4 of them tests); about 72 references to `cli.Command`, 22 `BoolFlag`, 16 `StringFlag`, 49 `Usage`, 18 `Description`, 15 `ArgsUsage` and 15 `ShellComplete` handlers. `internal/cmd/complete.go` has a custom completion protocol (`emitFlags`, colon-separated `--flag:usage` lines read by the fish script) built on `cli.DocGenerationFlag`. 13 tests call `.Run(ctx, args)` on commands. |
| How big is the test suite? | 613 `func Test…`, all standard library `testing`. No Ginkgo, Gomega or testify in `go.mod`. |
| Zola and Pages tooling | Zola v0.23.6, published 2026-09-12 (repository pushed 2026-11-16). GitHub reports its licence as EUPL-1.2 and the repository holds both `LICENSE` and `LICENSE-MIT`; this needs a decision under D13. Ginkgo v2.34.0 (2026-11-02), Gomega v1.44.0 (2026-09-24), cobra v1.10.2 (2025-12-03). |

## Problem

Define the documentation set, how its examples are produced and kept true, which tools build and publish it, and what is deliberately not changed (the CLI library, the existing tests, the ADR location), so that a later agent can implement it in steps.

## Decisions

### D1. Four kinds of documentation (decided)

| Kind | Purpose | Written | Lives in |
|---|---|---|---|
| **Scenarios** (guides) | "I am in situation X; what do I do, what will I see?" | Prose by hand; every command and output block generated (D2) | `site/content/guides/` |
| **Reference** | Exhaustive: every command, flag, config key, state and behaviour table | Generated where the source is code (D9), hand-written where it is behaviour | `site/content/reference/` |
| **FAQ** and troubleshooting | Short answers to recurring questions, quoting real output | Prose by hand, output generated | `site/content/faq/` |
| **Design** (ADRs, `DESIGN.md`) | Why wtg works the way it does | By hand | **`docs/`, unchanged** (D12) |

The README keeps only what wtg is and why, install, a 30-second quick start, and links into the site and the reference. It stops duplicating the command reference.

### D2. Every command and every line of output on the site comes from a transcript (decided)

The rule: **prose is hand-written; commands and their output are never typed into a page.** A scenario is executable. The acceptance suite (D3) runs it against the built binary and records a transcript (command, exit code, normalised output, per step). A Zola template (`site/templates/transcript.html`, pulled in with `{% set id = "…" %}{% set step = N %}{% include "transcript.html" %}`) renders a transcript step into the page. A Zola shortcode was the first idea; an `include` does the same job here because it can read the transcript data without extra plumbing. Because the shortcode prints the command that was executed, prose cannot show a command that was not run.

A CI check (`tools/checkdocs`) fails when any page under `site/content/guides/` or `site/content/faq/` contains a code block with a hand-typed `wtg …` line. It covers every such page by default; a page can be exempted only by naming it in `handTypedAllowed` in `tools/checkdocs/main.go`, which is empty. Only shell-completion setup lines are exempt, because they run in the user's own shell. The reference pages are generated by `tools/gendocs` (D9), so they are outside this check.

### D3. A new acceptance test suite, in Ginkgo, drives the real binary (decided)

- It builds `wtg` once (`gexec.Build`) and runs it as a subprocess in a sandbox (D4). It is a new test type, separate from the unit tests in `internal/cmd` and from `footprint_test.go`.
- It lives in a directory of its own, for example `acceptance/`, as a normal `go test` package, so the existing `go tool gotestsum … ./...` step runs it. It must work under `-race` and without the `ginkgo` CLI.
- One scenario is one `Describe` with `Ordered`, each step an `It`, or `By` steps inside one `It`. The structure mirrors the scenario page.
- Each step asserts the **exit code** and a few **key lines**. These assertions are committed and run on every pull request. A change in behaviour that breaks a scenario therefore fails CI, which is the point.
- The same run writes the transcripts the site needs (D5). Ginkgo's reporting hooks (`ReportAfterEach`, or the harness writing as it goes) are the place for this; the choice is left to the implementation.
- The existing 613 tests are **not** migrated (D8).

### D4. Harness rules (decided)

Each scenario runs in its own sandbox. Rules that make it safe, deterministic, and publishable:

1. **Isolation.** Per scenario temporary `HOME`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `GIT_CONFIG_GLOBAL`, with `GIT_CONFIG_NOSYSTEM=1`. The harness must never read or write the developer's real wtg config or git config.
2. **Determinism.** Pin `GIT_AUTHOR_NAME/EMAIL/DATE` and `GIT_COMMITTER_NAME/EMAIL/DATE`. Fixed repo names, branch names, and file contents. Output that contains time or duration (for example `slow: api 1m5s`) is not used in a scenario, or is normalised.
3. **Normalisation.** Replace the sandbox path (and its `/private` variant on macOS) by `~`. This is also what keeps local paths such as a developer's home directory out of a public site. Colour is kept (D6).
4. **One stream.** stdout and stderr go to the same pipe, so the order matches a terminal.
5. **Shell state persists between steps.** A `cd` in one step applies to the next. This matters because most commands infer the workspace from the current directory; `wtg path` / `wcd` are shown as the user would use them.
6. **Hidden setup.** Steps that only prepare the situation ("main moved on") are marked hidden and not rendered. Only what the reader would type is shown.
7. **Fixtures.** Small bare origins and clones named neutrally (`api`, `web`, `payments`), tiny Go modules where `go.work` is part of the story, a non-Go repo where it is not. They are built by the harness (git only); the style follows `internal/git/testhelper`.
8. **No private content.** Fixtures, transcripts and page text use neutral placeholders only (see `AGENTS.md`: this is a public project).
9. **Not the developer's wtg.** The binary under test is the one just built from the working tree, never `PATH`.

### D5. Transcripts are generated at site build, not committed; assertions are committed (decided)

Committed golden files would churn between macOS and Linux and between git versions (messages, abbreviation lengths), and a failed diff on prose-adjacent output teaches people to regenerate without reading. So:

- The **assertions** (exit code, key lines) are in the repository and run on every PR.
- The **transcripts** are produced on the release build (Linux CI, a fixed git) and consumed directly by the Zola build as data (`load_data`). They are not committed.
- Reviewing a documentation change therefore means reading the scenario code and the prose, and previewing the built site (the PR build, D11, uploads it as an artefact).

Committed goldens can be added later if review of output changes turns out to matter (Follow-ups).

### D6. Colour is preserved as HTML (decided)

The transcript keeps wtg's ANSI colour and the site renders it as styled spans (a small, tested converter in the build; sanitised, no raw HTML passthrough). Users see colour; the page should match. Colour that is stripped for accessibility is a styling choice of the site, not of the transcript. Making wtg itself honour `NO_COLOR` is a separate, user-facing change and is a follow-up.

### D7. Pull-request scenarios use a visibly labelled fake `gh` (decided)

`land --strategy pr` and the PR-related `delete -d` path need `gh`. Everything else in the suite is real. For PR scenarios the harness puts a **fake `gh` executable** first on `PATH` that answers the handful of calls wtg makes (`pr list`, `pr create`, `pr view`, `pr edit`, `auth status`). Any page that shows its output carries a visible note: "`gh` is simulated here; git is real."

**Settled (phase 3).** The fixtures' origin is the real URL `git@github.com:example/<repo>.git`, so `remote.ParseGitHubRemote` accepts it and wtg is unchanged. The harness has no `insteadOf` rule. Instead it puts a small `ssh` shim first on the sandbox's `PATH`. git runs `ssh git@github.com "git-upload-pack 'example/api.git'"`; the shim maps that to the bare repository of the same name in the sandbox and runs the matching `git upload-pack` / `receive-pack` on it. Two details matter:

- wtg starts git with `GIT_SSH_COMMAND=ssh -o BatchMode=yes` (see `internal/git/system.go`), so setting `GIT_SSH_COMMAND` in the sandbox has no effect. A shim named `ssh` on `PATH` does.
- `exec.Command` resolves a bare name such as `gh` or `ssh` against the test process's own `PATH`, not against the `Env` given to the child. The harness therefore resolves bare names in the sandbox `bin/` directory first (E7), or a real `gh` could be called from a developer machine.

`acceptance/cmd/fakegh` keeps its state per repository, because GitHub counts pull request numbers per repository and wtg calls `gh` for several repositories at once. It also tracks the archived state of a repository, so `--remote` scenarios are idempotent like the real thing.

### D8. Ginkgo: used for the new acceptance suite only; the existing tests are not migrated (decided)

Ginkgo is the preferred test library and the acceptance suite is where it earns its place: nested containers with `Ordered` express a scenario as a sequence of steps, `gexec` builds and runs the binary and captures output, labels select scenario groups, and reporting hooks are available for transcripts. Gomega's matchers read well for output assertions.

Moving the 613 standard-library tests would give nothing the documentation needs, and the migration would be large and noisy. The two styles coexist: `go test ./...` runs both, and `gotestsum` reports both. If the team moves the unit tests later, that is a separate decision with its own ADR (Follow-ups).

Considered and not chosen: `github.com/rogpeppe/go-internal/testscript` (txtar scripts). It is the idiomatic Go tool for CLI scenario tests and its scripts are readable as documentation, but it is not the library the maintainer prefers, and the transcripts and conditional hidden steps would need extra code either way.

### D9. Reference: generated from the command tree; stay on urfave/cli (decided)

The reference page for commands and flags is generated by a small tool (about 100 lines, for example `tools/gendocs`) that **walks the `cli.Command` tree** and renders `Name`, `Usage`, `ArgsUsage`, `Description`, flags (names, aliases, usage, defaults, env vars), and subcommands. Those fields are public.

- **Not `urfave/cli-docs`.** The spike showed that it drops `Description` and `ArgsUsage`, which carry the substance for commands like `land`; it is also older than six months and brings two new dependencies.
- **Not cobra.** A move to cobra would give a documentation generator, but the cost is out of proportion to the benefit: it rewrites 20 files and the 15 completion handlers and the custom completion protocol, and risks the shell completion behaviour that users rely on. The generator in this decision is smaller than the cost of one migration step, and it also has no new dependency. If the project later wants cobra for other reasons, the generator is small enough to be rewritten.
- **Config reference** embeds the output of `wtg config init -o -`, which already documents every setting and its default, so the reference cannot disagree with the scaffold.
- **Behaviour tables** that are hand-written (branch states, read-only members, strategies) move from the README into the reference as they are, not copied.

### D10. Scenario catalogue (decided; order is priority)

Each page opens with a context sentence and ends with "what you have now".

| # | Context | Commands shown | Generated? |
|---|---|---|---|
| 1 | I just installed wtg; what are the first steps? | `config init`, set discovery and spaces roots, `repo status`, shell completion and `wcd` | Yes. The install step (`go install`) is static text. |
| 2 | I want a new workspace with two projects; where is my stuff? | `new`, `path`, directory tree, `go.work`, `status`, `config path`, where state lives | Yes |
| 3 | I am mid-feature; how do I pick up changes in main? | `repo sync`, `rebase`, `push`, the diverged-branch `--force-with-lease` hint | Yes |
| 4 | I am done; how do I get it into main, and which strategy? | `land --dry-run`, `land`, `delete -d`, and a decision table (below) | `merge`: yes. `pr`: fake `gh` (D7). |
| 5 | A rebase conflicts | the abort-and-restore message, retry by hand | Yes |
| 6 | The feature needs a third repo, or one is no longer needed | `add`, `remove` | Yes |
| 7 | I only consume a library | `add --read-only`, `status` drift warning | Yes |
| 8 | Branch from `develop`, or continue an existing branch | `new --base`, `--branch` | Yes |
| 9 | Run the tests everywhere | `exec`, `--parallel`, `--timeout` | Yes (no timing output) |
| 10 | Preview and audit what wtg does | `--dry-run`, `--summary` | Yes |
| 11 | Review feedback arrives after the workspace is gone | `new` re-attaches the branch, `push` | Yes |
| 12 | Every workspace gets my `.env`, agent file and hook | `always.*` (moves in `always.md`) | Yes |
| 13 | `delete` refuses | unpushed commit, changed secret copy, `--yes` | Yes (prompt not shown, no TTY) |
| 14 | Retire old clones | `repo archive`, `unarchive` (moves in `archive.md`) | Yes |
| 15 | I stopped using wtg, or lost its state | plain git recovery ([ADR 0022](0022-no-tool-dependency.md)) | Partly |

Scenarios 1 to 4 are the first deliverable. The wording that scenario 4 has to carry, from [ADR 0019](0019-workspace-land.md):

- Use **`pr`** (the default) when the repo is shared or protected, or when repos depend on each other: `merge` gives no ordering across repos (D8 there), while reviewers decide the order of PRs.
- Use **`merge`** for repos you may push to directly. It only fast-forwards, so no merge commit is created and no history is rewritten.
- `rebase` first if `land` says it is not a fast-forward.
- GitHub's squash and rebase merge buttons are outside wtg; they are why `delete -d` consults `gh` (ADR 0019 D12).
- The strategy is a per-repo git config, `wtg.landStrategy`, never a tracked file (ADR 0019 D3, ADR 0022).

### D11. FAQ (decided)

Seeded from real error and result text so that answers quote transcripts instead of pasted text:

- Does wtg modify my clones? (No; branch and worktree entries only, [ADR 0022](0022-no-tool-dependency.md).)
- Do my teammates need wtg? (No.)
- Why won't `delete` delete? (unpushed commits, changed `always.secrets` copies, no terminal without `--yes`)
- Why does wtg never force-push?
- What does the ambiguous-repo-name error mean, and how do I fix it?
- What is the block in my pull request description? (the sibling block, `--no-link`)
- What if I `rm -rf` a workspace by hand? (`git worktree prune`)
- Does it work without Go? (yes; `go.work` is written only for repos with `go.mod`)
- Why must `wcd` be a shell function?
- Why is the output coloured when I pipe it?

### D12. Where documentation lives; the ADRs stay where they are (decided)

- **`docs/adr/` and `docs/DESIGN.md` do not move.** They are plain Markdown on `main`, at the paths that `AGENTS.md`, the ADRs, and agents already use. They are not published as a copy that could drift; the site links to them on GitHub (`main`), and may render them at build time from the same files if that proves useful. Nothing in this ADR makes the ADRs depend on the site.
- **`docs/always.md`, `docs/archive.md`, `docs/repo-sync.md` move into `site/content/`** (as feature guides or reference pages). They stay plain Markdown (plus Zola front matter), so they remain readable on `main` too.
- **The README slims down** (D1). Section anchors that other documents link to (`#workspace-commands`, `#read-only-members`) are checked, and updated or redirected.
- **Links that must be updated in the same change as the move:** the raw URL `https://raw.githubusercontent.com/suhlig/wtg/main/docs/always.md` in `skills/wtg-setup/SKILL.md`; README links to `docs/always.md`, `docs/archive.md`, `docs/repo-sync.md`; `AGENTS.md` and `docs/DESIGN.md`, which describe `docs/`. `CHANGELOG.md` entries that mention `docs/always.md` are history and stay as written.
- Every page the skills or agents need must be reachable as plain Markdown on `main`; `zola check` fails the build on a broken internal link.

### D13. Site: Zola on GitHub Pages, published on release tags (decided)

- **Generator: Zola.** One static binary, no Node or Python; shortcodes (for transcripts), `load_data` (for the JSON transcripts), a built-in search index, `zola check` for links. Cost: few ready-made documentation themes, so expect about 100 lines of templates and CSS of our own.
- **Publication: on release tags**, not on every push to `main`. The site then always matches what `go install github.com/suhlig/wtg@latest` gives users, and the transcripts come from exactly that tag. A manual `workflow_dispatch` covers urgent documentation fixes. A PR builds and checks the site (and uploads it as an artefact for review) without deploying.
- **Hosting: GitHub Pages** via `actions/deploy-pages`, with `permissions` limited per job (`pages: write`, `id-token: write` for the deploy job only; `contents: read` elsewhere).
- **Supply chain.** Zola is downloaded from its release page at a pinned version and verified by checksum, not installed through a third-party action. Maintenance was checked before pinning (release v0.23.6 on 2026-09-12, repository pushed 2026-11-16). **Licence, decided by the maintainer:** Zola's `Cargo.toml` says EUPL-1.2, and its README says code written after version 0.22 is EUPL-1.2 while older code stays MIT. That is acceptable here, because Zola is only a build tool: it is neither linked into wtg nor shipped in the published pages, and the pinned, checksummed binary is downloaded in CI only. The Hugo alternative below stays the fallback if this ever changes.

| Option | Verdict |
|---|---|
| **Zola** | Chosen: single binary, no runtime, the features above. |
| Hugo | Closest alternative; could be pinned through the `tool` block in `go.mod`, but that pulls in many dependencies and its templating is heavier. The fallback if the Zola licence is not acceptable. |
| mdBook | Good for the reference, but it is one linear book; a poor fit for guides plus an FAQ. |
| Docusaurus, Starlight | Capable, but they add a Node toolchain to a Go project. |

### D14. Keeping the documentation true (decided)

- A behaviour change that breaks a scenario fails CI (D3); the author updates the scenario in the same change.
- `AGENTS.md` gains one convention line: a change to user-visible behaviour updates the affected scenarios, and a new command or flag needs its reference entry (generated) and, where a user story exists, a scenario.
- The reference is generated at build time, so a new flag cannot be missing from it.
- The `CHANGELOG.md` rule is unchanged.

## Edge cases

| # | Situation | Handling |
|---|-----------|----------|
| E1 | Different git version on macOS and on CI changes message text or hash length | Assertions use key lines only (D3); transcripts come from the Linux release build only (D5) |
| E2 | A step needs a terminal (`delete` prompt, `--progress`) | Not shown. The page says what to expect in words, or uses `--yes` and shows the no-terminal refusal |
| E3 | Output contains a duration or timestamp | The scenario avoids that command or flag, or normalises the value; it never publishes a time-dependent line |
| E4 | Row order of a parallel command is not stable | Assertions do not depend on order; if a transcript shows a table, the scenario checks that order is stable (rows were stable in the measurements) |
| E5 | The sandbox path leaks into a transcript | Normalisation (D4.3) runs on every transcript; the CI grep also fails the build on a path starting with the OS temp directory or a home directory |
| E6 | A scenario fails on a developer machine because of their global git config | Isolation (D4.1) prevents it; a failure there is a harness bug |
| E7 | `gh` or `ssh` is really installed on the machine running the suite | The harness resolves bare command names in the sandbox `bin/` first (`exec.Command` would otherwise use the test process's own `PATH`), so the suite never calls the real one |
| E8 | A scenario step fails unexpectedly | The suite fails; the transcript step records the real output so the failure is visible in the CI log |
| E9 | The Zola build runs without transcripts (a contributor builds locally) | The transcript template prints a visible message that names the command that produces them, not an empty page |
| E10 | A page is moved and an external link or the skill's raw URL breaks | Moves are listed in D12; `zola check` covers internal links; the external raw URL is changed in the same PR |

## Implementation plan

Implemented. The phases were delivered together on `main` rather than as separate pull requests; deviations from the plan are noted per phase.

0. **This ADR**, the index row, and the `Open work` entries in `DESIGN.md`. Done.
1. **Spike, scenarios 1 and 2.** Done: harness (D4), `acceptance/` with Ginkgo and `gexec`, transcript writer, colour converter, normalisation check. Deviation: the first version was not reproducible. wtg pads table cells to the widest value, and that width was measured before the sandbox path was replaced by `~`; `os.MkdirTemp` suffixes vary in length, so the transcripts differed by one column between runs. The sandbox path now has a fixed length. That was not enough across machines: the padding width also depends on where the sandbox lives (`/private/var/folders/…` on macOS, `/tmp/…` on Linux CI), so the harness also strips trailing spaces from every line of output; they are invisible. `make check-determinism` (run in CI) runs the suite twice and compares the transcripts byte for byte.
2. **Site skeleton.** Done: `site/`, the transcript template, and the Pages workflow (build and check on every push and pull request; deploy on `v*` tags and manual dispatch). Zola is pinned at v0.23.6 and verified by SHA-256; a mismatch fails the build (an early draft carried on after a mismatch).
3. **Scenarios 3 and 4,** the strategy decision table, and the FAQ seed. Done, including the PR half of scenario 4 with the fake `gh` (D7) and scenario 13 for the refusals the FAQ quotes. Writing the PR scenario found a real bug: `wtg land` matched sibling pull requests by number alone, so two repos that both got `#1` linked no siblings. Fixed in `linkSiblings`, with a regression test and a `CHANGELOG.md` entry.
4. **Reference.** Done: `tools/gendocs`, the config scaffold embed, the three feature guides moved, the README slimmed, links updated (including the raw URL in `skills/wtg-setup/SKILL.md` and the references in ADRs 0014 to 0017 and 0024).
5. **Remaining scenarios.** Partly done: 1, 2, 3, 4, 6, 7, 10, 12, 13 and 14, plus the PR half of 4. Left open: see Follow-ups.
6. **`AGENTS.md` convention** of D14, and the `DESIGN.md` update. Done.

Two things the plan did not foresee:

- `.golangci.yml` exempts `acceptance/` and `tools/` from the `forbidigo` rules of ADR 0020 D7 (every filesystem change through `trace.FS`, every process through the git/gh boundaries). The harness and the doc tools are test and build infrastructure, they run outside wtg, and routing them through `trace.FS` would only record their own actions. Product code is still covered. (`staticcheck` ST1001 is also exempted in `acceptance/`, where the Gomega and Ginkgo dot imports are the idiom.)
- Transcripts are generated by `make transcripts` and ignored by git (D5), so `zola build` needs them first; E9 is handled by the template's "Transcript not generated" message and by `make docs`.

## Without wtg ([ADR 0022](0022-no-tool-dependency.md))

This ADR changes documentation, tests and CI only. No command, file format, git config key, remote ref or shared artefact of wtg changes. A collaborator who does not use wtg sees nothing, except that the project has a documentation site. The sandboxed scenarios use their own git configuration and never write to a user's repositories.

## Consequences

- Documentation examples become a test: a behaviour change that is not reflected in a scenario fails CI, and a scenario cannot show output the binary did not produce.
- A second test layer exists (Ginkgo, acceptance) next to 613 standard-library tests. Contributors need to know both; the acceptance suite is small and has one purpose.
- The site is built only from tags, so documentation fixes wait for a tag unless dispatched by hand.
- PR scenarios rely on a simulated `gh`; this is stated on the page and is a known gap to real GitHub behaviour.
- The README loses its reference content. Anyone who linked to a README anchor may need the new page.
- The CLI library does not change. The generator walks the `cli.Command` tree and must be kept in step with how commands are declared, which the build exercises.
- A build-time dependency on Zola (a pinned, checksummed download) is added; Go builds and `go install` are unaffected.

## Alternatives considered

- **Move to cobra for its documentation generator.** Rejected in D9: large, risky migration of commands and completion for a task a small walker does.
- **Use `urfave/cli-docs`.** Rejected in D9: it omits `Description` and `ArgsUsage` and adds dependencies.
- **Migrate all tests to Ginkgo now.** Rejected in D8: no benefit to the documentation work, large churn.
- **Reuse the in-process tests (`footprint_test.go`) as the source of examples.** Rejected: they skip `main.go`, so the output differs from what users see, and they use unpinned paths and dates.
- **Hand-written examples, reviewed by humans.** Rejected: that is how examples go stale.
- **Committed golden transcripts.** Deferred (D5): churn across platforms and git versions.
- **`testscript` instead of Ginkgo.** Considered in D8; not chosen.
- **Publish on every push to `main`.** Rejected in D13: the site would describe unreleased behaviour that `go install @latest` does not give.
- **mdBook, Hugo, Docusaurus, Starlight.** See D13.
- **Real `gh` against a real GitHub test repository.** Rejected: needs credentials and network in CI, and is not reproducible.

## Follow-ups

- Make wtg itself honour `NO_COLOR` (and `TERM=dumb`); today it colours piped output (D6). User-facing, so it needs its own change and a `CHANGELOG.md` entry.
- Committed golden transcripts, if reviewing output changes through the site artefact turns out to be too weak (D5).
- Migrating the unit tests to Ginkgo, if wanted, as a separate decision (D8).
- Per-release versions of the site, so that documentation for an older release stays reachable (D13).
- Man pages from the same command-tree walker as the reference (D9).
- Scenarios 5, 8, 9, 11 and 15 of D10 are not written: a rebase that conflicts (5), `new --base` and `--branch` (8), `exec` (9), review feedback after the workspace is gone (11), and recovery without wtg (15). Scenario 11 and 15 are the ones that most need a story rather than a command.
- `wtg delete -d` with a merged pull request consults `gh` (ADR 0019 D12); no scenario shows it yet. The fake `gh` would need a `MERGED` state.

## Decisions summary

| ID | Decision |
|----|----------|
| D1 | Four kinds of documentation: scenarios, reference, FAQ, and design (ADRs); the README shrinks to what, why, install, quick start, links |
| D2 | Commands and output on the site come only from transcripts of the real binary; prose is hand-written; a CI check catches hand-typed blocks |
| D3 | A new Ginkgo acceptance suite runs the built binary as a subprocess, one `Ordered` container per scenario, asserting exit code and key lines on every PR |
| D4 | Sandboxed, deterministic, normalised, single-stream runs; persistent `cd`; hidden setup steps; neutral fixtures; never the developer's config or binary on `PATH` |
| D5 | Transcripts are generated at site build on Linux, not committed; the assertions are committed |
| D6 | ANSI colour preserved and rendered as HTML spans |
| D7 | PR scenarios use a labelled fake `gh`; the remote-host question is a phase 1 spike |
| D8 | Ginkgo only for the new acceptance suite; the 613 existing tests stay |
| D9 | Reference generated by a small walker over the `cli.Command` tree; stay on urfave/cli; no cobra; no `cli-docs`; config reference embeds `wtg config init -o -` |
| D10 | Fifteen scenarios in priority order; 1 to 4 first; scenario 4 carries the `merge` / `pr` guidance |
| D11 | FAQ seeded from real output |
| D12 | ADRs and `DESIGN.md` stay in `docs/` on `main`; feature guides move into `site/content/`; README slimmed; links, including the skill's raw URL, updated in the same change |
| D13 | Zola on GitHub Pages, published on release tags and manual dispatch; PRs build and check; pinned, checksummed Zola; EUPL-1.2 accepted for a build-only tool; Hugo as fallback |
| D14 | A broken scenario fails CI; `AGENTS.md` gets a convention line; the reference is generated |
