package archive

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// isolateData points the wtg data directory at a temp dir for the test.
func isolateData(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

func TestPath(t *testing.T) {
	isolateData(t)
	if got := filepath.Base(Path()); got != "archived.yaml" {
		t.Errorf("Path base: got %q, want archived.yaml", got)
	}
}

func TestLoad_MissingFileIsEmpty(t *testing.T) {
	isolateData(t)
	f, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if f.Version != schemaVersion {
		t.Errorf("Version: got %d, want %d", f.Version, schemaVersion)
	}
	if len(f.Entries) != 0 {
		t.Errorf("Entries: got %d, want 0", len(f.Entries))
	}
}

func TestAppendAndLoad(t *testing.T) {
	isolateData(t)
	when := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	rec := Record{
		Name:       "github.com/something/foo",
		Origin:     "/home/me/git/github.com/something/foo",
		Remote:     "git@github.com:something/foo.git",
		Host:       "github.com",
		ArchivedAt: when,
	}
	if err := Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}

	f, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(f.Entries) != 1 {
		t.Fatalf("Entries: got %d, want 1", len(f.Entries))
	}
	got := f.Entries[0]
	if got.Name != rec.Name || got.Origin != rec.Origin || got.Remote != rec.Remote || got.Host != rec.Host {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, rec)
	}
	if !got.ArchivedAt.Equal(when) {
		t.Errorf("ArchivedAt: got %v, want %v", got.ArchivedAt, when)
	}
}

func TestAppend_ReplacesSameName(t *testing.T) {
	isolateData(t)
	if err := Append(Record{Name: "a", Origin: "/one"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := Append(Record{Name: "a", Origin: "/two"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	f, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(f.Entries) != 1 || f.Entries[0].Origin != "/two" {
		t.Errorf("expected the later entry to replace the earlier, got %+v", f.Entries)
	}
}

func TestRemove(t *testing.T) {
	isolateData(t)
	if err := Append(Record{Name: "a"}, Record{Name: "b"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := Remove("a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	f, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(f.Entries) != 1 || f.Entries[0].Name != "b" {
		t.Errorf("after Remove(a): got %+v", f.Entries)
	}
}

func TestRemove_LastEntryDeletesFile(t *testing.T) {
	isolateData(t)
	if err := Append(Record{Name: "a"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := Remove("a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(Path()); !os.IsNotExist(err) {
		t.Errorf("archive record should be removed when empty, stat err = %v", err)
	}
}
