package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suhlig/wtg/internal/config"
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

// --- resolveRepoPaths ---

func TestResolveRepoPaths_ExactPath_SingleRoot(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "api")
	paths, err := resolveRepoPaths(discoverCfg(root, 2), []string{"api"})
	if err != nil {
		t.Fatalf("resolveRepoPaths: %v", err)
	}
	if len(paths) != 1 || !strings.HasSuffix(paths[0], "api") {
		t.Errorf("got %v", paths)
	}
}

func TestResolveRepoPaths_ExactPath_MultipleRoots(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()
	makeRepo(t, root2, "frontend")
	cfg := &config.Config{Discovery: config.DiscoveryConfig{RootDirs: []string{root1, root2}, MaxDepth: 2}}
	paths, err := resolveRepoPaths(cfg, []string{"frontend"})
	if err != nil {
		t.Fatalf("resolveRepoPaths: %v", err)
	}
	if len(paths) != 1 || !strings.HasSuffix(paths[0], "frontend") {
		t.Errorf("got %v", paths)
	}
}

func TestResolveRepoPaths_Basename(t *testing.T) {
	// A nested repo can be addressed by its unique basename.
	root := t.TempDir()
	makeRepo(t, root, "org/api")
	paths, err := resolveRepoPaths(discoverCfg(root, 2), []string{"api"})
	if err != nil {
		t.Fatalf("resolveRepoPaths: %v", err)
	}
	if len(paths) != 1 || !strings.HasSuffix(paths[0], filepath.Join("org", "api")) {
		t.Errorf("got %v", paths)
	}
}

func TestResolveRepoPaths_PartialMatch(t *testing.T) {
	// A unique partial segment match resolves the repo, as in wtg add/new.
	root := t.TempDir()
	makeRepo(t, root, "github.com/uhlig-it/infrastructure")
	paths, err := resolveRepoPaths(discoverCfg(root, 3), []string{"infra"})
	if err != nil {
		t.Fatalf("resolveRepoPaths: %v", err)
	}
	if len(paths) != 1 || !strings.HasSuffix(paths[0], filepath.Join("uhlig-it", "infrastructure")) {
		t.Errorf("got %v", paths)
	}
}

func TestResolveRepoPaths_AmbiguousPartial_Errors(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "github.com/org/infrastructure")
	makeRepo(t, root, "github.com/org/infra-tools")
	_, err := resolveRepoPaths(discoverCfg(root, 3), []string{"infra"})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous error, got %v", err)
	}
}

func TestResolveRepoPaths_NotFound_SingleRoot(t *testing.T) {
	root := t.TempDir()
	_, err := resolveRepoPaths(discoverCfg(root, 2), []string{"nope"})
	if err == nil || !strings.Contains(err.Error(), "not found under") {
		t.Fatalf("expected 'not found under' error, got %v", err)
	}
}

func TestResolveRepoPaths_NotFound_MultipleRoots(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()
	cfg := &config.Config{Discovery: config.DiscoveryConfig{RootDirs: []string{root1, root2}, MaxDepth: 2}}
	_, err := resolveRepoPaths(cfg, []string{"nope"})
	if err == nil || !strings.Contains(err.Error(), "not found under any discovery root dir") {
		t.Fatalf("expected 'not found under any discovery root dir' error, got %v", err)
	}
}
