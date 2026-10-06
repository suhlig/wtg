package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/suhlig/wtg/internal/archive"
)

// makeArchivedRepo creates a clone under archiveRoot that looks archived (it
// has a .git entry) and records provenance pointing back at
// <discovery>/<name>. It returns the archived path and the recorded origin.
func makeArchivedRepo(t *testing.T, archiveRoot, discovery, name, remote, host string) (source, origin string) {
	t.Helper()
	source = filepath.Join(archiveRoot, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Join(source, ".git"), 0o750); err != nil {
		t.Fatalf("makeArchivedRepo %s: %v", name, err)
	}
	origin = filepath.Join(discovery, filepath.FromSlash(name))
	if err := archive.Append(archive.Record{
		Name: name, Origin: origin, Remote: remote, Host: host, ArchivedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("archive.Append: %v", err)
	}
	return source, origin
}

func TestRunRepoUnarchive_RestoresCloneAndForgetsProvenance(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	makeArchivedRepo(t, archiveRoot, discovery, "foo", "git@github.com:owner/foo.git", "github.com")
	makeArchivedRepo(t, archiveRoot, discovery, "bar", "git@github.com:owner/bar.git", "github.com")

	var out bytes.Buffer
	if err := RunRepoUnarchive(archiveCfg(discovery, archiveRoot), RepoUnarchiveArgs{Repos: []string{"foo", "bar"}}, &out); err != nil {
		t.Fatalf("RunRepoUnarchive: %v", err)
	}

	for _, name := range []string{"foo", "bar"} {
		if _, err := os.Stat(filepath.Join(discovery, name)); err != nil {
			t.Errorf("%s: origin missing: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(archiveRoot, name)); !os.IsNotExist(err) {
			t.Errorf("%s: archived clone still present", name)
		}
	}

	f, err := archive.Load()
	if err != nil {
		t.Fatalf("archive.Load: %v", err)
	}
	if len(f.Entries) != 0 {
		t.Errorf("provenance should be empty after restore, got %d entries", len(f.Entries))
	}

	got := out.String()
	if !strings.Contains(got, "restored to") {
		t.Errorf("output missing restore line: %q", got)
	}
	for _, want := range []string{"gh repo unarchive owner/foo --yes", "gh repo unarchive owner/bar --yes"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q: %q", want, got)
		}
	}
}

func TestRunRepoUnarchive_DryRunMakesNoChanges(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	source, origin := makeArchivedRepo(t, archiveRoot, discovery, "foo", "git@github.com:owner/foo.git", "github.com")

	var out bytes.Buffer
	if err := RunRepoUnarchive(archiveCfg(discovery, archiveRoot), RepoUnarchiveArgs{Repos: []string{"foo"}, DryRun: true}, &out); err != nil {
		t.Fatalf("RunRepoUnarchive --dry-run: %v", err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Error("dry-run must not move the archived clone")
	}
	if _, err := os.Stat(origin); !os.IsNotExist(err) {
		t.Error("dry-run must not create the origin")
	}
	f, err := archive.Load()
	if err != nil {
		t.Fatalf("archive.Load: %v", err)
	}
	if len(f.Entries) != 1 {
		t.Errorf("dry-run must not touch provenance, got %d entries", len(f.Entries))
	}
	got := out.String()
	if !strings.Contains(got, "would restore to") || !strings.Contains(got, "gh repo unarchive owner/foo --yes") {
		t.Errorf("dry-run output missing plan: %q", got)
	}
}

func TestRunRepoUnarchive_RefusesMissingArchive(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	origin := filepath.Join(discovery, "foo")
	if err := archive.Append(archive.Record{Name: "foo", Origin: origin, Remote: "git@github.com:owner/foo.git", Host: "github.com"}); err != nil {
		t.Fatalf("archive.Append: %v", err)
	}

	err := RunRepoUnarchive(archiveCfg(discovery, archiveRoot), RepoUnarchiveArgs{Repos: []string{"foo"}}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected missing-archive error, got: %v", err)
	}
	if _, statErr := os.Stat(origin); !os.IsNotExist(statErr) {
		t.Error("origin must not be created when the archived clone is missing")
	}
}

func TestRunRepoUnarchive_RefusesExistingOrigin(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	source, origin := makeArchivedRepo(t, archiveRoot, discovery, "foo", "git@github.com:owner/foo.git", "github.com")
	if err := os.MkdirAll(origin, 0o750); err != nil {
		t.Fatal(err)
	}

	err := RunRepoUnarchive(archiveCfg(discovery, archiveRoot), RepoUnarchiveArgs{Repos: []string{"foo"}}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected origin-exists error, got: %v", err)
	}
	if _, statErr := os.Stat(source); statErr != nil {
		t.Error("archived clone must not move when the origin is occupied")
	}
}

func TestRunRepoUnarchive_NoRecordedRepos(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()

	err := RunRepoUnarchive(archiveCfg(discovery, archiveRoot), RepoUnarchiveArgs{Repos: []string{"foo"}}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "no archived repos") {
		t.Errorf("expected no-recorded-repos error, got: %v", err)
	}
}

func TestRunRepoUnarchive_UnknownRepo(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	makeArchivedRepo(t, archiveRoot, discovery, "foo", "git@github.com:owner/foo.git", "github.com")

	err := RunRepoUnarchive(archiveCfg(discovery, archiveRoot), RepoUnarchiveArgs{Repos: []string{"nope"}}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not archived") {
		t.Errorf("expected not-archived error, got: %v", err)
	}
}

func TestRunRepoUnarchive_AmbiguousName(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	makeArchivedRepo(t, archiveRoot, discovery, "org1/dup", "git@github.com:owner/dup.git", "github.com")
	makeArchivedRepo(t, archiveRoot, discovery, "org2/dup", "git@github.com:owner/dup2.git", "github.com")

	err := RunRepoUnarchive(archiveCfg(discovery, archiveRoot), RepoUnarchiveArgs{Repos: []string{"dup"}}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected ambiguous-name error, got: %v", err)
	}
}

func TestRunRepoUnarchive_ArchiveDirOverride(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	configured := t.TempDir()
	override := t.TempDir()
	// The clone lives under the override dir, not the configured one.
	makeArchivedRepo(t, override, discovery, "foo", "git@github.com:owner/foo.git", "github.com")

	if err := RunRepoUnarchive(archiveCfg(discovery, configured), RepoUnarchiveArgs{Repos: []string{"foo"}, ArchiveDir: override}, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunRepoUnarchive --archive-dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(discovery, "foo")); err != nil {
		t.Errorf("clone should have been restored from the override dir: %v", err)
	}
}

func TestRunRepoUnarchive_RemoteUnarchivesUpstream(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	makeArchivedRepo(t, archiveRoot, discovery, "foo", "git@github.com:owner/foo.git", "github.com")

	gh := &fakeGH{archived: map[string]bool{"owner/foo": true}}
	var out bytes.Buffer
	if err := RunRepoUnarchive(archiveCfg(discovery, archiveRoot), RepoUnarchiveArgs{Repos: []string{"foo"}, Remote: true, GH: gh}, &out); err != nil {
		t.Fatalf("RunRepoUnarchive --remote: %v", err)
	}
	if len(gh.unarchiveCalls) != 1 || gh.unarchiveCalls[0] != "owner/foo" {
		t.Errorf("Unarchive calls: got %v, want [owner/foo]", gh.unarchiveCalls)
	}
	got := out.String()
	if !strings.Contains(got, "Unarchived on GitHub") || !strings.Contains(got, "owner/foo") {
		t.Errorf("output missing upstream summary: %q", got)
	}
	if strings.Contains(got, "gh repo unarchive") {
		t.Errorf("--remote should not print the suggestion block: %q", got)
	}
}

func TestRunRepoUnarchive_RemoteAlreadyUnarchivedIsIdempotent(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	makeArchivedRepo(t, archiveRoot, discovery, "foo", "git@github.com:owner/foo.git", "github.com")

	gh := &fakeGH{} // IsArchived -> false: already unarchived upstream
	var out bytes.Buffer
	if err := RunRepoUnarchive(archiveCfg(discovery, archiveRoot), RepoUnarchiveArgs{Repos: []string{"foo"}, Remote: true, GH: gh}, &out); err != nil {
		t.Fatalf("RunRepoUnarchive --remote: %v", err)
	}
	if len(gh.unarchiveCalls) != 0 {
		t.Errorf("already-unarchived repo should not be unarchived again: %v", gh.unarchiveCalls)
	}
	if !strings.Contains(out.String(), "Already unarchived on GitHub") {
		t.Errorf("output should note upstream was already unarchived: %q", out.String())
	}
}

func TestRunRepoUnarchive_RemoteRefusesNonGitHub(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	source, _ := makeArchivedRepo(t, archiveRoot, discovery, "foo", "https://gitlab.com/owner/foo.git", "gitlab.com")

	err := RunRepoUnarchive(archiveCfg(discovery, archiveRoot), RepoUnarchiveArgs{Repos: []string{"foo"}, Remote: true, GH: &fakeGH{}}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "github.com") {
		t.Errorf("expected non-GitHub refusal, got: %v", err)
	}
	if _, statErr := os.Stat(source); statErr != nil {
		t.Error("archived clone must not move when --remote cannot unarchive it")
	}
}

func TestRunRepoUnarchive_RemoteRefusesWhenGHMissing(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	source, _ := makeArchivedRepo(t, archiveRoot, discovery, "foo", "git@github.com:owner/foo.git", "github.com")

	gh := &fakeGH{availableErr: errors.New("gh CLI not found on PATH")}
	err := RunRepoUnarchive(archiveCfg(discovery, archiveRoot), RepoUnarchiveArgs{Repos: []string{"foo"}, Remote: true, GH: gh}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected gh-missing refusal, got: %v", err)
	}
	if _, statErr := os.Stat(source); statErr != nil {
		t.Error("archived clone must not move when gh is unavailable")
	}
}

func TestRunRepoUnarchive_RemoteRefusesWhenUnauthenticated(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	makeArchivedRepo(t, archiveRoot, discovery, "foo", "git@github.com:owner/foo.git", "github.com")

	gh := &fakeGH{authErr: errors.New("not authenticated")}
	err := RunRepoUnarchive(archiveCfg(discovery, archiveRoot), RepoUnarchiveArgs{Repos: []string{"foo"}, Remote: true, GH: gh}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "authenticated") {
		t.Errorf("expected auth refusal, got: %v", err)
	}
	if len(gh.unarchiveCalls) != 0 {
		t.Error("no unarchival should be attempted when unauthenticated")
	}
}

func TestRunRepoUnarchive_RemoteRefusesWhenStateCheckFails(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	makeArchivedRepo(t, archiveRoot, discovery, "foo", "git@github.com:owner/foo.git", "github.com")

	gh := &fakeGH{stateErr: map[string]error{"owner/foo": errors.New("repo not found")}}
	err := RunRepoUnarchive(archiveCfg(discovery, archiveRoot), RepoUnarchiveArgs{Repos: []string{"foo"}, Remote: true, GH: gh}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "upstream archive state") {
		t.Errorf("expected state-check refusal, got: %v", err)
	}
}

func TestRunRepoUnarchive_RemoteRollsBackOnFailure(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	sourceFoo, originFoo := makeArchivedRepo(t, archiveRoot, discovery, "foo", "git@github.com:owner/foo.git", "github.com")
	sourceBar, originBar := makeArchivedRepo(t, archiveRoot, discovery, "bar", "git@github.com:owner/bar.git", "github.com")

	gh := &fakeGH{
		archived:     map[string]bool{"owner/foo": true, "owner/bar": true},
		unarchiveErr: map[string]error{"owner/bar": errors.New("boom")},
	}
	err := RunRepoUnarchive(archiveCfg(discovery, archiveRoot), RepoUnarchiveArgs{Repos: []string{"foo", "bar"}, Remote: true, GH: gh}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected failure when the second upstream unarchive fails")
	}
	if len(gh.archiveCalls) != 1 || gh.archiveCalls[0] != "owner/foo" {
		t.Errorf("the unarchived repo should be re-archived on rollback, got %v", gh.archiveCalls)
	}
	for _, p := range []string{originFoo, originBar} {
		if _, statErr := os.Stat(p); !os.IsNotExist(statErr) {
			t.Errorf("%s should be gone after rollback", p)
		}
	}
	for _, p := range []string{sourceFoo, sourceBar} {
		if _, statErr := os.Stat(p); statErr != nil {
			t.Errorf("%s should be back in the archive after rollback", p)
		}
	}
	f, err := archive.Load()
	if err != nil {
		t.Fatalf("archive.Load: %v", err)
	}
	if len(f.Entries) != 2 {
		t.Errorf("provenance should be restored on rollback, got %d entries", len(f.Entries))
	}
}

func TestRunRepoUnarchive_RemoteDryRunMakesNoGHCalls(t *testing.T) {
	isolateState(t)
	discovery := t.TempDir()
	archiveRoot := t.TempDir()
	makeArchivedRepo(t, archiveRoot, discovery, "foo", "git@github.com:owner/foo.git", "github.com")

	gh := &fakeGH{archived: map[string]bool{"owner/foo": true}}
	var out bytes.Buffer
	if err := RunRepoUnarchive(archiveCfg(discovery, archiveRoot), RepoUnarchiveArgs{Repos: []string{"foo"}, Remote: true, DryRun: true, GH: gh}, &out); err != nil {
		t.Fatalf("RunRepoUnarchive --remote --dry-run: %v", err)
	}
	if gh.authHost != "" || len(gh.unarchiveCalls) != 0 {
		t.Error("dry-run must not call gh")
	}
	got := out.String()
	if !strings.Contains(got, "would restore to") || !strings.Contains(got, "Upstream repos would be unarchived on GitHub") {
		t.Errorf("dry-run output should show the remote plan: %q", got)
	}
}
