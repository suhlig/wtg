package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suhlig/wtg/internal/config"
	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/git/testhelper"
	"github.com/suhlig/wtg/internal/state"
)

// Real-git scenarios for read-only members (docs/adr/0024, implementation
// step 3). The mock-runner tests in readonly_test.go and drift_test.go pin the
// logic; these run the production runner against real repositories, so a
// mistake in how a git command is built cannot hide behind a stub.

// readOnlyEnv is a real repo with a bare origin and an empty workspace "feat".
type readOnlyEnv struct {
	repo     *testhelper.Repo
	cfg      *config.Config
	name     string // the repo's name in the workspace
	worktree string // where the repo's worktree lives in the workspace
	runner   git.Runner
}

func newReadOnlyEnv(t *testing.T) *readOnlyEnv {
	t.Helper()
	isolateState(t)
	local, _ := testhelper.InitWithRemote(t)
	cfg := &config.Config{
		Discovery: config.DiscoveryConfig{RootDir: filepath.Dir(local.Path), MaxDepth: 1},
		Spaces:    config.SpacesConfig{RootDir: t.TempDir()},
	}
	spacePath := filepath.Join(cfg.Spaces.RootDir, "feat")
	makeSpace(t, "feat", "feat", spacePath, nil, "")
	name := filepath.Base(local.Path)
	return &readOnlyEnv{
		repo:     local,
		cfg:      cfg,
		name:     name,
		worktree: filepath.Join(spacePath, name),
		runner:   git.New(),
	}
}

func (e *readOnlyEnv) addReadOnly(t *testing.T, ref string) {
	t.Helper()
	var out bytes.Buffer
	err := RunSpaceAdd(e.cfg, e.runner, SpaceAddArgs{Name: "feat", Repos: []string{e.name}, ReadOnly: true, Ref: ref}, &out)
	if err != nil {
		t.Fatalf("add --read-only: %v\n%s", err, out.String())
	}
}

func (e *readOnlyEnv) entry(t *testing.T) state.RepoEntry {
	t.Helper()
	sp, err := state.Load("feat")
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if len(sp.Repos) != 1 {
		t.Fatalf("state repos = %+v, want one entry", sp.Repos)
	}
	return sp.Repos[0]
}

// pushNewCommit adds a commit to the main clone's main branch and publishes it.
func (e *readOnlyEnv) pushNewCommit(t *testing.T, message string) {
	t.Helper()
	e.repo.Commit(message)
	e.repo.GitCmd("push", "origin", "main")
}

func (e *readOnlyEnv) edit(t *testing.T) string {
	t.Helper()
	path := filepath.Join(e.worktree, "README.md")
	if err := os.WriteFile(path, []byte("local edit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadOnlyGit_AddPinned_IsDetachedAtTagWithNoBranch(t *testing.T) {
	e := newReadOnlyEnv(t)
	e.repo.GitCmd("tag", "v1.0.0")
	tagged := gitOut(t, e.repo.Path, "rev-parse", "HEAD")
	e.pushNewCommit(t, "after the tag")
	refsBefore := gitOut(t, e.repo.Path, "for-each-ref", "refs/heads")

	e.addReadOnly(t, "v1.0.0")

	if got := gitOut(t, e.worktree, "rev-parse", "HEAD"); got != tagged {
		t.Errorf("worktree HEAD = %s, want the tag's commit %s", got, tagged)
	}
	if got := gitOut(t, e.worktree, "branch", "--show-current"); got != "" {
		t.Errorf("worktree is on branch %q, want detached", got)
	}
	if got := gitOut(t, e.repo.Path, "for-each-ref", "refs/heads"); got != refsBefore {
		t.Errorf("a read-only member must not create a branch:\nbefore: %s\nafter:  %s", refsBefore, got)
	}
	if got := e.entry(t); !got.ReadOnly || got.Ref != "v1.0.0" {
		t.Errorf("state entry = %+v, want read-only at v1.0.0", got)
	}
}

func TestReadOnlyGit_AddFollowing_DefaultsToOriginMain(t *testing.T) {
	e := newReadOnlyEnv(t)

	e.addReadOnly(t, "")

	if got := e.entry(t); !got.ReadOnly || got.Ref != "origin/main" {
		t.Errorf("state entry = %+v, want read-only following origin/main", got)
	}
	if want := gitOut(t, e.repo.Path, "rev-parse", "origin/main"); gitOut(t, e.worktree, "rev-parse", "HEAD") != want {
		t.Errorf("worktree is not at origin/main (%s)", want)
	}
	if got := gitOut(t, e.worktree, "branch", "--show-current"); got != "" {
		t.Errorf("worktree is on branch %q, want detached", got)
	}
}

func TestReadOnlyGit_AddWithUnknownRef_FailsBeforeCreatingAnything(t *testing.T) {
	e := newReadOnlyEnv(t)

	err := RunSpaceAdd(e.cfg, e.runner, SpaceAddArgs{Name: "feat", Repos: []string{e.name}, ReadOnly: true, Ref: "v9.9.9"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected an error for a ref that does not exist")
	}
	if _, serr := os.Stat(e.worktree); serr == nil {
		t.Error("a worktree was created although the ref does not resolve")
	}
	if sp, lerr := state.Load("feat"); lerr != nil || len(sp.Repos) != 0 {
		t.Errorf("state must stay empty after a failed add: %+v, %v", sp, lerr)
	}
}

func TestReadOnlyGit_Rebase_AdvancesFollowingMemberAndLeavesPinnedOne(t *testing.T) {
	t.Run("following", func(t *testing.T) {
		e := newReadOnlyEnv(t)
		e.addReadOnly(t, "")
		old := gitOut(t, e.worktree, "rev-parse", "HEAD")

		e.pushNewCommit(t, "upstream moved")

		var out bytes.Buffer
		if err := RunSpaceRebase(e.runner, "feat", "", false, &out); err != nil {
			t.Fatalf("RunSpaceRebase: %v\n%s", err, out.String())
		}
		tip := gitOut(t, e.repo.Path, "rev-parse", "origin/main")
		if got := gitOut(t, e.worktree, "rev-parse", "HEAD"); got != tip || got == old {
			t.Errorf("worktree HEAD = %s, want the new tip %s (was %s)", got, tip, old)
		}
		if !strings.Contains(out.String(), "advanced (was ") {
			t.Errorf("output: %q", out.String())
		}
		if got := gitOut(t, e.worktree, "branch", "--show-current"); got != "" {
			t.Errorf("advancing must keep the worktree detached, got branch %q", got)
		}
	})

	t.Run("pinned", func(t *testing.T) {
		e := newReadOnlyEnv(t)
		e.repo.GitCmd("tag", "v1.0.0")
		e.addReadOnly(t, "v1.0.0")
		old := gitOut(t, e.worktree, "rev-parse", "HEAD")

		e.pushNewCommit(t, "upstream moved")

		var out bytes.Buffer
		if err := RunSpaceRebase(e.runner, "feat", "", false, &out); err != nil {
			t.Fatalf("a pinned member must not make rebase fail: %v\n%s", err, out.String())
		}
		if got := gitOut(t, e.worktree, "rev-parse", "HEAD"); got != old {
			t.Errorf("a pinned member moved from %s to %s", old, got)
		}
		if !strings.Contains(out.String(), "pinned at v1.0.0") {
			t.Errorf("output: %q", out.String())
		}
	})

	t.Run("modified", func(t *testing.T) {
		e := newReadOnlyEnv(t)
		e.addReadOnly(t, "")
		old := gitOut(t, e.worktree, "rev-parse", "HEAD")
		edited := e.edit(t)

		e.pushNewCommit(t, "upstream moved")

		var out bytes.Buffer
		_ = RunSpaceRebase(e.runner, "feat", "", false, &out)
		if got := gitOut(t, e.worktree, "rev-parse", "HEAD"); got != old {
			t.Errorf("a modified member moved from %s to %s\n%s", old, got, out.String())
		}
		if data, _ := os.ReadFile(edited); string(data) != "local edit\n" { // #nosec G304 -- test path
			t.Errorf("the local edit was lost: %q", data)
		}
		if gitOut(t, e.worktree, "stash", "list") != "" {
			t.Error("nothing may be stashed")
		}
		if !strings.Contains(out.String(), "modified") {
			t.Errorf("a modified member must be reported: %q", out.String())
		}
	})
}

func TestReadOnlyGit_Promote_CleanMemberBecomesBranchWorktree(t *testing.T) {
	e := newReadOnlyEnv(t)
	e.repo.GitCmd("tag", "v1.0.0")
	e.addReadOnly(t, "v1.0.0")

	var out bytes.Buffer
	if err := RunSpaceAdd(e.cfg, e.runner, SpaceAddArgs{Name: "feat", Repos: []string{e.name}}, &out); err != nil {
		t.Fatalf("promote: %v\n%s", err, out.String())
	}

	if got := gitOut(t, e.worktree, "branch", "--show-current"); got != "feat" {
		t.Errorf("promoted worktree is on %q, want the workspace branch feat", got)
	}
	if got := gitOut(t, e.repo.Path, "branch", "--list", "feat"); got == "" {
		t.Error("the workspace branch was not created")
	}
	if got := e.entry(t); got.ReadOnly || got.Ref != "" {
		t.Errorf("state entry = %+v, want an ordinary member", got)
	}
}

func TestReadOnlyGit_Promote_ModifiedMemberIsRefused(t *testing.T) {
	e := newReadOnlyEnv(t)
	e.addReadOnly(t, "")
	edited := e.edit(t)

	err := RunSpaceAdd(e.cfg, e.runner, SpaceAddArgs{Name: "feat", Repos: []string{e.name}}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected a refusal to promote a modified member")
	}
	if data, _ := os.ReadFile(edited); string(data) != "local edit\n" { // #nosec G304 -- test path
		t.Errorf("the local edit was lost: %q", data)
	}
	if got := gitOut(t, e.repo.Path, "branch", "--list", "feat"); got != "" {
		t.Errorf("a refused promotion must not create the branch, got %q", got)
	}
	if got := e.entry(t); !got.ReadOnly {
		t.Errorf("state entry = %+v, want still read-only", got)
	}
}

// D7: the only place this feature could remove something the user did not ask
// to remove is a same-named branch in the member's main clone. The mock-runner
// test proves BranchDelete is not called; this proves the branch survives.
func TestReadOnlyGit_DeleteWithForceBranch_LeavesSameNamedBranchUntouched(t *testing.T) {
	e := newReadOnlyEnv(t)
	// A branch called like the workspace branch, left over from an earlier
	// workspace, with a commit that exists nowhere else.
	e.repo.GitCmd("checkout", "-b", "feat")
	e.repo.Commit("leftover work")
	e.repo.GitCmd("checkout", "main")
	before := gitOut(t, e.repo.Path, "rev-parse", "refs/heads/feat")

	e.addReadOnly(t, "")

	var out bytes.Buffer
	args := SpaceDeleteArgs{Name: "feat", ForceBranch: true, NoTerminal: true}
	if err := RunSpaceDelete(t.Context(), e.cfg, e.runner, nil, args, strings.NewReader(""), &out); err != nil {
		t.Fatalf("RunSpaceDelete: %v\n%s", err, out.String())
	}

	if got := gitOut(t, e.repo.Path, "rev-parse", "refs/heads/feat"); got != before {
		t.Errorf("branch feat changed from %s to %s", before, got)
	}
	if _, serr := os.Stat(e.worktree); serr == nil {
		t.Error("the read-only worktree was not removed")
	}
	if strings.Contains(out.String(), "unpushed") || strings.Contains(out.String(), "branch not deleted") {
		t.Errorf("a read-only member has no branch to warn about:\n%s", out.String())
	}
}

// --- drift (D6) against real repositories ---

// driftRepos is a provider repo (module example.com/lib) with the tags v1.0.0
// and v1.1.0 one commit apart, and a consumer directory requiring it.
type driftRepos struct {
	provider *testhelper.Repo
	entries  []state.RepoEntry
	first    string // commit of v1.0.0
}

func newDriftRepos(t *testing.T, consumerGoMod string) *driftRepos {
	t.Helper()
	provider := testhelper.Init(t)
	provider.WriteFile("go.mod", "module example.com/lib\n\ngo 1.22\n")
	provider.GitCmd("add", ".")
	provider.GitCmd("commit", "-m", "module")
	provider.GitCmd("tag", "v1.0.0")
	first := gitOut(t, provider.Path, "rev-parse", "HEAD")
	provider.Commit("second")
	provider.GitCmd("tag", "v1.1.0")

	consumer := t.TempDir()
	if err := os.WriteFile(filepath.Join(consumer, "go.mod"), []byte(consumerGoMod), 0o600); err != nil {
		t.Fatal(err)
	}
	return &driftRepos{
		provider: provider,
		first:    first,
		entries: []state.RepoEntry{
			{Name: "cli", WorktreePath: consumer},
			{Name: "lib", WorktreePath: provider.Path},
		},
	}
}

func (d *driftRepos) findings(t *testing.T) string {
	t.Helper()
	var texts []string
	for _, f := range driftFindings(git.New(), d.entries) {
		texts = append(texts, f.text)
	}
	return strings.Join(texts, "\n")
}

func requiring(version string) string {
	return "module example.com/cli\n\ngo 1.22\n\nrequire example.com/lib " + version + "\n"
}

func TestDriftGit_CheckoutBehindRequiredVersion_Warns(t *testing.T) {
	d := newDriftRepos(t, requiring("v1.1.0"))
	d.provider.GitCmd("checkout", "--detach", "v1.0.0")

	if got := d.findings(t); !strings.Contains(got, "lib is 1 commit behind v1.1.0, which cli requires") {
		t.Errorf("findings: %q", got)
	}
}

func TestDriftGit_CheckoutAheadOfRequiredVersion_IsANote(t *testing.T) {
	d := newDriftRepos(t, requiring("v1.0.0"))
	d.provider.GitCmd("checkout", "--detach", "v1.1.0")

	findings := driftFindings(git.New(), d.entries)
	if len(findings) != 1 || !findings[0].muted {
		t.Fatalf("want one muted note, got %+v", findings)
	}
	if !strings.Contains(findings[0].text, "lib is 1 commit ahead of v1.0.0, which cli requires") {
		t.Errorf("finding: %q", findings[0].text)
	}
}

func TestDriftGit_DivergedCheckout_IsReported(t *testing.T) {
	d := newDriftRepos(t, requiring("v1.1.0"))
	d.provider.GitCmd("checkout", "--detach", "v1.0.0")
	d.provider.Commit("elsewhere")

	if got := d.findings(t); !strings.Contains(got, "lib has diverged from v1.1.0, which cli requires") {
		t.Errorf("findings: %q", got)
	}
}

func TestDriftGit_CheckoutAtRequiredVersion_SaysNothing(t *testing.T) {
	d := newDriftRepos(t, requiring("v1.1.0"))
	d.provider.GitCmd("checkout", "--detach", "v1.1.0")

	if got := d.findings(t); got != "" {
		t.Errorf("findings: %q, want none", got)
	}
}

func TestDriftGit_PseudoVersion_ResolvesByCommitSuffix(t *testing.T) {
	d := newDriftRepos(t, "")
	pseudo := "v1.0.1-0.20260101120000-" + d.first[:12]
	if err := os.WriteFile(filepath.Join(d.entries[0].WorktreePath, "go.mod"), []byte(requiring(pseudo)), 0o600); err != nil {
		t.Fatal(err)
	}
	// The provider's HEAD is one commit past the pseudo-version's commit.
	if got := d.findings(t); !strings.Contains(got, "lib is 1 commit ahead of "+pseudo) {
		t.Errorf("findings: %q", got)
	}
}

func TestDriftGit_ReplacedModule_IsSkipped(t *testing.T) {
	d := newDriftRepos(t, requiring("v1.1.0")+"\nreplace example.com/lib => ../lib\n")
	d.provider.GitCmd("checkout", "--detach", "v1.0.0")

	if got := d.findings(t); got != "" {
		t.Errorf("a replace is the user's decision, got: %q", got)
	}
}

func TestDriftGit_VersionNotInTheClone_IsNotAFalseAlarm(t *testing.T) {
	d := newDriftRepos(t, requiring("v9.9.9"))
	d.provider.GitCmd("checkout", "--detach", "v1.0.0")

	if got := d.findings(t); got != "" {
		t.Errorf("an unresolvable version must say nothing, got: %q", got)
	}
}
