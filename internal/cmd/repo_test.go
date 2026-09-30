package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoffamey/wtg/internal/config"
)

// makeRepo creates a fake git repo (a directory with a .git subdir) under root.
func makeRepo(t *testing.T, root string, relPath string) string {
	t.Helper()
	dir := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("makeRepo %s: %v", relPath, err)
	}
	return dir
}

func discoverCfg(rootDir string, maxDepth int) *config.Config {
	return &config.Config{
		Discovery: config.DiscoveryConfig{RootDir: rootDir, MaxDepth: maxDepth},
	}
}

// --- discoverRepoPaths ---

func TestDiscoverRepoPaths_Flat(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "api")
	makeRepo(t, root, "frontend")

	paths, err := discoverRepoPaths(root, 2)
	if err != nil {
		t.Fatalf("discoverRepoPaths: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("got %d paths, want 2: %v", len(paths), paths)
	}
}

func TestDiscoverRepoPaths_Nested(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "myorg/api")
	makeRepo(t, root, "myorg/frontend")
	makeRepo(t, root, "infra")

	paths, err := discoverRepoPaths(root, 2)
	if err != nil {
		t.Fatalf("discoverRepoPaths: %v", err)
	}
	if len(paths) != 3 {
		t.Fatalf("got %d paths, want 3: %v", len(paths), paths)
	}
}

func TestDiscoverRepoPaths_MaxDepthRespected(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "shallow")  // depth 1 — included at maxDepth=1
	makeRepo(t, root, "org/deep") // depth 2 — excluded at maxDepth=1

	paths, err := discoverRepoPaths(root, 1)
	if err != nil {
		t.Fatalf("discoverRepoPaths: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("got %d paths, want 1: %v", len(paths), paths)
	}
	if !strings.HasSuffix(paths[0], "shallow") {
		t.Errorf("expected shallow, got %q", paths[0])
	}
}

func TestDiscoverRepoPaths_DoesNotRecurseIntoRepo(t *testing.T) {
	root := t.TempDir()
	// api is a repo that happens to contain another .git — should not be returned twice.
	makeRepo(t, root, "api")
	makeRepo(t, root, "api/vendor/lib") // nested; should be ignored

	paths, err := discoverRepoPaths(root, 3)
	if err != nil {
		t.Fatalf("discoverRepoPaths: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("got %d paths, want 1: %v", len(paths), paths)
	}
}

func TestDiscoverRepoPaths_SkipsHiddenDirs(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "visible")
	makeRepo(t, root, ".hidden") // should be skipped

	paths, err := discoverRepoPaths(root, 2)
	if err != nil {
		t.Fatalf("discoverRepoPaths: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("got %d paths, want 1: %v", len(paths), paths)
	}
}

func TestDiscoverRepoPaths_Empty(t *testing.T) {
	root := t.TempDir()
	paths, err := discoverRepoPaths(root, 2)
	if err != nil {
		t.Fatalf("discoverRepoPaths: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("got %d paths, want 0", len(paths))
	}
}

// --- discoverAllRepoPaths ---

func TestDiscoverAllRepoPaths_MultipleRoots(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()
	makeRepo(t, root1, "api")
	makeRepo(t, root2, "frontend")

	paths, err := discoverAllRepoPaths([]string{root1, root2}, 2)
	if err != nil {
		t.Fatalf("discoverAllRepoPaths: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("got %d paths, want 2: %v", len(paths), paths)
	}
}

func TestDiscoverAllRepoPaths_DeduplicatesOverlappingRoots(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "api")

	// Passing the same root twice should not duplicate results.
	paths, err := discoverAllRepoPaths([]string{root, root}, 2)
	if err != nil {
		t.Fatalf("discoverAllRepoPaths: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("got %d paths, want 1: %v", len(paths), paths)
	}
}

// --- repoName ---

func TestRepoName_SingleRoot(t *testing.T) {
	root := t.TempDir()
	absPath := filepath.Join(root, "myorg", "api")
	name := repoName([]string{root}, absPath)
	if name != "myorg/api" {
		t.Errorf("got %q, want %q", name, "myorg/api")
	}
}

func TestRepoName_MultipleRoots_UsesCorrectRoot(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()
	absPath := filepath.Join(root2, "lib")
	name := repoName([]string{root1, root2}, absPath)
	if name != "lib" {
		t.Errorf("got %q, want %q", name, "lib")
	}
}

func TestRepoName_NoMatchingRoot_ReturnsAbsPath(t *testing.T) {
	absPath := "/some/unrelated/path"
	name := repoName([]string{"/a", "/b"}, absPath)
	if name != absPath {
		t.Errorf("got %q, want %q", name, absPath)
	}
}

// --- resolveRepoPath ---

func TestResolveRepoPath_ExactPath_SingleRoot(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "api")
	p, err := resolveRepoPath([]string{root}, "api")
	if err != nil {
		t.Fatalf("resolveRepoPath: %v", err)
	}
	if !strings.HasSuffix(p, "api") {
		t.Errorf("got %q", p)
	}
}

func TestResolveRepoPath_ExactPath_MultipleRoots(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()
	makeRepo(t, root2, "frontend")
	p, err := resolveRepoPath([]string{root1, root2}, "frontend")
	if err != nil {
		t.Fatalf("resolveRepoPath: %v", err)
	}
	if !strings.HasSuffix(p, "frontend") {
		t.Errorf("got %q", p)
	}
}

func TestResolveRepoPath_NotFound_SingleRoot(t *testing.T) {
	root := t.TempDir()
	_, err := resolveRepoPath([]string{root}, "nope")
	if err == nil || !strings.Contains(err.Error(), "not found under") {
		t.Fatalf("expected 'not found under' error, got %v", err)
	}
}

func TestResolveRepoPath_NotFound_MultipleRoots(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()
	_, err := resolveRepoPath([]string{root1, root2}, "nope")
	if err == nil || !strings.Contains(err.Error(), "not found under any discovery root dir") {
		t.Fatalf("expected 'not found under any discovery root dir' error, got %v", err)
	}
}
