// Package trace records every mutating git, gh, and filesystem operation that
// wtg performs, so that the --summary and --dry-run flags can print or suppress
// them. See docs/adr/0020-command-summary.md and docs/adr/0021-dry-run.md.
package trace

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
)

// Entry is a single recorded operation.
type Entry struct {
	// Seq is the global sequence number (1-based, assigned by the Recorder).
	Seq int
	// Repo is the -C path for git calls, the context path for gh calls, or ""
	// for filesystem and workspace-level operations.
	Repo string
	// Line is the copy-pasteable representation of the operation, e.g.
	//   "git -C /path worktree add …"
	//   "(wtg) write /path/go.work"
	Line string
	// ExitCode is the actual exit code, or -1 when the call was skipped (dry-run).
	// 0 means success; a non-zero code after a real run is marked in the output.
	ExitCode int
	// Skipped is true when the entry was recorded but not executed (--dry-run).
	Skipped bool
}

// Recorder collects entries in order. It is safe for concurrent use.
type Recorder struct {
	mu      sync.Mutex
	entries []Entry
	seq     int
	dryRun  bool // when true, mutating calls are recorded and skipped
}

// NewRecorder returns a new Recorder. When dryRun is true, mutating calls
// recorded through the runner and FS boundaries are skipped.
func NewRecorder(dryRun bool) *Recorder { return &Recorder{dryRun: dryRun} }

// SetDryRun enables or disables dry-run mode. This is called by main's Before
// hook after flag parsing so the recorder (already held by all runners) picks
// up the flag value.
func (r *Recorder) SetDryRun(v bool) {
	r.mu.Lock()
	r.dryRun = v
	r.mu.Unlock()
}

// DryRun reports whether the recorder is in dry-run mode.
func (r *Recorder) DryRun() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dryRun
}

// Record appends an entry and returns it with its sequence number assigned.
// exitCode should be 0 on success, the actual exit code on failure, or -1 for
// skipped (dry-run) calls.
func (r *Recorder) Record(repo, line string, exitCode int, skipped bool) Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	e := Entry{Seq: r.seq, Repo: repo, Line: line, ExitCode: exitCode, Skipped: skipped}
	r.entries = append(r.entries, e)
	return e
}

// Entries returns a snapshot of all recorded entries in order.
func (r *Recorder) Entries() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Entry, len(r.entries))
	copy(out, r.entries)
	return out
}

// Print writes a summary of mutating entries to w, grouped by repo. It is
// called after the command finishes (including on error).
// heading is "Commands run" for --summary and "Would run" for --dry-run.
func (r *Recorder) Print(w io.Writer, heading string) {
	entries := r.Entries()

	// Collect repos in first-seen order and group entries.
	var repos []string
	byRepo := make(map[string][]Entry)
	for _, e := range entries {
		key := e.Repo
		if key == "" {
			key = "(wtg)"
		}
		if _, seen := byRepo[key]; !seen {
			repos = append(repos, key)
		}
		byRepo[key] = append(byRepo[key], e)
	}

	if len(entries) == 0 {
		fmt.Fprintf(w, "\n%s: (none)\n", heading)
		return
	}

	fmt.Fprintf(w, "\n%s:\n", heading)
	for _, repo := range repos {
		for _, e := range byRepo[repo] {
			line := "  " + e.Line
			if !e.Skipped && e.ExitCode != 0 {
				line = fmt.Sprintf("  %s   # exit %d", e.Line, e.ExitCode)
			}
			fmt.Fprintln(w, line)
		}
	}
}

// Classify reports whether a git subcommand (first positional arg) is
// read-only. Unknown verbs are treated as mutating (see ADR 0020 D3).
func Classify(args []string) bool {
	if len(args) == 0 {
		return false
	}
	verb := args[0]
	switch verb {
	case "rev-parse", "rev-list", "merge-base", "symbolic-ref":
		// symbolic-ref without --delete is a read
		for _, a := range args[1:] {
			if a == "--delete" {
				return false
			}
		}
		return true
	case "status", "worktree":
		// "worktree list" is read; "worktree add" / "worktree remove" are mutations
		if len(args) > 1 && args[1] == "list" {
			return true
		}
		if verb == "status" {
			return true
		}
		return false
	case "stash":
		// "stash list" is a read
		return len(args) > 1 && args[1] == "list"
	case "remote":
		// "remote" (list) and "remote get-url" are reads; "remote set-head" is mutating
		if len(args) == 1 {
			return true
		}
		return args[1] == "get-url"
	case "config":
		// "config --get <key>" is a read; other config forms (--add, --unset, etc.) are mutating
		for _, a := range args[1:] {
			if a == "--get" {
				return true
			}
		}
		return false
	}
	// Everything else — fetch, push, merge, rebase, branch -d/-D, worktree add,
	// worktree remove, ls-remote, remote set-head, etc. — is mutating.
	return false
}

// ClassifyGH reports whether a gh subcommand (first positional arg(s)) is read-only.
func ClassifyGH(args []string) bool {
	if len(args) < 2 {
		return false
	}
	key := args[0] + " " + args[1]
	switch key {
	case "pr list", "pr view", "repo view", "auth status":
		return true
	}
	return false
}

// FS is the only way wtg code changes the filesystem. The production
// implementation records each operation and then performs it. The dry-run
// implementation records and skips. See docs/adr/0020-command-summary.md D4.
type FS interface {
	WriteFile(path string, data []byte, perm fs.FileMode) error
	MkdirAll(path string, perm fs.FileMode) error
	Remove(path string) error
	RemoveAll(path string) error
	Rename(oldPath, newPath string) error
	Symlink(target, link string) error
}

// OsFS is the production FS: records operations and performs them with os.
// When the recorder is in dry-run mode, mutations are recorded as skipped and
// the filesystem is not touched.
type OsFS struct {
	rec *Recorder
}

// NewOsFS returns an OsFS. rec may be nil (no recording, no dry-run skipping).
func NewOsFS(rec *Recorder) *OsFS { return &OsFS{rec: rec} }

// dryRun reports whether the FS is in dry-run mode.
func (f *OsFS) dryRun() bool { return f.rec != nil && f.rec.DryRun() }

func (f *OsFS) record(line string, err error, skipped bool) {
	if f.rec == nil {
		return
	}
	code := 0
	if err != nil {
		code = 1
	}
	f.rec.Record("", line, code, skipped)
}

func (f *OsFS) WriteFile(path string, data []byte, perm fs.FileMode) error {
	if f.dryRun() {
		f.record(fmt.Sprintf("(wtg) write %s", path), nil, true)
		return nil
	}
	err := os.WriteFile(path, data, perm)
	f.record(fmt.Sprintf("(wtg) write %s", path), err, false)
	return err
}

func (f *OsFS) MkdirAll(path string, perm fs.FileMode) error {
	if f.dryRun() {
		f.record(fmt.Sprintf("(wtg) mkdir %s", path), nil, true)
		return nil
	}
	err := os.MkdirAll(path, perm)
	f.record(fmt.Sprintf("(wtg) mkdir %s", path), err, false)
	return err
}

func (f *OsFS) Remove(path string) error {
	if f.dryRun() {
		f.record(fmt.Sprintf("(wtg) remove %s", path), nil, true)
		return nil
	}
	err := os.Remove(path)
	f.record(fmt.Sprintf("(wtg) remove %s", path), err, false)
	return err
}

func (f *OsFS) RemoveAll(path string) error {
	if f.dryRun() {
		f.record(fmt.Sprintf("(wtg) remove -r %s", path), nil, true)
		return nil
	}
	err := os.RemoveAll(path)
	f.record(fmt.Sprintf("(wtg) remove -r %s", path), err, false)
	return err
}

func (f *OsFS) Rename(oldPath, newPath string) error {
	if f.dryRun() {
		f.record(fmt.Sprintf("(wtg) rename %s -> %s", oldPath, newPath), nil, true)
		return nil
	}
	err := os.Rename(oldPath, newPath)
	f.record(fmt.Sprintf("(wtg) rename %s -> %s", oldPath, newPath), err, false)
	return err
}

func (f *OsFS) Symlink(target, link string) error {
	if f.dryRun() {
		f.record(fmt.Sprintf("(wtg) symlink %s -> %s", link, target), nil, true)
		return nil
	}
	err := os.Symlink(target, link)
	f.record(fmt.Sprintf("(wtg) symlink %s -> %s", link, target), err, false)
	return err
}

// fmtArgs shell-quotes arguments that contain spaces or special characters,
// so the printed line is copy-pasteable.
func fmtArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t\n\"'\\") {
			a = "'" + strings.ReplaceAll(a, "'", "'\\''") + "'"
		}
		quoted[i] = a
	}
	return strings.Join(quoted, " ")
}

// FormatGitLine returns the copy-pasteable representation of a git call.
func FormatGitLine(repoPath string, args []string) string {
	return fmt.Sprintf("git -C %s %s", repoPath, fmtArgs(args))
}

// FormatGHLine returns the copy-pasteable representation of a gh call.
func FormatGHLine(args []string) string {
	return "gh " + fmtArgs(args)
}
