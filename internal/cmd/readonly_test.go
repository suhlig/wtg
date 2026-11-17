package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/gomod"
	"github.com/suhlig/wtg/internal/state"
)

// constLib is the module path used throughout these tests: a library that is
// consumed, never changed.
const constLib = "github.com/example/config-lib/v2"

// fakeGo is a gomod.Runner returning configured versions per consumer
// directory; anything unset reports the module as not a dependency.
type fakeGo struct {
	versions map[string]string // consumer dir -> version
	replaced map[string]bool   // consumer dir -> replaced
	errs     map[string]error  // consumer dir -> error
}

func (f *fakeGo) RequiredVersion(dir, _ string) (string, bool, error) {
	if err := f.errs[dir]; err != nil {
		return "", false, err
	}
	version, ok := f.versions[dir]
	if !ok {
		return "", false, gomod.ErrNotDependency
	}
	return version, f.replaced[dir], nil
}

// readOnlySpace creates a space whose state holds one read-only member named
// name, pinned to ref.
func readOnlySpace(t *testing.T, name, repoRoot, spacePath, ref string) *state.Space {
	t.Helper()
	sp := &state.Space{Name: "feat", Branch: "geoff/feat", Path: spacePath}
	sp.Repos = append(sp.Repos, state.RepoEntry{
		Name:         name,
		RepoPath:     filepath.Join(repoRoot, name),
		WorktreePath: filepath.Join(spacePath, name),
		ReadOnly:     true,
		Ref:          ref,
	})
	if err := state.Save(sp); err != nil {
		t.Fatalf("save space: %v", err)
	}
	return sp
}

// addReadOnlyRunner answers the calls a read-only add makes for a repo with no
// go.mod: fetch, default branch, and the ref resolving.
func addReadOnlyRunner(added *[]string) *testRunner {
	return &testRunner{
		fetchFn:         func(string) error { return nil },
		defaultBranchFn: func(string) (string, error) { return "main", nil },
		showFileFn:      func(_, _, _ string) ([]byte, error) { return nil, os.ErrNotExist },
		resolveRefFn: func(_, ref string) (string, error) {
			return "commit-" + ref, nil
		},
		worktreeAddDetachedFn: func(_, worktreePath, ref string) error {
			*added = append(*added, worktreePath+"@"+ref)
			return nil
		},
	}
}

// --- adding read-only members ---

func TestRunSpaceAdd_ReadOnlyFollowsDefaultBranch(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "config-lib")
	spacePath := filepath.Join(spacesRoot, "feat")
	makeSpace(t, "feat", "geoff/feat", spacePath, nil, root)
	cfg := spaceCreateCfg(root, spacesRoot)

	var added []string
	var out bytes.Buffer
	args := SpaceAddArgs{Name: "feat", Repos: []string{"config-lib"}, ReadOnly: true}
	if err := RunSpaceAdd(cfg, addReadOnlyRunner(&added), args, &out); err != nil {
		t.Fatalf("RunSpaceAdd: %v", err)
	}

	if len(added) != 1 || !strings.HasSuffix(added[0], "/config-lib@commit-origin/main") {
		t.Fatalf("expected a detached worktree at origin/main, got %v", added)
	}
	sp, err := state.Load("feat")
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if len(sp.Repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(sp.Repos))
	}
	got := sp.Repos[0]
	if !got.ReadOnly {
		t.Error("state should record read_only")
	}
	if got.Ref != "origin/main" {
		t.Errorf("ref = %q, want origin/main", got.Ref)
	}
	if !strings.Contains(out.String(), "read-only @ origin/main") {
		t.Errorf("output should name the pinned ref: %q", out.String())
	}
}

func TestRunSpaceAdd_ReadOnlyExplicitRef(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "config-lib")
	spacePath := filepath.Join(spacesRoot, "feat")
	makeSpace(t, "feat", "geoff/feat", spacePath, nil, root)
	cfg := spaceCreateCfg(root, spacesRoot)

	var added []string
	r := addReadOnlyRunner(&added)
	args := SpaceAddArgs{Name: "feat", Repos: []string{"config-lib"}, ReadOnly: true, Ref: "v2.0.0"}
	if err := RunSpaceAdd(cfg, r, args, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunSpaceAdd: %v", err)
	}

	sp, err := state.Load("feat")
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if sp.Repos[0].Ref != "v2.0.0" {
		t.Errorf("ref = %q, want v2.0.0", sp.Repos[0].Ref)
	}
}

func TestRunSpaceAdd_ReadOnlyUnresolvableRefFailsBeforeCreating(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "config-lib")
	spacePath := filepath.Join(spacesRoot, "feat")
	makeSpace(t, "feat", "geoff/feat", spacePath, nil, root)
	cfg := spaceCreateCfg(root, spacesRoot)

	created := false
	r := addReadOnlyRunner(&[]string{})
	r.resolveRefFn = func(_, _ string) (string, error) { return "", errors.New("unknown revision") }
	r.worktreeAddDetachedFn = func(_, _, _ string) error { created = true; return nil }

	args := SpaceAddArgs{Name: "feat", Repos: []string{"config-lib"}, ReadOnly: true, Ref: "v9.9.9"}
	err := RunSpaceAdd(cfg, r, args, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "does not resolve") {
		t.Fatalf("expected an unresolvable-ref error, got %v", err)
	}
	if created {
		t.Error("nothing may be created when pre-flight fails")
	}
}

func TestRunSpaceAdd_ReadOnlyDerivesRefFromConsumer(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "config-lib")
	spacePath := filepath.Join(spacesRoot, "feat")
	makeSpace(t, "feat", "geoff/feat", spacePath, []string{"api"}, root)
	cfg := spaceCreateCfg(root, spacesRoot)

	// The consumer must have a go.mod on disk: that is where `go list` runs.
	apiWorktree := filepath.Join(spacePath, "api")
	if err := os.MkdirAll(apiWorktree, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(apiWorktree, "go.mod"), []byte("module example.com/api\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	var added []string
	r := addReadOnlyRunner(&added)
	// The repo is a Go module; the consumer selects v2.0.0 of it.
	r.showFileFn = func(_, rev, path string) ([]byte, error) {
		if path != "go.mod" || !strings.HasPrefix(rev, "origin/") {
			return nil, os.ErrNotExist
		}
		return []byte("module " + constLib + "\n"), nil
	}

	args := SpaceAddArgs{
		Name:     "feat",
		Repos:    []string{"config-lib"},
		ReadOnly: true,
		Go:       &fakeGo{versions: map[string]string{apiWorktree: "v2.0.0"}},
	}
	if err := RunSpaceAdd(cfg, r, args, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunSpaceAdd: %v", err)
	}
	sp, err := state.Load("feat")
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	var got state.RepoEntry
	for _, e := range sp.Repos {
		if e.Name == "config-lib" {
			got = e
		}
	}
	if got.Ref != "v2.0.0" {
		t.Errorf("ref = %q, want the version the consumer requires (v2.0.0)", got.Ref)
	}
}

func TestRunSpaceAdd_ReadOnlyConsumersDisagree(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "config-lib")
	spacePath := filepath.Join(spacesRoot, "feat")
	makeSpace(t, "feat", "geoff/feat", spacePath, []string{"api", "web"}, root)
	cfg := spaceCreateCfg(root, spacesRoot)

	goMod := []byte("module " + constLib + "\n")
	apiWorktree := filepath.Join(spacePath, "api")
	webWorktree := filepath.Join(spacePath, "web")
	for _, dir := range []string{apiWorktree, webWorktree} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), goMod, 0o644); err != nil {
			t.Fatalf("write go.mod: %v", err)
		}
	}

	r := addReadOnlyRunner(&[]string{})
	r.showFileFn = func(_, _, path string) ([]byte, error) {
		if path != "go.mod" {
			return nil, os.ErrNotExist
		}
		return goMod, nil
	}

	args := SpaceAddArgs{
		Name:     "feat",
		Repos:    []string{"config-lib"},
		ReadOnly: true,
		Go: &fakeGo{versions: map[string]string{
			apiWorktree: "v2.0.0",
			webWorktree: "v2.1.0",
		}},
	}
	err := RunSpaceAdd(cfg, r, args, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected an error when consumers require different versions")
	}
	for _, want := range []string{"v2.0.0", "v2.1.0", "--ref"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestRunSpaceAdd_ReadOnlyWithoutGoFallsBack(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "config-lib")
	spacePath := filepath.Join(spacesRoot, "feat")
	makeSpace(t, "feat", "geoff/feat", spacePath, []string{"api"}, root)
	cfg := spaceCreateCfg(root, spacesRoot)

	apiWorktree := filepath.Join(spacePath, "api")
	if err := os.MkdirAll(apiWorktree, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(apiWorktree, "go.mod"), []byte("module example.com/api\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	r := addReadOnlyRunner(&[]string{})
	r.showFileFn = func(_, _, path string) ([]byte, error) {
		if path != "go.mod" {
			return nil, os.ErrNotExist
		}
		return []byte("module " + constLib + "\n"), nil
	}

	var out bytes.Buffer
	args := SpaceAddArgs{
		Name:     "feat",
		Repos:    []string{"config-lib"},
		ReadOnly: true,
		Go:       &fakeGo{errs: map[string]error{apiWorktree: gomod.ErrGoMissing}},
	}
	if err := RunSpaceAdd(cfg, r, args, &out); err != nil {
		t.Fatalf("RunSpaceAdd: %v", err)
	}
	if !strings.Contains(out.String(), "pinning") {
		t.Errorf("expected one explanatory line about the fallback, got %q", out.String())
	}
	sp, err := state.Load("feat")
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	for _, e := range sp.Repos {
		if e.Name == "config-lib" && e.Ref != "origin/main" {
			t.Errorf("ref = %q, want the origin/main fallback", e.Ref)
		}
	}
}

// --- refusals ---

func TestAddCommand_RefRequiresReadOnly(t *testing.T) {
	isolateState(t)
	app := &cli.Command{
		Name:     "wtg",
		Flags:    []cli.Flag{&cli.StringFlag{Name: "config"}},
		Commands: []*cli.Command{AddCommand(&testRunner{})},
	}
	err := app.Run(context.Background(), []string{"wtg", "add", "--ref", "v1.0.0", "feat", "api"})
	if err == nil || !strings.Contains(err.Error(), "--ref only applies") {
		t.Fatalf("expected --ref to require --read-only, got %v", err)
	}
}

func TestRunSpaceAdd_ReadOnlyOnReadWriteMemberFails(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "api")
	spacePath := filepath.Join(spacesRoot, "feat")
	makeSpace(t, "feat", "geoff/feat", spacePath, []string{"api"}, root)
	cfg := spaceCreateCfg(root, spacesRoot)

	r := &testRunner{statusFn: func(string) (git.RepoStatus, error) {
		return git.RepoStatus{Branch: "geoff/feat"}, nil
	}}
	args := SpaceAddArgs{Name: "feat", Repos: []string{"api"}, ReadOnly: true}
	err := RunSpaceAdd(cfg, r, args, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "wtg remove") {
		t.Fatalf("expected an error naming `wtg remove`, got %v", err)
	}
}

func TestRunSpaceAdd_ReadOnlyAdoptsDetachedWorktree(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "config-lib")
	spacePath := filepath.Join(spacesRoot, "feat")
	// Space state was created before ADR 0024 without ReadOnly: true
	makeSpace(t, "feat", "geoff/feat", spacePath, []string{"config-lib"}, root)
	cfg := spaceCreateCfg(root, spacesRoot)

	var checkedOut string
	r := &testRunner{
		fetchFn: func(string) error { return nil },
		defaultBranchFn: func(string) (string, error) { return "main", nil },
		showFileFn: func(_, _, _ string) ([]byte, error) { return nil, os.ErrNotExist },
		statusFn: func(string) (git.RepoStatus, error) {
			// Worktree is on detached HEAD
			return git.RepoStatus{Branch: ""}, nil
		},
		resolveRefFn: func(_, ref string) (string, error) {
			return "commit-" + ref, nil
		},
		checkoutDetachedFn: func(worktreePath, ref string) error {
			checkedOut = worktreePath + "@" + ref
			return nil
		},
	}

	var out bytes.Buffer
	args := SpaceAddArgs{Name: "feat", Repos: []string{"config-lib"}, ReadOnly: true}
	if err := RunSpaceAdd(cfg, r, args, &out); err != nil {
		t.Fatalf("RunSpaceAdd (adopt): %v", err)
	}

	if !strings.HasSuffix(checkedOut, "/config-lib@commit-origin/main") {
		t.Errorf("expected checkout detached at commit-origin/main, got %q", checkedOut)
	}

	sp, err := state.Load("feat")
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if len(sp.Repos) != 1 {
		t.Fatalf("repos = %+v, want 1 repo", sp.Repos)
	}
	got := sp.Repos[0]
	if !got.ReadOnly || got.Ref != "origin/main" {
		t.Errorf("adopted repo state = %+v, want ReadOnly: true, Ref: origin/main", got)
	}
}

func TestRunSpaceAdd_ReadOnlyAdoptsDetachedWorktreeWithRef(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "config-lib")
	spacePath := filepath.Join(spacesRoot, "feat")
	makeSpace(t, "feat", "geoff/feat", spacePath, []string{"config-lib"}, root)
	cfg := spaceCreateCfg(root, spacesRoot)

	var checkedOut string
	r := &testRunner{
		fetchFn: func(string) error { return nil },
		showFileFn: func(_, _, _ string) ([]byte, error) { return nil, os.ErrNotExist },
		statusFn: func(string) (git.RepoStatus, error) {
			return git.RepoStatus{Branch: ""}, nil
		},
		resolveRefFn: func(_, ref string) (string, error) {
			return "commit-" + ref, nil
		},
		checkoutDetachedFn: func(worktreePath, ref string) error {
			checkedOut = worktreePath + "@" + ref
			return nil
		},
	}

	var out bytes.Buffer
	args := SpaceAddArgs{Name: "feat", Repos: []string{"config-lib"}, ReadOnly: true, Ref: "v1.2.3"}
	if err := RunSpaceAdd(cfg, r, args, &out); err != nil {
		t.Fatalf("RunSpaceAdd (adopt with ref): %v", err)
	}

	if !strings.HasSuffix(checkedOut, "/config-lib@commit-v1.2.3") {
		t.Errorf("expected checkout detached at commit-v1.2.3, got %q", checkedOut)
	}

	sp, err := state.Load("feat")
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	got := sp.Repos[0]
	if !got.ReadOnly || got.Ref != "v1.2.3" {
		t.Errorf("adopted repo state = %+v, want ReadOnly: true, Ref: v1.2.3", got)
	}
}

func TestRunSpaceAdd_ReadOnlyAdoptRefusedWhenModified(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "config-lib")
	spacePath := filepath.Join(spacesRoot, "feat")
	makeSpace(t, "feat", "geoff/feat", spacePath, []string{"config-lib"}, root)
	cfg := spaceCreateCfg(root, spacesRoot)

	r := &testRunner{
		statusFn: func(string) (git.RepoStatus, error) {
			return git.RepoStatus{
				Branch: "",
				Files:  []git.FileStatus{{Path: "lib.go", Index: '.', Worktree: 'M'}},
			}, nil
		},
	}

	args := SpaceAddArgs{Name: "feat", Repos: []string{"config-lib"}, ReadOnly: true}
	err := RunSpaceAdd(cfg, r, args, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("expected uncommitted changes error, got %v", err)
	}
}

func TestRunSpaceAdd_PromoteReevaluatesGoWorkAddsGoMod(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "api")
	makeRepo(t, root, "config-lib")
	spacePath := filepath.Join(spacesRoot, "feat")
	readOnlySpace(t, "config-lib", root, spacePath, "v1.0.0")
	cfg := spaceCreateCfg(root, spacesRoot)

	// Worktree at v1.0.0 had no go.mod on disk
	libWorktree := filepath.Join(spacePath, "config-lib")
	if err := os.MkdirAll(libWorktree, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	r := &testRunner{
		statusFn: func(string) (git.RepoStatus, error) { return git.RepoStatus{}, nil },
		branchExistsFn: func(string, string) (bool, error) { return false, nil },
		remoteBranchExistsFn: func(string, string) (bool, error) { return false, nil },
		worktreeRemoveFn: func(string, string, bool) error { return nil },
		worktreeAddFn: func(string, string, string, string, bool) error { return nil },
		// When checking the branch, ShowFile reports go.mod exists on the branch!
		showFileFn: func(_, _, path string) ([]byte, error) {
			if path == "go.mod" {
				return []byte("module " + constLib + "\n\ngo 1.21\n"), nil
			}
			return nil, os.ErrNotExist
		},
	}

	// Promote config-lib to branch worktree
	args := SpaceAddArgs{Name: "feat", Repos: []string{"config-lib"}}
	if err := RunSpaceAdd(cfg, r, args, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunSpaceAdd (promote): %v", err)
	}

	goWorkBytes, err := os.ReadFile(filepath.Join(spacePath, "go.work"))
	if err != nil {
		t.Fatalf("go.work should be created when branch has go.mod: %v", err)
	}
	if !strings.Contains(string(goWorkBytes), "use (\n\t./config-lib\n)") && !strings.Contains(string(goWorkBytes), "use ./config-lib") {
		t.Errorf("go.work should include config-lib:\n%s", goWorkBytes)
	}
}

func TestRunSpaceAdd_PromoteReevaluatesGoWorkDropsGoMod(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "config-lib")
	spacePath := filepath.Join(spacesRoot, "feat")
	readOnlySpace(t, "config-lib", root, spacePath, "v1.0.0")
	cfg := spaceCreateCfg(root, spacesRoot)

	// Worktree at v1.0.0 had a go.mod on disk
	libWorktree := filepath.Join(spacePath, "config-lib")
	if err := os.MkdirAll(libWorktree, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(libWorktree, "go.mod"), []byte("module "+constLib+"\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	goWorkPath := filepath.Join(spacePath, "go.work")
	if err := os.WriteFile(goWorkPath, []byte("go 1.21\n\nuse ./config-lib\n"), 0o644); err != nil {
		t.Fatalf("write go.work: %v", err)
	}

	r := &testRunner{
		statusFn: func(string) (git.RepoStatus, error) { return git.RepoStatus{}, nil },
		branchExistsFn: func(string, string) (bool, error) { return false, nil },
		remoteBranchExistsFn: func(string, string) (bool, error) { return false, nil },
		worktreeRemoveFn: func(string, string, bool) error { return nil },
		worktreeAddFn: func(string, string, string, string, bool) error { return nil },
		// Branch dropped go.mod!
		showFileFn: func(_, _, _ string) ([]byte, error) {
			return nil, os.ErrNotExist
		},
	}

	// Promote config-lib to branch worktree
	args := SpaceAddArgs{Name: "feat", Repos: []string{"config-lib"}}
	if err := RunSpaceAdd(cfg, r, args, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunSpaceAdd (promote): %v", err)
	}

	// go.work should be removed because no member in the workspace has a go.mod
	if _, err := os.Stat(goWorkPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("go.work should be removed when all members drop go.mod, got err: %v", err)
	}
}

func TestRunSpaceAdd_ReadOnlyOnReadOnlyMemberFails(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "config-lib")
	spacePath := filepath.Join(spacesRoot, "feat")
	readOnlySpace(t, "config-lib", root, spacePath, "v2.0.0")
	cfg := spaceCreateCfg(root, spacesRoot)

	args := SpaceAddArgs{Name: "feat", Repos: []string{"config-lib"}, ReadOnly: true}
	err := RunSpaceAdd(cfg, &testRunner{}, args, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "already a read-only member") {
		t.Fatalf("expected an already-a-member error, got %v", err)
	}
}

// --- promotion ---

func TestRunSpaceAdd_PromotesReadOnlyMember(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "config-lib")
	spacePath := filepath.Join(spacesRoot, "feat")
	readOnlySpace(t, "config-lib", root, spacePath, "v2.0.0")
	cfg := spaceCreateCfg(root, spacesRoot)

	var removed, added bool
	r := &testRunner{
		statusFn:         func(string) (git.RepoStatus, error) { return git.RepoStatus{}, nil },
		branchExistsFn:   func(_, _ string) (bool, error) { return false, nil },
		worktreeRemoveFn: func(_, _ string, _ bool) error { removed = true; return nil },
		worktreeAddFn:    func(_, _, _, _ string, _ bool) error { added = true; return nil },
	}
	if err := RunSpaceAdd(cfg, r, SpaceAddArgs{Name: "feat", Repos: []string{"config-lib"}}, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunSpaceAdd: %v", err)
	}
	if !removed || !added {
		t.Errorf("promotion must remove the detached worktree (%v) and create a branch worktree (%v)", removed, added)
	}
	sp, err := state.Load("feat")
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if sp.Repos[0].ReadOnly || sp.Repos[0].Ref != "" {
		t.Errorf("promoted entry should be read-write with no ref, got %+v", sp.Repos[0])
	}
}

func TestRunSpaceAdd_PromotionRefusedWhenModified(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "config-lib")
	spacePath := filepath.Join(spacesRoot, "feat")
	readOnlySpace(t, "config-lib", root, spacePath, "v2.0.0")
	cfgf := spaceCreateCfg(root, spacesRoot)

	r := &testRunner{statusFn: func(string) (git.RepoStatus, error) {
		return git.RepoStatus{Files: []git.FileStatus{{Path: "lib.go", Index: '.', Worktree: 'M'}}}, nil
	}}

	err := RunSpaceAdd(cfgf, r, SpaceAddArgs{Name: "feat", Repos: []string{"config-lib"}}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected a promotion of a modified member to be refused")
	}
	for _, want := range []string{"uncommitted changes", "lib.go"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

// --- symlink upgrade (E2) ---

func TestRunSpaceAdd_ReadOnlyUpgradesSymlink(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	repoPath := makeRepo(t, root, "config-lib")
	spacePath := filepath.Join(spacesRoot, "feat")
	sp := &state.Space{Name: "feat", Branch: "geoff/feat", Path: spacePath}
	sp.Repos = append(sp.Repos, state.RepoEntry{
		Name:         "config-lib",
		RepoPath:     repoPath,
		WorktreePath: filepath.Join(spacePath, "config-lib"),
		Symlink:      true,
	})
	if err := state.Save(sp); err != nil {
		t.Fatalf("save space: %v", err)
	}
	if err := os.MkdirAll(spacePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	link := filepath.Join(spacePath, "config-lib")
	if err := os.Symlink(repoPath, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	cfg := spaceCreateCfg(root, spacesRoot)

	var added []string
	args := SpaceAddArgs{Name: "feat", Repos: []string{"config-lib"}, ReadOnly: true}
	if err := RunSpaceAdd(cfg, addReadOnlyRunner(&added), args, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunSpaceAdd: %v", err)
	}
	if fi, err := os.Lstat(link); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		t.Error("the symlink should have been replaced by a detached worktree")
	}
	sp, err := state.Load("feat")
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if !sp.Repos[0].ReadOnly || sp.Repos[0].Symlink {
		t.Errorf("entry should be a read-only member, not a symlink: %+v", sp.Repos[0])
	}
}

// --- rebase (D4) ---

func TestRunSpaceRebase_ReadOnlyAdvances(t *testing.T) {
	isolateState(t)
	spacePath := t.TempDir()
	readOnlySpace(t, "config-lib", t.TempDir(), spacePath, "origin/main")

	var checkedOut string
	r := &testRunner{
		fetchFn: func(string) error { return nil },
		statusFn: func(string) (git.RepoStatus, error) {
			return git.RepoStatus{}, nil // clean, detached
		},
		resolveRefFn: func(_, ref string) (string, error) {
			if ref == "HEAD" {
				return "old", nil
			}
			return "new", nil
		},
		checkoutDetachedFn: func(_, ref string) error { checkedOut = ref; return nil },
	}

	var out bytes.Buffer
	if err := RunSpaceRebase(r, "feat", "", false, &out); err != nil {
		t.Fatalf("RunSpaceRebase: %v", err)
	}
	if checkedOut != "new" {
		t.Errorf("checked out %q, want the new tip", checkedOut)
	}
	if !strings.Contains(out.String(), "advanced (was a1b2c3d)") {
		t.Errorf("output should report the advance: %q", out.String())
	}
}

func TestRunSpaceRebase_ReadOnlyUpToDate(t *testing.T) {
	isolateState(t)
	readOnlySpace(t, "config-lib", t.TempDir(), t.TempDir(), "origin/main")

	r := &testRunner{
		fetchFn:      func(string) error { return nil },
		statusFn:     func(string) (git.RepoStatus, error) { return git.RepoStatus{}, nil },
		resolveRefFn: func(_, _ string) (string, error) { return "same", nil },
	}

	var out bytes.Buffer
	if err := RunSpaceRebase(r, "feat", "", false, &out); err != nil {
		t.Fatalf("RunSpaceRebase: %v", err)
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Errorf("output should say up to date: %q", out.String())
	}
}

func TestRunSpaceRebase_ReadOnlyPinnedIsLeftAlone(t *testing.T) {
	isolateState(t)
	readOnlySpace(t, "config-lib", t.TempDir(), t.TempDir(), "v2.0.0")

	// No fetch, no resolve, no checkout: a pinned member is reported, not moved.
	var out bytes.Buffer
	r := &testRunner{statusFn: func(string) (git.RepoStatus, error) { return git.RepoStatus{}, nil }}
	if err := RunSpaceRebase(r, "feat", "", false, &out); err != nil {
		t.Fatalf("RunSpaceRebase: %v", err)
	}
	if !strings.Contains(out.String(), "pinned at v2.0.0") {
		t.Errorf("output should report the pin: %q", out.String())
	}
}

func TestRunSpaceRebase_ReadOnlyModifiedIsSkipped(t *testing.T) {
	isolateState(t)
	readOnlySpace(t, "config-lib", t.TempDir(), t.TempDir(), "origin/main")

	r := &testRunner{
		fetchFn: func(string) error { return nil },
		statusFn: func(string) (git.RepoStatus, error) {
			return git.RepoStatus{Files: []git.FileStatus{{Path: "lib.go", Worktree: 'M'}}}, nil
		},
	}

	var out bytes.Buffer
	err := RunSpaceRebase(r, "feat", "", false, &out)
	if err == nil {
		t.Fatal("a modified read-only member needs attention")
	}
	if !strings.Contains(out.String(), "must stay clean") {
		t.Errorf("output should explain the skip: %q", out.String())
	}
}

// --- push / land / delete / remove ---

func TestRunSpacePush_SkipsReadOnlyMember(t *testing.T) {
	isolateState(t)
	readOnlySpace(t, "config-lib", t.TempDir(), t.TempDir(), "v2.0.0")

	var out bytes.Buffer
	if err := RunSpacePush(&testRunner{}, "feat", &out); err != nil {
		t.Fatalf("a read-only member must not fail push: %v", err)
	}
	if !strings.Contains(out.String(), readOnlySkipMsg) {
		t.Errorf("output should report the skip: %q", out.String())
	}
}

func TestRunSpaceLand_SkipsReadOnlyMemberWithoutAttention(t *testing.T) {
	isolateState(t)
	readOnlySpace(t, "config-lib", t.TempDir(), t.TempDir(), "v2.0.0")

	var out bytes.Buffer
	args := SpaceLandArgs{Name: "feat", NoLink: true}
	if err := RunSpaceLand(context.Background(), &testRunner{}, &fakeGH{}, args, &out); err != nil {
		t.Fatalf("a read-only member must not fail land: %v", err)
	}
	if !strings.Contains(out.String(), readOnlySkipMsg) {
		t.Errorf("output should report the skip: %q", out.String())
	}
}

func TestRunSpaceRemove_ReadOnlyMemberSkipsBranchLogic(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "api")
	makeRepo(t, root, "config-lib")
	spacePath := filepath.Join(spacesRoot, "feat")
	makeSpace(t, "feat", "geoff/feat", spacePath, []string{"api"}, root)
	sp, err := state.Load("feat")
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	sp.Repos = append(sp.Repos, state.RepoEntry{
		Name:         "config-lib",
		RepoPath:     filepath.Join(root, "config-lib"),
		WorktreePath: filepath.Join(spacePath, "config-lib"),
		ReadOnly:     true,
		Ref:          "v2.0.0",
	})
	if err := state.Save(sp); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	cfg := spaceCreateCfg(root, spacesRoot)

	// BranchDelete and CommitsOnNoRemote are left unset: calling either panics.
	r := &testRunner{
		statusFn:         func(string) (git.RepoStatus, error) { return git.RepoStatus{}, nil },
		worktreeRemoveFn: func(_, _ string, _ bool) error { return nil },
	}
	args := SpaceRemoveArgs{Name: "feat", Repos: []string{"config-lib"}, ForceBranch: true}
	var out bytes.Buffer
	if err := RunSpaceRemove(t.Context(), cfg, r, nil, args, strings.NewReader(""), &out, nil); err != nil {
		t.Fatalf("RunSpaceRemove: %v", err)
	}
	if !strings.Contains(out.String(), "worktree removed") {
		t.Errorf("output should report the removed worktree: %q", out.String())
	}
	if strings.Contains(out.String(), "branch not deleted") {
		t.Errorf("no branch is ours to delete: %q", out.String())
	}
}

func TestRunSpaceDelete_ReadOnlyMemberSkipsBranchLogic(t *testing.T) {
	root := t.TempDir()
	spacesRoot := t.TempDir()
	isolateState(t)
	makeRepo(t, root, "config-lib")
	spacePath := filepath.Join(spacesRoot, "feat")
	readOnlySpace(t, "config-lib", root, spacePath, "origin/main")
	cfg := spaceCreateCfg(root, spacesRoot)

	// A repo that happens to have a same-named branch must keep it: no branch
	// lookup or deletion happens for a read-only member (ADR 0024 D7).
	r := &testRunner{
		statusFn:         func(string) (git.RepoStatus, error) { return git.RepoStatus{}, nil },
		worktreeRemoveFn: func(_, _ string, _ bool) error { return nil },
	}
	args := SpaceDeleteArgs{Name: "feat", ForceBranch: true, Yes: true}
	var out bytes.Buffer
	if err := RunSpaceDelete(t.Context(), cfg, r, nil, args, strings.NewReader(""), &out); err != nil {
		t.Fatalf("RunSpaceDelete: %v", err)
	}
}
