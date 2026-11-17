package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/suhlig/wtg/internal/state"
	"github.com/suhlig/wtg/internal/trace"
)

// execSpace creates a space whose worktree paths are real directories.
func execSpace(t *testing.T, name string, repos []string) *state.Space {
	t.Helper()
	sp := &state.Space{
		Name:      name,
		Branch:    name,
		Path:      t.TempDir(),
		CreatedAt: time.Now(),
	}
	for _, n := range repos {
		dir := filepath.Join(sp.Path, n)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		sp.Repos = append(sp.Repos, state.RepoEntry{
			Name:         n,
			RepoPath:     "/repos/" + n,
			WorktreePath: dir,
		})
	}
	if err := state.Save(sp); err != nil {
		t.Fatalf("execSpace save: %v", err)
	}
	return sp
}

// --- sequential mode tests ---

func TestRunSpaceExec_RunsInEachWorktree(t *testing.T) {
	isolateState(t)
	execSpace(t, "feat", []string{"api", "svc"})

	var out bytes.Buffer
	if err := RunSpaceExec("feat", []string{"echo", "hello"}, false, &out); err != nil {
		t.Fatalf("RunSpaceExec: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "api") || !strings.Contains(got, "svc") {
		t.Errorf("output missing repo headers: %q", got)
	}
	if strings.Count(got, "hello") != 2 {
		t.Errorf("expected 'hello' twice (once per repo): %q", got)
	}
}

func TestRunSpaceExec_OutputInRepoOrder(t *testing.T) {
	isolateState(t)
	execSpace(t, "feat", []string{"api", "svc"})

	var out bytes.Buffer
	if err := RunSpaceExec("feat", []string{"echo", "hello"}, false, &out); err != nil {
		t.Fatalf("RunSpaceExec: %v", err)
	}
	got := out.String()
	if strings.Index(got, "api") > strings.Index(got, "svc") {
		t.Errorf("repos should appear in state order: %q", got)
	}
}

func TestRunSpaceExec_ContinuesAfterFailure(t *testing.T) {
	isolateState(t)
	execSpace(t, "feat", []string{"api", "svc"})

	var out bytes.Buffer
	// 'false' exits with code 1.
	err := RunSpaceExec("feat", []string{"false"}, false, &out)
	if err == nil {
		t.Fatal("expected error when command fails")
	}
	// Both repos should have been attempted despite the first failing.
	if !strings.Contains(err.Error(), "api") || !strings.Contains(err.Error(), "svc") {
		t.Errorf("error should name all failed repos: %v", err)
	}
}

func TestRunSpaceExec_UnknownSpace(t *testing.T) {
	isolateState(t)
	var out bytes.Buffer
	if err := RunSpaceExec("nonexistent", []string{"echo", "hi"}, false, &out); err == nil {
		t.Fatal("expected error for unknown space")
	}
}

func TestRunSpaceExec_SkipsSymlinks(t *testing.T) {
	isolateState(t)
	sp := execSpace(t, "feat", []string{"api"})
	sp.Repos = append(sp.Repos, state.RepoEntry{
		Name:         "shared",
		RepoPath:     "/repos/shared",
		WorktreePath: "/nonexistent/shared", // never accessed if skipped correctly
		Symlink:      true,
	})
	if err := state.Save(sp); err != nil {
		t.Fatalf("save: %v", err)
	}

	var out bytes.Buffer
	if err := RunSpaceExec("feat", []string{"echo", "hello"}, false, &out); err != nil {
		t.Fatalf("RunSpaceExec: %v", err)
	}
	got := out.String()
	if strings.Count(got, "hello") != 1 {
		t.Errorf("expected 'hello' once (symlink repo skipped): %q", got)
	}
	if !strings.Contains(got, "shared") || !strings.Contains(got, "skipped") {
		t.Errorf("expected skip notice for symlink repo: %q", got)
	}
}

func TestRunSpaceExec_PassesStdinThrough(t *testing.T) {
	isolateState(t)
	execSpace(t, "feat", []string{"api"})

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	if _, err := w.WriteString("hello from stdin"); err != nil {
		t.Fatalf("write pipe: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}

	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	var out bytes.Buffer
	if err := RunSpaceExec("feat", []string{"cat"}, false, &out); err != nil {
		t.Fatalf("RunSpaceExec: %v", err)
	}
	if !strings.Contains(out.String(), "hello from stdin") {
		t.Errorf("expected child to read piped stdin, got: %q", out.String())
	}
}

func TestRunSpaceExec_RunsInWorktreeDir(t *testing.T) {
	isolateState(t)
	sp := execSpace(t, "feat", []string{"api"})

	var out bytes.Buffer
	if err := RunSpaceExec("feat", []string{"pwd"}, false, &out); err != nil {
		t.Fatalf("RunSpaceExec: %v", err)
	}
	// pwd output should be the worktree path.
	want := sp.Repos[0].WorktreePath
	if !strings.Contains(out.String(), want) {
		t.Errorf("expected cwd %q in output: %q", want, out.String())
	}
}

// --- parallel mode tests ---

func TestRunSpaceExec_Parallel_RunsInEachWorktree(t *testing.T) {
	isolateState(t)
	// Override terminal width to 0 so progress bar uses plain \r (no ANSI cursor-up).
	orig := execTermWidthFn
	execTermWidthFn = func() int { return 0 }
	defer func() { execTermWidthFn = orig }()

	execSpace(t, "feat", []string{"api", "svc"})

	var out bytes.Buffer
	if err := RunSpaceExec("feat", []string{"echo", "hello"}, true, &out); err != nil {
		t.Fatalf("RunSpaceExec parallel: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "api") || !strings.Contains(got, "svc") {
		t.Errorf("output missing repo headers: %q", got)
	}
	if strings.Count(got, "hello") != 2 {
		t.Errorf("expected 'hello' twice (once per repo): %q", got)
	}
}

func TestRunSpaceExec_Parallel_OutputInRepoOrder(t *testing.T) {
	isolateState(t)
	orig := execTermWidthFn
	execTermWidthFn = func() int { return 0 }
	defer func() { execTermWidthFn = orig }()

	execSpace(t, "feat", []string{"api", "svc"})

	var out bytes.Buffer
	if err := RunSpaceExec("feat", []string{"echo", "hello"}, true, &out); err != nil {
		t.Fatalf("RunSpaceExec parallel: %v", err)
	}
	got := out.String()
	if strings.Index(got, "api") > strings.Index(got, "svc") {
		t.Errorf("repos should appear in state order even when run in parallel: %q", got)
	}
}

func TestRunSpaceExec_Parallel_ContinuesAfterFailure(t *testing.T) {
	isolateState(t)
	orig := execTermWidthFn
	execTermWidthFn = func() int { return 0 }
	defer func() { execTermWidthFn = orig }()

	execSpace(t, "feat", []string{"api", "svc"})

	var out bytes.Buffer
	err := RunSpaceExec("feat", []string{"false"}, true, &out)
	if err == nil {
		t.Fatal("expected error when command fails")
	}
	if !strings.Contains(err.Error(), "api") || !strings.Contains(err.Error(), "svc") {
		t.Errorf("error should name all failed repos: %v", err)
	}
}

func TestRunSpaceExec_Parallel_SkipsSymlinks(t *testing.T) {
	isolateState(t)
	orig := execTermWidthFn
	execTermWidthFn = func() int { return 0 }
	defer func() { execTermWidthFn = orig }()

	sp := execSpace(t, "feat", []string{"api"})
	sp.Repos = append(sp.Repos, state.RepoEntry{
		Name:         "shared",
		RepoPath:     "/repos/shared",
		WorktreePath: "/nonexistent/shared",
		Symlink:      true,
	})
	if err := state.Save(sp); err != nil {
		t.Fatalf("save: %v", err)
	}

	var out bytes.Buffer
	if err := RunSpaceExec("feat", []string{"echo", "hello"}, true, &out); err != nil {
		t.Fatalf("RunSpaceExec parallel: %v", err)
	}
	got := out.String()
	if strings.Count(got, "hello") != 1 {
		t.Errorf("expected 'hello' once (symlink repo skipped): %q", got)
	}
	if !strings.Contains(got, "shared") || !strings.Contains(got, "skipped") {
		t.Errorf("expected skip notice for symlink repo: %q", got)
	}
}

func TestRunSpaceExec_Parallel_OutputIsComplete(t *testing.T) {
	isolateState(t)
	orig := execTermWidthFn
	execTermWidthFn = func() int { return 0 }
	defer func() { execTermWidthFn = orig }()

	execSpace(t, "feat", []string{"api", "svc"})

	var out bytes.Buffer
	if err := RunSpaceExec("feat", []string{"echo", "hello"}, true, &out); err != nil {
		t.Fatalf("RunSpaceExec parallel: %v", err)
	}
	got := out.String()
	// Each repo's section must contain the output followed by the result symbol.
	apiIdx := strings.Index(got, "api")
	svcIdx := strings.Index(got, "svc")
	if apiIdx < 0 || svcIdx < 0 {
		t.Fatalf("missing repo sections: %q", got)
	}
	// Verify "hello" appears in each section (before the next section header).
	apiSection := got[apiIdx:svcIdx]
	svcSection := got[svcIdx:]
	if !strings.Contains(apiSection, "hello") {
		t.Errorf("api section missing output: %q", apiSection)
	}
	if !strings.Contains(svcSection, "hello") {
		t.Errorf("svc section missing output: %q", svcSection)
	}
}

// --- resolveExecArgs ---

func TestResolveExecArgs_ExplicitWorkspace(t *testing.T) {
	isolateState(t)
	execSpace(t, "feat", []string{"api"})

	spaceName, execArgs, err := resolveExecArgs([]string{"feat", "git", "status"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spaceName != "feat" {
		t.Errorf("spaceName = %q, want feat", spaceName)
	}
	if len(execArgs) != 2 || execArgs[0] != "git" || execArgs[1] != "status" {
		t.Errorf("execArgs = %v, want [git status]", execArgs)
	}
}

func TestResolveExecArgs_InferFromCWD(t *testing.T) {
	isolateState(t)
	sp := execSpace(t, "feat", []string{"api"})

	// t.Chdir sets PWD so os.Getwd() returns the logical path that matches sp.Path.
	t.Chdir(sp.Repos[0].WorktreePath)

	spaceName, execArgs, err := resolveExecArgs([]string{"echo", "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spaceName != "feat" {
		t.Errorf("spaceName = %q, want feat", spaceName)
	}
	if len(execArgs) != 2 || execArgs[0] != "echo" || execArgs[1] != "hi" {
		t.Errorf("execArgs = %v, want [echo hi]", execArgs)
	}
}

func TestResolveExecArgs_InferFromCWD_NotInAnySpace(t *testing.T) {
	isolateState(t) // empty state — no spaces

	_, _, err := resolveExecArgs([]string{"echo", "hi"})
	if err == nil {
		t.Fatal("expected error when CWD is not inside any workspace")
	}
}

func TestResolveExecArgs_NoArgs(t *testing.T) {
	isolateState(t)

	_, _, err := resolveExecArgs([]string{})
	if err == nil {
		t.Fatal("expected error for empty args")
	}
}

func TestResolveExecArgs_ExplicitWorkspaceNoCmd(t *testing.T) {
	isolateState(t)
	execSpace(t, "feat", []string{"api"})

	_, _, err := resolveExecArgs([]string{"feat"})
	if err == nil {
		t.Fatal("expected error when workspace given but no command follows")
	}
}

func TestRunSpaceExec_InferFromCWD(t *testing.T) {
	isolateState(t)
	sp := execSpace(t, "feat", []string{"api", "svc"})

	// t.Chdir sets PWD so os.Getwd() returns the logical path that matches sp.Path.
	t.Chdir(sp.Repos[0].WorktreePath)

	var out bytes.Buffer
	spaceName, execArgs, err := resolveExecArgs([]string{"echo", "hello"})
	if err != nil {
		t.Fatalf("resolveExecArgs: %v", err)
	}
	if err := RunSpaceExec(spaceName, execArgs, false, &out); err != nil {
		t.Fatalf("RunSpaceExec: %v", err)
	}
	if strings.Count(out.String(), "hello") != 2 {
		t.Errorf("expected 'hello' twice (once per repo): %q", out.String())
	}
}

func TestRunSpaceExec_DryRun(t *testing.T) {
	isolateState(t)
	sp := execSpace(t, "feat", []string{"api", "svc"})

	rec := trace.NewRecorder(true)
	fs := trace.NewOsFS(rec)

	var out bytes.Buffer
	// "nonexistent-cmd-xyz" would fail if executed
	if err := RunSpaceExec("feat", []string{"nonexistent-cmd-xyz", "arg1"}, false, &out, fs); err != nil {
		t.Fatalf("RunSpaceExec: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "api") || !strings.Contains(got, "svc") {
		t.Errorf("output missing repo headers: %q", got)
	}
	if !strings.Contains(got, "would run in") || !strings.Contains(got, "nonexistent-cmd-xyz arg1") {
		t.Errorf("expected 'would run in' in output, got: %q", got)
	}
	if !strings.Contains(got, sp.Repos[0].WorktreePath) {
		t.Errorf("expected worktree path in output, got: %q", got)
	}
}

// --- timeout ---

func TestRunSpaceExec_TimeoutStopsOnlyTheSlowRepo(t *testing.T) {
	isolateState(t)
	sp := execSpace(t, "feat", []string{"api", "svc"})
	// Only api's worktree has the marker file, so only api's command sleeps.
	if err := os.WriteFile(filepath.Join(sp.Repos[0].WorktreePath, "slow"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	script := "if [ -e slow ]; then sleep 30; fi; echo done-in-$(basename $PWD)"

	var out bytes.Buffer
	start := time.Now()
	err := RunSpaceExecTimeout("feat", []string{"sh", "-c", script}, false, 300*time.Millisecond, &out)
	if err == nil || !strings.Contains(err.Error(), "command failed in: api") || strings.Contains(err.Error(), "svc") {
		t.Fatalf("error = %v, want failure in api only", err)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Errorf("slow command was not stopped promptly: %s", elapsed)
	}
	got := out.String()
	if !strings.Contains(got, "timed out after 300ms") {
		t.Errorf("output should say the command timed out: %q", got)
	}
	if !strings.Contains(got, "done-in-svc") {
		t.Errorf("other repos must still run: %q", got)
	}
	if ActiveProcesses(time.Now()) != "" {
		t.Errorf("stopped command still tracked: %q", ActiveProcesses(time.Now()))
	}
}

func TestRunSpaceExec_Parallel_TimeoutStopsOnlyTheSlowRepo(t *testing.T) {
	isolateState(t)
	orig := execTermWidthFn
	execTermWidthFn = func() int { return 0 }
	defer func() { execTermWidthFn = orig }()
	sp := execSpace(t, "feat", []string{"api", "svc"})
	if err := os.WriteFile(filepath.Join(sp.Repos[0].WorktreePath, "slow"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	script := "if [ -e slow ]; then sleep 30; fi; echo done-in-$(basename $PWD)"

	var out bytes.Buffer
	err := RunSpaceExecTimeout("feat", []string{"sh", "-c", script}, true, 300*time.Millisecond, &out)
	if err == nil || !strings.Contains(err.Error(), "command failed in: api") || strings.Contains(err.Error(), "svc") {
		t.Fatalf("error = %v, want failure in api only", err)
	}
	got := out.String()
	if !strings.Contains(got, "timed out after 300ms") || !strings.Contains(got, "done-in-svc") {
		t.Errorf("unexpected output: %q", got)
	}
}

func TestRunSpaceExec_NoTimeoutByDefault(t *testing.T) {
	isolateState(t)
	execSpace(t, "feat", []string{"api"})
	var out bytes.Buffer
	if err := RunSpaceExec("feat", []string{"sh", "-c", "sleep 0.3; echo finished"}, false, &out); err != nil {
		t.Fatalf("RunSpaceExec: %v", err)
	}
	if !strings.Contains(out.String(), "finished") {
		t.Errorf("command should run to completion: %q", out.String())
	}
}

// With every command stuck, the parallel bar names the slow repo.
func TestRunSpaceExec_Parallel_ProgressNamesSlowRepo(t *testing.T) {
	isolateState(t)
	fastSlowTimers(t)
	orig := execTermWidthFn
	execTermWidthFn = func() int { return 0 }
	defer func() { execTermWidthFn = orig }()
	execSpace(t, "feat", []string{"api"})

	var out syncBuf
	done := make(chan error, 1)
	go func() { done <- RunSpaceExec("feat", []string{"sleep", "3"}, true, &out) }()

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "slow: api") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := out.String(); !strings.Contains(got, "(slow: api") {
		t.Errorf("progress should name the slow repo, got %q", got)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// On a terminal the command is not moved to a group of its own (SIGTTIN would
// stop an interactive command), so only the command itself is stopped; its
// timeout is still reported.
func TestRunSpaceExec_Timeout_OnTerminal(t *testing.T) {
	isolateState(t)
	execSpace(t, "feat", []string{"api"})
	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return true }
	defer func() { stdinIsTerminal = orig }()

	var out bytes.Buffer
	err := RunSpaceExecTimeout("feat", []string{"sleep", "30"}, false, 200*time.Millisecond, &out)
	if err == nil || !strings.Contains(out.String(), "timed out after 200ms") {
		t.Errorf("err=%v out=%q, want a reported timeout", err, out.String())
	}
}
