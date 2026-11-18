+++
title = "Frequently Asked Questions"
description = "Frequently asked questions and troubleshooting tips for wtg."
+++

# Frequently Asked Questions

### Does wtg modify my main clones?

No. `wtg` creates linked worktrees alongside your main clones and never changes the working directory, branch, or HEAD of your discovery clones (except for `repo sync` fast-forwarding the default branch when you explicitly ask it to).

### Do my teammates need wtg?

No. Collaborators never need `wtg` installed. `wtg` creates standard Git branches and linked worktrees. Shared repositories remain plain Git repositories, and per-repo configurations use standard `wtg.*` Git config keys that stay local to your machine.

### Why won't `wtg delete` delete my workspace?

`wtg delete` prioritizes safety and will refuse or prompt for confirmation if:
1. There are **uncommitted changes** in any worktree.
2. There are **unpushed commits** that exist on no remote branch.
3. A local copy of an **`always.secrets`** file was edited and differs from the source clone.
4. It is running in a non-interactive script / pipeline without `--yes`.

To force-delete branches and ignore unmerged commits, use `wtg delete -D` (or `--yes` in scripts).

### Why does wtg never force-push?

`wtg` is designed to never overwrite remote history automatically. If a branch has diverged (for example after a `wtg rebase`), `wtg push` reports the divergence and shows you the explicit command to run if you wish to overwrite:
```
git push --force-with-lease origin <branch>
```

### What does the ambiguous repo name error mean?

When you pass a short name like `api` or `infra` and multiple discovered repositories share that name (e.g. `github.com/myorg/api` and `github.com/client/api`), `wtg` refuses to guess. It lists all matching candidates so you can provide the disambiguated path segment or full path.

### What is the sibling block in my pull request description?

When you land a workspace using `wtg land` with the `pr` strategy, `wtg` links related pull requests together by adding a markdown block:
```markdown
<!-- wtg:siblings:start -->
**Part of workspace `my-feature`.** These pull requests belong together:
- example/api#12
- example/web#34
<!-- wtg:siblings:end -->
```
Pass `--no-link` to `wtg land` if you want to suppress this sibling block.

### What if I delete a workspace directory by hand with `rm -rf`?

If a workspace directory is removed manually without `wtg delete`, Git's administrative metadata still knows about the registered worktrees. Run:
```sh
git worktree prune
```
in your main clones to clean up stale metadata.

### Does wtg work for non-Go repositories?

Yes! `wtg` works with repositories in any programming language. It only generates a `go.work` file when at least one repository in the workspace contains a `go.mod` file.

### Why must `wcd` be a shell function?

A child process (such as a CLI binary) cannot change the working directory of its parent shell. Therefore, `wcd` must be a shell function that calls `cd "$(wtg path "$1")"`.

### Why is output coloured when piped?

`wtg` formats styled terminal output using Lipgloss to provide visual clarity. If you need plain text in tests or scripts, the output can be parsed with standard ANSI stripping tools.
