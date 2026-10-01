package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/geoffamey/wtg/internal/config"
)

// configApp wraps ConfigCommand in a root carrying the global --config flag.
func configApp() *cli.Command {
	return &cli.Command{
		Name:     "wtg",
		Flags:    []cli.Flag{&cli.StringFlag{Name: "config"}},
		Commands: []*cli.Command{ConfigCommand()},
	}
}

// TestConfigInit_DoesNotClobberLegacyYAML is a regression test: `config init`
// without -o must write the canonical config.toml, never overwrite an existing
// config.yaml with TOML content.
func TestConfigInit_DoesNotClobberLegacyYAML(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	wtgDir := filepath.Join(dir, "wtg")
	if err := os.MkdirAll(wtgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yamlPath := filepath.Join(wtgDir, "config.yaml")
	yamlContent := "discovery:\n  root_dir: ~/myrepos\n"
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := configApp().Run(context.Background(), []string{"wtg", "config", "init", "--force"}); err != nil {
		t.Fatalf("config init: %v", err)
	}

	got, _ := os.ReadFile(yamlPath)
	if string(got) != yamlContent {
		t.Errorf("config.yaml was modified: %q", got)
	}
	tomlData, err := os.ReadFile(filepath.Join(wtgDir, "config.toml"))
	if err != nil {
		t.Fatalf("config.toml not created: %v", err)
	}
	if string(tomlData) != configTemplate {
		t.Error("config.toml is not the template")
	}
}

func TestConfigInit_RejectsNonTOMLOutput(t *testing.T) {
	err := runConfigInit(filepath.Join(t.TempDir(), "config.yaml"), true, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error writing TOML template to a .yaml path")
	}
}

func TestConfigInit_NotesShadowedYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("x: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runConfigInit(filepath.Join(dir, "config.toml"), false, &out); err != nil {
		t.Fatalf("runConfigInit: %v", err)
	}
	if !strings.Contains(out.String(), "config.yaml") {
		t.Errorf("expected a shadow note mentioning config.yaml, got %q", out.String())
	}
}

func TestConfigInit_WritesTemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wtg", "config.toml")
	var out bytes.Buffer
	if err := runConfigInit(path, false, &out); err != nil {
		t.Fatalf("runConfigInit: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written config: %v", err)
	}
	if string(data) != configTemplate {
		t.Errorf("written content does not match template")
	}
	if !strings.Contains(out.String(), path) {
		t.Errorf("output should mention path, got %q", out.String())
	}
	// The required keys ship uncommented, so a fresh scaffold loads with them set.
	cfg, err := config.Load(path)
	if err != nil {
		t.Errorf("Load scaffolded config: %v", err)
	}
	if cfg.Discovery.RootDir == "" {
		t.Error("scaffold should set discovery.root_dir")
	}
	if cfg.Spaces.RootDir == "" {
		t.Error("scaffold should set spaces.root_dir")
	}
}

func TestConfigInit_RefusesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runConfigInit(path, false, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected refusal, got nil")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error should name the path, got %q", err.Error())
	}
}

func TestConfigInit_ForceOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runConfigInit(path, true, &bytes.Buffer{}); err != nil {
		t.Fatalf("runConfigInit --force: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != configTemplate {
		t.Errorf("force did not overwrite with template")
	}
}

func TestConfigInit_Stdout_NeverRefuses(t *testing.T) {
	var out bytes.Buffer
	// "-" writes to out and must not refuse even though the target "exists" notionally.
	if err := runConfigInit("-", false, &out); err != nil {
		t.Fatalf("runConfigInit -: %v", err)
	}
	if out.String() != configTemplate {
		t.Errorf("stdout output should be the template")
	}
}

func TestConfigPrint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("hello = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, note bytes.Buffer
	if err := runConfigPrint(path, &out, &note); err != nil {
		t.Fatalf("runConfigPrint: %v", err)
	}
	if out.String() != "hello = 1\n" {
		t.Errorf("contents: got %q", out.String())
	}
	if note.Len() != 0 {
		t.Errorf("note should be empty when file exists, got %q", note.String())
	}
}

func TestConfigPrint_Missing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such.toml")
	var out, note bytes.Buffer
	if err := runConfigPrint(path, &out, &note); err != nil {
		t.Fatalf("runConfigPrint missing: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout should be empty for missing file, got %q", out.String())
	}
	if !strings.Contains(note.String(), path) {
		t.Errorf("note should name the path, got %q", note.String())
	}
}

// TestConfigTemplate_CoversAllKeys guards against the hand-written template drifting
// from the Config struct: every koanf key (section and leaf) must appear in it.
func TestConfigTemplate_CoversAllKeys(t *testing.T) {
	for _, key := range koanfTags(reflect.TypeFor[config.Config]()) {
		if !strings.Contains(configTemplate, key) {
			t.Errorf("config template is missing key %q", key)
		}
	}
}

func TestResolveEditor(t *testing.T) {
	t.Run("VISUAL takes precedence over EDITOR", func(t *testing.T) {
		t.Setenv("VISUAL", "code --wait")
		t.Setenv("EDITOR", "nano")
		got := resolveEditor()
		want := []string{"code", "--wait"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("resolveEditor() = %v, want %v", got, want)
		}
	})

	t.Run("EDITOR is used when VISUAL is empty", func(t *testing.T) {
		t.Setenv("VISUAL", "")
		t.Setenv("EDITOR", "vim -u NONE")
		got := resolveEditor()
		want := []string{"vim", "-u", "NONE"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("resolveEditor() = %v, want %v", got, want)
		}
	})

	t.Run("Fallback to PATH binaries when env vars unset", func(t *testing.T) {
		t.Setenv("VISUAL", "")
		t.Setenv("EDITOR", "")
		// Create a temp dir with a fake 'nano' binary and set PATH to only that dir
		tmpDir := t.TempDir()
		nanoPath := filepath.Join(tmpDir, "nano")
		if err := os.WriteFile(nanoPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", tmpDir)

		got := resolveEditor()
		want := []string{"nano"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("resolveEditor() = %v, want %v", got, want)
		}
	})

	t.Run("Returns nil when no editor found", func(t *testing.T) {
		t.Setenv("VISUAL", "")
		t.Setenv("EDITOR", "")
		t.Setenv("PATH", t.TempDir()) // empty directory

		got := resolveEditor()
		if got != nil {
			t.Errorf("resolveEditor() = %v, want nil", got)
		}
	})
}

func TestRunConfigEdit(t *testing.T) {
	t.Run("Executes editor with config path", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "nested", "config.toml")
		recordedArgsFile := filepath.Join(dir, "args.txt")

		// Create a helper script that records its arguments
		scriptPath := filepath.Join(dir, "mock-editor.sh")
		scriptContent := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + recordedArgsFile + "\n"
		if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755); err != nil {
			t.Fatal(err)
		}

		t.Setenv("VISUAL", scriptPath+" --flag")
		t.Setenv("EDITOR", "")

		if err := runConfigEdit(configPath); err != nil {
			t.Fatalf("runConfigEdit: %v", err)
		}

		// Ensure the directory was created
		if _, err := os.Stat(filepath.Dir(configPath)); err != nil {
			t.Errorf("expected config dir to be created: %v", err)
		}

		// Verify args passed to editor
		data, err := os.ReadFile(recordedArgsFile)
		if err != nil {
			t.Fatalf("read args: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		wantLines := []string{"--flag", configPath}
		if !reflect.DeepEqual(lines, wantLines) {
			t.Errorf("editor args = %v, want %v", lines, wantLines)
		}
	})

	t.Run("Errors when no editor found", func(t *testing.T) {
		t.Setenv("VISUAL", "")
		t.Setenv("EDITOR", "")
		t.Setenv("PATH", t.TempDir())

		err := runConfigEdit(filepath.Join(t.TempDir(), "config.toml"))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "no editor found") {
			t.Errorf("expected 'no editor found' error, got %v", err)
		}
	})
}

func TestConfigEditCommand(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	recordedArgsFile := filepath.Join(dir, "args.txt")

	scriptPath := filepath.Join(dir, "mock-editor.sh")
	scriptContent := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + recordedArgsFile + "\n"
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("VISUAL", scriptPath)
	t.Setenv("EDITOR", "")

	app := configApp()
	if err := app.Run(context.Background(), []string{"wtg", "--config", configPath, "config", "edit"}); err != nil {
		t.Fatalf("config edit: %v", err)
	}

	data, err := os.ReadFile(recordedArgsFile)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	if strings.TrimSpace(string(data)) != configPath {
		t.Errorf("args = %q, want %q", strings.TrimSpace(string(data)), configPath)
	}
}

func koanfTags(t reflect.Type) []string {
	var tags []string
	for f := range t.Fields() {
		tag, _, _ := strings.Cut(f.Tag.Get("koanf"), ",")
		if tag == "" {
			continue
		}
		tags = append(tags, tag)
		if f.Type.Kind() == reflect.Struct {
			tags = append(tags, koanfTags(f.Type)...)
		}
	}
	return tags
}
