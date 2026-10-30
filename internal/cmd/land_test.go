package cmd

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/suhlig/wtg/internal/git"
)

func TestWithSiblingBlock_append(t *testing.T) {
	body := "Some PR description."
	got := withSiblingBlock(body, "my-feature", []string{"owner/api#1", "owner/payments#2"})
	if !strings.Contains(got, siblingBlockStart) {
		t.Errorf("missing start marker: %q", got)
	}
	if !strings.Contains(got, siblingBlockEnd) {
		t.Errorf("missing end marker: %q", got)
	}
	if !strings.Contains(got, "my-feature") {
		t.Errorf("missing workspace name: %q", got)
	}
	if !strings.Contains(got, "owner/api#1") {
		t.Errorf("missing sibling ref: %q", got)
	}
	if !strings.Contains(got, "Some PR description.") {
		t.Errorf("original body was lost: %q", got)
	}
}

func TestWithSiblingBlock_empty(t *testing.T) {
	got := withSiblingBlock("", "feat", []string{"owner/b#1"})
	if !strings.HasPrefix(got, siblingBlockStart) {
		t.Errorf("expected block at start of empty body, got: %q", got)
	}
}

func TestWithSiblingBlock_replace(t *testing.T) {
	initial := withSiblingBlock("Original body.", "feat", []string{"owner/a#1"})
	updated := withSiblingBlock(initial, "feat", []string{"owner/a#1", "owner/b#2"})

	// The original body must be present and the markers appear only once.
	if strings.Count(updated, siblingBlockStart) != 1 {
		t.Errorf("start marker appears %d times, want 1: %q", strings.Count(updated, siblingBlockStart), updated)
	}
	if !strings.Contains(updated, "owner/b#2") {
		t.Errorf("updated block missing new sibling: %q", updated)
	}
	if !strings.Contains(updated, "Original body.") {
		t.Errorf("original body was lost after replace: %q", updated)
	}
}

func TestWithSiblingBlock_idempotent(t *testing.T) {
	body := "A description."
	siblings := []string{"owner/x#5"}
	first := withSiblingBlock(body, "feat", siblings)
	second := withSiblingBlock(first, "feat", siblings)
	if first != second {
		t.Errorf("withSiblingBlock not idempotent:\nfirst:  %q\nsecond: %q", first, second)
	}
}

// --- RunSpaceLand: exit codes and --dry-run (ADR 0019 D11.1, ADR 0021 D3) ---

// dryGH is a fakeGH that reports dry-run mode, like the production runner does
// when the recorder is in dry-run mode.
type dryGH struct{ *fakeGH }

func (dryGH) DryRun() bool { return true }

// dryGit is a testRunner that reports dry-run mode.
type dryGit struct{ *testRunner }

func (dryGit) DryRun() bool { return true }

func landRunner() *testRunner {
	return &testRunner{
		statusFn:        func(string) (git.RepoStatus, error) { return git.RepoStatus{Branch: "feat"}, nil },
		defaultBranchFn: func(string) (string, error) { return "main", nil },
		commitsAheadFn:  func(_, _, _ string) (int, error) { return 1, nil },
		configGetFn:     func(_, _ string) (string, bool, error) { return "", false, nil },
		remoteURLFn:     func(string, string) (string, error) { return "git@github.com:owner/repo.git", nil },
		pushFn:          func(string, string) error { return nil },
		pushToFn:        func(_, _, _ string) error { return nil },
		fastForwardFn:   func(_, _ string) error { return nil },
	}
}

func TestRunSpaceLand_ExitsNonZeroWhenARepoNeedsAttention(t *testing.T) {
	isolateState(t)
	makeSpace(t, "feat", "feat", t.TempDir(), []string{"github.com/owner/api"}, "/repos")
	r := landRunner()
	r.statusFn = func(string) (git.RepoStatus, error) { return git.RepoStatus{Branch: "other"}, nil }

	err := RunSpaceLand(t.Context(), r, &fakeGH{}, SpaceLandArgs{Name: "feat", NoLink: true}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "needs attention") {
		t.Fatalf("a repo on the wrong branch must make land fail, got %v", err)
	}
}

func TestRunSpaceLand_NothingToLandIsNotAFailure(t *testing.T) {
	isolateState(t)
	makeSpace(t, "feat", "feat", t.TempDir(), []string{"github.com/owner/api"}, "/repos")
	r := landRunner()
	r.commitsAheadFn = func(_, _, _ string) (int, error) { return 0, nil }

	if err := RunSpaceLand(t.Context(), r, &fakeGH{}, SpaceLandArgs{Name: "feat", NoLink: true}, io.Discard); err != nil {
		t.Fatalf("nothing to land must exit 0: %v", err)
	}
}

func TestRunSpaceLand_DryRun_PlansAndDoesNotLinkBodies(t *testing.T) {
	isolateState(t)
	makeSpace(t, "feat", "feat", t.TempDir(), []string{"github.com/owner/api", "github.com/owner/web"}, "/repos")
	gh := &fakeGH{}

	var out bytes.Buffer
	err := RunSpaceLand(t.Context(), dryGit{landRunner()}, dryGH{gh}, SpaceLandArgs{Name: "feat"}, &out)
	if err != nil {
		t.Fatalf("RunSpaceLand: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "would push feat and open PR") {
		t.Errorf("expected plan wording, got:\n%s", got)
	}
	if !strings.Contains(got, "would link 2 sibling PRs") {
		t.Errorf("expected 'would link 2 sibling PRs', got:\n%s", got)
	}
	if len(gh.prBodies) != 0 {
		t.Errorf("a dry run must not write PR bodies: %v", gh.prBodies)
	}
}

func TestRunSpaceLand_DryRun_MergeStrategyPlansPushToDefaultBranch(t *testing.T) {
	isolateState(t)
	makeSpace(t, "feat", "feat", t.TempDir(), []string{"github.com/owner/api"}, "/repos")
	// A production dry-run runner skips PushTo at the boundary; this test only
	// checks the wording that the command derives from the dry-run state.
	r := landRunner()

	var out bytes.Buffer
	if err := RunSpaceLand(t.Context(), dryGit{r}, &fakeGH{}, SpaceLandArgs{Name: "feat", Strategy: "merge"}, &out); err != nil {
		t.Fatalf("RunSpaceLand: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "would push feat to origin/main") || strings.Contains(got, "landed (merge)") {
		t.Errorf("expected plan wording, got:\n%s", got)
	}
}

func TestRunSpaceLand_PRStrategy_NonGitHubRemoteFails(t *testing.T) {
	isolateState(t)
	makeSpace(t, "feat", "feat", t.TempDir(), []string{"github.com/owner/api"}, "/repos")
	r := landRunner()
	r.remoteURLFn = func(string, string) (string, error) { return "git@example.com:owner/repo.git", nil }
	var pushed bool
	r.pushFn = func(string, string) error { pushed = true; return nil }

	var out bytes.Buffer
	err := RunSpaceLand(t.Context(), r, &fakeGH{}, SpaceLandArgs{Name: "feat", Strategy: "pr", NoLink: true}, &out)
	if err == nil {
		t.Fatal("a non-GitHub remote with the pr strategy must fail the run")
	}
	if !pushed {
		t.Error("the branch is pushed before the failure is reported (ADR 0019 E10)")
	}
	if !strings.Contains(out.String(), "pr strategy needs a GitHub remote") {
		t.Errorf("expected the E10 message, got:\n%s", out.String())
	}
}
