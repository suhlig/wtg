package acceptance

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Scenario 4, second half: the default "pr" strategy. gh is simulated by
// acceptance/cmd/fakegh (ADR 0027 D7); git and the github.com remote are real
// as far as wtg can tell (see the ssh shim in harness.go).
var _ = Describe("Scenario 4b: Landing changes through pull requests", Ordered, func() {
	var sb *Sandbox

	BeforeAll(func() {
		sb = NewSandbox(
			"land-with-pr",
			"Landing changes through pull requests",
			"I am done and the repos are shared; how do I open pull requests?",
			wtgBinPath,
			fakeGHBinPath,
		)

		sb.RunHidden(wtgBinPath, "config", "init")

		// Real clones usually sit under host/owner/repo, which is one level
		// deeper than the default discovery depth.
		sb.WriteConfig("[discovery]\nmax_depth = 3\n")

		sb.InitRepo("github.com/example/api", map[string]string{
			"go.mod": "module example.com/api\n\ngo 1.22\n",
			"api.go": "package api\n",
		})
		sb.InitRepo("github.com/example/web", map[string]string{
			"go.mod": "module example.com/web\n\ngo 1.22\n",
			"web.go": "package web\n",
		})

		sb.RunHidden(wtgBinPath, "new", "my-feature", "api", "web")

		for _, repo := range []string{"api", "web"} {
			file := repo + ".go"
			worktree := filepath.Join(sb.SpacesDir, "my-feature", "github.com", "example", repo)
			Expect(os.WriteFile(filepath.Join(worktree, file), []byte("package "+repo+"\n\nfunc Hello() string { return \"world\" }\n"), 0644)).To(Succeed())
			sb.RunHidden("git", "-C", worktree, "add", file)
			sb.RunHidden("git", "-C", worktree, "commit", "-m", "implement Hello")
		}

		sb.Cd(filepath.Join(sb.SpacesDir, "my-feature"))
	})

	AfterAll(func() {
		sb.Cleanup()
	})

	It("opens a pull request in every repo", func() {
		res := sb.RunWtg("land")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("https://github.com/example/api/pull/1"))
		Expect(res.OutputPlain).To(ContainSubstring("https://github.com/example/web/pull/1"))
	})

	It("finds the existing pull requests when run again", func() {
		res := sb.RunWtg("land")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("https://github.com/example/api/pull/1"))
		Expect(res.OutputPlain).NotTo(ContainSubstring("pull/2"))
	})

	It("links the sibling pull requests in each description", func() {
		res := sb.RunCmd("gh", "pr", "view", "1", "--repo", "example/api", "--json", "body", "--jq", ".body")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("wtg:siblings:start"))
		Expect(res.OutputPlain).To(ContainSubstring("example/web#1"))
	})
})
