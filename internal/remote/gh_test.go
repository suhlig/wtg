package remote

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// installFakeGH writes an executable `gh` shim into a temp dir and prepends it
// to PATH. The returned function reads back the invocations the shim recorded,
// one per line as "HOST=<host> <args...>". Behaviour is steered by env vars the
// test sets: GH_AUTH_EXIT, GH_VIEW_JSON, GH_MUTATE_EXIT.
func installFakeGH(t *testing.T) func() []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake gh shim requires a POSIX shell")
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	script := `#!/bin/sh
printf 'HOST=%s %s\n' "$GH_HOST" "$*" >> "` + logPath + `"
case "$1 $2" in
  "auth status")
    echo "logged in to github.com"
    exit "${GH_AUTH_EXIT:-0}" ;;
  "repo view")
    printf '%s\n' "$GH_VIEW_JSON" ;;
esac
exit "${GH_MUTATE_EXIT:-0}"
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o700); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() []string {
		data, err := os.ReadFile(logPath)
		if err != nil {
			return nil
		}
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}
}

func TestSystemRunnerAvailable(t *testing.T) {
	installFakeGH(t)
	if err := (SystemRunner{}).Available(); err != nil {
		t.Errorf("Available: %v", err)
	}
}

func TestSystemRunnerAvailableMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := (SystemRunner{}).Available(); err == nil {
		t.Error("Available should fail when gh is not on PATH")
	}
}

func TestSystemRunnerAuthStatus(t *testing.T) {
	calls := installFakeGH(t)
	if err := (SystemRunner{}).AuthStatus(context.Background(), GitHubHost); err != nil {
		t.Fatalf("AuthStatus: %v", err)
	}
	assertCall(t, calls(), "HOST=github.com auth status --hostname github.com")
}

func TestSystemRunnerAuthStatusUnauthenticated(t *testing.T) {
	installFakeGH(t)
	t.Setenv("GH_AUTH_EXIT", "1")
	err := (SystemRunner{}).AuthStatus(context.Background(), GitHubHost)
	if err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("expected authentication error, got: %v", err)
	}
}

func TestSystemRunnerIsArchived(t *testing.T) {
	calls := installFakeGH(t)
	t.Setenv("GH_VIEW_JSON", `{"isArchived": true}`)

	got, err := (SystemRunner{}).IsArchived(context.Background(), GitHubHost, "owner/foo")
	if err != nil {
		t.Fatalf("IsArchived: %v", err)
	}
	if !got {
		t.Error("IsArchived = false, want true")
	}
	assertCall(t, calls(), "HOST=github.com repo view owner/foo --json isArchived")
}

func TestSystemRunnerArchiveAndUnarchive(t *testing.T) {
	calls := installFakeGH(t)
	if err := (SystemRunner{}).Archive(context.Background(), GitHubHost, "owner/foo"); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if err := (SystemRunner{}).Unarchive(context.Background(), GitHubHost, "owner/foo"); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	got := calls()
	assertCall(t, got, "HOST=github.com repo archive owner/foo --yes")
	assertCall(t, got, "HOST=github.com repo unarchive owner/foo --yes")
}

func TestSystemRunnerArchiveFailure(t *testing.T) {
	installFakeGH(t)
	t.Setenv("GH_MUTATE_EXIT", "1")
	if err := (SystemRunner{}).Archive(context.Background(), GitHubHost, "owner/foo"); err == nil {
		t.Error("Archive should surface a gh failure")
	}
}

func assertCall(t *testing.T, calls []string, want string) {
	t.Helper()
	if slices.Contains(calls, want) {
		return
	}
	t.Errorf("gh calls %v do not include %q", calls, want)
}
