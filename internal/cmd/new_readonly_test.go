package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suhlig/wtg/internal/state"
)

// newReadOnlyRunner answers the calls `wtg new` makes for one read-write repo
// with a new branch plus read-only members: the branch worktree is recorded in
// branched, the detached ones in detached.
func newReadOnlyRunner(branched, detached *[]string) *testRunner {
	r := addReadOnlyRunner(detached)
	r.branchExistsFn = func(_, _ string) (bool, error) { return false, nil }
	r.worktreeAddFn = func(_, worktreePath, _, _ string, _ bool) error {
		*branched = append(*branched, worktreePath)
		return nil
	}
	return r
}

// --- wtg new --read-only (ADR 0024 follow-up) ---

func TestRunSpaceNew_ReadOnlyMixedWorkspace(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "api")
	makeRepo(t, root, "config-lib")
	cfg := spaceCreateCfg(root, spacesRoot)

	var branched, detached []string
	var out bytes.Buffer
	args := SpaceNewArgs{Name: "feat", Repos: []string{"api"}, ReadOnly: []string{"config-lib"}}
	if err := RunSpaceNew(cfg, newReadOnlyRunner(&branched, &detached), args, &out); err != nil {
		t.Fatalf("RunSpaceNew: %v", err)
	}

	if len(branched) != 1 || !strings.HasSuffix(branched[0], "/api") {
		t.Errorf("only api should get a branch worktree, got %v", branched)
	}
	if len(detached) != 1 || !strings.HasSuffix(detached[0], "/config-lib@commit-origin/main") {
		t.Errorf("config-lib should be detached at origin/main, got %v", detached)
	}

	sp, err := state.Load("feat")
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	byName := map[string]state.RepoEntry{}
	for _, r := range sp.Repos {
		byName[r.Name] = r
	}
	if len(byName) != 2 {
		t.Fatalf("expected 2 repos in state, got %+v", sp.Repos)
	}
	if byName["api"].ReadOnly || byName["api"].Ref != "" {
		t.Errorf("api should be an ordinary member: %+v", byName["api"])
	}
	if lib := byName["config-lib"]; !lib.ReadOnly || lib.Ref != "origin/main" {
		t.Errorf("config-lib should be read-only following origin/main: %+v", lib)
	}
	if !strings.Contains(out.String(), "read-only @ origin/main") {
		t.Errorf("output should name the pinned ref: %q", out.String())
	}
}

func TestRunSpaceNew_ReadOnlyRepeatableWithRef(t *testing.T) {
	root := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "api")
	makeRepo(t, root, "config-lib")
	makeRepo(t, root, "codegen")
	cfg := spaceCreateCfg(root, t.TempDir())

	var branched, detached []string
	args := SpaceNewArgs{
		Name:     "feat",
		Repos:    []string{"api"},
		ReadOnly: []string{"config-lib", "codegen"},
		Ref:      "v2.0.0",
	}
	if err := RunSpaceNew(cfg, newReadOnlyRunner(&branched, &detached), args, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunSpaceNew: %v", err)
	}

	if len(detached) != 2 {
		t.Fatalf("expected 2 detached worktrees, got %v", detached)
	}
	for _, d := range detached {
		if !strings.HasSuffix(d, "@commit-v2.0.0") {
			t.Errorf("--ref applies to every read-only repo, got %q", d)
		}
	}
	sp, err := state.Load("feat")
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	for _, r := range sp.Repos {
		if r.Name != "api" && (!r.ReadOnly || r.Ref != "v2.0.0") {
			t.Errorf("%s should be pinned to v2.0.0: %+v", r.Name, r)
		}
	}
}

func TestRunSpaceNew_ReadOnlyUnresolvableRefFailsBeforeCreating(t *testing.T) {
	root := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "api")
	makeRepo(t, root, "config-lib")
	cfg := spaceCreateCfg(root, t.TempDir())

	var branched, detached []string
	r := newReadOnlyRunner(&branched, &detached)
	r.resolveRefFn = func(_, _ string) (string, error) { return "", errors.New("unknown revision") }

	args := SpaceNewArgs{Name: "feat", Repos: []string{"api"}, ReadOnly: []string{"config-lib"}, Ref: "v9.9.9"}
	err := RunSpaceNew(cfg, r, args, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "does not resolve") {
		t.Fatalf("expected an unresolvable-ref error, got %v", err)
	}
	if len(branched)+len(detached) != 0 {
		t.Errorf("nothing may be created when pre-flight fails: branched %v, detached %v", branched, detached)
	}
	if _, err := state.Load("feat"); err == nil {
		t.Error("state must not be saved when pre-flight fails")
	}
}

func TestRunSpaceNew_ReadOnlyUnknownRepo(t *testing.T) {
	root := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "api")
	cfg := spaceCreateCfg(root, t.TempDir())

	var branched, detached []string
	args := SpaceNewArgs{Name: "feat", Repos: []string{"api"}, ReadOnly: []string{"nonexistent"}}
	err := RunSpaceNew(cfg, newReadOnlyRunner(&branched, &detached), args, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("expected an error naming the unknown repo, got %v", err)
	}
	if len(branched)+len(detached) != 0 {
		t.Errorf("nothing may be created, got branched %v, detached %v", branched, detached)
	}
}

func TestRunSpaceNew_ReadOnlyAndReadWriteOverlap(t *testing.T) {
	root := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "api")
	cfg := spaceCreateCfg(root, t.TempDir())

	var branched, detached []string
	args := SpaceNewArgs{Name: "feat", Repos: []string{"api"}, ReadOnly: []string{"api"}}
	err := RunSpaceNew(cfg, newReadOnlyRunner(&branched, &detached), args, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "both") {
		t.Fatalf("expected an overlap error, got %v", err)
	}
	if len(branched)+len(detached) != 0 {
		t.Errorf("nothing may be created, got branched %v, detached %v", branched, detached)
	}
}

func TestRunSpaceNew_ReadOnlyDerivesRefFromReadWriteMembers(t *testing.T) {
	root := t.TempDir()
	isolateState(t)
	apiPath := makeRepo(t, root, "api")
	makeRepo(t, root, "config-lib")
	cfg := spaceCreateCfg(root, t.TempDir())

	// The read-write repo is the consumer: `go list` runs in its main clone,
	// because the workspace has no worktree for it yet.
	if err := os.WriteFile(filepath.Join(apiPath, "go.mod"), []byte("module example.com/api\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	var branched, detached []string
	r := newReadOnlyRunner(&branched, &detached)
	r.showFileFn = func(_, rev, path string) ([]byte, error) {
		if path != "go.mod" || !strings.HasPrefix(rev, "origin/") {
			return nil, os.ErrNotExist
		}
		return []byte("module " + constLib + "\n"), nil
	}

	args := SpaceNewArgs{
		Name:     "feat",
		Repos:    []string{"api"},
		ReadOnly: []string{"config-lib"},
		Go:       &fakeGo{versions: map[string]string{apiPath: "v2.0.0"}},
	}
	if err := RunSpaceNew(cfg, r, args, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunSpaceNew: %v", err)
	}

	sp, err := state.Load("feat")
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	for _, r := range sp.Repos {
		if r.Name == "config-lib" && r.Ref != "v2.0.0" {
			t.Errorf("config-lib ref = %q, want the version api requires, v2.0.0", r.Ref)
		}
	}
}

func TestRunSpaceNew_ReadOnlyListedInGoWorkFromPinnedCommit(t *testing.T) {
	// The go.mod that matters is the one at the pinned commit, not in the main
	// clone (ADR 0024 D7): the main clone here has none.
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "api")
	makeRepo(t, root, "config-lib")
	cfg := spaceCreateCfg(root, spacesRoot)

	var branched, detached []string
	r := newReadOnlyRunner(&branched, &detached)
	r.showFileFn = func(_, _, path string) ([]byte, error) {
		if path == "go.mod" {
			return []byte("module example.com/config-lib\n\ngo 1.24\n"), nil
		}
		return nil, os.ErrNotExist
	}

	args := SpaceNewArgs{Name: "feat", Repos: []string{"api"}, ReadOnly: []string{"config-lib"}, Ref: "v1.0.0"}
	if err := RunSpaceNew(cfg, r, args, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunSpaceNew: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(spacesRoot, "feat", "go.work"))
	if err != nil {
		t.Fatalf("go.work not written: %v", err)
	}
	if !strings.Contains(string(data), "./config-lib") {
		t.Errorf("go.work should list the read-only member: %q", data)
	}
	if strings.Contains(string(data), "./api") {
		t.Errorf("api has no go.mod and must not be listed: %q", data)
	}
	sp, err := state.Load("feat")
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if !sp.GoWorkspace {
		t.Error("state should record a Go workspace")
	}
}

func TestRunSpaceNew_ReadOnlyNamedRepoIsNotSymlinkedByAlwaysRepos(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "api")
	makeRepo(t, root, "docs")
	cfg := alwaysCfg(root, spacesRoot, []string{"docs"})

	var branched, detached []string
	args := SpaceNewArgs{Name: "feat", Repos: []string{"api"}, ReadOnly: []string{"docs"}}
	if err := RunSpaceNew(cfg, newReadOnlyRunner(&branched, &detached), args, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunSpaceNew: %v", err)
	}

	if len(detached) != 1 || !strings.Contains(detached[0], "docs") {
		t.Errorf("docs should be a detached worktree, got %v", detached)
	}
	if fi, err := os.Lstat(filepath.Join(spacesRoot, "feat", "docs")); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		t.Error("docs is named explicitly and must not also be symlinked")
	}
	sp, err := state.Load("feat")
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	for _, r := range sp.Repos {
		if r.Name == "docs" && (r.Symlink || !r.ReadOnly) {
			t.Errorf("docs should be a read-only member, not a symlink: %+v", r)
		}
	}
}

func TestRunSpaceNew_ReadOnlyDotResolvesToCurrentRepo(t *testing.T) {
	// "." is accepted as a --read-only value and resolved before name matching
	// (ADR 0025 D4), like any repo argument.
	root := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "api")
	lib := makeRepo(t, root, "config-lib")
	cfg := spaceCreateCfg(root, t.TempDir())

	var branched, detached []string
	r := newReadOnlyRunner(&branched, &detached)
	cwdRunner := inRepo(lib, "")
	r.toplevelFn = cwdRunner.toplevelFn
	r.worktreeListFn = cwdRunner.worktreeListFn

	args := SpaceNewArgs{Name: "feat", Repos: []string{"api"}, ReadOnly: []string{"."}}
	if err := RunSpaceNew(cfg, r, args, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunSpaceNew: %v", err)
	}
	if len(detached) != 1 || !strings.Contains(detached[0], "config-lib") {
		t.Errorf("\".\" should pin the repo containing the current directory, got %v", detached)
	}
}

func TestRunSpaceNew_ReadOnlyRollbackRemovesEverythingCreated(t *testing.T) {
	root := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "api")
	makeRepo(t, root, "config-lib")
	cfg := spaceCreateCfg(root, t.TempDir())

	var branched, detached, removed, deletedBranches []string
	r := newReadOnlyRunner(&branched, &detached)
	r.worktreeAddDetachedFn = func(_, _, _ string) error { return fmt.Errorf("disk full") }
	r.worktreeRemoveFn = func(_, worktreePath string, _ bool) error {
		removed = append(removed, worktreePath)
		return nil
	}
	r.branchDeleteFn = func(_, branch string, _ bool) error {
		deletedBranches = append(deletedBranches, branch)
		return nil
	}

	args := SpaceNewArgs{Name: "feat", Repos: []string{"api"}, ReadOnly: []string{"config-lib"}}
	err := RunSpaceNew(cfg, r, args, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("expected the original cause, got %v", err)
	}
	if len(removed) != 1 || !strings.HasSuffix(removed[0], "/api") {
		t.Errorf("the api worktree should be rolled back, got %v", removed)
	}
	if len(deletedBranches) != 1 || deletedBranches[0] != "feat" {
		t.Errorf("the branch created for api should be deleted, got %v", deletedBranches)
	}
	if _, err := state.Load("feat"); err == nil {
		t.Error("state should not be saved when the saga fails")
	}
}

func TestRunSpaceNew_ReadOnlyNeverCreatesTheBranch(t *testing.T) {
	// A read-only member has no workspace branch: only the read-write repo may
	// be checked out on it, and a recorded base never reaches the read-only one.
	root := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "api")
	makeRepo(t, root, "config-lib")
	cfg := spaceCreateCfg(root, t.TempDir())

	var branches []string
	var detached []string
	r := addReadOnlyRunner(&detached)
	r.branchExistsFn = func(repo, _ string) (bool, error) {
		if strings.HasSuffix(repo, "config-lib") {
			t.Errorf("the branch must not be looked up in a read-only repo: %s", repo)
		}
		return false, nil
	}
	r.worktreeAddFn = func(repo, _, branch, _ string, _ bool) error {
		branches = append(branches, repo+":"+branch)
		return nil
	}

	args := SpaceNewArgs{Name: "feat", Repos: []string{"api"}, ReadOnly: []string{"config-lib"}, Base: "origin/develop"}
	if err := RunSpaceNew(cfg, r, args, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunSpaceNew: %v", err)
	}
	if len(branches) != 1 || !strings.HasSuffix(branches[0], "api:feat") {
		t.Errorf("only api is checked out on the branch, got %v", branches)
	}
	sp, err := state.Load("feat")
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	for _, e := range sp.Repos {
		if e.Name == "config-lib" && e.Base != "" {
			t.Errorf("a read-only member records no base branch, got %q", e.Base)
		}
	}
}

// --- command line ---

func TestNewCommand_RefWithoutReadOnly(t *testing.T) {
	err := newApp(&testRunner{}).Run(context.Background(), []string{"wtg", "new", "feat", "api", "--ref", "v1.0.0"})
	if err == nil || !strings.Contains(err.Error(), "--ref only applies to a read-only member") {
		t.Fatalf("expected --ref without --read-only to be rejected, got %v", err)
	}
}

func TestNewCommand_ReadOnlyFlagIsRepeatable(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "api")
	makeRepo(t, root, "config-lib")
	makeRepo(t, root, "codegen")

	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	toml := fmt.Sprintf("[discovery]\nroot_dir = %q\nmax_depth = 2\n\n[spaces]\nroot_dir = %q\n", root, spacesRoot)
	if err := os.WriteFile(cfgPath, []byte(toml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var branched, detached []string
	err := newApp(newReadOnlyRunner(&branched, &detached)).Run(context.Background(), []string{
		"wtg", "--config", cfgPath, "new", "feat", "api",
		"--read-only", "config-lib", "--read-only", "codegen", "--ref", "v1.0.0",
	})
	if err != nil {
		t.Fatalf("wtg new: %v", err)
	}
	if len(branched) != 1 || len(detached) != 2 {
		t.Fatalf("expected 1 branch worktree and 2 read-only ones, got %v and %v", branched, detached)
	}
	sp, err := state.Load("feat")
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	readOnly := 0
	for _, r := range sp.Repos {
		if r.ReadOnly {
			readOnly++
		}
	}
	if readOnly != 2 {
		t.Errorf("expected 2 read-only members in state, got %+v", sp.Repos)
	}
}

func TestNewCommand_OnlyReadOnlyReposIsRejected(t *testing.T) {
	// Positional repos stay required: a workspace is one branch across repos.
	err := newApp(&testRunner{}).Run(context.Background(), []string{"wtg", "new", "feat", "--read-only", "config-lib"})
	if err == nil || !strings.Contains(err.Error(), "at least one repo") {
		t.Fatalf("expected the repo requirement to hold, got %v", err)
	}
}
