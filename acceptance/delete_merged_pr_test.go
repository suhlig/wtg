package acceptance

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// gh is simulated, see land_with_pr_test.go. The reviewer's squash merge is set
// up with git and the fake gh's `pr merge`, which only marks the pull request
// MERGED.
var _ = Describe("Cleaning up after a squash merge", Ordered, func() {
	var sb *Sandbox
	var apiClone string

	BeforeAll(func() {
		sb = NewSandbox(
			"delete-merged-pr",
			"Cleaning up after a squash merge",
			"My pull requests were squash-merged on GitHub; why can wtg delete the branches?",
			wtgBinPath,
			fakeGHBinPath,
		)

		sb.RunHidden(wtgBinPath, "config", "init")

		apiClone, _ = sb.InitRepo("api", map[string]string{
			"go.mod": "module example.com/api\n\ngo 1.22\n",
			"api.go": "package api\n",
		})
		sb.InitRepo("web", map[string]string{
			"go.mod": "module example.com/web\n\ngo 1.22\n",
			"web.go": "package web\n",
		})

		sb.RunHidden(wtgBinPath, "new", "my-feature", "api", "web")
		for _, repo := range []string{"api", "web"} {
			file := repo + ".go"
			worktree := filepath.Join(sb.SpacesDir, "my-feature", repo)
			Expect(os.WriteFile(filepath.Join(worktree, file), []byte("package "+repo+"\n\nfunc Hello() string { return \"world\" }\n"), 0644)).To(Succeed())
			sb.RunHidden("git", "-C", worktree, "add", file)
			sb.RunHidden("git", "-C", worktree, "commit", "-m", "implement Hello")
		}

		sb.Cd(filepath.Join(sb.SpacesDir, "my-feature"))
		sb.RunHidden(wtgBinPath, "land")

		// The reviewer squash-merges both pull requests, and GitHub deletes
		// the head branches. The squash commit is a new commit: the feature
		// branch is not an ancestor of main.
		for _, repo := range []string{"api", "web"} {
			bare := filepath.Join(sb.BareOriginDir, repo+".git")
			tempClone, err := os.MkdirTemp("", "temp-merge-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(os.RemoveAll, tempClone)
			sb.RunHidden("git", "clone", bare, tempClone)
			sb.RunHidden("git", "-C", tempClone, "merge", "--squash", "origin/my-feature")
			sb.RunHidden("git", "-C", tempClone, "commit", "-m", "Implement Hello (#1)")
			sb.RunHidden("git", "-C", tempClone, "push", "origin", "main")
			sb.RunHidden("git", "-C", tempClone, "push", "origin", "--delete", "my-feature")
			sb.RunHidden("gh", "pr", "merge", "1", "--repo", "example/"+repo, "--squash")
		}

		// The next fetch drops the remote-tracking branches that are gone.
		for _, repo := range []string{"api", "web"} {
			sb.RunHidden("git", "-C", filepath.Join(sb.DiscoveryDir, repo), "fetch", "--prune")
		}
	})

	AfterAll(func() {
		sb.Cleanup()
	})

	It("deletes the workspace and names the merged pull request in each repo", func() {
		sb.Cd(sb.HomeDir)
		res := sb.RunWtg("delete", "my-feature", "-d")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("PR #1 merged"))
		Expect(res.OutputPlain).NotTo(ContainSubstring("unpushed"))
	})

	It("leaves no feature branch behind", func() {
		sb.Cd(apiClone)
		res := sb.RunCmd("git", "branch", "--list")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).NotTo(ContainSubstring("my-feature"))
	})
})
