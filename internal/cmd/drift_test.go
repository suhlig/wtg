package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/state"
)

// driftSpace creates a space with an "api" consumer and a "config-lib" library,
// both with a go.mod in their worktree, and returns it.
func driftSpace(t *testing.T, apiGoMod, libGoMod string) (*state.Space, string) {
	t.Helper()
	isolateState(t)
	root := t.TempDir()
	spacePath := filepath.Join(t.TempDir(), "feat")
	sp := makeSpace(t, "feat", "geoff/feat", spacePath, []string{"api"}, root)
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
	for name, content := range map[string]string{"api": apiGoMod, "config-lib": libGoMod} {
		dir := filepath.Join(spacePath, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0o644); err != nil {
			t.Fatalf("write go.mod: %v", err)
		}
	}
	return sp, spacePath
}

const libGoMod = "module " + constLib + "\n\ngo 1.24\n"

func apiRequiring(version string) string {
	return "module example.com/api\n\ngo 1.24\n\nrequire " + constLib + " " + version + "\n"
}

// driftRunner answers the calls the check makes: a tag that resolves, and the
// given rev-list counts.
func driftRunner(behind, ahead int) *testRunner {
	return &testRunner{
		statusFn: func(string) (git.RepoStatus, error) { return git.RepoStatus{Branch: "geoff/feat"}, nil },
		resolveRefFn: func(_, ref string) (string, error) {
			if ref != "refs/tags/v2.0.0" {
				return "", os.ErrNotExist
			}
			return "tagcommit", nil
		},
		revListCountFn: func(_, a, b string) (int, int, error) {
			if a != "refs/tags/v2.0.0" || b != "HEAD" {
				return 0, 0, nil
			}
			return behind, ahead, nil
		},
	}
}

func TestRunSpaceStatus_DriftBehind(t *testing.T) {
	driftSpace(t, apiRequiring("v2.0.0"), libGoMod)

	var out bytes.Buffer
	if err := RunSpaceStatus(driftRunner(12, 0), []string{"feat"}, false, &out); err != nil {
		t.Fatalf("RunSpaceStatus: %v", err)
	}
	want := "config-lib is 12 commits behind v2.0.0, which api requires"
	if !strings.Contains(out.String(), want) {
		t.Errorf("output should report the drift %q: %q", want, out.String())
	}
}

func TestRunSpaceStatus_DriftAheadIsANote(t *testing.T) {
	driftSpace(t, apiRequiring("v2.0.0"), libGoMod)

	var out bytes.Buffer
	if err := RunSpaceStatus(driftRunner(0, 3), []string{"feat"}, false, &out); err != nil {
		t.Fatalf("RunSpaceStatus: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "config-lib is 3 commits ahead of v2.0.0, which api requires") {
		t.Errorf("output should report the lead: %q", got)
	}
	if !strings.Contains(got, "until a tag is cut") {
		t.Errorf("output should say the branch cannot build elsewhere yet: %q", got)
	}
}

func TestRunSpaceStatus_DriftSameCommitIsSilent(t *testing.T) {
	driftSpace(t, apiRequiring("v2.0.0"), libGoMod)

	var out bytes.Buffer
	if err := RunSpaceStatus(driftRunner(0, 0), []string{"feat"}, false, &out); err != nil {
		t.Fatalf("RunSpaceStatus: %v", err)
	}
	if strings.Contains(out.String(), "requires") {
		t.Errorf("no drift expected when the checkout is the required commit: %q", out.String())
	}
}

func TestRunSpaceStatus_DriftSkipsReplacedModule(t *testing.T) {
	apiGoMod := apiRequiring("v2.0.0") + "\nreplace " + constLib + " => ../config-lib\n"
	driftSpace(t, apiGoMod, libGoMod)

	// ResolveRef is left unset: calling it would fail the check, so a silent run
	// proves the replaced module is skipped before any git call.
	r := &testRunner{statusFn: func(string) (git.RepoStatus, error) { return git.RepoStatus{}, nil }}

	var out bytes.Buffer
	if err := RunSpaceStatus(r, []string{"feat"}, false, &out); err != nil {
		t.Fatalf("RunSpaceStatus: %v", err)
	}
	if strings.Contains(out.String(), "requires") {
		t.Errorf("a replaced module must not be reported: %q", out.String())
	}
}

func TestRunSpaceStatus_DriftUnresolvableVersionIsSilent(t *testing.T) {
	driftSpace(t, apiRequiring("v2.0.0"), libGoMod)

	r := &testRunner{
		statusFn:     func(string) (git.RepoStatus, error) { return git.RepoStatus{}, nil },
		resolveRefFn: func(_, _ string) (string, error) { return "", os.ErrNotExist },
	}
	var out bytes.Buffer
	if err := RunSpaceStatus(r, []string{"feat"}, false, &out); err != nil {
		t.Fatalf("RunSpaceStatus: %v", err)
	}
	if strings.Contains(out.String(), "requires") {
		t.Errorf("an unresolvable version must stay silent, not become a false alarm: %q", out.String())
	}
}

func TestRunSpaceStatus_ReadOnlyMemberRow(t *testing.T) {
	isolateState(t)
	spacePath := t.TempDir()
	readOnlySpace(t, "config-lib", t.TempDir(), spacePath, "v2.0.0")

	r := &testRunner{statusFn: func(string) (git.RepoStatus, error) {
		return git.RepoStatus{Files: []git.FileStatus{{Path: "lib.go", Index: '.', Worktree: 'M'}}}, nil
	}}
	var out bytes.Buffer
	if err := RunSpaceStatus(r, []string{"feat"}, false, &out); err != nil {
		t.Fatalf("RunSpaceStatus: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "read-only @ v2.0.0") {
		t.Errorf("a read-only member should show its ref instead of a branch: %q", got)
	}
	if strings.Contains(got, "(detached)") {
		t.Errorf("a read-only member must not be reported as an unexpected detached HEAD: %q", got)
	}
	if !strings.Contains(got, "1 modified") {
		t.Errorf("a modified read-only member should be flagged: %q", got)
	}
}

func TestRunSpaceStatus_ReadOnlyPseudoVersionDrift(t *testing.T) {
	// A pseudo-version resolves through its commit suffix, not a tag. The module
	// path carries no /vN suffix: a v0 pseudo-version is invalid for a /v2 path.
	const pseudoLib = "example.com/org/lib"
	const pseudo = "v0.0.0-20260101120000-0123456789ab"
	apiGoMod := "module example.com/api\n\ngo 1.24\n\nrequire " + pseudoLib + " " + pseudo + "\n"
	driftSpace(t, apiGoMod, "module "+pseudoLib+"\n\ngo 1.24\n")

	r := &testRunner{
		statusFn: func(string) (git.RepoStatus, error) { return git.RepoStatus{}, nil },
		resolveRefFn: func(_, ref string) (string, error) {
			if ref != "0123456789ab" {
				return "", os.ErrNotExist
			}
			return "0123456789ab", nil
		},
		revListCountFn: func(_, _, _ string) (int, int, error) { return 1, 0, nil },
	}
	var out bytes.Buffer
	if err := RunSpaceStatus(r, []string{"feat"}, false, &out); err != nil {
		t.Fatalf("RunSpaceStatus: %v", err)
	}
	if !strings.Contains(out.String(), "is 1 commit behind "+pseudo) {
		t.Errorf("pseudo-version drift not reported: %q", out.String())
	}
}

func TestRunSpaceStatus_DriftSelectedVersionThroughTransitiveWorkspaceMember(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	spacePath := filepath.Join(t.TempDir(), "feat")
	sp := makeSpace(t, "feat", "geoff/feat", spacePath, []string{"api", "service-a"}, root)
	sp.Repos = append(sp.Repos, state.RepoEntry{
		Name:         "config-lib",
		RepoPath:     filepath.Join(root, "config-lib"),
		WorktreePath: filepath.Join(spacePath, "config-lib"),
		ReadOnly:     true,
		Ref:          "v2.1.0",
	})
	if err := state.Save(sp); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	// api requires config-lib at v2.0.0 directly.
	// But api also requires service-a at v1.0.0, and service-a requires config-lib at v2.1.0.
	// Under MVS, api's selected version of config-lib is v2.1.0.
	apiGoMod := "module example.com/api\n\ngo 1.24\n\nrequire (\n\texample.com/service-a v1.0.0\n\t" + constLib + " v2.0.0\n)\n"
	serviceAGoMod := "module example.com/service-a\n\ngo 1.24\n\nrequire " + constLib + " v2.1.0\n"
	libGoMod := "module " + constLib + "\n\ngo 1.24\n"

	for name, content := range map[string]string{"api": apiGoMod, "service-a": serviceAGoMod, "config-lib": libGoMod} {
		dir := filepath.Join(spacePath, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0o644); err != nil {
			t.Fatalf("write go.mod: %v", err)
		}
	}

	r := &testRunner{
		statusFn: func(string) (git.RepoStatus, error) { return git.RepoStatus{Branch: "geoff/feat"}, nil },
		resolveRefFn: func(_, ref string) (string, error) {
			if ref != "refs/tags/v2.1.0" {
				return "", os.ErrNotExist
			}
			return "tagcommit-v2.1.0", nil
		},
		revListCountFn: func(_, a, b string) (int, int, error) {
			if a == "refs/tags/v2.1.0" && b == "HEAD" {
				return 4, 0, nil
			}
			return 0, 0, nil
		},
	}

	var out bytes.Buffer
	if err := RunSpaceStatus(r, []string{"feat"}, false, &out); err != nil {
		t.Fatalf("RunSpaceStatus: %v", err)
	}
	// Both api and service-a should report config-lib is 4 commits behind v2.1.0
	// (api because MVS raised v2.0.0 -> v2.1.0 via service-a).
	wantAPI := "config-lib is 4 commits behind v2.1.0, which api requires"
	wantService := "config-lib is 4 commits behind v2.1.0, which service-a requires"
	if !strings.Contains(out.String(), wantAPI) {
		t.Errorf("output should report drift for api against selected v2.1.0: %q", out.String())
	}
	if !strings.Contains(out.String(), wantService) {
		t.Errorf("output should report drift for service-a against v2.1.0: %q", out.String())
	}
}
