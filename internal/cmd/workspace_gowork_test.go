package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeGoWorkFixture writes a go.work with content into a fresh space dir and
// returns the space path and the go.work path.
func writeGoWorkFixture(t *testing.T, content string) (spacePath, goWorkPath string) {
	t.Helper()
	spacePath = t.TempDir()
	goWorkPath = filepath.Join(spacePath, "go.work")
	if err := os.WriteFile(goWorkPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write go.work: %v", err)
	}
	return spacePath, goWorkPath
}

func goWorkTargets(spacePath string, names ...string) ([]*repoTarget, []bool) {
	var targets []*repoTarget
	var hasGoMod []bool
	for _, n := range names {
		targets = append(targets, &repoTarget{name: n, worktreePath: filepath.Join(spacePath, n)})
		hasGoMod = append(hasGoMod, true)
	}
	return targets, hasGoMod
}

func TestWriteGoWork_PreservesNonUseDirectives(t *testing.T) {
	existing := `// managed by hand
go 1.24

toolchain go1.24.1

use (
	./api
)

replace example.com/lib => ../lib
`
	spacePath, goWorkPath := writeGoWorkFixture(t, existing)
	targets, hasGoMod := goWorkTargets(spacePath, "api", "frontend")

	if err := writeGoWork(goWorkPath, spacePath, targets, hasGoMod, "1.24"); err != nil {
		t.Fatalf("writeGoWork: %v", err)
	}

	data, err := os.ReadFile(goWorkPath)
	if err != nil {
		t.Fatalf("read go.work: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"// managed by hand",
		"toolchain go1.24.1",
		"replace example.com/lib => ../lib",
		"./api",
		"./frontend",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("go.work missing %q:\n%s", want, got)
		}
	}
}

func TestWriteGoWork_DropsRemovedUse(t *testing.T) {
	existing := "go 1.24\n\nuse (\n\t./api\n\t./frontend\n)\n\nreplace example.com/lib => ../lib\n"
	spacePath, goWorkPath := writeGoWorkFixture(t, existing)
	targets, hasGoMod := goWorkTargets(spacePath, "api")

	if err := writeGoWork(goWorkPath, spacePath, targets, hasGoMod, "1.24"); err != nil {
		t.Fatalf("writeGoWork: %v", err)
	}

	data, _ := os.ReadFile(goWorkPath)
	got := string(data)
	if strings.Contains(got, "./frontend") {
		t.Errorf("go.work should not contain ./frontend:\n%s", got)
	}
	if !strings.Contains(got, "./api") {
		t.Errorf("go.work missing ./api:\n%s", got)
	}
	if !strings.Contains(got, "replace example.com/lib => ../lib") {
		t.Errorf("go.work lost replace directive:\n%s", got)
	}
}

func TestWriteGoWork_GoDirectiveOnlyRaised(t *testing.T) {
	tests := []struct {
		name, existing, goVersion, want, notWant string
	}{
		{"keeps higher", "go 1.25\n\nuse ./api\n", "1.24", "go 1.25", "go 1.24"},
		{"raises lower", "go 1.22\n\nuse ./api\n", "1.24", "go 1.24", "go 1.22"},
		{"adds missing", "use ./api\n", "1.24", "go 1.24", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spacePath, goWorkPath := writeGoWorkFixture(t, tc.existing)
			targets, hasGoMod := goWorkTargets(spacePath, "api")

			if err := writeGoWork(goWorkPath, spacePath, targets, hasGoMod, tc.goVersion); err != nil {
				t.Fatalf("writeGoWork: %v", err)
			}

			data, _ := os.ReadFile(goWorkPath)
			got := string(data)
			if !strings.Contains(got, tc.want) {
				t.Errorf("go.work missing %q:\n%s", tc.want, got)
			}
			if tc.notWant != "" && strings.Contains(got, tc.notWant) {
				t.Errorf("go.work should not contain %q:\n%s", tc.notWant, got)
			}
		})
	}
}

func TestWriteGoWork_UnparseableFileIsRegenerated(t *testing.T) {
	spacePath, goWorkPath := writeGoWorkFixture(t, "this is not a go.work {{{\n")
	targets, hasGoMod := goWorkTargets(spacePath, "api")

	if err := writeGoWork(goWorkPath, spacePath, targets, hasGoMod, "1.24"); err != nil {
		t.Fatalf("writeGoWork: %v", err)
	}

	data, _ := os.ReadFile(goWorkPath)
	want := "go 1.24\n\nuse (\n\t./api\n)\n"
	if string(data) != want {
		t.Errorf("go.work:\ngot:  %q\nwant: %q", string(data), want)
	}
}

func TestWriteGoWork_NoExistingFileWritesFresh(t *testing.T) {
	spacePath := t.TempDir()
	goWorkPath := filepath.Join(spacePath, "go.work")
	targets, hasGoMod := goWorkTargets(spacePath, "api", "myorg/lib")

	if err := writeGoWork(goWorkPath, spacePath, targets, hasGoMod, "1.24"); err != nil {
		t.Fatalf("writeGoWork: %v", err)
	}

	data, _ := os.ReadFile(goWorkPath)
	want := "go 1.24\n\nuse (\n\t./api\n\t./myorg/lib\n)\n"
	if string(data) != want {
		t.Errorf("go.work:\ngot:  %q\nwant: %q", string(data), want)
	}
}
