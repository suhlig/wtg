// Package archive records where retired repo clones came from, so they can be
// found and restored later. The record lives in the wtg XDG data directory,
// alongside the space state, and is written by `wtg repo archive` (see
// docs/adr/0013-repo-archive.md).
package archive

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/suhlig/wtg/internal/state"
	"github.com/suhlig/wtg/internal/trace"
)

// schemaVersion is the on-disk format version written at the top of the file.
const schemaVersion = 1

// Record describes one archived repo clone.
type Record struct {
	Name       string    `yaml:"name"`   // short name, e.g. "github.com/something/foo"
	Origin     string    `yaml:"origin"` // absolute discovery path it was moved from
	Remote     string    `yaml:"remote,omitempty"`
	Host       string    `yaml:"host,omitempty"`
	ArchivedAt time.Time `yaml:"archived_at"`
}

// File is the on-disk document: a versioned list of records.
type File struct {
	Version int      `yaml:"version"`
	Entries []Record `yaml:"entries"`
}

// Path returns the archive record file: $XDG_DATA_HOME/wtg/archived.yaml
// (default ~/.local/share/wtg/archived.yaml).
func Path() string {
	return filepath.Join(state.DataRoot(), "archived.yaml")
}

// Load reads the archive record. A missing file is not an error; it yields an
// empty record at the current schema version.
func Load() (*File, error) {
	data, err := os.ReadFile(Path())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &File{Version: schemaVersion}, nil
		}
		return nil, fmt.Errorf("read archive record: %w", err)
	}
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse archive record: %w", err)
	}
	if f.Version == 0 {
		f.Version = schemaVersion
	}
	return &f, nil
}

// Save writes the record, creating the data directory if needed.
func Save(f *File, fsys ...trace.FS) error {
	var fsInst trace.FS = trace.NewOsFS(nil)
	if len(fsys) > 0 && fsys[0] != nil {
		fsInst = fsys[0]
	}
	if f.Version == 0 {
		f.Version = schemaVersion
	}
	if err := fsInst.MkdirAll(filepath.Dir(Path()), 0o750); err != nil { // #nosec G301 -- user data dir
		return fmt.Errorf("create data dir: %w", err)
	}
	data, err := yaml.Marshal(f)
	if err != nil {
		return fmt.Errorf("marshal archive record: %w", err)
	}
	if err := fsInst.WriteFile(Path(), data, 0o600); err != nil {
		return fmt.Errorf("write archive record: %w", err)
	}
	return nil
}

// Append adds records, replacing any existing entry with the same name.
func Append(records ...Record) error {
	return AppendFS(trace.NewOsFS(nil), records...)
}

// AppendFS adds records using the specified FS, replacing any existing entry with the same name.
func AppendFS(fsInst trace.FS, records ...Record) error {
	f, err := Load()
	if err != nil {
		return err
	}
	for _, rec := range records {
		f.set(rec)
	}
	return Save(f, fsInst)
}

// Remove deletes the named entries. When no entries remain the file is removed,
// so a rolled-back first archive leaves no trace.
func Remove(names ...string) error {
	return RemoveFS(trace.NewOsFS(nil), names...)
}

// RemoveFS deletes the named entries using the specified FS. When no entries
// remain the file is removed, so a rolled-back first archive leaves no trace.
func RemoveFS(fsInst trace.FS, names ...string) error {
	f, err := Load()
	if err != nil {
		return err
	}
	for _, name := range names {
		f.delete(name)
	}
	if len(f.Entries) == 0 {
		if err := fsInst.Remove(Path()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove archive record: %w", err)
		}
		return nil
	}
	return Save(f, fsInst)
}

// set inserts rec, replacing an existing entry with the same name.
func (f *File) set(rec Record) {
	for i := range f.Entries {
		if f.Entries[i].Name == rec.Name {
			f.Entries[i] = rec
			return
		}
	}
	f.Entries = append(f.Entries, rec)
}

// delete removes the entry named name, if present.
func (f *File) delete(name string) {
	kept := f.Entries[:0]
	for _, r := range f.Entries {
		if r.Name != name {
			kept = append(kept, r)
		}
	}
	f.Entries = kept
}
