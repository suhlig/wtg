package cmd

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/state"
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

// --- recorded base branch (ADR 0018 D1, ADR 0019 D4) ---

// spaceWithBase records a per-repo base branch for the single repo in a space
// whose short name is name.
func spaceWithBase(t *testing.T, name, base string) {
	t.Helper()
	sp, err := state.Load("feat")
	if err != nil {
		t.Fatalf("load space: %v", err)
	}
	for i := range sp.Repos {
		if sp.Repos[i].Name == name {
			sp.Repos[i].Base = base
		}
	}
	if err := state.Save(sp); err != nil {
		t.Fatalf("save space: %v", err)
	}
}

func TestRunSpaceLand_UsesRecordedBaseBranch(t *testing.T) {
	isolateState(t)
	makeSpace(t, "feat", "feat", t.TempDir(), []string{"github.com/owner/api"}, "/repos")
	spaceWithBase(t, "github.com/owner/api", "develop")

	r := landRunner()
	r.defaultBranchFn = func(string) (string, error) {
		t.Error("a recorded base must win over the default branch")
		return "main", nil
	}
	var gotBase, gotRemote string
	r.commitsAheadFn = func(_, base, _ string) (int, error) { gotBase = base; return 1, nil }
	r.pushToFn = func(_, _, remoteBranch string) error { gotRemote = remoteBranch; return nil }

	var out bytes.Buffer
	if err := RunSpaceLand(t.Context(), r, &fakeGH{}, SpaceLandArgs{Name: "feat", Strategy: "merge", NoLink: true}, &out); err != nil {
		t.Fatalf("RunSpaceLand: %v\n%s", err, out.String())
	}
	if gotBase != "origin/develop" {
		t.Errorf("counted commits against %q, want origin/develop", gotBase)
	}
	if gotRemote != "develop" {
		t.Errorf("merge-landed into %q, want develop", gotRemote)
	}
}

func TestRunSpaceLand_RecordedBaseIsThePRBase(t *testing.T) {
	isolateState(t)
	makeSpace(t, "feat", "feat", t.TempDir(), []string{"github.com/owner/api"}, "/repos")
	spaceWithBase(t, "github.com/owner/api", "develop")

	gh := &fakeGH{}
	if err := RunSpaceLand(t.Context(), landRunner(), gh, SpaceLandArgs{Name: "feat", Strategy: "pr", NoLink: true}, io.Discard); err != nil {
		t.Fatalf("RunSpaceLand: %v", err)
	}
	if gh.createBase != "develop" {
		t.Errorf("PR base: got %q, want develop", gh.createBase)
	}
}

// --- --delete-remote-branch (ADR 0019 D13) ---

func TestRunSpaceLand_MergeDeletesRemoteBranch(t *testing.T) {
	isolateState(t)
	makeSpace(t, "feat", "feat", t.TempDir(), []string{"github.com/owner/api"}, "/repos")

	r := landRunner()
	r.remoteBranchExistsFn = func(_, _ string) (bool, error) { return true, nil }
	var deleted string
	r.pushDeleteRemoteFn = func(_, branch string) error { deleted = branch; return nil }

	var out bytes.Buffer
	if err := RunSpaceLand(t.Context(), r, &fakeGH{}, SpaceLandArgs{Name: "feat", Strategy: "merge", NoLink: true, DeleteRemoteBranch: true}, &out); err != nil {
		t.Fatalf("RunSpaceLand: %v\n%s", err, out.String())
	}
	if deleted != "feat" {
		t.Errorf("deleted %q on the remote, want feat", deleted)
	}
	if !strings.Contains(out.String(), "origin/feat deleted") {
		t.Errorf("the row should say the remote branch is gone, got:\n%s", out.String())
	}
}

func TestRunSpaceLand_MergeDeleteRemoteBranch_NotPushed(t *testing.T) {
	isolateState(t)
	makeSpace(t, "feat", "feat", t.TempDir(), []string{"github.com/owner/api"}, "/repos")

	r := landRunner()
	r.remoteBranchExistsFn = func(_, _ string) (bool, error) { return false, nil }
	// pushDeleteRemoteFn stays nil: a branch that was never pushed must not be
	// deleted, and the stub panics if it is called.

	var out bytes.Buffer
	if err := RunSpaceLand(t.Context(), r, &fakeGH{}, SpaceLandArgs{Name: "feat", Strategy: "merge", NoLink: true, DeleteRemoteBranch: true}, &out); err != nil {
		t.Fatalf("RunSpaceLand: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "landed (merge)") {
		t.Errorf("expected a plain merge-landing row, got:\n%s", out.String())
	}
}

func TestRunSpaceLand_MergeDeleteRemoteBranch_FailureNeedsAttention(t *testing.T) {
	isolateState(t)
	makeSpace(t, "feat", "feat", t.TempDir(), []string{"github.com/owner/api"}, "/repos")

	r := landRunner()
	r.remoteBranchExistsFn = func(_, _ string) (bool, error) { return true, nil }
	r.pushDeleteRemoteFn = func(_, _ string) error { return errors.New("push rejected") }

	var out bytes.Buffer
	err := RunSpaceLand(t.Context(), r, &fakeGH{}, SpaceLandArgs{Name: "feat", Strategy: "merge", NoLink: true, DeleteRemoteBranch: true}, &out)
	if err == nil {
		t.Fatalf("a failed remote-branch deletion must exit non-zero:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "origin/feat not deleted") {
		t.Errorf("expected the deletion failure to be reported, got:\n%s", out.String())
	}
}

func TestRunSpaceLand_DryRun_PlansRemoteBranchDeletion(t *testing.T) {
	isolateState(t)
	makeSpace(t, "feat", "feat", t.TempDir(), []string{"github.com/owner/api"}, "/repos")

	var out bytes.Buffer
	err := RunSpaceLand(t.Context(), dryGit{landRunner()}, &fakeGH{}, SpaceLandArgs{Name: "feat", Strategy: "merge", NoLink: true, DeleteRemoteBranch: true}, &out)
	if err != nil {
		t.Fatalf("RunSpaceLand: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "would push feat to origin/main and delete origin/feat") {
		t.Errorf("expected the plan to include the deletion, got:\n%s", out.String())
	}
}

// --- land --delete (ADR 0019 D13) ---

func TestLandCommand_DeleteStaysPutWhenLandNeedsAttention(t *testing.T) {
	isolateState(t)
	makeSpace(t, "feat", "feat", t.TempDir(), []string{"github.com/owner/api"}, "/repos")

	r := landRunner()
	r.statusFn = func(string) (git.RepoStatus, error) { return git.RepoStatus{Branch: "other"}, nil }

	err := LandCommand(r, &fakeGH{}).Run(t.Context(), []string{"land", "feat", "--delete", "--no-link", "--strategy", "merge"})
	if err == nil {
		t.Fatal("a repo on the wrong branch must fail the land")
	}
	if _, statErr := state.Load("feat"); statErr != nil {
		t.Errorf("a failed land must not delete the workspace: %v", statErr)
	}
}
