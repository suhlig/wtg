package acceptance

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// gh is simulated, see land_with_pr_test.go.
var _ = Describe("Review feedback after the workspace is gone", Ordered, func() {
	var sb *Sandbox
	var spaceDir string

	BeforeAll(func() {
		sb = NewSandbox(
			"review-feedback",
			"Review feedback after the workspace is gone",
			"The pull requests are open and I cleaned up; now the reviewer asks for a change",
			wtgBinPath,
			fakeGHBinPath,
		)

		sb.RunHidden(wtgBinPath, "config", "init")

		sb.InitRepo("api", map[string]string{
			"go.mod": "module example.com/api\n\ngo 1.22\n",
			"api.go": "package api\n",
		})

		spaceDir = filepath.Join(sb.SpacesDir, "my-feature")
		sb.RunHidden(wtgBinPath, "new", "my-feature", "api")
		worktree := filepath.Join(spaceDir, "api")
		Expect(os.WriteFile(filepath.Join(worktree, "api.go"), []byte("package api\n\nfunc Hello() string { return \"world\" }\n"), 0644)).To(Succeed())
		sb.RunHidden("git", "-C", worktree, "add", "api.go")
		sb.RunHidden("git", "-C", worktree, "commit", "-m", "implement Hello")

		// The pull request is open, then the workspace is cleaned up.
		sb.Cd(spaceDir)
		sb.RunHidden(wtgBinPath, "land")
	})

	AfterAll(func() {
		sb.Cleanup()
	})

	It("deletes the workspace and its branch, which origin still has", func() {
		res := sb.RunWtg("delete", "-d", "--yes")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("deleted"))
	})

	It("re-creates the workspace on the branch from origin", func() {
		sb.Cd(sb.HomeDir)
		res := sb.RunWtg("new", "my-feature", "api")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("created space"))
	})

	It("has the commit that was reviewed", func() {
		sb.Cd(filepath.Join(spaceDir, "api"))
		res := sb.RunCmd("git", "log", "--oneline")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("implement Hello"))
	})

	It("pushes the follow-up commit to the same branch", func() {
		worktree := filepath.Join(spaceDir, "api")
		Expect(os.WriteFile(filepath.Join(worktree, "api.go"), []byte("package api\n\nfunc Hello() string { return \"world!\" }\n"), 0644)).To(Succeed())
		sb.RunHidden("git", "add", "api.go")
		sb.RunHidden("git", "commit", "-m", "address review feedback")

		sb.Cd(spaceDir)
		res := sb.RunWtg("push")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("pushed my-feature"))
	})

	It("finds the open pull request instead of creating another", func() {
		res := sb.RunWtg("land")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("https://github.com/example/api/pull/1"))
		Expect(res.OutputPlain).NotTo(ContainSubstring("pull/2"))
	})
})
