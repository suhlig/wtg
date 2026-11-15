// Package gomod reads module metadata from a consumer's build list.
//
// It exists so that wtg never reimplements Go's version selection: what a
// consumer requires is asked of the toolchain (ADR 0024 D8), and a required
// version is turned back into a git ref without needing the module cache.
package gomod

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"

	"github.com/suhlig/wtg/internal/trace"
)

// Errors reported by Runner.RequiredVersion.
var (
	// ErrNotDependency means the consumer's build list does not contain the
	// module at all, so the consumer has no opinion about its version.
	ErrNotDependency = errors.New("not a known dependency of the consumer")
	// ErrGoMissing means the go binary is not on PATH.
	ErrGoMissing = errors.New("the go command is not available on PATH")
)

// Runner resolves the version a consumer's build list selects for a module.
// It is an interface, like git.Runner, so tests can substitute a fake.
type Runner interface {
	// RequiredVersion returns the version the module at dir requires for
	// modulePath, and whether the consumer replaces that module. dir must be
	// the directory holding the consumer's go.mod.
	RequiredVersion(dir, modulePath string) (version string, replaced bool, err error)
}

// SystemRunner asks the Go toolchain: GOWORK=off go list -m, with
// -mod=readonly so neither go.mod nor go.sum is touched.
type SystemRunner struct{}

// New returns a SystemRunner.
func New() *SystemRunner { return &SystemRunner{} }

// listTemplate prints the selected version, plus " replaced" for a replaced
// module. See ADR 0024 D8.
const listTemplate = "{{.Version}}{{if .Replace}} replaced{{end}}"

// RequiredVersion runs "GOWORK=off GOFLAGS=-mod=readonly go list -m" in dir.
//
// GOWORK=off is essential: in workspace mode a used module is reported as the
// main module, with no version. -mod=readonly is essential too: it works in
// repos with a vendor/ directory for modules that are in the build list but
// not vendored, and it never rewrites go.mod or go.sum.
func (r *SystemRunner) RequiredVersion(dir, modulePath string) (string, bool, error) {
	cmd := trace.Command("go", "list", "-m", "-f", listTemplate, modulePath)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GOWORK=off",
		"GOFLAGS=-mod=readonly",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", false, ErrGoMissing
		}
		msg := strings.TrimSpace(stderr.String())
		if strings.Contains(msg, "not a known dependency") {
			return "", false, ErrNotDependency
		}
		return "", false, fmt.Errorf("go list -m %s in %s: %w: %s", modulePath, dir, err, msg)
	}

	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) == 0 {
		return "", false, fmt.Errorf("go list -m %s in %s: empty output", modulePath, dir)
	}
	replaced := len(fields) > 1 && fields[1] == "replaced"
	return fields[0], replaced, nil
}

// ModulePath returns the module path declared by the go.mod content, including
// any /vN suffix, or "" if the content is not a go.mod.
func ModulePath(goMod []byte) string {
	return modfile.ModulePath(goMod)
}

// Requirement is one module a consumer's go.mod requires.
type Requirement struct {
	Path     string
	Version  string
	Replaced bool // the consumer replaces this module; the user decided
}

// Requirements returns the requires of a consumer's go.mod content, each
// annotated with whether the consumer replaces that module.
//
// This is the offline counterpart of Runner.RequiredVersion: it reads what the
// file literally says, without minimum version selection, and is used by the
// drift check in `wtg status`, which must not need the network or a populated
// module cache (ADR 0024 D6, E9).
func Requirements(goMod []byte) []Requirement {
	f, err := modfile.Parse("go.mod", goMod, nil)
	if err != nil {
		return nil
	}
	reqs := make([]Requirement, 0, len(f.Require))
	for _, r := range f.Require {
		reqs = append(reqs, Requirement{
			Path:     r.Mod.Path,
			Version:  r.Mod.Version,
			Replaced: replaces(f, r.Mod.Path),
		})
	}
	return reqs
}

// replaces reports whether f replaces modulePath with something else.
func replaces(f *modfile.File, modulePath string) bool {
	for _, r := range f.Replace {
		if r.Old.Path == modulePath {
			return true
		}
	}
	return false
}

// RefForVersion turns a version into the git revision that names it: a
// pseudo-version becomes its commit suffix, anything else is a tag of that
// name. ok is false when the version names neither.
func RefForVersion(version string) (ref string, ok bool) {
	if version == "" {
		return "", false
	}
	if module.IsPseudoVersion(version) {
		rev, err := module.PseudoVersionRev(version)
		if err != nil {
			return "", false
		}
		return rev, true
	}
	return version, true
}

// IsPseudoVersion reports whether version is a pseudo-version.
func IsPseudoVersion(version string) bool { return module.IsPseudoVersion(version) }
