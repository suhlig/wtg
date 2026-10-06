package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Runner runs the read-only and mutating `gh` commands that back
// `wtg repo archive --remote`. It is an interface so command tests can inject a
// fake; the real implementation is SystemRunner.
type Runner interface {
	// Available reports whether the gh CLI is installed and on PATH.
	Available() error
	// AuthStatus verifies gh is authenticated for host.
	AuthStatus(ctx context.Context, host string) error
	// IsArchived reports whether ownerRepo is already archived on host. It is
	// the read-only check that makes the archive idempotent.
	IsArchived(ctx context.Context, host, ownerRepo string) (bool, error)
	// Archive archives ownerRepo on host.
	Archive(ctx context.Context, host, ownerRepo string) error
	// Unarchive reverses Archive. It is used only to roll back a failed saga.
	Unarchive(ctx context.Context, host, ownerRepo string) error
}

// SystemRunner runs the gh CLI found on PATH.
type SystemRunner struct{}

// Available reports whether gh is installed.
func (SystemRunner) Available() error {
	if _, err := exec.LookPath("gh"); err != nil {
		return errors.New("gh CLI not found on PATH; install it from https://cli.github.com to use --remote")
	}
	return nil
}

// AuthStatus verifies gh is authenticated for host.
func (SystemRunner) AuthStatus(ctx context.Context, host string) error {
	out, err := ghOutput(ctx, host, "auth", "status", "--hostname", host)
	if err != nil {
		return fmt.Errorf("gh is not authenticated for %s; run `gh auth login --hostname %s` first:\n%s",
			host, host, strings.TrimSpace(string(out)))
	}
	return nil
}

// IsArchived reports whether ownerRepo is already archived on host.
func (SystemRunner) IsArchived(ctx context.Context, host, ownerRepo string) (bool, error) {
	out, err := ghOutput(ctx, host, "repo", "view", ownerRepo, "--json", "isArchived")
	if err != nil {
		return false, fmt.Errorf("gh repo view %s failed:\n%s", ownerRepo, strings.TrimSpace(string(out)))
	}
	var view struct {
		IsArchived bool `json:"isArchived"`
	}
	if err := json.Unmarshal(out, &view); err != nil {
		return false, fmt.Errorf("parse gh repo view output for %s: %w", ownerRepo, err)
	}
	return view.IsArchived, nil
}

// Archive archives ownerRepo on host.
func (SystemRunner) Archive(ctx context.Context, host, ownerRepo string) error {
	if out, err := ghOutput(ctx, host, "repo", "archive", ownerRepo, "--yes"); err != nil {
		return fmt.Errorf("gh repo archive %s failed:\n%s", ownerRepo, strings.TrimSpace(string(out)))
	}
	return nil
}

// Unarchive reverses Archive.
func (SystemRunner) Unarchive(ctx context.Context, host, ownerRepo string) error {
	if out, err := ghOutput(ctx, host, "repo", "unarchive", ownerRepo, "--yes"); err != nil {
		return fmt.Errorf("gh repo unarchive %s failed:\n%s", ownerRepo, strings.TrimSpace(string(out)))
	}
	return nil
}

// ghOutput runs gh with args and returns its combined output. GH_HOST pins the
// command to host so a GitHub Enterprise default in the user's gh config cannot
// redirect an archival.
func ghOutput(ctx context.Context, host string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...) // #nosec G204 -- gh with a parsed host and owner/repo slug
	cmd.Env = append(os.Environ(), "GH_HOST="+host)
	return cmd.CombinedOutput()
}
