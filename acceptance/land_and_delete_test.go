package acceptance

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Landing changes and cleaning up", Ordered, func() {
	var sb *Sandbox

	BeforeAll(func() {
		sb = NewSandbox(
			"land-and-delete",
			"Landing changes and cleaning up",
			"I am done; how do I get it into main, and which strategy?",
			wtgBinPath,
			fakeGHBinPath,
		)

		sb.RunHidden(wtgBinPath, "config", "init")

		sb.InitRepo("api", map[string]string{
			"go.mod": "module example.com/api\n\ngo 1.22\n",
			"api.go": "package api\n",
		})
		sb.InitRepo("web", map[string]string{
			"go.mod": "module example.com/web\n\ngo 1.22\n",
			"web.go": "package web\n",
		})

		sb.RunHidden(wtgBinPath, "new", "my-feature", "api", "web")

		// Make a commit in api worktree
		apiWorktree := filepath.Join(sb.SpacesDir, "my-feature", "api")
		Expect(os.WriteFile(filepath.Join(apiWorktree, "api.go"), []byte("package api\n\nfunc Hello() string { return \"world\" }\n"), 0644)).To(Succeed())
		sb.RunHidden("git", "-C", apiWorktree, "add", "api.go")
		sb.RunHidden("git", "-C", apiWorktree, "commit", "-m", "implement Hello")

		spaceDir := filepath.Join(sb.SpacesDir, "my-feature")
		sb.Cd(spaceDir)
	})

	AfterAll(func() {
		sb.Cleanup()
	})

	It("previews landing with dry run", func() {
		res := sb.RunWtg("land", "--dry-run", "--strategy", "merge")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("DRY RUN"))
		Expect(res.OutputPlain).To(ContainSubstring("would push my-feature to origin/main"))
	})

	It("lands changes using merge strategy", func() {
		res := sb.RunWtg("land", "--strategy", "merge")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("landed (merge)"))
	})

	It("deletes the workspace and clean merged branches", func() {
		res := sb.RunWtg("delete", "-d", "--yes")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("deleted"))
	})
})
