package cmd

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/geoffamey/wtg/internal/git"
	"github.com/geoffamey/wtg/internal/ui"
)

// syncRunner builds a testRunner configured for sync scenarios.
// statusSeq is a list of RepoStatus values returned in order on successive
// calls to Status (before fetch, after fetch).
func syncRunner(defaultBranch string, statusSeq []git.RepoStatus, fetchErr, ffErr error) *testRunner {
	callCount := 0
	return &testRunner{
		defaultBranchFn: func(string) (string, error) { return defaultBranch, nil },
		statusFn: func(string) (git.RepoStatus, error) {
			s := statusSeq[callCount]
			callCount++
			return s, nil
		},
		fetchFn:       func(string) error { return fetchErr },
		fastForwardFn: func(string, string) error { return ffErr },
	}
}

func runSync(t *testing.T, root string, runner *testRunner, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	if err := RunSync(discoverCfg(root, 2), runner, args, false, &out); err != nil {
		t.Fatalf("RunSync: %v", err)
	}
	return out.String()
}

// --- syncOne ---

func TestSyncOne_SkippedNoRemotes(t *testing.T) {
	root := t.TempDir()
	r := &testRunner{
		remotesListFn: func(string) ([]string, error) { return nil, nil },
	}
	sym, msg := syncOne(root, r)
	if sym != "" || msg != "" {
		t.Errorf("expected empty sym/msg for no-remote repo, got sym=%q msg=%q", sym, msg)
	}
}

func TestRunSync_SkipsRepoWithNoRemotes(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "no-remote")
	makeRepo(t, root, "has-remote")

	r := &testRunner{
		remotesListFn: func(repoPath string) ([]string, error) {
			if strings.HasSuffix(repoPath, "no-remote") {
				return nil, nil
			}
			return []string{"origin"}, nil
		},
		defaultBranchFn: func(string) (string, error) { return "main", nil },
		statusFn:        func(string) (git.RepoStatus, error) { return git.RepoStatus{Branch: "main"}, nil },
		fetchFn:         func(string) error { return nil },
		fastForwardFn:   func(string, string) error { return nil },
	}
	got := runSync(t, root, r)
	if strings.Contains(got, "no-remote") {
		t.Errorf("repo without remotes should be silently omitted: %q", got)
	}
	if !strings.Contains(got, "has-remote") {
		t.Errorf("repo with remote should appear: %q", got)
	}
}

func TestSyncOne_UpToDate(t *testing.T) {
	root := t.TempDir()
	clean := git.RepoStatus{Branch: "main"}
	r := syncRunner("main", []git.RepoStatus{clean, clean}, nil, nil)
	sym, msg := syncOne(root, r)
	if sym != ui.SymOK {
		t.Errorf("sym: got %q, want %q", sym, ui.SymOK)
	}
	if !strings.Contains(msg, "up to date") {
		t.Errorf("msg: %q", msg)
	}
}

func TestSyncOne_FastForwarded(t *testing.T) {
	root := t.TempDir()
	before := git.RepoStatus{Branch: "main"}
	after := git.RepoStatus{Branch: "main", Behind: 3}
	r := syncRunner("main", []git.RepoStatus{before, after}, nil, nil)
	sym, msg := syncOne(root, r)
	if sym != ui.SymUp {
		t.Errorf("sym: got %q, want %q", sym, ui.SymUp)
	}
	if !strings.Contains(msg, "3 commits") {
		t.Errorf("msg: %q", msg)
	}
	if !strings.Contains(msg, "origin/main") {
		t.Errorf("msg missing branch: %q", msg)
	}
}

func TestSyncOne_FastForwarded_OneCommit(t *testing.T) {
	root := t.TempDir()
	before := git.RepoStatus{Branch: "main"}
	after := git.RepoStatus{Branch: "main", Behind: 1}
	r := syncRunner("main", []git.RepoStatus{before, after}, nil, nil)
	_, msg := syncOne(root, r)
	if !strings.Contains(msg, "1 commit") || strings.Contains(msg, "1 commits") {
		t.Errorf("singular commit: %q", msg)
	}
}

func TestSyncOne_SkippedDirty(t *testing.T) {
	root := t.TempDir()
	dirty := git.RepoStatus{
		Branch: "main",
		Files:  []git.FileStatus{{Path: "dirty.go", Index: '?', Worktree: '?'}},
	}
	r := &testRunner{
		defaultBranchFn: func(string) (string, error) { return "main", nil },
		statusFn:        func(string) (git.RepoStatus, error) { return dirty, nil },
	}
	sym, msg := syncOne(root, r)
	if sym != ui.SymWarn {
		t.Errorf("sym: got %q, want %q", sym, ui.SymWarn)
	}
	if !strings.Contains(msg, "dirty") {
		t.Errorf("msg: %q", msg)
	}
}

func TestSyncOne_SkippedWrongBranch(t *testing.T) {
	root := t.TempDir()
	r := &testRunner{
		defaultBranchFn: func(string) (string, error) { return "main", nil },
		statusFn:        func(string) (git.RepoStatus, error) { return git.RepoStatus{Branch: "feature"}, nil },
	}
	sym, msg := syncOne(root, r)
	if sym != ui.SymWarn {
		t.Errorf("sym: got %q, want %q", sym, ui.SymWarn)
	}
	if !strings.Contains(msg, "feature") || !strings.Contains(msg, "main") {
		t.Errorf("msg should mention both branches: %q", msg)
	}
}

func TestSyncOne_FetchError(t *testing.T) {
	root := t.TempDir()
	clean := git.RepoStatus{Branch: "main"}
	r := syncRunner("main", []git.RepoStatus{clean}, fmt.Errorf("network error"), nil)
	sym, msg := syncOne(root, r)
	if sym != ui.SymFail {
		t.Errorf("sym: got %q, want %q", sym, ui.SymFail)
	}
	if !strings.Contains(msg, "network error") {
		t.Errorf("msg: %q", msg)
	}
}

func TestSyncOne_FastForwardError(t *testing.T) {
	root := t.TempDir()
	before := git.RepoStatus{Branch: "main"}
	after := git.RepoStatus{Branch: "main", Behind: 2}
	r := syncRunner("main", []git.RepoStatus{before, after}, nil, fmt.Errorf("conflict"))
	sym, msg := syncOne(root, r)
	if sym != ui.SymFail {
		t.Errorf("sym: got %q, want %q", sym, ui.SymFail)
	}
	if !strings.Contains(msg, "conflict") {
		t.Errorf("msg: %q", msg)
	}
}

// --- RunSync ---

func TestRunSync_AllRepos(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "api")
	makeRepo(t, root, "frontend")

	clean := git.RepoStatus{Branch: "main"}
	var calls atomic.Int32
	r := &testRunner{
		defaultBranchFn: func(string) (string, error) { return "main", nil },
		statusFn:        func(string) (git.RepoStatus, error) { calls.Add(1); return clean, nil },
		fetchFn:         func(string) error { return nil },
		fastForwardFn:   func(string, string) error { return nil },
	}

	got := runSync(t, root, r)
	if !strings.Contains(got, "api") || !strings.Contains(got, "frontend") {
		t.Errorf("output missing repo names: %q", got)
	}
}

func TestRunSync_Parallel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		makeRepo(t, root, "api")
		makeRepo(t, root, "svc")
		makeRepo(t, root, "web")

		gate := make(chan struct{})
		var started atomic.Int32

		r := &testRunner{
			defaultBranchFn: func(string) (string, error) { return "main", nil },
			statusFn:        func(string) (git.RepoStatus, error) { return git.RepoStatus{Branch: "main"}, nil },
			fetchFn: func(string) error {
				started.Add(1)
				<-gate
				return nil
			},
		}

		done := make(chan error, 1)
		go func() {
			done <- RunSync(discoverCfg(root, 1), r, nil, false, io.Discard)
		}()

		synctest.Wait()

		if n := started.Load(); n != 3 {
			t.Errorf("expected 3 concurrent fetches before any completed, got %d", n)
		}

		close(gate)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

func TestRunRepoStatus_Parallel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		makeRepo(t, root, "api")
		makeRepo(t, root, "svc")
		makeRepo(t, root, "web")

		gate := make(chan struct{})
		var started atomic.Int32

		r := &testRunner{
			statusFn: func(string) (git.RepoStatus, error) {
				started.Add(1)
				<-gate
				return git.RepoStatus{Branch: "main"}, nil
			},
			defaultBranchFn: func(string) (string, error) { return "main", nil },
		}

		done := make(chan error, 1)
		go func() {
			done <- RunRepoStatus(discoverCfg(root, 1), r, nil, false, io.Discard)
		}()

		synctest.Wait()

		if n := started.Load(); n != 3 {
			t.Errorf("expected 3 concurrent status calls before any completed, got %d", n)
		}

		close(gate)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

func TestRunSync_NamedRepos(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "api")
	makeRepo(t, root, "frontend")

	clean := git.RepoStatus{Branch: "main"}
	r := &testRunner{
		defaultBranchFn: func(string) (string, error) { return "main", nil },
		statusFn:        func(string) (git.RepoStatus, error) { return clean, nil },
		fetchFn:         func(string) error { return nil },
		fastForwardFn:   func(string, string) error { return nil },
	}

	got := runSync(t, root, r, "api") // only api
	if !strings.Contains(got, "api") {
		t.Errorf("output missing api: %q", got)
	}
	if strings.Contains(got, "frontend") {
		t.Errorf("output should not contain frontend: %q", got)
	}
}

func TestRunSync_Progress(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "api")
	makeRepo(t, root, "frontend")

	clean := git.RepoStatus{Branch: "main"}
	r := &testRunner{
		defaultBranchFn: func(string) (string, error) { return "main", nil },
		statusFn:        func(string) (git.RepoStatus, error) { return clean, nil },
		fetchFn:         func(string) error { return nil },
	}

	var out bytes.Buffer
	if err := RunSync(discoverCfg(root, 2), r, nil, true, &out); err != nil {
		t.Fatalf("RunSync: %v", err)
	}
	got := out.String()
	// Progress line comes before the table; find it via the first newline.
	firstNewline := strings.Index(got, "\n")
	if firstNewline < 0 {
		t.Fatalf("expected newline in progress output, got %q", got)
	}
	// The final reprint is at the end of the progress section; grab everything
	// after the last \r before the newline — that's the definitive progress line.
	progressSection := got[:firstNewline]
	lastCR := strings.LastIndex(progressSection, "\r")
	if lastCR < 0 {
		t.Fatalf("expected \\r in progress output, got %q", progressSection)
	}
	progressLine := progressSection[lastCR+1:]
	// Must be bracketed.
	if !strings.HasPrefix(progressLine, "[") || !strings.HasSuffix(progressLine, "]") {
		t.Errorf("progress line not bracketed: %q", progressLine)
	}
	// Two repos succeed → two ✓ symbols inside the brackets (may be ANSI-wrapped).
	if strings.Count(progressLine, ui.SymOK) != 2 {
		t.Errorf("expected two %s inside brackets, got %q", ui.SymOK, progressLine)
	}
}

// TestRunSync_ProgressWrap verifies that when the progress bar is wider than
// the terminal, cursor-up escape sequences are emitted before each \r so the
// bar overwrites itself rather than printing new lines.
func TestRunSync_ProgressWrap(t *testing.T) {
	root := t.TempDir()
	// Create 5 repos; simulate a terminal only 3 columns wide so the bar
	// ( "[·····]" = 7 chars ) wraps across 3 terminal lines.
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		makeRepo(t, root, name)
	}

	clean := git.RepoStatus{Branch: "main"}
	r := &testRunner{
		defaultBranchFn: func(string) (string, error) { return "main", nil },
		statusFn:        func(string) (git.RepoStatus, error) { return clean, nil },
		fetchFn:         func(string) error { return nil },
	}

	// Override the terminal-width function for this test.
	orig := termWidthFn
	termWidthFn = func() int { return 3 }
	t.Cleanup(func() { termWidthFn = orig })

	var out bytes.Buffer
	if err := RunSync(discoverCfg(root, 5), r, nil, true, &out); err != nil {
		t.Fatalf("RunSync: %v", err)
	}
	got := out.String()

	// With 5 slots + 2 brackets = 7 chars and termWidth=3, each reprint (after
	// the first) must be preceded by at least one \x1b[A cursor-up sequence.
	const cursorUp = "\x1b[A"
	if !strings.Contains(got, cursorUp) {
		t.Errorf("expected cursor-up escape in output when bar wraps, got %q", got)
	}
}

func TestRunSync_UnknownRepo(t *testing.T) {
	root := t.TempDir()
	var out bytes.Buffer
	err := RunSync(discoverCfg(root, 2), &testRunner{}, []string{"no-such-repo"}, false, &out)
	if err == nil {
		t.Fatal("expected error for unknown repo")
	}
}

func TestRunSync_NoRootDir(t *testing.T) {
	// When no root dirs are configured, RunSync succeeds with no output.
	var out bytes.Buffer
	if err := RunSync(discoverCfg("", 2), &testRunner{}, nil, false, &out); err != nil {
		t.Fatalf("unexpected error with no root dirs: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output, got: %q", out.String())
	}
}
