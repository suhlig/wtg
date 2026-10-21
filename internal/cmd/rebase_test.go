package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/ui"
)

// rebaseRunner returns a runner for a healthy repo on branch "geoff/feat"
// that is behind origin/main; tests override the fields they care about.
func rebaseRunner() *testRunner {
	return &testRunner{
		fetchFn:         func(string) error { return nil },
		defaultBranchFn: func(string) (string, error) { return "main", nil },
		statusFn: func(string) (git.RepoStatus, error) {
			return git.RepoStatus{Branch: "geoff/feat"}, nil
		},
		isAncestorFn: func(string, string, string) (bool, error) { return false, nil },
		rebaseFn:     func(string, string) error { return nil },
	}
}

func runRebase(t *testing.T, r *testRunner, onto string) (string, error) {
	t.Helper()
	isolateState(t)
	makeSpace(t, "feat", "geoff/feat", t.TempDir(), []string{"api"}, "/repos")
	var out bytes.Buffer
	err := RunSpaceRebase(r, "feat", onto, false, &out)
	return out.String(), err
}

func TestRunSpaceRebase_Rebased(t *testing.T) {
	r := rebaseRunner()
	var gotOnto string
	r.rebaseFn = func(_, onto string) error { gotOnto = onto; return nil }

	out, err := runRebase(t, r, "")
	if err != nil {
		t.Fatalf("RunSpaceRebase: %v", err)
	}
	if gotOnto != "origin/main" {
		t.Errorf("rebased onto %q, want origin/main", gotOnto)
	}
	if !strings.Contains(out, ui.SymUp) || !strings.Contains(out, "rebased (was a1b2c3d)") {
		t.Errorf("output: %q", out)
	}
}

func TestRunSpaceRebase_OntoOverride(t *testing.T) {
	r := rebaseRunner()
	r.defaultBranchFn = nil // must not be consulted
	var gotOnto string
	r.rebaseFn = func(_, onto string) error { gotOnto = onto; return nil }

	if _, err := runRebase(t, r, "develop"); err != nil {
		t.Fatalf("RunSpaceRebase: %v", err)
	}
	if gotOnto != "origin/develop" {
		t.Errorf("rebased onto %q, want origin/develop", gotOnto)
	}
}

func TestRunSpaceRebase_UpToDate(t *testing.T) { // E3
	r := rebaseRunner()
	r.isAncestorFn = func(string, string, string) (bool, error) { return true, nil }
	r.rebaseFn = nil // must not be called

	out, err := runRebase(t, r, "")
	if err != nil {
		t.Fatalf("RunSpaceRebase: %v", err)
	}
	if !strings.Contains(out, "up to date") {
		t.Errorf("output: %q", out)
	}
}

func TestRunSpaceRebase_SkipsSymlinks(t *testing.T) { // E1
	isolateState(t)
	makeSpaceWithSymlink(t, "feat", "geoff/feat", t.TempDir(), "/repos", "api", "shared")

	var rebased []string
	r := rebaseRunner()
	r.rebaseFn = func(p, _ string) error { rebased = append(rebased, p); return nil }

	var out bytes.Buffer
	if err := RunSpaceRebase(r, "feat", "", false, &out); err != nil {
		t.Fatalf("RunSpaceRebase: %v", err)
	}
	if len(rebased) != 1 || !strings.Contains(rebased[0], "api") {
		t.Errorf("rebased: %v", rebased)
	}
	if !strings.Contains(out.String(), "symlink") {
		t.Errorf("output: %q", out.String())
	}
}

func TestRunSpaceRebase_Skips(t *testing.T) { // E2, E6, E7
	tests := []struct {
		name string
		mod  func(*testRunner)
		want string
	}{
		{"unmerged paths", func(r *testRunner) {
			r.statusFn = func(string) (git.RepoStatus, error) {
				return git.RepoStatus{Branch: "geoff/feat", Files: []git.FileStatus{{Path: "a", Index: 'U', Worktree: 'U'}}}, nil
			}
		}, "unresolved conflicts"},
		{"merge in progress", func(r *testRunner) {
			r.inProgressFn = func(string) (string, error) { return "merge", nil }
		}, "merge in progress"},
		{"rebase in progress", func(r *testRunner) {
			r.inProgressFn = func(string) (string, error) { return "rebase", nil }
		}, "rebase in progress; run git rebase --continue or --abort"},
		{"wrong branch", func(r *testRunner) {
			r.statusFn = func(string) (git.RepoStatus, error) { return git.RepoStatus{Branch: "other"}, nil }
		}, "on other, expected geoff/feat"},
		{"detached", func(r *testRunner) {
			r.statusFn = func(string) (git.RepoStatus, error) { return git.RepoStatus{}, nil }
		}, "on detached HEAD, expected geoff/feat"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := rebaseRunner()
			r.rebaseFn = nil // must not be called
			tt.mod(r)
			out, err := runRebase(t, r, "")
			if err == nil {
				t.Error("expected non-zero exit for a skipped repo that needs attention")
			}
			if !strings.Contains(out, "skipped — ") || !strings.Contains(out, tt.want) {
				t.Errorf("output missing %q: %q", tt.want, out)
			}
		})
	}
}

func TestRunSpaceRebase_DirtyTreeIsNotSkipped(t *testing.T) { // D6
	r := rebaseRunner()
	r.statusFn = func(string) (git.RepoStatus, error) {
		return git.RepoStatus{Branch: "geoff/feat", Files: []git.FileStatus{{Path: "a", Index: '.', Worktree: 'M'}}}, nil
	}
	called := false
	r.rebaseFn = func(string, string) error { called = true; return nil }

	if _, err := runRebase(t, r, ""); err != nil {
		t.Fatalf("RunSpaceRebase: %v", err)
	}
	if !called {
		t.Error("dirty worktree should still be rebased")
	}
}

func TestRunSpaceRebase_NoRemoteOmitted(t *testing.T) { // E8
	r := rebaseRunner()
	r.remotesListFn = func(string) ([]string, error) { return nil, nil }
	r.fetchFn = nil
	r.rebaseFn = nil

	out, err := runRebase(t, r, "")
	if err != nil {
		t.Fatalf("RunSpaceRebase: %v", err)
	}
	if strings.Contains(out, "api") {
		t.Errorf("repo without remote should be omitted: %q", out)
	}
}

func TestRunSpaceRebase_FetchFails(t *testing.T) { // E9
	r := rebaseRunner()
	r.fetchFn = func(string) error { return errors.New("offline") }
	r.rebaseFn = nil

	out, err := runRebase(t, r, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out, "fetch failed: offline") {
		t.Errorf("output: %q", out)
	}
}

func TestRunSpaceRebase_DefaultBranchFails(t *testing.T) { // E10
	r := rebaseRunner()
	r.defaultBranchFn = func(string) (string, error) { return "", errors.New("cannot determine default branch: x") }
	r.rebaseFn = nil

	out, err := runRebase(t, r, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out, "cannot determine default branch") {
		t.Errorf("output: %q", out)
	}
}

func TestRunSpaceRebase_OntoMissing(t *testing.T) { // E11
	r := rebaseRunner()
	r.isAncestorFn = func(string, string, string) (bool, error) { return false, errors.New("bad revision") }
	r.rebaseFn = nil

	out, err := runRebase(t, r, "nope")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out, "origin/nope") {
		t.Errorf("output: %q", out)
	}
}

func TestRunSpaceRebase_ConflictAborts(t *testing.T) { // D2, E5
	r := rebaseRunner()
	r.rebaseFn = func(string, string) error { return fmt.Errorf("%w: boom", git.ErrRebaseConflict) }
	aborted := false
	r.rebaseAbortFn = func(string) error { aborted = true; return nil }

	out, err := runRebase(t, r, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !aborted {
		t.Error("rebase was not aborted")
	}
	for _, want := range []string{"conflict", "uncommitted changes were restored", "rebase --autostash origin/main"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %q", want, out)
		}
	}
}

func TestRunSpaceRebase_ConflictAbortFails(t *testing.T) {
	r := rebaseRunner()
	r.rebaseFn = func(string, string) error { return git.ErrRebaseConflict }
	r.rebaseAbortFn = func(string) error { return errors.New("locked") }

	out, err := runRebase(t, r, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out, "could not abort") || !strings.Contains(out, "rebase --abort") {
		t.Errorf("output: %q", out)
	}
}

func TestRunSpaceRebase_AutostashConflict(t *testing.T) { // D6, E20
	r := rebaseRunner()
	r.rebaseFn = func(string, string) error { return git.ErrAutostashConflict }

	out, err := runRebase(t, r, "")
	if err == nil {
		t.Fatal("expected non-zero exit")
	}
	for _, want := range []string{"rebased (was a1b2c3d)", "conflict with the new base", "git stash drop"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %q", want, out)
		}
	}
}

func TestRunSpaceRebase_UntrackedBlocked(t *testing.T) { // E19
	r := rebaseRunner()
	r.rebaseFn = func(string, string) error { return git.ErrUntrackedBlocked }

	out, err := runRebase(t, r, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out, "blocked by untracked files") {
		t.Errorf("output: %q", out)
	}
}

func TestRunSpaceRebase_OtherRepoUnaffectedByFailure(t *testing.T) { // D3
	isolateState(t)
	makeSpace(t, "feat", "geoff/feat", t.TempDir(), []string{"api", "svc"}, "/repos")

	r := rebaseRunner()
	var rebased atomic.Int32
	r.rebaseFn = func(p string, _ string) error {
		if strings.HasSuffix(p, "api") {
			return git.ErrRebaseConflict
		}
		rebased.Add(1)
		return nil
	}
	r.rebaseAbortFn = func(string) error { return nil }

	var out bytes.Buffer
	err := RunSpaceRebase(r, "feat", "", false, &out)
	if err == nil || !strings.Contains(err.Error(), "api") || strings.Contains(err.Error(), "svc") {
		t.Errorf("error should name only api: %v", err)
	}
	if rebased.Load() != 1 {
		t.Errorf("svc should have been rebased")
	}
}

func TestRunSpaceRebase_Progress(t *testing.T) {
	isolateState(t)
	makeSpace(t, "feat", "geoff/feat", t.TempDir(), []string{"api"}, "/repos")

	var out bytes.Buffer
	if err := RunSpaceRebase(rebaseRunner(), "feat", "", true, &out); err != nil {
		t.Fatalf("RunSpaceRebase: %v", err)
	}
	if !strings.Contains(out.String(), "[") || !strings.Contains(out.String(), "fetching") {
		t.Errorf("expected progress bar with phase counts: %q", out.String())
	}
}

func TestRunSpaceRebase_UnknownSpace(t *testing.T) {
	isolateState(t)
	if err := RunSpaceRebase(&testRunner{}, "nonexistent", "", false, io.Discard); err == nil {
		t.Fatal("expected error for unknown space")
	}
}

func TestRunSpaceRebase_Parallel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		isolateState(t)
		makeSpace(t, "feat", "geoff/feat", t.TempDir(), []string{"api", "svc", "web"}, "/repos")

		gate := make(chan struct{})
		var started atomic.Int32
		r := rebaseRunner()
		r.fetchFn = func(string) error {
			started.Add(1)
			<-gate
			return nil
		}

		done := make(chan error, 1)
		go func() { done <- RunSpaceRebase(r, "feat", "", false, io.Discard) }()

		synctest.Wait()
		if n := started.Load(); n != 3 {
			t.Errorf("expected 3 concurrent fetches, got %d", n)
		}
		close(gate)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}
