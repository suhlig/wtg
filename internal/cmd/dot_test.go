package cmd

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suhlig/wtg/internal/config"
	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/state"
)

// inRepo returns a testRunner whose Toplevel answers with toplevel and whose
// worktree list reports toplevel as a linked worktree of main (main == "" means
// toplevel is the main clone itself).
func inRepo(toplevel, main string) *testRunner {
	return &testRunner{
		toplevelFn: func(string) (string, error) { return toplevel, nil },
		worktreeListFn: func(string) ([]git.WorktreeInfo, error) {
			if main == "" {
				return []git.WorktreeInfo{{Path: toplevel}}, nil
			}
			return []git.WorktreeInfo{{Path: main}, {Path: toplevel}}, nil
		},
	}
}

// --- resolveRepoArgs ---

func TestResolveRepoArgs_ReplacesDotWithShortName(t *testing.T) {
	root := t.TempDir()
	repo := makeRepo(t, root, "github.com/example/api")
	cfg := discoverCfg(root, 3)

	got, err := resolveRepoArgs(inRepo(repo, ""), cfg, []string{"other", "."})
	if err != nil {
		t.Fatalf("resolveRepoArgs: %v", err)
	}
	want := "github.com/example/api"
	if got[1] != want {
		t.Errorf("got[1] = %q, want %q", got[1], want)
	}
	if got[0] != "other" {
		t.Errorf("got[0] = %q, want other", got[0])
	}
}

func TestResolveRepoArgs_WorktreeResolvesToMainClone(t *testing.T) {
	root := t.TempDir()
	main := makeRepo(t, root, "api")
	worktree := filepath.Join(t.TempDir(), "api")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	cfg := discoverCfg(root, 2)

	got, err := resolveRepoArgs(inRepo(worktree, main), cfg, []string{"."})
	if err != nil {
		t.Fatalf("resolveRepoArgs: %v", err)
	}
	if got[0] != "api" {
		t.Errorf("got = %v, want [api]", got)
	}
}

func TestResolveRepoArgs_SymlinkedDiscoveryRoot(t *testing.T) {
	realRoot := t.TempDir()
	repo := makeRepo(t, realRoot, "api")
	linkRoot := filepath.Join(t.TempDir(), "repos")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Fatalf("symlink root: %v", err)
	}
	cfg := discoverCfg(linkRoot, 2)

	// Git reports the physical path; discovery recorded the path under the
	// symlinked root. They must still be recognized as the same repo.
	got, err := resolveRepoArgs(inRepo(repo, ""), cfg, []string{"."})
	if err != nil {
		t.Fatalf("resolveRepoArgs: %v", err)
	}
	if got[0] != "api" {
		t.Errorf("got = %v, want [api]", got)
	}
}

func TestResolveRepoArgs_NoDotMakesNoGitCall(t *testing.T) {
	// An empty testRunner panics on any git call, so this also proves "." is the
	// only argument that triggers resolution.
	got, err := resolveRepoArgs(&testRunner{}, nil, []string{"api", "svc"})
	if err != nil {
		t.Fatalf("resolveRepoArgs: %v", err)
	}
	if len(got) != 2 || got[0] != "api" || got[1] != "svc" {
		t.Errorf("got = %v, want [api svc]", got)
	}
}

func TestResolveRepoArgs_NotInRepository(t *testing.T) {
	cfg := discoverCfg(t.TempDir(), 2)
	r := &testRunner{toplevelFn: func(string) (string, error) {
		return "", os.ErrNotExist
	}}

	_, err := resolveRepoArgs(r, cfg, []string{"."})
	if err == nil || !strings.Contains(err.Error(), "not inside a git repository") {
		t.Fatalf("expected a not-inside-a-repository error, got %v", err)
	}
}

func TestResolveRepoArgs_OutsideDiscoveryRoots(t *testing.T) {
	root := t.TempDir()
	cfg := discoverCfg(root, 2)
	outside := makeRepo(t, t.TempDir(), "elsewhere/api")

	_, err := resolveRepoArgs(inRepo(outside, ""), cfg, []string{"."})
	if err == nil {
		t.Fatal("expected an error for a repo outside every discovery root")
	}
	for _, want := range []string{"not under any discovery root", outside, root} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

// --- workspace position ---

func TestResolveSpaceArg_DotMeansCurrentWorkspace(t *testing.T) {
	isolateState(t)
	spacePath := t.TempDir()
	makeSpace(t, "feat", "geoff/feat", spacePath, []string{"api"}, "/repos")
	t.Chdir(spacePath)

	got, err := resolveSpaceArg(".", false)
	if err != nil {
		t.Fatalf("resolveSpaceArg: %v", err)
	}
	if got != "feat" {
		t.Errorf("got %q, want feat", got)
	}
}

func TestResolveSpaceArg_DotOutsideWorkspaceErrors(t *testing.T) {
	isolateState(t)
	t.Chdir(t.TempDir())

	if _, err := resolveSpaceArg(".", false); err == nil {
		t.Fatal("expected an error when the current directory is in no workspace")
	}
}

func TestResolveExecArgs_DotMeansCurrentWorkspace(t *testing.T) {
	isolateState(t)
	sp := execSpace(t, "feat", []string{"api"})
	t.Chdir(sp.Repos[0].WorktreePath)

	spaceName, execArgs, err := resolveExecArgs([]string{".", "git", "status"})
	if err != nil {
		t.Fatalf("resolveExecArgs: %v", err)
	}
	if spaceName != "feat" {
		t.Errorf("spaceName = %q, want feat", spaceName)
	}
	if len(execArgs) != 2 || execArgs[0] != "git" || execArgs[1] != "status" {
		t.Errorf("execArgs = %v, want [git status]", execArgs)
	}
}

func TestResolveSpaceAndRepos_DotIsAlwaysARepo(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	spacePath := t.TempDir()
	makeSpace(t, "feat", "geoff/feat", spacePath, []string{"api"}, root)
	repo := makeRepo(t, root, "api")
	t.Chdir(spacePath)

	spaceName, repos, err := resolveSpaceAndRepos(inRepo(repo, ""), discoverCfg(root, 2), []string{"."})
	if err != nil {
		t.Fatalf("resolveSpaceAndRepos: %v", err)
	}
	if spaceName != "feat" {
		t.Errorf("spaceName = %q, want feat", spaceName)
	}
	if len(repos) != 1 || repos[0] != "api" {
		t.Errorf("repos = %v, want [api]", repos)
	}
}

func TestRejectDotAsWorkspace(t *testing.T) {
	if err := rejectDotAsWorkspace("."); err == nil {
		t.Fatal("expected \".\" to be rejected as a workspace name")
	}
	if err := rejectDotAsWorkspace("my-feature"); err != nil {
		t.Errorf("unexpected error for a normal name: %v", err)
	}
}

func TestNewCommand_RejectsDotAsWorkspace(t *testing.T) {
	isolateState(t)
	err := newApp(&testRunner{}).Run(context.Background(), []string{"wtg", "new", ".", "api"})
	if err == nil || !strings.Contains(err.Error(), "not a workspace name") {
		t.Fatalf("expected \".\" to be rejected as a workspace name, got %v", err)
	}
}

func TestRunSpaceNew_DotWinsOverAlwaysRepos(t *testing.T) {
	// A repo named by "." must become a worktree even if it is configured in
	// always.repos: resolving to a short name is what makes the two agree
	// (ADR 0025 D4).
	isolateState(t)
	root := t.TempDir()
	repoPath := makeRepo(t, root, "shared")
	cfg := &config.Config{
		Discovery: config.DiscoveryConfig{RootDir: root, MaxDepth: 2},
		Spaces:    config.SpacesConfig{RootDir: t.TempDir()},
		Always:    config.AlwaysConfig{Repos: []string{"shared"}},
	}
	created := 0
	r := &testRunner{
		toplevelFn:     func(string) (string, error) { return repoPath, nil },
		worktreeListFn: func(string) ([]git.WorktreeInfo, error) { return []git.WorktreeInfo{{Path: repoPath}}, nil },
		branchExistsFn: func(_, _ string) (bool, error) { return false, nil },
		worktreeAddFn:  func(_, _, _, _ string, _ bool) error { created++; return nil },
	}

	args := SpaceNewArgs{Name: "feat", Repos: []string{"."}}
	if err := RunSpaceNew(cfg, r, args, io.Discard); err != nil {
		t.Fatalf("RunSpaceNew: %v", err)
	}
	if created != 1 {
		t.Errorf("expected one worktree, got %d", created)
	}
	sp, err := state.Load("feat")
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if len(sp.Repos) != 1 || sp.Repos[0].Symlink {
		t.Errorf("the repo must be a worktree, not a symlink: %+v", sp.Repos)
	}
}

func TestRunRepoUnarchive_RejectsDot(t *testing.T) {
	isolateState(t)
	err := RunRepoUnarchive(&config.Config{}, RepoUnarchiveArgs{Repos: []string{"."}}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "archived") {
		t.Fatalf("expected \".\" to be rejected with the archived-repo reason, got %v", err)
	}
}
