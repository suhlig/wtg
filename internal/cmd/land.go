package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/urfave/cli/v3"

	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/remote"
	"github.com/suhlig/wtg/internal/state"
	"github.com/suhlig/wtg/internal/trace"
	"github.com/suhlig/wtg/internal/ui"
)

// LandCommand returns the `wtg land` command.
// See docs/adr/0019-workspace-land.md.
func LandCommand(runner git.Runner, gh remote.Runner) *cli.Command {
	return &cli.Command{
		Name:      "land",
		Usage:     "publish each repo's branch as a merge or pull request",
		ArgsUsage: "[<workspace>]",
		Description: `For every repo in the workspace that has commits beyond origin/<default>,
either fast-forwards the remote default branch (strategy "merge") or
pushes the branch and creates a pull request (strategy "pr").

Strategy is read from git config wtg.landStrategy (per-clone or via an
includeIf rule in ~/.gitconfig). The command-line flag overrides it.
When no setting is found, "pr" is used.

After all PRs are created, each one is updated with a sibling block
listing the other PRs in the workspace (use --no-link to suppress).

The workspace argument is optional when run from inside a workspace.`,
		ShellComplete: completeSpaces,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "strategy",
				Usage: `override land strategy for all repos ("merge" or "pr")`,
			},
			&cli.BoolFlag{
				Name:  "draft",
				Usage: "create pull requests as drafts (pr strategy only)",
			},
			&cli.BoolFlag{
				Name:  "no-link",
				Usage: "skip linking sibling pull requests",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			spaceName, err := resolveSpaceArg(cmd.Args().First(), cmd.Args().Len() == 0)
			if err != nil {
				return err
			}
			return RunSpaceLand(ctx, runner, gh, SpaceLandArgs{
				Name:     spaceName,
				Strategy: cmd.String("strategy"),
				Draft:    cmd.Bool("draft"),
				NoLink:   cmd.Bool("no-link"),
			}, os.Stdout)
		},
	}
}

// SpaceLandArgs holds parsed arguments for RunSpaceLand.
type SpaceLandArgs struct {
	Name     string
	Strategy string // "merge", "pr", or "" (read from git config)
	Draft    bool
	NoLink   bool
}

// landResult is the outcome of landing one repo.
type landResult struct {
	opResult
	pr remote.PR // filled for "pr" strategy when a PR was found/created
}

// RunSpaceLand publishes each repo's branch. See docs/adr/0019-workspace-land.md.
func RunSpaceLand(ctx context.Context, runner git.Runner, gh remote.Runner, args SpaceLandArgs, out io.Writer) error {
	if args.Strategy != "" && args.Strategy != "merge" && args.Strategy != "pr" {
		return fmt.Errorf("unknown --strategy %q: must be \"merge\" or \"pr\"", args.Strategy)
	}

	sp, err := state.Load(args.Name)
	if err != nil {
		return fmt.Errorf("load space %q: %w", args.Name, err)
	}

	// Pre-flight: when any repo needs the "pr" strategy, ensure gh is available
	// and authenticated on the relevant hosts.
	if err := landPreflightGH(ctx, runner, gh, sp, args.Strategy); err != nil {
		return err
	}

	results := make([]landResult, len(sp.Repos))

	var wg sync.WaitGroup
	for i, r := range sp.Repos {
		wg.Add(1)
		go func(i int, r state.RepoEntry) {
			defer wg.Done()
			results[i] = landOne(ctx, runner, gh, sp, r, args)
		}(i, r)
	}
	wg.Wait()

	tbl := ui.NewTableWriter(out)
	var attention []string
	for _, res := range results {
		if res.name == "" {
			continue
		}
		tbl.Row(res.name, res.render())
		if res.sym == ui.SymFail || res.sym == ui.SymWarn {
			attention = append(attention, res.name)
		}
	}
	tbl.Flush()

	// Sibling PR linking pass.
	if !args.NoLink {
		if err := linkSiblings(ctx, gh, sp.Branch, results, out); err != nil {
			attention = append(attention, "sibling linking")
		}
	}

	if len(attention) > 0 {
		return fmt.Errorf("land needs attention in: %s", strings.Join(attention, ", "))
	}
	return nil
}

// landPreflightGH checks gh availability and authentication for repos that
// need the "pr" strategy. It runs the checks once per distinct host.
func landPreflightGH(ctx context.Context, runner git.Runner, gh remote.Runner, sp *state.Space, strategyOverride string) error {
	var needsPR bool
	for _, r := range sp.Repos {
		if r.Symlink {
			continue
		}
		s := resolveStrategy(runner, r.RepoPath, strategyOverride)
		if s == "pr" {
			needsPR = true
			break
		}
	}
	if !needsPR {
		return nil
	}
	if err := gh.Available(); err != nil {
		return err
	}
	// Collect distinct GitHub hosts from remote URLs.
	hosts := make(map[string]bool)
	for _, r := range sp.Repos {
		if r.Symlink {
			continue
		}
		s := resolveStrategy(runner, r.RepoPath, strategyOverride)
		if s != "pr" {
			continue
		}
		remoteURL, err := runner.RemoteURL(r.WorktreePath, "origin")
		if err != nil {
			continue
		}
		_, host, _ := remote.ParseGitHubRemote(remoteURL)
		if host != "" {
			hosts[host] = true
		}
	}
	for host := range hosts {
		if err := gh.AuthStatus(ctx, host); err != nil {
			return err
		}
	}
	return nil
}

// resolveStrategy returns the effective land strategy for a repo.
func resolveStrategy(runner git.Runner, repoPath, override string) string {
	if override != "" {
		return override
	}
	val, found, _ := runner.ConfigGet(repoPath, "wtg.landStrategy")
	if found && (val == "merge" || val == "pr") {
		return val
	}
	return "pr"
}

// landOne performs the land operation for one repo.
func landOne(ctx context.Context, runner git.Runner, gh remote.Runner, sp *state.Space, r state.RepoEntry, args SpaceLandArgs) landResult {
	fail := func(format string, a ...any) landResult {
		return landResult{opResult: opResult{r.Name, ui.SymFail, fmt.Sprintf(format, a...)}}
	}
	warn := func(format string, a ...any) landResult {
		return landResult{opResult: opResult{r.Name, ui.SymWarn, fmt.Sprintf(format, a...)}}
	}

	if r.Symlink {
		return warn("skipped — symlink (always.repos)")
	}

	// Check worktree is on the expected branch.
	st, err := runner.Status(r.WorktreePath)
	if err != nil {
		return fail("status: %v", err)
	}
	if st.Branch != sp.Branch {
		on := st.Branch
		if on == "" {
			on = "detached HEAD"
		}
		return warn("skipped — on %s, expected %s", on, sp.Branch)
	}
	if len(st.Files) > 0 {
		// Warn about uncommitted changes, but continue landing commits.
		_ = st // the warning is appended to the result below
	}

	// Determine default branch.
	defaultBranch, err := runner.DefaultBranch(r.WorktreePath)
	if err != nil {
		return fail("cannot determine default branch: %v", err)
	}
	base := "origin/" + defaultBranch

	// Count commits to land.
	ahead, err := runner.CommitsAhead(r.WorktreePath, base, sp.Branch)
	if err != nil {
		return fail("counting commits: %v", err)
	}
	if ahead == 0 {
		return landResult{opResult: opResult{r.Name, ui.SymSkipped, "nothing to land"}}
	}

	strategy := resolveStrategy(runner, r.RepoPath, args.Strategy)

	uncommittedWarn := ""
	if len(st.Files) > 0 {
		uncommittedWarn = " ⚠ uncommitted changes not landed"
	}

	switch strategy {
	case "merge":
		return landMerge(runner, r, sp.Branch, defaultBranch, uncommittedWarn)
	case "pr":
		return landPR(ctx, runner, gh, r, sp.Branch, defaultBranch, args.Draft, uncommittedWarn)
	default:
		return fail("unknown strategy %q", strategy)
	}
}

// landMerge performs the "merge" strategy: fast-forward the remote default branch.
func landMerge(runner git.Runner, r state.RepoEntry, branch, defaultBranch, uncommittedWarn string) landResult {
	fail := func(format string, a ...any) landResult {
		return landResult{opResult: opResult{r.Name, ui.SymFail, fmt.Sprintf(format, a...)}}
	}

	if err := runner.PushTo(r.WorktreePath, branch, defaultBranch); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "non-fast-forward") || strings.Contains(msg, "rejected") {
			return fail("not a fast-forward — run `wtg rebase`, then retry")
		}
		return fail("push failed: %v", msg)
	}

	// Best-effort fast-forward of the main clone's local default branch.
	// Failure here does not fail the land — `wtg repo sync` handles it.
	_ = runner.FastForward(r.RepoPath, defaultBranch)

	if trace.IsDry(runner) {
		msg := fmt.Sprintf("would push %s to origin/%s%s", branch, defaultBranch, uncommittedWarn)
		return landResult{opResult: opResult{r.Name, ui.SymPlan, msg}}
	}
	msg := "landed (merge)" + uncommittedWarn
	return landResult{opResult: opResult{r.Name, ui.SymOK, msg}}
}

// landPR performs the "pr" strategy: push and find-or-create a PR.
func landPR(ctx context.Context, runner git.Runner, gh remote.Runner, r state.RepoEntry, branch, defaultBranch string, draft bool, uncommittedWarn string) landResult {
	fail := func(format string, a ...any) landResult {
		return landResult{opResult: opResult{r.Name, ui.SymFail, fmt.Sprintf(format, a...)}}
	}
	warn := func(pr remote.PR, format string, a ...any) landResult {
		return landResult{opResult: opResult{r.Name, ui.SymWarn, fmt.Sprintf(format, a...)}, pr: pr}
	}

	// Determine host and slug.
	remoteURL, err := runner.RemoteURL(r.WorktreePath, "origin")
	if err != nil {
		return fail("no origin remote: %v", err)
	}
	ownerRepo, host, ok := remote.ParseGitHubRemote(remoteURL)
	if !ok {
		if err := runner.Push(r.WorktreePath, branch); err != nil {
			return fail("pushed but pr strategy needs a GitHub remote (push also failed: %v)", err)
		}
		return fail("pr strategy needs a GitHub remote (branch pushed)")
	}

	// Look for an existing PR first (idempotent).
	existingPR, found, err := gh.FindPR(ctx, host, ownerRepo, branch)
	if err != nil {
		return fail("looking for existing PR: %v", err)
	}
	if found {
		msg := fmt.Sprintf("existing PR %s%s", existingPR.URL, uncommittedWarn)
		return landResult{opResult: opResult{r.Name, ui.SymOK, msg}, pr: existingPR}
	}

	// Push the branch.
	if err := runner.Push(r.WorktreePath, branch); err != nil {
		msg := err.Error()
		if hint := divergedHint(runner, r.WorktreePath, branch); hint != "" {
			msg += "\n" + hint
		}
		return fail("push failed: %v", msg)
	}

	// Create the PR.
	pr, err := gh.CreatePR(ctx, host, ownerRepo, branch, defaultBranch, draft)
	if err != nil {
		return warn(remote.PR{}, "pushed, PR not created: %v", err)
	}

	if trace.IsDry(runner) {
		msg := fmt.Sprintf("would push %s and open PR%s", branch, uncommittedWarn)
		return landResult{opResult: opResult{r.Name, ui.SymPlan, msg}, pr: pr}
	}
	msg := fmt.Sprintf("PR %s%s", pr.URL, uncommittedWarn)
	return landResult{opResult: opResult{r.Name, ui.SymOK, msg}, pr: pr}
}

// linkSiblings edits each PR's body to include the sibling block (D7).
func linkSiblings(ctx context.Context, gh remote.Runner, spaceName string, results []landResult, out io.Writer) error {
	// Collect all PRs that were created or found.
	type prEntry struct {
		host      string
		ownerRepo string
		pr        remote.PR
	}
	var prs []prEntry
	for _, res := range results {
		if res.pr.Number == 0 {
			continue
		}
		// We need the host and ownerRepo — rebuild from the result name.
		// The result name is the short name, e.g. "github.com/owner/repo".
		// Extract host and ownerRepo from it.
		parts := strings.SplitN(res.name, "/", 2)
		if len(parts) != 2 {
			continue
		}
		prs = append(prs, prEntry{host: parts[0], ownerRepo: parts[1], pr: res.pr})
	}

	if len(prs) < 2 {
		return nil // nothing to link
	}

	// ADR 0021 D3: a dry run does not write PR bodies; it says what it would do.
	if trace.IsDry(gh) {
		_, _ = fmt.Fprintf(out, "%s would link %d sibling PRs\n", ui.SymPlan, len(prs))
		return nil
	}

	// For each PR, build the sibling references (all others).
	var linkErr error
	for _, entry := range prs {
		var siblings []string
		for _, other := range prs {
			if other.pr.Number == entry.pr.Number || other.host != entry.host {
				continue
			}
			siblings = append(siblings, fmt.Sprintf("%s#%d", other.ownerRepo, other.pr.Number))
		}
		if len(siblings) == 0 {
			continue
		}

		currentBody, err := gh.PRBody(ctx, entry.host, entry.ownerRepo, entry.pr.Number)
		if err != nil {
			_, _ = fmt.Fprintf(out, "  %s %s: PR linked, siblings not linked: %v\n", ui.SymWarn, entry.ownerRepo, err)
			linkErr = err
			continue
		}

		newBody := withSiblingBlock(currentBody, spaceName, siblings)
		if newBody == currentBody {
			continue
		}
		if err := gh.SetPRBody(ctx, entry.host, entry.ownerRepo, entry.pr.Number, newBody); err != nil {
			_, _ = fmt.Fprintf(out, "  %s %s: PR created, siblings not linked: %v\n", ui.SymWarn, entry.ownerRepo, err)
			linkErr = err
		}
	}
	return linkErr
}

const (
	siblingBlockStart = "<!-- wtg:siblings:start -->"
	siblingBlockEnd   = "<!-- wtg:siblings:end -->"
)

// withSiblingBlock inserts or replaces the sibling block in body.
// It is idempotent: applying it twice yields the same result.
func withSiblingBlock(body, spaceName string, siblings []string) string {
	var lines []string
	lines = append(lines, siblingBlockStart)
	lines = append(lines, fmt.Sprintf("**Part of workspace `%s`.** These pull requests belong together:", spaceName))
	lines = append(lines, "")
	for _, s := range siblings {
		lines = append(lines, "- "+s)
	}
	lines = append(lines, siblingBlockEnd)
	block := strings.Join(lines, "\n")

	startIdx := strings.Index(body, siblingBlockStart)
	endIdx := strings.Index(body, siblingBlockEnd)
	if startIdx >= 0 && endIdx > startIdx {
		// Replace existing block.
		before := body[:startIdx]
		after := body[endIdx+len(siblingBlockEnd):]
		return strings.TrimRight(before, "\n") + "\n\n" + block + strings.TrimLeft(after, "\n")
	}

	// Append.
	if body == "" {
		return block
	}
	return strings.TrimRight(body, "\n") + "\n\n" + block
}
