package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/suhlig/wtg/internal/config"
	"github.com/suhlig/wtg/internal/state"
	"github.com/suhlig/wtg/internal/trace"
	"github.com/suhlig/wtg/internal/ui"
)

// runSpaceScript invokes cfg.Always.Run (when set) after a space lifecycle
// event, passing space context via WTG_* environment variables. It is a
// best-effort side effect: the space operation has already succeeded, so a
// missing or failing script only prints a warning and never returns an error.
//
// sp describes the space state the script should see: the final state for
// create/add/remove, or the pre-deletion state for delete. eventRepos lists the
// repos this specific event affected (added or removed).
func runSpaceScript(cfg *config.Config, event string, sp *state.Space, eventRepos []string, out io.Writer, fsys ...trace.FS) {
	if cfg.Always.Run == "" {
		return
	}
	var f trace.FS = trace.NewOsFS(nil)
	if len(fsys) > 0 && fsys[0] != nil {
		f = fsys[0]
	}

	paths := make([]string, len(sp.Repos))
	for i, r := range sp.Repos {
		paths[i] = r.WorktreePath
	}

	cmd := trace.Command(cfg.Always.Run)
	cmd.Env = append(os.Environ(),
		"WTG_SPACE_NAME="+sp.Name,
		"WTG_SPACE_ROOT="+sp.Path,
		"WTG_SPACE_BRANCH="+sp.Branch,
		"WTG_SPACE_EVENT="+event,
		"WTG_REPOS="+strings.Join(repoNames(sp), "\n"),
		"WTG_REPO_PATHS="+strings.Join(paths, "\n"),
		"WTG_EVENT_REPOS="+strings.Join(eventRepos, "\n"),
	)
	cmd.Stdout = out
	cmd.Stderr = out
	line := fmt.Sprintf("%s %s", cfg.Always.Run, event)
	if err := trace.RunProcess(f, "", line, func() error {
		return cmd.Run()
	}); err != nil {
		_, _ = fmt.Fprintf(out, "  %s always.run script failed: %v\n", ui.SymWarn, err)
	}
}

// repoNames returns the short names of every repo in the space.
func repoNames(sp *state.Space) []string {
	names := make([]string, len(sp.Repos))
	for i, r := range sp.Repos {
		names[i] = r.Name
	}
	return names
}
