package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// runCompletion runs fn as the ShellComplete handler of a subcommand "c" under
// a root with --config, and returns what it wrote to stdout, one entry per
// line. args are the arguments after "c", so len(args) is what cmd.NArg()
// reports, as it does while a shell completes that position.
func runCompletion(t *testing.T, configPath string, fn cli.ShellCompleteFunc, args ...string) []string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	app := &cli.Command{
		Name:  "wtg",
		Flags: []cli.Flag{&cli.StringFlag{Name: "config"}},
		Commands: []*cli.Command{{
			Name:  "c",
			Flags: []cli.Flag{&cli.BoolFlag{Name: "flag", Usage: "a flag"}},
			Action: func(ctx context.Context, cmd *cli.Command) error {
				fn(ctx, cmd)
				return nil
			},
		}},
	}
	runErr := app.Run(context.Background(), append([]string{"wtg", "--config", configPath, "c"}, args...))
	_ = w.Close()
	os.Stdout = orig
	if runErr != nil {
		t.Fatalf("run completion: %v", runErr)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// completionConfig writes a config whose discovery root is root and returns
// its path.
func completionConfig(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	toml := fmt.Sprintf("[discovery]\nroot_dir = %q\nmax_depth = 2\n", root)
	if err := os.WriteFile(path, []byte(toml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// indexOf returns the position of want in lines, or -1.
func indexOf(lines []string, want string) int {
	for i, l := range lines {
		if l == want {
			return i
		}
	}
	return -1
}

// --- "." in completion (ADR 0025 follow-up) ---

func TestCompleteRepos_OffersDotFirst(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "api")
	makeRepo(t, root, "frontend")

	got := runCompletion(t, completionConfig(t, root), completeRepos)

	if len(got) < 3 || got[0] != "." || got[1] != "api" || got[2] != "frontend" {
		t.Errorf("want \".\" then the repos in order, got %v", got)
	}
}

func TestCompleteRepos_NoReposOffersNoDot(t *testing.T) {
	// "." would resolve to nothing: with no discovered repo it is not a candidate.
	got := runCompletion(t, completionConfig(t, t.TempDir()), completeRepos)

	if indexOf(got, ".") >= 0 {
		t.Errorf("\".\" must not be offered when no repo is discovered: %v", got)
	}
}

func TestCompleteRepos_FlagsStillEmitted(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root, "api")

	got := runCompletion(t, completionConfig(t, root), completeRepos)

	if indexOf(got, "--flag:a flag") < 0 {
		t.Errorf("flags should still be offered after the repos: %v", got)
	}
}

func TestCompleteSpaces_OffersDotFirst(t *testing.T) {
	isolateState(t)
	makeSpace(t, "alpha", "alpha", t.TempDir(), nil, "")
	makeSpace(t, "beta", "beta", t.TempDir(), nil, "")

	got := runCompletion(t, completionConfig(t, t.TempDir()), completeSpaces)

	if len(got) < 3 || got[0] != "." || got[1] != "alpha" || got[2] != "beta" {
		t.Errorf("want \".\" then the spaces in order, got %v", got)
	}
}

func TestCompleteSpaces_NoSpacesOffersNoDot(t *testing.T) {
	isolateState(t)

	got := runCompletion(t, completionConfig(t, t.TempDir()), completeSpaces)

	if indexOf(got, ".") >= 0 {
		t.Errorf("\".\" must not be offered when no workspace exists: %v", got)
	}
}

func TestCompleteSpaceThenRepos_OffersDotAtBothPositions(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	makeRepo(t, root, "api")
	makeSpace(t, "alpha", "alpha", t.TempDir(), nil, "")
	cfg := completionConfig(t, root)

	// Position 0 completes a workspace, or "." as a repo (ADR 0025 D7).
	first := runCompletion(t, cfg, completeSpaceThenRepos, "")
	if len(first) < 2 || first[0] != "." || first[1] != "alpha" {
		t.Errorf("position 0: want \".\" then spaces, got %v", first)
	}
	// Position 1+ completes repos.
	second := runCompletion(t, cfg, completeSpaceThenRepos, "alpha", "")
	if len(second) < 2 || second[0] != "." || second[1] != "api" {
		t.Errorf("position 1: want \".\" then repos, got %v", second)
	}
}

func TestCompleteSpaceMembers_OffersDotBeforeMembers(t *testing.T) {
	isolateState(t)
	root := t.TempDir()
	makeSpace(t, "alpha", "alpha", t.TempDir(), []string{"api", "frontend"}, root)

	got := runCompletion(t, completionConfig(t, root), completeSpaceMembers, "alpha", "")

	if len(got) < 3 || got[0] != "." || got[1] != "api" || got[2] != "frontend" {
		t.Errorf("want \".\" then the members, got %v", got)
	}
}

func TestCompleteSpaceMembers_EmptySpaceOffersNoDot(t *testing.T) {
	isolateState(t)
	makeSpace(t, "alpha", "alpha", t.TempDir(), nil, "")

	got := runCompletion(t, completionConfig(t, t.TempDir()), completeSpaceMembers, "alpha", "")

	if indexOf(got, ".") >= 0 {
		t.Errorf("\".\" must not be offered when the workspace has no members: %v", got)
	}
}

func TestCompleteReposAfterFirst_NoDotForTheWorkspaceName(t *testing.T) {
	// `wtg new <workspace>` names a workspace first and rejects "." there
	// (ADR 0025 D7), so position 0 offers no repos and no ".".
	root := t.TempDir()
	makeRepo(t, root, "api")
	cfg := completionConfig(t, root)

	first := runCompletion(t, cfg, completeReposAfterFirst, "")
	if indexOf(first, ".") >= 0 || indexOf(first, "api") >= 0 {
		t.Errorf("position 0 names a new workspace and completes nothing: %v", first)
	}
	second := runCompletion(t, cfg, completeReposAfterFirst, "feat", "")
	if len(second) < 2 || second[0] != "." || second[1] != "api" {
		t.Errorf("position 1: want \".\" then repos, got %v", second)
	}
}

func TestCompleteArchivedRepos_NeverOffersDot(t *testing.T) {
	// `repo unarchive .` is rejected: an archived clone is not in discovery
	// (ADR 0025 D1).
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	got := runCompletion(t, completionConfig(t, t.TempDir()), completeArchivedRepos)

	if indexOf(got, ".") >= 0 {
		t.Errorf("\".\" must not be offered for unarchive: %v", got)
	}
}
