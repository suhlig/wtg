package trace_test

import (
	"strings"
	"testing"

	"github.com/suhlig/wtg/internal/trace"
)

func TestClassify_readOnly(t *testing.T) {
	readOnly := [][]string{
		{"rev-parse", "--short", "HEAD"},
		{"rev-list", "--count", "--branches", "--not", "--remotes"},
		{"merge-base", "--is-ancestor", "a", "b"},
		{"symbolic-ref", "refs/remotes/origin/HEAD"},
		{"status", "--porcelain=v2", "--branch"},
		{"worktree", "list", "--porcelain"},
		{"stash", "list"},
		{"remote"},
		{"remote", "get-url", "origin"},
		{"config", "--get", "wtg.landStrategy"},
	}
	for _, args := range readOnly {
		if !trace.Classify(args) {
			t.Errorf("Classify(%v) = false, want true (read-only)", args)
		}
	}
}

func TestClassify_mutating(t *testing.T) {
	mutating := [][]string{
		{"worktree", "add", "-b", "my-feat", "/some/path"},
		{"worktree", "remove", "/some/path"},
		{"fetch", "origin"},
		{"push", "--set-upstream", "origin", "my-feat"},
		{"rebase", "--autostash", "origin/main"},
		{"branch", "-d", "my-feat"},
		{"branch", "-D", "my-feat"},
		{"merge", "--ff-only", "origin/main"},
		{"ls-remote", "--symref", "origin", "HEAD"},
		{"remote", "set-head", "origin", "--auto"},
		{"symbolic-ref", "--delete", "refs/something"},
	}
	for _, args := range mutating {
		if trace.Classify(args) {
			t.Errorf("Classify(%v) = true, want false (mutating)", args)
		}
	}
}

func TestClassifyGH_readOnly(t *testing.T) {
	readOnly := [][]string{
		{"pr", "list"},
		{"pr", "view", "123"},
		{"repo", "view", "owner/foo"},
		{"auth", "status", "--hostname", "github.com"},
	}
	for _, args := range readOnly {
		if !trace.ClassifyGH(args) {
			t.Errorf("ClassifyGH(%v) = false, want true", args)
		}
	}
}

func TestClassifyGH_mutating(t *testing.T) {
	mutating := [][]string{
		{"pr", "create", "--title", "feat"},
		{"pr", "edit", "123"},
		{"repo", "archive", "owner/foo"},
		{"repo", "unarchive", "owner/foo"},
	}
	for _, args := range mutating {
		if trace.ClassifyGH(args) {
			t.Errorf("ClassifyGH(%v) = true, want false", args)
		}
	}
}

func TestRecorder_groupsAndPrints(t *testing.T) {
	rec := trace.NewRecorder(false)
	rec.Record("/repo/a", "git -C /repo/a fetch origin", 0, false)
	rec.Record("/repo/b", "git -C /repo/b fetch origin", 0, false)
	rec.Record("/repo/a", "git -C /repo/a push origin my-feat", 0, false)
	rec.Record("", "(wtg) write /space/go.work", 0, false)

	var sb strings.Builder
	rec.Print(&sb, "Commands run")
	out := sb.String()

	if !strings.Contains(out, "Commands run:") {
		t.Errorf("output missing heading: %q", out)
	}
	if !strings.Contains(out, "git -C /repo/a fetch origin") {
		t.Errorf("missing first fetch: %q", out)
	}
	if !strings.Contains(out, "git -C /repo/b fetch origin") {
		t.Errorf("missing second fetch: %q", out)
	}
	if !strings.Contains(out, "git -C /repo/a push origin my-feat") {
		t.Errorf("missing push: %q", out)
	}
	if !strings.Contains(out, "(wtg) write /space/go.work") {
		t.Errorf("missing fs entry: %q", out)
	}
}

func TestRecorder_empty(t *testing.T) {
	rec := trace.NewRecorder(false)
	var sb strings.Builder
	rec.Print(&sb, "Commands run")
	if !strings.Contains(sb.String(), "(none)") {
		t.Errorf("empty recorder should print (none), got: %q", sb.String())
	}
}

func TestRecorder_exitCode(t *testing.T) {
	rec := trace.NewRecorder(false)
	rec.Record("/repo/a", "git -C /repo/a push origin my-feat", 1, false)

	var sb strings.Builder
	rec.Print(&sb, "Commands run")
	if !strings.Contains(sb.String(), "exit 1") {
		t.Errorf("failed entry should show exit code: %q", sb.String())
	}
}

func TestRecorder_dryRunSkips(t *testing.T) {
	rec := trace.NewRecorder(true)
	rec.Record("/repo/a", "git -C /repo/a fetch origin", -1, true) // skipped
	rec.Record("", "(wtg) write /space/go.work", -1, true)          // skipped

	var sb strings.Builder
	rec.Print(&sb, "Would run")
	out := sb.String()

	if !strings.Contains(out, "Would run:") {
		t.Errorf("dry-run output missing heading: %q", out)
	}
	if !strings.Contains(out, "git -C /repo/a fetch origin") {
		t.Errorf("skipped entry should still appear in Would run: %q", out)
	}
	if strings.Contains(out, "exit") {
		t.Errorf("skipped entry must not show exit code: %q", out)
	}
}

func TestRecorder_setDryRun(t *testing.T) {
	rec := trace.NewRecorder(false)
	if rec.DryRun() {
		t.Error("expected DryRun=false initially")
	}
	rec.SetDryRun(true)
	if !rec.DryRun() {
		t.Error("expected DryRun=true after SetDryRun(true)")
	}
}
