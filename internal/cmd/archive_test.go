package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/suhlig/wtg/internal/archive"
	"github.com/suhlig/wtg/internal/config"
	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/state"
)

// makeRepoDir creates a directory that looks like a git repo (it has a .git
// entry) at <root>/<name> and returns its absolute path.
func makeRepoDir(t *testing.T, root, name string) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Join(p, ".git"), 0o750); err != nil {
		t.Fatalf("makeRepoDir %s: %v", name, err)
	}
	return p
}

func archiveCfg(discoveryRoot, archiveRoot string) *config.Config {
	return &config.Config{
		Discovery: config.DiscoveryConfig{RootDir: discoveryRoot, MaxDepth: 2},
		Archive:   config.ArchiveConfig{RootDir: archiveRoot},
	}
}

// cleanRunner reports a clean, idle repo whose origin is github.com/<owner>/<name>.
func cleanRunner() *testRunner {
	return &testRunner{
		remoteURLFn: func(repoPath, _ string) (string, error) {
			return "git@github.com:owner/" + filepath.Base(repoPath) + ".git", nil
		},
		worktreeListFn: func(string) ([]git.WorktreeInfo, error) { return nil, nil },
		statusFn:       func(string) (git.RepoStatus, error) { return git.RepoStatus{}, nil },
		pendingWorkFn:  func(string) (int, int, error) { return 0, 0, nil },
	}
}

func TestRunRepoArchive_MovesCloneAndRecordsProvenance(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	foo := makeRepoDir(t, discovery, "foo")
	makeRepoDir(t, discovery, "bar")

	var out bytes.Buffer
	if err := RunRepoArchive(archiveCfg(discovery, archiveRoot), cleanRunner(), RepoArchiveArgs{Repos: []string{"foo", "bar"}}, &out); err != nil {
		t.Fatalf("RunRepoArchive: %v", err)
	}

	for _, name := range []string{"foo", "bar"} {
		if _, err := os.Stat(filepath.Join(discovery, name)); !os.IsNotExist(err) {
			t.Errorf("%s: original still exists", name)
		}
		if _, err := os.Stat(filepath.Join(archiveRoot, name)); err != nil {
			t.Errorf("%s: destination missing: %v", name, err)
		}
	}

	f, err := archive.Load()
	if err != nil {
		t.Fatalf("archive.Load: %v", err)
	}
	if len(f.Entries) != 2 {
		t.Fatalf("provenance entries: got %d, want 2", len(f.Entries))
	}
	byName := make(map[string]archive.Record, len(f.Entries))
	for _, e := range f.Entries {
		byName[e.Name] = e
	}
	if byName["foo"].Origin != foo {
		t.Errorf("origin: got %q, want %q", byName["foo"].Origin, foo)
	}
	if byName["foo"].Host != "github.com" {
		t.Errorf("host: got %q, want github.com", byName["foo"].Host)
	}

	got := out.String()
	if !strings.Contains(got, "moved to") {
		t.Errorf("output missing move line: %q", got)
	}
	for _, want := range []string{"gh repo archive owner/foo --yes", "gh repo archive owner/bar --yes"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q: %q", want, got)
		}
	}
}

func TestRunRepoArchive_RefusesPendingWorkWithoutForce(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	foo := makeRepoDir(t, discovery, "foo")

	r := cleanRunner()
	r.statusFn = func(string) (git.RepoStatus, error) {
		return git.RepoStatus{Files: []git.FileStatus{{Path: "x"}}}, nil
	}

	var out bytes.Buffer
	err := RunRepoArchive(archiveCfg(discovery, archiveRoot), r, RepoArchiveArgs{Repos: []string{"foo"}}, &out)
	if err == nil {
		t.Fatal("expected refusal for uncommitted changes")
	}
	if !strings.Contains(err.Error(), "uncommitted") || !strings.Contains(err.Error(), "--force") {
		t.Errorf("error should mention uncommitted work and --force: %v", err)
	}
	if _, statErr := os.Stat(foo); statErr != nil {
		t.Error("clone should not have moved")
	}
}

func TestRunRepoArchive_ForceProceeds(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	makeRepoDir(t, discovery, "foo")

	r := cleanRunner()
	r.statusFn = func(string) (git.RepoStatus, error) {
		return git.RepoStatus{Files: []git.FileStatus{{Path: "x"}}}, nil
	}

	if err := RunRepoArchive(archiveCfg(discovery, archiveRoot), r, RepoArchiveArgs{Repos: []string{"foo"}, Force: true}, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunRepoArchive with --force: %v", err)
	}
	if _, err := os.Stat(filepath.Join(archiveRoot, "foo")); err != nil {
		t.Errorf("clone should have moved: %v", err)
	}
}

func TestRunRepoArchive_RefusesUnpushedWithoutForce(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	makeRepoDir(t, discovery, "foo")

	r := cleanRunner()
	r.pendingWorkFn = func(string) (int, int, error) { return 3, 0, nil }

	err := RunRepoArchive(archiveCfg(discovery, archiveRoot), r, RepoArchiveArgs{Repos: []string{"foo"}}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not on any remote") {
		t.Errorf("expected unpushed refusal, got: %v", err)
	}
}

func TestRunRepoArchive_RefusesStashesWithoutForce(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	makeRepoDir(t, discovery, "foo")

	r := cleanRunner()
	r.pendingWorkFn = func(string) (int, int, error) { return 0, 1, nil }

	err := RunRepoArchive(archiveCfg(discovery, archiveRoot), r, RepoArchiveArgs{Repos: []string{"foo"}}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "stash") {
		t.Errorf("expected stash refusal, got: %v", err)
	}
}

func TestRunRepoArchive_RefusesLiveWorktreeEvenWithForce(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	foo := makeRepoDir(t, discovery, "foo")
	live := t.TempDir() // a linked worktree path that exists on disk

	r := cleanRunner()
	r.worktreeListFn = func(string) ([]git.WorktreeInfo, error) {
		return []git.WorktreeInfo{{Path: foo, Branch: "main"}, {Path: live, Branch: "feat"}}, nil
	}

	err := RunRepoArchive(archiveCfg(discovery, archiveRoot), r, RepoArchiveArgs{Repos: []string{"foo"}, Force: true}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "linked worktree") {
		t.Errorf("expected live-worktree error, got: %v", err)
	}
	if _, statErr := os.Stat(foo); statErr != nil {
		t.Error("clone must not move when a live worktree blocks it")
	}
}

func TestRunRepoArchive_RefusesStaleWorktree(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	foo := makeRepoDir(t, discovery, "foo")
	stale := filepath.Join(t.TempDir(), "gone") // registered but not on disk

	r := cleanRunner()
	r.worktreeListFn = func(string) ([]git.WorktreeInfo, error) {
		return []git.WorktreeInfo{{Path: foo, Branch: "main"}, {Path: stale}}, nil
	}

	err := RunRepoArchive(archiveCfg(discovery, archiveRoot), r, RepoArchiveArgs{Repos: []string{"foo"}}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Errorf("expected stale-worktree error, got: %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "worktree prune") {
		t.Errorf("stale error should suggest git worktree prune: %v", err)
	}
}

func TestRunRepoArchive_RefusesRepoReferencedBySpace(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	foo := makeRepoDir(t, discovery, "foo")

	sp := &state.Space{
		Name:      "my-feature",
		Branch:    "b",
		Path:      t.TempDir(),
		CreatedAt: time.Now(),
		Repos: []state.RepoEntry{{
			Name:         "foo",
			RepoPath:     foo,
			WorktreePath: filepath.Join(t.TempDir(), "foo"),
		}},
	}
	if err := state.Save(sp); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	err := RunRepoArchive(archiveCfg(discovery, archiveRoot), cleanRunner(), RepoArchiveArgs{Repos: []string{"foo"}}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected refusal for a repo referenced by a space")
	}
	if !strings.Contains(err.Error(), "my-feature") || !strings.Contains(err.Error(), "wtg remove my-feature foo") {
		t.Errorf("error should name the space and the fix: %v", err)
	}
}

func TestRunRepoArchive_RefusesAlwaysRepo(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	makeRepoDir(t, discovery, "foo")

	cfg := archiveCfg(discovery, archiveRoot)
	cfg.Always.Repos = []string{"foo"}

	err := RunRepoArchive(cfg, cleanRunner(), RepoArchiveArgs{Repos: []string{"foo"}}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "always.repos") {
		t.Errorf("expected always.repos refusal, got: %v", err)
	}
}

func TestRunRepoArchive_BatchIsAllOrNothing(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	foo := makeRepoDir(t, discovery, "foo")
	bar := makeRepoDir(t, discovery, "bar")

	r := cleanRunner()
	r.statusFn = func(repoPath string) (git.RepoStatus, error) {
		if filepath.Base(repoPath) == "bar" {
			return git.RepoStatus{Files: []git.FileStatus{{Path: "x"}}}, nil
		}
		return git.RepoStatus{}, nil
	}

	err := RunRepoArchive(archiveCfg(discovery, archiveRoot), r, RepoArchiveArgs{Repos: []string{"foo", "bar"}}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected refusal because bar is dirty")
	}
	for _, p := range []string{foo, bar} {
		if _, statErr := os.Stat(p); statErr != nil {
			t.Errorf("%s should not have moved", filepath.Base(p))
		}
	}
	if _, statErr := os.Stat(archive.Path()); !os.IsNotExist(statErr) {
		t.Error("no provenance should have been written")
	}
}

func TestRunRepoArchive_RefusesExistingDestination(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	foo := makeRepoDir(t, discovery, "foo")
	if err := os.MkdirAll(filepath.Join(archiveRoot, "foo"), 0o750); err != nil {
		t.Fatal(err)
	}

	err := RunRepoArchive(archiveCfg(discovery, archiveRoot), cleanRunner(), RepoArchiveArgs{Repos: []string{"foo"}}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected destination-exists error, got: %v", err)
	}
	if _, statErr := os.Stat(foo); statErr != nil {
		t.Error("clone should not have moved")
	}
}

func TestRunRepoArchive_RejectsArchiveRootInsideDiscovery(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	makeRepoDir(t, discovery, "foo")
	nested := filepath.Join(discovery, "archived") // inside the discovery root

	err := RunRepoArchive(archiveCfg(discovery, nested), cleanRunner(), RepoArchiveArgs{Repos: []string{"foo"}}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "inside discovery root") {
		t.Errorf("expected inside-discovery-root error, got: %v", err)
	}
}

func TestRunRepoArchive_DryRunMakesNoChanges(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	foo := makeRepoDir(t, discovery, "foo")

	var out bytes.Buffer
	if err := RunRepoArchive(archiveCfg(discovery, archiveRoot), cleanRunner(), RepoArchiveArgs{Repos: []string{"foo"}, DryRun: true}, &out); err != nil {
		t.Fatalf("RunRepoArchive --dry-run: %v", err)
	}
	if _, err := os.Stat(foo); err != nil {
		t.Error("dry-run must not move the clone")
	}
	if _, err := os.Stat(filepath.Join(archiveRoot, "foo")); !os.IsNotExist(err) {
		t.Error("dry-run must not create the destination")
	}
	if _, err := os.Stat(archive.Path()); !os.IsNotExist(err) {
		t.Error("dry-run must not write provenance")
	}
	got := out.String()
	if !strings.Contains(got, "would move to") || !strings.Contains(got, "gh repo archive owner/foo --yes") {
		t.Errorf("dry-run output missing plan: %q", got)
	}
}

func TestRunRepoArchive_ArchiveDirOverride(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	override := t.TempDir()
	makeRepoDir(t, discovery, "foo")

	if err := RunRepoArchive(archiveCfg(discovery, archiveRoot), cleanRunner(), RepoArchiveArgs{Repos: []string{"foo"}, ArchiveDir: override}, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunRepoArchive --archive-dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(override, "foo")); err != nil {
		t.Errorf("clone should have moved to the override dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(archiveRoot, "foo")); !os.IsNotExist(err) {
		t.Error("configured archive.root_dir should not be used when --archive-dir is set")
	}
}

func TestRunRepoArchive_NonGitHubRemoteHasNoSuggestion(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	makeRepoDir(t, discovery, "foo")

	r := cleanRunner()
	r.remoteURLFn = func(string, string) (string, error) { return "https://gitlab.com/owner/foo.git", nil }

	var out bytes.Buffer
	if err := RunRepoArchive(archiveCfg(discovery, archiveRoot), r, RepoArchiveArgs{Repos: []string{"foo"}}, &out); err != nil {
		t.Fatalf("RunRepoArchive: %v", err)
	}
	if strings.Contains(out.String(), "gh repo archive") {
		t.Errorf("should not suggest gh for a non-GitHub remote: %q", out.String())
	}
	if !strings.Contains(out.String(), "moved to") {
		t.Errorf("repo should still be archived: %q", out.String())
	}
}

func TestRunRepoArchive_UnknownRepo(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	makeRepoDir(t, discovery, "foo")

	err := RunRepoArchive(archiveCfg(discovery, archiveRoot), cleanRunner(), RepoArchiveArgs{Repos: []string{"nope"}}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got: %v", err)
	}
}

func TestRunRepoArchive_AmbiguousName(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	makeRepoDir(t, discovery, "org1/dup")
	makeRepoDir(t, discovery, "org2/dup")

	err := RunRepoArchive(archiveCfg(discovery, archiveRoot), cleanRunner(), RepoArchiveArgs{Repos: []string{"dup"}}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected ambiguous-name error, got: %v", err)
	}
}
