package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suhlig/wtg/internal/config"
	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/state"
	"github.com/suhlig/wtg/internal/trace"
)

// deleteRunner builds a testRunner for delete scenarios. statusFn is called for
// each worktree path to check dirty/unpushed state.
func deleteRunner(statusFn func(string) (git.RepoStatus, error)) *testRunner {
	return &testRunner{
		statusFn:         statusFn,
		worktreeRemoveFn: func(_, _ string, _ bool) error { return nil },
	}
}

func cleanStatus(_ string) (git.RepoStatus, error) {
	return git.RepoStatus{}, nil
}

// --- space not found ---

func TestRunSpaceDelete_SpaceNotFound(t *testing.T) {
	isolateState(t)
	var out bytes.Buffer
	err := RunSpaceDelete(&config.Config{}, &testRunner{}, SpaceDeleteArgs{Name: "nonexistent"}, &bytes.Buffer{}, &out)
	if err == nil {
		t.Fatal("expected error when space does not exist")
	}
}

// --- clean delete, no branch removal ---

func TestRunSpaceDelete_RemovesWorktrees(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	spacePath := t.TempDir()
	makeSpace(t, "feat", "feat", spacePath, []string{"api", "frontend"}, root)

	var removed []string
	r := deleteRunner(cleanStatus)
	r.worktreeRemoveFn = func(_, worktreePath string, _ bool) error {
		removed = append(removed, worktreePath)
		return nil
	}

	var out bytes.Buffer
	if err := RunSpaceDelete(&config.Config{}, r, SpaceDeleteArgs{Name: "feat"}, &bytes.Buffer{}, &out); err != nil {
		t.Fatalf("RunSpaceDelete: %v", err)
	}
	if len(removed) != 2 {
		t.Errorf("expected 2 worktree removes, got %d", len(removed))
	}
	// State should be gone.
	if _, err := state.Load("feat"); err == nil {
		t.Error("state should be deleted after successful delete")
	}
}

func TestRunSpaceDelete_NotForced_WhenClean(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	spacePath := t.TempDir()
	makeSpace(t, "feat", "feat", spacePath, []string{"api"}, root)

	var usedForce bool
	r := deleteRunner(cleanStatus)
	r.worktreeRemoveFn = func(_, _ string, force bool) error {
		usedForce = force
		return nil
	}

	var out bytes.Buffer
	if err := RunSpaceDelete(&config.Config{}, r, SpaceDeleteArgs{Name: "feat"}, &bytes.Buffer{}, &out); err != nil {
		t.Fatalf("RunSpaceDelete: %v", err)
	}
	if usedForce {
		t.Error("WorktreeRemove should not use force when worktrees are clean")
	}
}

// --- dirty / unpushed prompts ---

func TestRunSpaceDelete_DirtyWorktree_UserDeclines(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	spacePath := t.TempDir()
	makeSpace(t, "feat", "feat", spacePath, []string{"api"}, root)

	dirtyStatus := func(_ string) (git.RepoStatus, error) {
		return git.RepoStatus{Files: []git.FileStatus{{Path: "a.go", Index: 'M'}}}, nil
	}
	r := deleteRunner(dirtyStatus)

	var out bytes.Buffer
	// "n\n" → user declines
	err := RunSpaceDelete(&config.Config{}, r, SpaceDeleteArgs{Name: "feat"}, strings.NewReader("n\n"), &out)
	if err == nil || !strings.Contains(err.Error(), "aborted: nothing deleted") {
		t.Fatalf("declined prompt must exit non-zero with 'aborted: nothing deleted', got %v", err)
	}
	// State should still exist (nothing deleted).
	if _, err := state.Load("feat"); err != nil {
		t.Error("state should not be deleted when user declines")
	}
	if !strings.Contains(out.String(), "uncommitted changes") {
		t.Errorf("output should mention uncommitted changes: %q", out.String())
	}
}

func TestRunSpaceDelete_DirtyWorktree_UserConfirms(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	spacePath := t.TempDir()
	makeSpace(t, "feat", "feat", spacePath, []string{"api"}, root)

	dirtyStatus := func(_ string) (git.RepoStatus, error) {
		return git.RepoStatus{Files: []git.FileStatus{{Path: "a.go", Index: 'M'}}}, nil
	}
	var usedForce bool
	r := deleteRunner(dirtyStatus)
	r.worktreeRemoveFn = func(_, _ string, force bool) error {
		usedForce = force
		return nil
	}

	var out bytes.Buffer
	if err := RunSpaceDelete(&config.Config{}, r, SpaceDeleteArgs{Name: "feat"}, strings.NewReader("y\n"), &out); err != nil {
		t.Fatalf("RunSpaceDelete: %v", err)
	}
	if !usedForce {
		t.Error("WorktreeRemove should use force when user confirms despite dirty state")
	}
	if _, err := state.Load("feat"); err == nil {
		t.Error("state should be deleted after confirmed delete")
	}
}

func TestRunSpaceDelete_UnpushedCommits_Warned(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	spacePath := t.TempDir()
	makeSpace(t, "feat", "feat", spacePath, []string{"api"}, root)

	r := deleteRunner(func(_ string) (git.RepoStatus, error) {
		return git.RepoStatus{Branch: "feat"}, nil
	})
	r.commitsOnNoRemoteFn = func(_, ref string) (int, error) {
		if ref != "refs/heads/feat" {
			t.Errorf("unexpected ref %q", ref)
		}
		return 2, nil
	}

	var out bytes.Buffer
	_ = RunSpaceDelete(&config.Config{}, r, SpaceDeleteArgs{Name: "feat"}, strings.NewReader("n\n"), &out)
	if !strings.Contains(out.String(), "unpushed") {
		t.Errorf("output should mention unpushed commits: %q", out.String())
	}
}

// --- branch deletion ---

func TestRunSpaceDelete_DeleteBranch(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	spacePath := t.TempDir()
	makeSpace(t, "feat", "mybranch", spacePath, []string{"api"}, root)

	var deletedBranch string
	var deletedForce bool
	r := deleteRunner(cleanStatus)
	r.branchDeleteFn = func(_, branch string, force bool) error {
		deletedBranch = branch
		deletedForce = force
		return nil
	}

	var out bytes.Buffer
	if err := RunSpaceDelete(&config.Config{}, r, SpaceDeleteArgs{Name: "feat", DeleteBranch: true}, &bytes.Buffer{}, &out); err != nil {
		t.Fatalf("RunSpaceDelete: %v", err)
	}
	if deletedBranch != "mybranch" {
		t.Errorf("expected branch mybranch to be deleted, got %q", deletedBranch)
	}
	if !deletedForce {
		t.Error("-d uses git branch -D internally once the commits-on-no-remote check passed (ADR 0019 D11 Q1)")
	}
}

func TestRunSpaceDelete_DeleteBranch_RefusedWhenCommitsOnNoRemote(t *testing.T) {
	isolateState(t)
	makeSpace(t, "feat", "mybranch", t.TempDir(), []string{"api"}, t.TempDir())

	r := deleteRunner(cleanStatus)
	r.commitsOnNoRemoteFn = func(_, _ string) (int, error) { return 1, nil }
	// branchDeleteFn intentionally not set: BranchDelete must not be called.

	var out bytes.Buffer
	err := RunSpaceDelete(&config.Config{}, r, SpaceDeleteArgs{Name: "feat", DeleteBranch: true, Yes: true}, &bytes.Buffer{}, &out)
	if err != nil {
		t.Fatalf("RunSpaceDelete: %v", err)
	}
	if !strings.Contains(out.String(), "exist on no remote") {
		t.Errorf("expected refusal message, got %q", out.String())
	}
}

func TestRunSpaceDelete_WarnsAboutOtherCheckedOutBranch(t *testing.T) {
	isolateState(t)
	makeSpace(t, "feat", "feat", t.TempDir(), []string{"api"}, t.TempDir())

	r := deleteRunner(func(_ string) (git.RepoStatus, error) { return git.RepoStatus{Branch: "scratch"}, nil })
	r.commitsOnNoRemoteFn = func(_, ref string) (int, error) {
		if ref == "refs/heads/scratch" {
			return 3, nil
		}
		return 0, nil
	}
	var out bytes.Buffer
	_ = RunSpaceDelete(&config.Config{}, r, SpaceDeleteArgs{Name: "feat"}, strings.NewReader("n\n"), &out)
	if !strings.Contains(out.String(), "3 unpushed commit(s) on scratch") {
		t.Errorf("expected warning for the checked-out branch, got %q", out.String())
	}
}

func TestRunSpaceDelete_NoTerminal_RefusesWithoutYes(t *testing.T) {
	isolateState(t)
	makeSpace(t, "feat", "feat", t.TempDir(), []string{"api"}, t.TempDir())
	r := deleteRunner(func(_ string) (git.RepoStatus, error) {
		return git.RepoStatus{Files: []git.FileStatus{{Path: "a.go", Index: 'M'}}}, nil
	})
	err := RunSpaceDelete(&config.Config{}, r, SpaceDeleteArgs{Name: "feat", NoTerminal: true}, strings.NewReader("y\n"), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected a refusal that mentions --yes, got %v", err)
	}
	if _, err := state.Load("feat"); err != nil {
		t.Error("nothing may be deleted")
	}
}

func TestRunSpaceDelete_Yes_SkipsPrompt(t *testing.T) {
	isolateState(t)
	makeSpace(t, "feat", "feat", t.TempDir(), []string{"api"}, t.TempDir())
	r := deleteRunner(func(_ string) (git.RepoStatus, error) {
		return git.RepoStatus{Files: []git.FileStatus{{Path: "a.go", Index: 'M'}}}, nil
	})
	if err := RunSpaceDelete(&config.Config{}, r, SpaceDeleteArgs{Name: "feat", Yes: true, NoTerminal: true}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("RunSpaceDelete: %v", err)
	}
	if _, err := state.Load("feat"); err == nil {
		t.Error("state should be deleted with --yes")
	}
}

// --- always.secrets safety (ADR 0019 D11, change 5) ---

func TestRunSpaceDelete_ChangedSecret_KeepsRepoAndFails(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	spacePath := t.TempDir()
	makeSpace(t, "feat", "feat", spacePath, []string{"api", "web"}, root)
	for _, repo := range []string{"api", "web"} {
		if err := os.MkdirAll(filepath.Join(root, repo), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(spacePath, repo), 0o750); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(root, repo, ".env.local"), []byte("A=1\n"), 0o600)
		_ = os.WriteFile(filepath.Join(spacePath, repo, ".env.local"), []byte("A=1\n"), 0o600)
	}
	// api's copy was edited after it was seeded.
	_ = os.WriteFile(filepath.Join(spacePath, "api", ".env.local"), []byte("A=2\n"), 0o600)

	var removed []string
	r := deleteRunner(cleanStatus)
	r.worktreeRemoveFn = func(_, wt string, _ bool) error { removed = append(removed, filepath.Base(wt)); return nil }
	cfg := &config.Config{Always: config.AlwaysConfig{Secrets: []string{".env.local"}}}

	var out bytes.Buffer
	err := RunSpaceDelete(cfg, r, SpaceDeleteArgs{Name: "feat", ForceBranch: false, Yes: true}, &bytes.Buffer{}, &out)
	if err == nil {
		t.Fatal("expected a non-zero exit when a repo is kept")
	}
	if len(removed) != 1 || removed[0] != "web" {
		t.Errorf("only the unchanged repo may be removed, got %v", removed)
	}
	if !strings.Contains(out.String(), ".env.local differs from the main clone") {
		t.Errorf("expected a kept line, got %q", out.String())
	}
	sp, loadErr := state.Load("feat")
	if loadErr != nil {
		t.Fatalf("state must remain: %v", loadErr)
	}
	if len(sp.Repos) != 1 || sp.Repos[0].Name != "api" {
		t.Errorf("state must list only the kept repo, got %+v", sp.Repos)
	}

	// Once the copy is reconciled, a second run finishes the job.
	_ = os.WriteFile(filepath.Join(root, "api", ".env.local"), []byte("A=2\n"), 0o600)
	if err := RunSpaceDelete(cfg, r, SpaceDeleteArgs{Name: "feat", Yes: true}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if _, err := state.Load("feat"); err == nil {
		t.Error("state should be gone after the second run")
	}
}

func TestRunSpaceDelete_SecretSourceMissing_Keeps(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	spacePath := t.TempDir()
	makeSpace(t, "feat", "feat", spacePath, []string{"api"}, root)
	_ = os.MkdirAll(filepath.Join(spacePath, "api"), 0o750)
	_ = os.WriteFile(filepath.Join(spacePath, "api", ".env.local"), []byte("A=1\n"), 0o600)
	cfg := &config.Config{Always: config.AlwaysConfig{Secrets: []string{".env.local"}}}
	if err := RunSpaceDelete(cfg, deleteRunner(cleanStatus), SpaceDeleteArgs{Name: "feat", Yes: true}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("the worktree holds the only copy: delete must keep the repo and fail")
	}
}

func TestRunSpaceDelete_SecretCopyMissingInWorktree_Deletes(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	makeSpace(t, "feat", "feat", t.TempDir(), []string{"api"}, root)
	_ = os.MkdirAll(filepath.Join(root, "api"), 0o750)
	_ = os.WriteFile(filepath.Join(root, "api", ".env.local"), []byte("A=1\n"), 0o600)
	cfg := &config.Config{Always: config.AlwaysConfig{Secrets: []string{".env.local"}}}
	if err := RunSpaceDelete(cfg, deleteRunner(cleanStatus), SpaceDeleteArgs{Name: "feat", Yes: true}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("a missing copy loses nothing: %v", err)
	}
}

func TestRunSpaceDelete_ForceBranch(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	spacePath := t.TempDir()
	makeSpace(t, "feat", "mybranch", spacePath, []string{"api"}, root)

	var deletedForce bool
	r := deleteRunner(cleanStatus)
	r.branchDeleteFn = func(_, _ string, force bool) error {
		deletedForce = force
		return nil
	}

	var out bytes.Buffer
	if err := RunSpaceDelete(&config.Config{}, r, SpaceDeleteArgs{Name: "feat", ForceBranch: true}, &bytes.Buffer{}, &out); err != nil {
		t.Fatalf("RunSpaceDelete: %v", err)
	}
	if !deletedForce {
		t.Error("-D should force-delete branch")
	}
}

func TestRunSpaceDelete_BranchDeleteFailure_StateStillDeleted(t *testing.T) {
	// BranchDelete fails (e.g. unmerged with -d) → SymWarn, but state is deleted
	// because the worktree was already removed successfully.
	isolateState(t)
	root := t.TempDir()
	spacePath := t.TempDir()
	makeSpace(t, "feat", "feat", spacePath, []string{"api"}, root)

	r := deleteRunner(cleanStatus)
	r.branchDeleteFn = func(_, _ string, _ bool) error {
		return fmt.Errorf("branch not fully merged")
	}

	var out bytes.Buffer
	if err := RunSpaceDelete(&config.Config{}, r, SpaceDeleteArgs{Name: "feat", DeleteBranch: true}, &bytes.Buffer{}, &out); err != nil {
		t.Fatalf("RunSpaceDelete should succeed when only branch deletion fails: %v", err)
	}
	// State should still be deleted — the worktree is gone.
	if _, stateErr := state.Load("feat"); stateErr == nil {
		t.Error("state should be deleted even when branch deletion fails")
	}
	// Output should indicate the branch issue.
	if !strings.Contains(out.String(), "branch not deleted") {
		t.Errorf("output should mention branch not deleted: %q", out.String())
	}
}

func TestRunSpaceDelete_NoBranchDelete_WhenNoFlag(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	spacePath := t.TempDir()
	makeSpace(t, "feat", "feat", spacePath, []string{"api"}, root)

	r := deleteRunner(cleanStatus)
	// branchDeleteFn intentionally not set — panics if called unexpectedly.

	var out bytes.Buffer
	if err := RunSpaceDelete(&config.Config{}, r, SpaceDeleteArgs{Name: "feat"}, &bytes.Buffer{}, &out); err != nil {
		t.Fatalf("RunSpaceDelete: %v", err)
	}
}

// --- multi-repo behaviour ---

func TestRunSpaceDelete_PartialFailure_StatePreserved(t *testing.T) {
	// First repo removed OK, second fails → hadError=true, state preserved.
	isolateState(t)
	root := t.TempDir()
	spacePath := t.TempDir()
	makeSpace(t, "feat", "feat", spacePath, []string{"api", "frontend"}, root)

	callCount := 0
	r := deleteRunner(cleanStatus)
	r.worktreeRemoveFn = func(_, _ string, _ bool) error {
		callCount++
		if callCount == 2 {
			return fmt.Errorf("locked")
		}
		return nil
	}

	var out bytes.Buffer
	err := RunSpaceDelete(&config.Config{}, r, SpaceDeleteArgs{Name: "feat"}, &bytes.Buffer{}, &out)
	if err == nil {
		t.Fatal("expected error when one worktree removal fails")
	}
	// State must survive because one worktree could not be removed.
	if _, stateErr := state.Load("feat"); stateErr != nil {
		t.Error("state should be preserved on partial failure")
	}
	// Both repos should appear in output.
	got := out.String()
	if !strings.Contains(got, "api") || !strings.Contains(got, "frontend") {
		t.Errorf("output should show both repos: %q", got)
	}
}

func TestRunSpaceDelete_MixedDirtyClean_PromptFires(t *testing.T) {
	// One repo dirty, one clean → prompt shown, both removed with force on confirm.
	isolateState(t)
	root := t.TempDir()
	spacePath := t.TempDir()
	makeSpace(t, "feat", "feat", spacePath, []string{"api", "frontend"}, root)

	statusCalls := map[string]git.RepoStatus{
		"api":      {Files: []git.FileStatus{{Path: "a.go", Index: 'M'}}},
		"frontend": {},
	}
	statusFn := func(path string) (git.RepoStatus, error) {
		for name, st := range statusCalls {
			if strings.Contains(path, name) {
				return st, nil
			}
		}
		return git.RepoStatus{}, nil
	}

	var forcedPaths []string
	r := &testRunner{
		statusFn: statusFn,
		worktreeRemoveFn: func(_, worktreePath string, force bool) error {
			if force {
				forcedPaths = append(forcedPaths, worktreePath)
			}
			return nil
		},
	}

	var out bytes.Buffer
	if err := RunSpaceDelete(&config.Config{}, r, SpaceDeleteArgs{Name: "feat"}, strings.NewReader("y\n"), &out); err != nil {
		t.Fatalf("RunSpaceDelete: %v", err)
	}
	// Both repos should be removed with force=true since any warning triggers force.
	if len(forcedPaths) != 2 {
		t.Errorf("expected both repos removed with force, got %d: %v", len(forcedPaths), forcedPaths)
	}
}

func TestRunSpaceDelete_StatusError_SkippedSilently(t *testing.T) {
	// Status fails (e.g. worktree externally deleted) — no prompt, delete proceeds.
	isolateState(t)
	root := t.TempDir()
	spacePath := t.TempDir()
	makeSpace(t, "feat", "feat", spacePath, []string{"api"}, root)

	r := &testRunner{
		statusFn:         func(_ string) (git.RepoStatus, error) { return git.RepoStatus{}, fmt.Errorf("no such path") },
		worktreeRemoveFn: func(_, _ string, _ bool) error { return nil },
	}

	var out bytes.Buffer
	if err := RunSpaceDelete(&config.Config{}, r, SpaceDeleteArgs{Name: "feat"}, &bytes.Buffer{}, &out); err != nil {
		t.Fatalf("RunSpaceDelete: %v", err)
	}
	// No prompt should have been written (no warnings).
	if strings.Contains(out.String(), "[y/N]") {
		t.Errorf("should not prompt when status errors are skipped: %q", out.String())
	}
}

func TestRunSpaceDelete_EmptySpace_DeletesState(t *testing.T) {
	isolateState(t)
	spacePath := t.TempDir()
	makeSpace(t, "feat", "feat", spacePath, nil, "")

	var out bytes.Buffer
	if err := RunSpaceDelete(&config.Config{}, &testRunner{}, SpaceDeleteArgs{Name: "feat"}, &bytes.Buffer{}, &out); err != nil {
		t.Fatalf("RunSpaceDelete: %v", err)
	}
	if _, err := state.Load("feat"); err == nil {
		t.Error("state should be deleted for empty space")
	}
}

func TestRunSpaceDelete_DryRun(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	spacePath := t.TempDir()
	makeSpace(t, "feat", "feat", spacePath, []string{"api"}, root)

	dirtyStatus := func(_ string) (git.RepoStatus, error) {
		return git.RepoStatus{Files: []git.FileStatus{{Path: "a.go", Index: 'M'}}}, nil
	}
	r := &testRunner{
		statusFn: dirtyStatus,
		worktreeRemoveFn: func(_, _ string, _ bool) error {
			return nil
		},
	}

	rec := trace.NewRecorder(true)
	fs := trace.NewOsFS(rec)

	var out bytes.Buffer
	// Empty reader — if it prompts, it would fail or decline
	if err := RunSpaceDelete(&config.Config{}, r, SpaceDeleteArgs{Name: "feat"}, &bytes.Buffer{}, &out, fs); err != nil {
		t.Fatalf("RunSpaceDelete: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "would ask: Delete workspace anyway?") {
		t.Errorf("expected 'would ask' in output, got: %q", got)
	}
	if !strings.Contains(got, "would remove worktree") {
		t.Errorf("expected 'would remove worktree' in output, got: %q", got)
	}
	// State should still exist because FS was in dry-run mode
	if _, err := state.Load("feat"); err != nil {
		t.Errorf("state should still exist after dry-run delete: %v", err)
	}
}

// --- go.work / go.work.sum cleanup ---

func TestRunSpaceDelete_RemovesGoWorkAndSum(t *testing.T) {
	isolateState(t)
	spacePath := t.TempDir()
	sp := makeSpace(t, "feat", "feat", spacePath, nil, "")
	sp.GoWorkspace = true
	if err := state.Save(sp); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	// Write both files so os.Remove has something to delete.
	goWork := filepath.Join(spacePath, "go.work")
	goWorkSum := filepath.Join(spacePath, "go.work.sum")
	if err := os.WriteFile(goWork, []byte("go 1.24\n"), 0o644); err != nil {
		t.Fatalf("write go.work: %v", err)
	}
	if err := os.WriteFile(goWorkSum, []byte(""), 0o644); err != nil {
		t.Fatalf("write go.work.sum: %v", err)
	}

	var out bytes.Buffer
	if err := RunSpaceDelete(&config.Config{}, &testRunner{}, SpaceDeleteArgs{Name: "feat"}, &bytes.Buffer{}, &out); err != nil {
		t.Fatalf("RunSpaceDelete: %v", err)
	}
	if _, err := os.Stat(goWork); err == nil {
		t.Error("go.work should be removed on space delete")
	}
	if _, err := os.Stat(goWorkSum); err == nil {
		t.Error("go.work.sum should be removed on space delete")
	}
}

// --- error handling ---

func TestRunSpaceDelete_WorktreeRemoveError_StatePreserved(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	spacePath := t.TempDir()
	makeSpace(t, "feat", "feat", spacePath, []string{"api"}, root)

	r := deleteRunner(cleanStatus)
	r.worktreeRemoveFn = func(_, _ string, _ bool) error {
		return fmt.Errorf("locked worktree")
	}

	var out bytes.Buffer
	err := RunSpaceDelete(&config.Config{}, r, SpaceDeleteArgs{Name: "feat"}, &bytes.Buffer{}, &out)
	if err == nil {
		t.Fatal("expected error when worktree removal fails")
	}
	// State must not be deleted.
	if _, stateErr := state.Load("feat"); stateErr != nil {
		t.Error("state should be preserved when worktree removal fails")
	}
	if !strings.Contains(out.String(), "locked worktree") {
		t.Errorf("output should mention the error: %q", out.String())
	}
}

// --- always.files cleanup ---

func TestRunSpaceDelete_AlwaysFiles_RemovedBeforeDirectoryDelete(t *testing.T) {
	isolateState(t)
	spacePath := t.TempDir()
	makeSpace(t, "feat", "feat", spacePath, nil, "")

	// Seed a file that simulates what wtg new would have copied.
	seeded := filepath.Join(spacePath, "CLAUDE.md")
	if err := os.WriteFile(seeded, []byte("context\n"), 0o644); err != nil {
		t.Fatalf("write seeded file: %v", err)
	}

	// Use a config that lists the source path (basename must match the seeded file).
	srcPath := filepath.Join(t.TempDir(), "CLAUDE.md")
	cfg := &config.Config{
		Always: config.AlwaysConfig{Files: []string{srcPath}},
	}

	var out bytes.Buffer
	if err := RunSpaceDelete(cfg, &testRunner{}, SpaceDeleteArgs{Name: "feat"}, &bytes.Buffer{}, &out); err != nil {
		t.Fatalf("RunSpaceDelete: %v", err)
	}

	// The seeded file should be gone.
	if _, err := os.Stat(seeded); err == nil {
		t.Error("always.files copy should be removed on space delete")
	}
	// No warning about non-empty directory should appear.
	if strings.Contains(out.String(), "directory not empty") {
		t.Errorf("space directory should be removable after always.files cleanup: %q", out.String())
	}
}

func TestRunSpaceDelete_PrunesNestedWorktreeDirs(t *testing.T) {
	// A repo whose short name contains slashes leaves org/group directories
	// behind once its worktree is removed. Delete must prune those so the space
	// root can be removed without a "directory not empty" warning.
	isolateState(t)
	root := t.TempDir()
	spacePath := t.TempDir()
	nested := filepath.Join(spacePath, "github.com", "suhlig", "dspictl")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	makeSpace(t, "feat", "feat", spacePath, []string{"github.com/suhlig/dspictl"}, root)

	r := deleteRunner(cleanStatus)
	// Simulate git worktree remove: only the leaf worktree dir disappears.
	r.worktreeRemoveFn = func(_, worktreePath string, _ bool) error {
		return os.Remove(worktreePath)
	}

	var out bytes.Buffer
	if err := RunSpaceDelete(&config.Config{}, r, SpaceDeleteArgs{Name: "feat"}, &bytes.Buffer{}, &out); err != nil {
		t.Fatalf("RunSpaceDelete: %v", err)
	}
	if strings.Contains(out.String(), "directory not empty") {
		t.Errorf("space directory should be removable after pruning nested dirs: %q", out.String())
	}
	// Intermediate org/group dirs and the space root should all be gone.
	if _, err := os.Stat(filepath.Join(spacePath, "github.com", "suhlig")); !os.IsNotExist(err) {
		t.Errorf("expected nested dir to be pruned: %v", err)
	}
	if _, err := os.Stat(spacePath); !os.IsNotExist(err) {
		t.Errorf("space root should be removed: %v", err)
	}
}
