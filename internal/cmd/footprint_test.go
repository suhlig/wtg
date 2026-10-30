package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/suhlig/wtg/internal/config"
	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/git/testhelper"
	"github.com/suhlig/wtg/internal/state"
	"github.com/suhlig/wtg/internal/trace"
)

// dryRecorder bundles a dry-run recorder with an FS that shares it.
type dryRecorder struct {
	*trace.Recorder
	fs trace.FS
}

func newDryRecorder() dryRecorder {
	rec := trace.NewRecorder(true)
	return dryRecorder{Recorder: rec, fs: trace.NewOsFS(rec)}
}

// These tests use real repositories. They check the invariant of ADR 0022 D6
// (wtg leaves no footprint in a repository) and the `land && delete` chain of
// ADR 0019 D11.

// gitOut runs git in dir and returns trimmed stdout.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...) // #nosec G204 -- test helper
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// dirListing returns "relative/path:content" for every file below dir, sorted.
func dirListing(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		data, _ := os.ReadFile(p) // #nosec G304 -- test walks its own temp dir
		rel, _ := filepath.Rel(dir, p)
		lines = append(lines, rel+":"+string(data))
		return nil
	})
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// lineDiff returns the lines of after that are not in before.
func lineDiff(before, after string) []string {
	seen := map[string]bool{}
	for l := range strings.SplitSeq(before, "\n") {
		seen[l] = true
	}
	var added []string
	for l := range strings.SplitSeq(after, "\n") {
		if !seen[l] {
			added = append(added, l)
		}
	}
	return added
}

// footprint captures everything of a repository that a collaborator could see
// or that wtg could have touched: refs, tracked files, config, hooks, info/.
type footprint struct {
	refs, files, config, hooks, info, worktrees string
}

func snapshotRepo(t *testing.T, repo string) footprint {
	t.Helper()
	gitDir := filepath.Join(repo, ".git")
	cfg, _ := os.ReadFile(filepath.Join(gitDir, "config")) // #nosec G304 -- test temp dir
	return footprint{
		refs:      gitOut(t, repo, "for-each-ref", "--format=%(refname) %(objectname)"),
		files:     gitOut(t, repo, "ls-files"),
		config:    string(cfg),
		hooks:     dirListing(t, filepath.Join(gitDir, "hooks")),
		info:      dirListing(t, filepath.Join(gitDir, "info")),
		worktrees: gitOut(t, repo, "worktree", "list", "--porcelain"),
	}
}

// realSpace creates a real repo with a bare origin and a wtg workspace "feat"
// containing one worktree of it, using the production runner.
func realSpace(t *testing.T) (repo *testhelper.Repo, cfg *config.Config, worktree string) {
	t.Helper()
	isolateState(t)
	local, _ := testhelper.InitWithRemote(t)
	cfg = &config.Config{
		Discovery: config.DiscoveryConfig{RootDir: filepath.Dir(local.Path), MaxDepth: 1},
		Spaces:    config.SpacesConfig{RootDir: t.TempDir()},
	}
	name := filepath.Base(local.Path)
	var out bytes.Buffer
	if err := RunSpaceNew(cfg, git.New(), SpaceNewArgs{Name: "feat", Repos: []string{name}}, &out); err != nil {
		t.Fatalf("RunSpaceNew: %v\n%s", err, out.String())
	}
	return local, cfg, filepath.Join(cfg.Spaces.RootDir, "feat", name)
}

func TestFootprint_NewThenDeleteRestoresRepo(t *testing.T) {
	isolateState(t)
	local, _ := testhelper.InitWithRemote(t)
	cfg := &config.Config{
		Discovery: config.DiscoveryConfig{RootDir: filepath.Dir(local.Path), MaxDepth: 1},
		Spaces:    config.SpacesConfig{RootDir: t.TempDir()},
	}
	name := filepath.Base(local.Path)
	runner := git.New()

	before := snapshotRepo(t, local.Path)

	if err := RunSpaceNew(cfg, runner, SpaceNewArgs{Name: "feat", Repos: []string{name}}, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunSpaceNew: %v", err)
	}
	after := snapshotRepo(t, local.Path)

	// Only a branch and a worktree entry were added.
	if after.config != before.config {
		t.Errorf(".git/config changed:\n%s", after.config)
	}
	if after.hooks != before.hooks {
		t.Error(".git/hooks changed")
	}
	if after.info != before.info {
		t.Error(".git/info changed")
	}
	if after.files != before.files {
		t.Error("tracked files changed")
	}
	if added := lineDiff(before.refs, after.refs); len(added) != 1 || !strings.HasPrefix(added[0], "refs/heads/feat ") {
		t.Errorf("expected exactly one new ref refs/heads/feat, got %q", added)
	}
	if !strings.Contains(after.worktrees, filepath.Join(cfg.Spaces.RootDir, "feat")) {
		t.Errorf("expected the new worktree to be listed:\n%s", after.worktrees)
	}

	// delete -d returns the repository to exactly where it started.
	if err := RunSpaceDelete(cfg, runner, SpaceDeleteArgs{Name: "feat", DeleteBranch: true, NoTerminal: true}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("RunSpaceDelete: %v", err)
	}
	if got := snapshotRepo(t, local.Path); got != before {
		t.Errorf("repository differs after new + delete -d:\nbefore: %+v\nafter:  %+v", before, got)
	}
}

func TestFootprint_DryRunChangesNothing(t *testing.T) {
	isolateState(t)
	local, _ := testhelper.InitWithRemote(t)
	cfg := &config.Config{
		Discovery: config.DiscoveryConfig{RootDir: filepath.Dir(local.Path), MaxDepth: 1},
		Spaces:    config.SpacesConfig{RootDir: t.TempDir()},
	}
	rec := newDryRecorder()
	before := snapshotRepo(t, local.Path)
	stateBefore := dirListing(t, os.Getenv("XDG_DATA_HOME"))

	var out bytes.Buffer
	if err := RunSpaceNew(cfg, git.New(rec.Recorder), SpaceNewArgs{Name: "feat", Repos: []string{filepath.Base(local.Path)}}, &out, rec.fs); err != nil {
		t.Fatalf("RunSpaceNew: %v", err)
	}
	if got := snapshotRepo(t, local.Path); got != before {
		t.Errorf("dry-run changed the repository:\n%+v", got)
	}
	if got := dirListing(t, os.Getenv("XDG_DATA_HOME")); got != stateBefore {
		t.Error("dry-run changed the data directory")
	}
	if _, err := os.Stat(filepath.Join(cfg.Spaces.RootDir, "feat")); err == nil {
		t.Error("dry-run created the workspace directory")
	}
	if !strings.Contains(out.String(), "would create space") || strings.Contains(out.String(), "created space \"feat\"") {
		t.Errorf("result rows must use plan wording:\n%s", out.String())
	}
}

func TestLandThenDelete_MergeStrategy_PushedEarlierThenMoreCommits(t *testing.T) {
	local, cfg, wt := realSpace(t)
	local.GitCmd("config", "--local", "wtg.landStrategy", "merge")
	runner := git.New()

	// ADR 0019 D11 case 5: pushed with -u, then more commits, then merge-landed.
	w := testhelper.RepoAt(t, wt)
	w.Commit("first")
	w.GitCmd("push", "-u", "origin", "feat")
	w.Commit("second")

	if err := RunSpaceLand(t.Context(), runner, nil, SpaceLandArgs{Name: "feat", NoLink: true}, &bytes.Buffer{}); err != nil {
		t.Fatalf("land: %v", err)
	}

	// No warning, no prompt (no terminal, no --yes): delete -d must just work.
	var out bytes.Buffer
	if err := RunSpaceDelete(cfg, runner, SpaceDeleteArgs{Name: "feat", DeleteBranch: true, NoTerminal: true}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("delete after land: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "unpushed") {
		t.Errorf("false alarm after merge-landing:\n%s", out.String())
	}
	if branches := gitOut(t, local.Path, "branch", "--list", "feat"); branches != "" {
		t.Errorf("branch feat should be deleted by -d, got %q", branches)
	}
}

func TestDelete_NeverPushedBranch_IsProtected(t *testing.T) {
	local, cfg, wt := realSpace(t)
	testhelper.RepoAt(t, wt).Commit("only here")
	runner := git.New()

	// Without --yes and a terminal, delete refuses: the commit exists nowhere else.
	err := RunSpaceDelete(cfg, runner, SpaceDeleteArgs{Name: "feat", NoTerminal: true}, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected refusal mentioning --yes, got %v", err)
	}
	if _, err := state.Load("feat"); err != nil {
		t.Fatal("nothing may be deleted")
	}

	// With --yes the worktree goes, and -d still refuses to drop the branch.
	var out bytes.Buffer
	if err := RunSpaceDelete(cfg, runner, SpaceDeleteArgs{Name: "feat", DeleteBranch: true, Yes: true}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("delete --yes: %v", err)
	}
	if gitOut(t, local.Path, "branch", "--list", "feat") == "" {
		t.Error("-d must keep a branch whose commit exists on no remote")
	}
	if !strings.Contains(out.String(), "exist on no remote") {
		t.Errorf("expected the reason in the output:\n%s", out.String())
	}
}
