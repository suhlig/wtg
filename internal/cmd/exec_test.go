package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/suhlig/wtg/internal/state"
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
