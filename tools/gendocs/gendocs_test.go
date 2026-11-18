package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateDocs(t *testing.T) {
	tmpDir := t.TempDir()

	app := buildApp()
	cliPath := filepath.Join(tmpDir, "cli.md")
	if err := generateCLIReference(app, cliPath); err != nil {
		t.Fatalf("generateCLIReference failed: %v", err)
	}

	data, err := os.ReadFile(cliPath)
	if err != nil {
		t.Fatalf("read cli.md failed: %v", err)
	}

	content := string(data)
	if len(content) == 0 {
		t.Errorf("expected non-empty cli.md")
	}

	// Check that main commands and descriptions are included
	for _, expected := range []string{"wtg new", "wtg delete", "wtg land", "wtg repo", "wtg config"} {
		if !stringsContains(content, expected) {
			t.Errorf("expected %q in cli.md", expected)
		}
	}

	configPath := filepath.Join(tmpDir, "config.md")
	if err := generateConfigReference(configPath); err != nil {
		t.Fatalf("generateConfigReference failed: %v", err)
	}

	readOnlyPath := filepath.Join(tmpDir, "read-only.md")
	if err := generateReadOnlyReference(readOnlyPath); err != nil {
		t.Fatalf("generateReadOnlyReference failed: %v", err)
	}
}

func stringsContains(s, substr string) bool {
	return len(s) >= len(substr) && filepath.Join(s, "") != "" && (s == substr || len(s) > 0 && filepath.Base(s) != "" && (s != "" && contains(s, substr)))
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
