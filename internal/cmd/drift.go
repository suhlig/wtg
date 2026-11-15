package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/suhlig/wtg/internal/git"
	"github.com/suhlig/wtg/internal/gomod"
	"github.com/suhlig/wtg/internal/state"
	"github.com/suhlig/wtg/internal/ui"
)

// driftFinding is one line printed under a space's status table.
type driftFinding struct {
	text  string
	muted bool // a note, not a warning: the checkout is ahead of what is required
}

// goWorkMember is a member that contributes to go.work: a worktree, read-only or
// not, with a go.mod. Symlink members are excluded because they are not listed
// in go.work (ADR 0014), so nothing they pin can be overridden.
type goWorkMember struct {
	entry  state.RepoEntry
	module string // the module directive of its go.mod
	goMod  []byte
}

// driftFindings compares, for every pair of go.work members, the version a
// consumer requires with what the required member has checked out (ADR 0024 D6).
// A member in go.work overrides what other members' go.mod files pin, so a
// difference is what a build here sees versus what CI builds.
//
// The check is offline and non-fatal: it resolves versions against local refs
// only, says nothing about a version it cannot resolve, and never fails the
// command.
func driftFindings(runner git.Runner, entries []state.RepoEntry) []driftFinding {
	members := goWorkMembers(entries)
	if len(members) < 2 {
		return nil
	}
	byModule := make(map[string]*goWorkMember, len(members))
	for i := range members {
		if members[i].module != "" {
			byModule[members[i].module] = &members[i]
		}
	}

	var findings []driftFinding
	for i := range members {
		consumer := &members[i]
		for _, req := range gomod.Requirements(consumer.goMod) {
			required, ok := byModule[req.Path]
			if !ok || required.entry.Name == consumer.entry.Name {
				continue // no other member provides that module
			}
			if req.Replaced {
				continue // E8: the user decided; do not second-guess a replace
			}
			if f, ok := driftFor(runner, consumer, required, req.Version); ok {
				findings = append(findings, f)
			}
		}
	}
	return findings
}

// driftFor returns the finding for one (consumer, required member, version)
// triple. ok is false when there is nothing to say: the checkout is the required
// commit, or the version cannot be resolved locally (E9).
func driftFor(runner git.Runner, consumer, required *goWorkMember, version string) (driftFinding, bool) {
	rev, ok := gomod.RefForVersion(version)
	if !ok {
		return driftFinding{}, false
	}
	if !gomod.IsPseudoVersion(version) {
		// A version is a tag. Prefer refs/tags so a same-named branch or commit
		// cannot stand in for a tag that is not there.
		rev = "refs/tags/" + version
	}
	if _, err := runner.ResolveRef(required.entry.WorktreePath, rev); err != nil {
		return driftFinding{}, false // "cannot verify" must never become a false alarm
	}
	behind, ahead, err := runner.RevListCount(required.entry.WorktreePath, rev, "HEAD")
	if err != nil {
		return driftFinding{}, false
	}

	name := required.entry.Name
	by := consumer.entry.Name
	switch {
	case behind == 0 && ahead == 0:
		return driftFinding{}, false
	case behind > 0 && ahead > 0:
		return driftFinding{
			text: fmt.Sprintf("%s has diverged from %s, which %s requires", name, version, by),
		}, true
	case behind > 0:
		return driftFinding{
			text: fmt.Sprintf("%s is %s behind %s, which %s requires", name, commitWord(behind), version, by),
		}, true
	default:
		// Ahead is the intended state for a repo being edited, so this is a note,
		// not a warning — but it says the branch cannot build elsewhere yet.
		return driftFinding{
			text:  fmt.Sprintf("%s is %s ahead of %s, which %s requires (this will not build without go.work until a tag is cut)", name, commitWord(ahead), version, by),
			muted: true,
		}, true
	}
}

// goWorkMembers returns the space's members that have a go.mod of their own.
func goWorkMembers(entries []state.RepoEntry) []goWorkMember {
	var members []goWorkMember
	for _, e := range entries {
		if e.Symlink {
			continue // a symlink is the shared main clone and is not in go.work
		}
		data, err := os.ReadFile(filepath.Join(e.WorktreePath, "go.mod")) // #nosec G304 -- path derived from state
		if err != nil {
			continue
		}
		members = append(members, goWorkMember{entry: e, module: gomod.ModulePath(data), goMod: data})
	}
	return members
}

// renderDrift renders the drift lines for one space, if any.
func renderDrift(findings []driftFinding) []string {
	lines := make([]string, len(findings))
	for i, f := range findings {
		if f.muted {
			lines[i] = ui.Muted.Render(f.text)
			continue
		}
		lines[i] = ui.Warn.Render(ui.SymWarn+" ") + f.text
	}
	return lines
}

// commitWord renders a commit count with a singular and plural form.
func commitWord(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return fmt.Sprintf("%d commits", n)
}
