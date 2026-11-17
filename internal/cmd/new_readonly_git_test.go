package cmd

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/suhlig/wtg/internal/config"
	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/git/testhelper"
	"github.com/suhlig/wtg/internal/state"
)

// cloneInto clones a fresh repo with a bare origin into root/name and returns
// it. Unlike testhelper.InitWithRemote, several of these can share one
// discovery root.
func cloneInto(t *testing.T, root, name string) *testhelper.Repo {
	t.Helper()
	_, remote := testhelper.InitWithRemote(t)
	testhelper.RepoAt(t, root).GitCmd("clone", remote.Path, name)
	repo := testhelper.RepoAt(t, filepath.Join(root, name))
	repo.GitCmd("config", "user.email", "test@example.com")
	repo.GitCmd("config", "user.name", "Test")
	repo.GitCmd("remote", "set-head", "origin", "--auto")
	return repo
}

// A mixed workspace built in one `wtg new`, against real repositories: the
// read-write repo is on the workspace branch, the read-only one is detached at
// its tag and owns no branch.
func TestRunSpaceNewGit_ReadOnlyMixedWorkspace(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	api := cloneInto(t, root, "api")
	lib := cloneInto(t, root, "config-lib")
	lib.GitCmd("tag", "v1.0.0")
	tagged := gitOut(t, lib.Path, "rev-parse", "HEAD")
	lib.Commit("after the tag")
	lib.GitCmd("push", "origin", "main")
	libBranchesBefore := gitOut(t, lib.Path, "for-each-ref", "refs/heads")

	cfg := &config.Config{
		Discovery: config.DiscoveryConfig{RootDir: root, MaxDepth: 1},
		Spaces:    config.SpacesConfig{RootDir: t.TempDir()},
	}
	var out bytes.Buffer
	args := SpaceNewArgs{Name: "feat", Repos: []string{"api"}, ReadOnly: []string{"config-lib"}, Ref: "v1.0.0"}
	if err := RunSpaceNew(cfg, git.New(), args, &out); err != nil {
		t.Fatalf("RunSpaceNew: %v\n%s", err, out.String())
	}

	spaceRoot := filepath.Join(cfg.Spaces.RootDir, "feat")
	if got := gitOut(t, filepath.Join(spaceRoot, "api"), "branch", "--show-current"); got != "feat" {
		t.Errorf("api is on %q, want the workspace branch feat", got)
	}
	libWorktree := filepath.Join(spaceRoot, "config-lib")
	if got := gitOut(t, libWorktree, "rev-parse", "HEAD"); got != tagged {
		t.Errorf("config-lib HEAD = %s, want the tag's commit %s", got, tagged)
	}
	if got := gitOut(t, libWorktree, "branch", "--show-current"); got != "" {
		t.Errorf("config-lib is on branch %q, want detached", got)
	}
	if got := gitOut(t, lib.Path, "for-each-ref", "refs/heads"); got != libBranchesBefore {
		t.Errorf("a read-only member must not create a branch:\nbefore: %s\nafter:  %s", libBranchesBefore, got)
	}
	if got := gitOut(t, api.Path, "branch", "--list", "feat"); got == "" {
		t.Error("the workspace branch was not created in api")
	}

	sp, err := state.Load("feat")
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	byName := map[string]state.RepoEntry{}
	for _, r := range sp.Repos {
		byName[r.Name] = r
	}
	if r := byName["api"]; r.ReadOnly {
		t.Errorf("api should be an ordinary member: %+v", r)
	}
	if r := byName["config-lib"]; !r.ReadOnly || r.Ref != "v1.0.0" {
		t.Errorf("config-lib should be read-only at v1.0.0: %+v", r)
	}
}
