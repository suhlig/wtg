package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/urfave/cli/v3"

	"github.com/suhlig/wtg/internal/archive"
	"github.com/suhlig/wtg/internal/config"
	"github.com/suhlig/wtg/internal/state"
)

// emitFlags prints all visible flags for cmd in --name:usage form, for use by
// ShellComplete handlers that otherwise only emit positional completions.
// The colon-separated format is parsed by the fish completion script to show
// descriptions alongside flag names.
func emitFlags(cmd *cli.Command) {
	for _, f := range cmd.VisibleFlags() {
		usage := ""
		if df, ok := f.(cli.DocGenerationFlag); ok {
			usage = df.GetUsage()
		}
		for _, name := range f.Names() {
			var flag string
			if len(name) == 1 {
				flag = "-" + name
			} else {
				flag = "--" + name
			}
			if usage != "" {
				fmt.Printf("%s:%s\n", flag, usage)
			} else {
				fmt.Println(flag)
			}
		}
	}
}

// completeSpaces outputs space names for shell completion.
func completeSpaces(_ context.Context, cmd *cli.Command) {
	spaces, err := state.List()
	if err != nil {
		return
	}
	sort.Slice(spaces, func(i, j int) bool { return spaces[i].Name < spaces[j].Name })
	for _, sp := range spaces {
		fmt.Println(sp.Name)
	}
	emitFlags(cmd)
}

// completionName returns the name to offer for completion: the repo's
// basename when it uniquely identifies the repo within names, otherwise the
// full slash-separated path. This mirrors how repo names are resolved.
func completionName(names []string, name string) string {
	base := pathBase(name)
	count := 0
	for _, n := range names {
		if pathBase(n) == base {
			count++
		}
	}
	if count == 1 {
		return base
	}
	return name
}

// pathBase returns the last element of a slash-separated path.
func pathBase(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

// completeRepos outputs discovered repo names for shell completion.
func completeRepos(_ context.Context, cmd *cli.Command) {
	cfg, err := config.Load(cmd.Root().String("config"))
	if err != nil {
		return
	}
	roots := cfg.DiscoveryRootDirs()
	if len(roots) == 0 {
		return
	}
	paths, err := discoverAllRepoPaths(roots, cfg.Discovery.MaxDepth)
	if err != nil {
		return
	}
	sort.Strings(paths)
	for _, p := range paths {
		_, _ = fmt.Fprintln(os.Stdout, repoName(roots, p))
	}
	emitFlags(cmd)
}

// completeArchivedRepos outputs archived repo names (from the provenance
// record) for `wtg repo unarchive` shell completion.
func completeArchivedRepos(_ context.Context, cmd *cli.Command) {
	f, err := archive.Load()
	if err != nil {
		return
	}
	names := make([]string, 0, len(f.Entries))
	for _, e := range f.Entries {
		names = append(names, e.Name)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Println(completionName(names, n))
	}
	emitFlags(cmd)
}

// completeSpaceAtFirst completes a space name only at position 0.
// Used for commands like exec where position 1+ is a pass-through command.
func completeSpaceAtFirst(ctx context.Context, cmd *cli.Command) {
	if cmd.NArg() <= 1 {
		completeSpaces(ctx, cmd)
	}
}

// completeReposAfterFirst completes discovered repos only at position 1+.
// Used for commands where position 0 is a new name (e.g. space create).
//
// NArg is 1 while completing position 0 (the partial token counts as an arg),
// so repos are only offered once a second token is being completed (NArg >= 2).
func completeReposAfterFirst(ctx context.Context, cmd *cli.Command) {
	if cmd.NArg() >= 2 {
		completeRepos(ctx, cmd)
	} else {
		emitFlags(cmd)
	}
}

// completeSpaceThenRepos completes a space name at position 0, then discovered
// repo names at position 1+. Used for commands like space add.
func completeSpaceThenRepos(ctx context.Context, cmd *cli.Command) {
	if cmd.NArg() <= 1 {
		completeSpaces(ctx, cmd)
	} else {
		completeRepos(ctx, cmd)
	}
}

// completeSpaceMembers completes a space name at position 0, then the names of
// repos already in that space at position 1+. Used for space remove.
func completeSpaceMembers(ctx context.Context, cmd *cli.Command) {
	if cmd.NArg() <= 1 {
		completeSpaces(ctx, cmd)
		return
	}
	sp, err := state.Load(cmd.Args().First())
	if err != nil {
		return
	}
	sort.Slice(sp.Repos, func(i, j int) bool { return sp.Repos[i].Name < sp.Repos[j].Name })
	names := make([]string, len(sp.Repos))
	for i, r := range sp.Repos {
		names[i] = r.Name
	}
	for _, n := range names {
		fmt.Println(completionName(names, n))
	}
	emitFlags(cmd)
}
