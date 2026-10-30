package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/suhlig/wtg/internal/trace"
)

// PR holds the number and URL of a GitHub pull request.
type PR struct {
	Number int
	URL    string
}

// Runner runs the read-only and mutating `gh` commands that back
// `wtg repo archive --remote` and `wtg land`. It is an interface so command
// tests can inject a fake; the real implementation is SystemRunner.
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

	// FindPR returns the first open PR with the given head branch, or found=false.
	FindPR(ctx context.Context, host, ownerRepo, head string) (pr PR, found bool, err error)
	// CreatePR opens a new PR. draft creates a draft PR.
	CreatePR(ctx context.Context, host, ownerRepo, head, base string, draft bool) (PR, error)
	// PRBody returns the current body of a PR.
	PRBody(ctx context.Context, host, ownerRepo string, number int) (string, error)
	// SetPRBody replaces the body of a PR.
	SetPRBody(ctx context.Context, host, ownerRepo string, number int, body string) error
}

// SystemRunner runs the gh CLI found on PATH. rec may be nil (no recording).
type SystemRunner struct {
	rec *trace.Recorder
}

// NewSystemRunner returns a SystemRunner. rec may be nil.
func NewSystemRunner(rec *trace.Recorder) *SystemRunner { return &SystemRunner{rec: rec} }

// Available reports whether gh is installed.
func (s *SystemRunner) Available() error {
	if _, err := exec.LookPath("gh"); err != nil {
		return errors.New("gh CLI not found on PATH; install it from https://cli.github.com to use --remote")
	}
	return nil
}

// AuthStatus verifies gh is authenticated for host.
func (s *SystemRunner) AuthStatus(ctx context.Context, host string) error {
	out, err := s.ghOutput(ctx, host, "auth", "status", "--hostname", host)
	if err != nil {
		return fmt.Errorf("gh is not authenticated for %s; run `gh auth login --hostname %s` first:\n%s",
			host, host, strings.TrimSpace(string(out)))
	}
	return nil
}

// IsArchived reports whether ownerRepo is already archived on host.
func (s *SystemRunner) IsArchived(ctx context.Context, host, ownerRepo string) (bool, error) {
	out, err := s.ghOutput(ctx, host, "repo", "view", ownerRepo, "--json", "isArchived")
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
func (s *SystemRunner) Archive(ctx context.Context, host, ownerRepo string) error {
	if out, err := s.ghOutput(ctx, host, "repo", "archive", ownerRepo, "--yes"); err != nil {
		return fmt.Errorf("gh repo archive %s failed:\n%s", ownerRepo, strings.TrimSpace(string(out)))
	}
	return nil
}

// Unarchive reverses Archive.
func (s *SystemRunner) Unarchive(ctx context.Context, host, ownerRepo string) error {
	if out, err := s.ghOutput(ctx, host, "repo", "unarchive", ownerRepo, "--yes"); err != nil {
		return fmt.Errorf("gh repo unarchive %s failed:\n%s", ownerRepo, strings.TrimSpace(string(out)))
	}
	return nil
}

// ghOutput runs gh with args and returns its combined output. GH_HOST pins the
// command to host so a GitHub Enterprise default in the user's gh config cannot
// redirect an archival. Mutating calls are recorded when a Recorder is present.
// In dry-run mode, mutating calls are recorded as skipped and return success.
func (s *SystemRunner) ghOutput(ctx context.Context, host string, args ...string) ([]byte, error) {
	isMutating := !trace.ClassifyGH(args)
	if s.rec != nil && s.rec.DryRun() && isMutating {
		s.rec.Record("", trace.FormatGHLine(args), -1, true)
		return []byte("{}"), nil // return valid JSON so callers that unmarshal don't panic
	}
	cmd := exec.CommandContext(ctx, "gh", args...) // #nosec G204 -- gh with a parsed host and owner/repo slug
	cmd.Env = append(os.Environ(), "GH_HOST="+host)
	out, err := cmd.CombinedOutput()
	if s.rec != nil && isMutating {
		code := 0
		if err != nil {
			code = 1
		}
		s.rec.Record("", trace.FormatGHLine(args), code, false)
	}
	return out, err
}

// FindPR returns the first open PR with head as its head branch, or found=false.
func (s *SystemRunner) FindPR(ctx context.Context, host, ownerRepo, head string) (PR, bool, error) {
	out, err := s.ghOutput(ctx, host, "pr", "list", "--repo", ownerRepo, "--head", head, "--state", "open", "--json", "number,url")
	if err != nil {
		return PR{}, false, fmt.Errorf("gh pr list %s failed:\n%s", ownerRepo, strings.TrimSpace(string(out)))
	}
	var prs []struct {
		Number int    `json:"number"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal(out, &prs); err != nil {
		return PR{}, false, fmt.Errorf("parse gh pr list output: %w", err)
	}
	if len(prs) == 0 {
		return PR{}, false, nil
	}
	return PR{Number: prs[0].Number, URL: prs[0].URL}, true, nil
}

// CreatePR creates a new pull request and returns it.
func (s *SystemRunner) CreatePR(ctx context.Context, host, ownerRepo, head, base string, draft bool) (PR, error) {
	args := []string{"pr", "create", "--repo", ownerRepo, "--head", head, "--base", base, "--fill"}
	if draft {
		args = append(args, "--draft")
	}
	out, err := s.ghOutput(ctx, host, args...)
	if err != nil {
		return PR{}, fmt.Errorf("gh pr create %s failed:\n%s", ownerRepo, strings.TrimSpace(string(out)))
	}
	// gh pr create returns the PR URL on stdout
	prURL := strings.TrimSpace(string(out))
	// Parse the PR number from the URL (/pull/<number>)
	var number int
	parts := strings.Split(prURL, "/")
	if len(parts) > 0 {
		fmt.Sscanf(parts[len(parts)-1], "%d", &number)
	}
	return PR{Number: number, URL: prURL}, nil
}

// PRBody returns the current body text of a PR.
func (s *SystemRunner) PRBody(ctx context.Context, host, ownerRepo string, number int) (string, error) {
	out, err := s.ghOutput(ctx, host, "pr", "view", fmt.Sprintf("%d", number), "--repo", ownerRepo, "--json", "body")
	if err != nil {
		return "", fmt.Errorf("gh pr view %d failed:\n%s", number, strings.TrimSpace(string(out)))
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		return "", fmt.Errorf("parse gh pr view output: %w", err)
	}
	return body.Body, nil
}

// SetPRBody replaces the body of a PR.
func (s *SystemRunner) SetPRBody(ctx context.Context, host, ownerRepo string, number int, body string) error {
	out, err := s.ghOutput(ctx, host, "pr", "edit", fmt.Sprintf("%d", number), "--repo", ownerRepo, "--body", body)
	if err != nil {
		return fmt.Errorf("gh pr edit %d failed:\n%s", number, strings.TrimSpace(string(out)))
	}
	return nil
}
