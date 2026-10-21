package git_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/git/testhelper"
)

// rebaseFixture is a local clone on branch "feat" (cut from main) plus a helper
// that advances origin/main from a second clone and fetches it.
type rebaseFixture struct {
	local  *testhelper.Repo
	remote *testhelper.Repo
}

func newRebaseFixture(t *testing.T) *rebaseFixture {
	t.Helper()
	local, remote := testhelper.InitWithRemote(t)
	local.GitCmd("checkout", "-b", "feat")
	return &rebaseFixture{local: local, remote: remote}
}

// advanceMain commits file=content to origin/main from a second clone, then
// fetches into the local clone.
func (f *rebaseFixture) advanceMain(t *testing.T, file, content string) {
	t.Helper()
	second := testhelper.RepoAt(t, t.TempDir())
	second.GitCmd("clone", f.remote.Path, ".")
	second.GitCmd("config", "user.email", "test@example.com")
	second.GitCmd("config", "user.name", "Test")
	second.WriteFile(file, content)
	second.GitCmd("add", ".")
	second.GitCmd("commit", "-m", "upstream "+file)
	second.GitCmd("push", "origin", "main")
	if err := runner().Fetch(f.local.Path); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304 -- test path
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRebase_Clean(t *testing.T) {
	t.Parallel()
	f := newRebaseFixture(t)
	f.local.Commit("feature")
	f.advanceMain(t, "up.txt", "up")

	if err := runner().Rebase(f.local.Path, "origin/main"); err != nil {
		t.Fatalf("Rebase: %v", err)
	}
	ok, err := runner().IsAncestor(f.local.Path, "origin/main", "HEAD")
	if err != nil || !ok {
		t.Errorf("IsAncestor after rebase = %v, %v", ok, err)
	}
}

func TestRebase_DirtyTreeKeepsChanges(t *testing.T) {
	t.Parallel()
	f := newRebaseFixture(t)
	f.local.Commit("feature")
	f.advanceMain(t, "up.txt", "up")
	f.local.WriteFile("README.md", "# edited\n") // tracked, uncommitted

	if err := runner().Rebase(f.local.Path, "origin/main"); err != nil {
		t.Fatalf("Rebase: %v", err)
	}
	if got := readFile(t, filepath.Join(f.local.Path, "README.md")); got != "# edited\n" {
		t.Errorf("uncommitted change lost: %q", got)
	}
}

func TestRebase_ConflictThenAbortRestoresEverything(t *testing.T) {
	t.Parallel()
	f := newRebaseFixture(t)
	f.local.WriteFile("c.txt", "feature\n")
	f.local.GitCmd("add", ".")
	f.local.GitCmd("commit", "-m", "feature c")
	f.advanceMain(t, "c.txt", "upstream\n")
	f.local.WriteFile("README.md", "# dirty\n")

	r := runner()
	before, _ := r.ShortRev(f.local.Path, "HEAD")

	err := r.Rebase(f.local.Path, "origin/main")
	if !errors.Is(err, git.ErrRebaseConflict) {
		t.Fatalf("Rebase err = %v, want ErrRebaseConflict", err)
	}
	if op, _ := r.InProgress(f.local.Path); op != "rebase" {
		t.Errorf("InProgress = %q, want rebase", op)
	}
	if err := r.RebaseAbort(f.local.Path); err != nil {
		t.Fatalf("RebaseAbort: %v", err)
	}
	if op, _ := r.InProgress(f.local.Path); op != "" {
		t.Errorf("InProgress after abort = %q", op)
	}
	if after, _ := r.ShortRev(f.local.Path, "HEAD"); after != before {
		t.Errorf("HEAD moved: %s -> %s", before, after)
	}
	if got := readFile(t, filepath.Join(f.local.Path, "README.md")); got != "# dirty\n" {
		t.Errorf("uncommitted change not restored: %q", got)
	}
}

func TestRebase_AutostashConflictIsDetected(t *testing.T) {
	t.Parallel()
	f := newRebaseFixture(t)
	f.local.Commit("feature")
	f.advanceMain(t, "README.md", "# upstream\n")
	f.local.WriteFile("README.md", "# local edit\n")

	r := runner()
	err := r.Rebase(f.local.Path, "origin/main")
	if !errors.Is(err, git.ErrAutostashConflict) {
		t.Fatalf("Rebase err = %v, want ErrAutostashConflict", err)
	}
	if op, _ := r.InProgress(f.local.Path); op != "" {
		t.Errorf("rebase should be complete, InProgress = %q", op)
	}
	if ok, _ := r.IsAncestor(f.local.Path, "origin/main", "HEAD"); !ok {
		t.Error("branch should be rebased onto origin/main")
	}
}

func TestRebase_UntrackedFileBlocks(t *testing.T) {
	t.Parallel()
	f := newRebaseFixture(t)
	f.local.Commit("feature")
	f.advanceMain(t, "new.txt", "upstream\n")
	f.local.WriteFile("new.txt", "untracked\n")

	r := runner()
	before, _ := r.ShortRev(f.local.Path, "HEAD")
	err := r.Rebase(f.local.Path, "origin/main")
	if !errors.Is(err, git.ErrUntrackedBlocked) {
		t.Fatalf("Rebase err = %v, want ErrUntrackedBlocked", err)
	}
	if after, _ := r.ShortRev(f.local.Path, "HEAD"); after != before {
		t.Errorf("HEAD moved: %s -> %s", before, after)
	}
	if got := readFile(t, filepath.Join(f.local.Path, "new.txt")); got != "untracked\n" {
		t.Errorf("untracked file changed: %q", got)
	}
}

func TestIsAncestor(t *testing.T) {
	t.Parallel()
	f := newRebaseFixture(t)
	f.local.Commit("feature")
	r := runner()

	if ok, err := r.IsAncestor(f.local.Path, "origin/main", "HEAD"); err != nil || !ok {
		t.Errorf("origin/main should be ancestor of HEAD: %v, %v", ok, err)
	}
	if ok, err := r.IsAncestor(f.local.Path, "HEAD", "origin/main"); err != nil || ok {
		t.Errorf("HEAD should not be ancestor of origin/main: %v, %v", ok, err)
	}
	if _, err := r.IsAncestor(f.local.Path, "origin/nope", "HEAD"); err == nil {
		t.Error("expected error for unknown ref")
	}
}

func TestInProgress_Merge(t *testing.T) {
	t.Parallel()
	f := newRebaseFixture(t)
	if op, err := runner().InProgress(f.local.Path); err != nil || op != "" {
		t.Fatalf("clean repo: %q, %v", op, err)
	}
	f.local.WriteFile("c.txt", "feature\n")
	f.local.GitCmd("add", ".")
	f.local.GitCmd("commit", "-m", "feature c")
	f.advanceMain(t, "c.txt", "upstream\n")

	// A conflicting merge exits non-zero and leaves MERGE_HEAD behind.
	merge := exec.Command("git", "merge", "origin/main") // #nosec G204 -- test
	merge.Dir = f.local.Path
	if err := merge.Run(); err == nil {
		t.Fatal("expected merge conflict")
	}
	if op, err := runner().InProgress(f.local.Path); err != nil || op != "merge" {
		t.Errorf("InProgress = %q, %v; want merge", op, err)
	}
}
