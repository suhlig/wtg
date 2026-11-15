package gomod

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// fixtureModule writes a tiny module tree with a consumer that requires lib
// through a directory replacement, so the test needs neither the network nor a
// populated module cache. It returns the consumer directory.
func fixtureModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	lib := filepath.Join(root, "lib")
	writeFile(t, filepath.Join(lib, "go.mod"), "module example.com/org/lib/v2\n\ngo 1.21\n")
	writeFile(t, filepath.Join(lib, "lib.go"), "package lib\n")

	consumer := filepath.Join(root, "consumer")
	writeFile(t, filepath.Join(consumer, "go.mod"), `module example.com/app

go 1.21

require example.com/org/lib/v2 v2.0.0

replace example.com/org/lib/v2 => ../lib
`)
	writeFile(t, filepath.Join(consumer, "app.go"), "package app\n\nimport _ \"example.com/org/lib/v2\"\n")
	return consumer
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func requireGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not on PATH")
	}
}

func TestSystemRunner_RequiredVersion(t *testing.T) {
	requireGo(t)
	consumer := fixtureModule(t)

	goModBefore, err := os.ReadFile(filepath.Join(consumer, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}

	version, replaced, err := New().RequiredVersion(consumer, "example.com/org/lib/v2")
	if err != nil {
		t.Fatalf("RequiredVersion: %v", err)
	}
	if version != "v2.0.0" {
		t.Errorf("version = %q, want v2.0.0", version)
	}
	if !replaced {
		t.Error("replaced = false, want true for a directory replacement")
	}

	// -mod=readonly must leave the consumer's files alone (ADR 0024 D8).
	goModAfter, err := os.ReadFile(filepath.Join(consumer, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if string(goModBefore) != string(goModAfter) {
		t.Errorf("go.mod changed:\nbefore:\n%s\nafter:\n%s", goModBefore, goModAfter)
	}
	if _, err := os.Stat(filepath.Join(consumer, "go.sum")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("go.sum should not have been created: %v", err)
	}
}

func TestSystemRunner_RequiredVersion_NotADependency(t *testing.T) {
	requireGo(t)
	consumer := fixtureModule(t)

	_, _, err := New().RequiredVersion(consumer, "example.com/other/module")
	if !errors.Is(err, ErrNotDependency) {
		t.Fatalf("expected ErrNotDependency, got %v", err)
	}
}

func TestRequirements(t *testing.T) {
	goMod := []byte(`module example.com/app

go 1.21

require (
	example.com/org/lib/v2 v2.0.0
	example.com/org/tool v0.4.0
)

replace example.com/org/tool => ../tool
`)
	reqs := Requirements(goMod)
	if len(reqs) != 2 {
		t.Fatalf("got %d requirements, want 2: %+v", len(reqs), reqs)
	}
	if reqs[0].Path != "example.com/org/lib/v2" || reqs[0].Version != "v2.0.0" || reqs[0].Replaced {
		t.Errorf("reqs[0] = %+v", reqs[0])
	}
	if reqs[1].Path != "example.com/org/tool" || !reqs[1].Replaced {
		t.Errorf("reqs[1] = %+v, want a replaced module", reqs[1])
	}
}

func TestRequirements_InvalidGoMod(t *testing.T) {
	if got := Requirements([]byte("not a go.mod\n")); got != nil {
		t.Errorf("got %+v, want nil for a go.mod that does not parse", got)
	}
}

func TestRefForVersion(t *testing.T) {
	tests := []struct {
		version string
		want    string
		ok      bool
	}{
		{"v2.0.0", "v2.0.0", true},
		{"v0.0.0-20260101120000-0123456789ab", "0123456789ab", true},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := RefForVersion(tt.version)
		if got != tt.want || ok != tt.ok {
			t.Errorf("RefForVersion(%q) = (%q, %v), want (%q, %v)", tt.version, got, ok, tt.want, tt.ok)
		}
	}
}
