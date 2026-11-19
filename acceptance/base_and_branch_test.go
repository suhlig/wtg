package acceptance

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Branching from develop, or continuing an existing branch", Ordered, func() {
	var sb *Sandbox

	BeforeAll(func() {
		sb = NewSandbox(
			"base-and-branch",
			"Branching from develop, or continuing an existing branch",
			"The work starts from develop, or a colleague's branch already exists",
			wtgBinPath,
			fakeGHBinPath,
		)

		sb.RunHidden(wtgBinPath, "config", "init")

		apiClone, apiBare := sb.InitRepo("api", map[string]string{
			"go.mod": "module example.com/api\n\ngo 1.22\n",
			"api.go": "package api\n",
		})

		// Origin has a develop branch and a colleague's branch that this
		// clone has not seen yet.
		tempClone, err := os.MkdirTemp("", "temp-origin-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(os.RemoveAll, tempClone)
		sb.RunHidden("git", "clone", apiBare, tempClone)

		sb.RunHidden("git", "-C", tempClone, "checkout", "-b", "develop")
		Expect(os.WriteFile(filepath.Join(tempClone, "develop.txt"), []byte("not on main yet\n"), 0644)).To(Succeed())
		sb.RunHidden("git", "-C", tempClone, "add", "develop.txt")
		sb.RunHidden("git", "-C", tempClone, "commit", "-m", "add develop-only change")
		sb.RunHidden("git", "-C", tempClone, "push", "origin", "develop")

		sb.RunHidden("git", "-C", tempClone, "checkout", "-b", "colleague/fix-login", "main")
		Expect(os.WriteFile(filepath.Join(tempClone, "login.txt"), []byte("redirect fixed\n"), 0644)).To(Succeed())
		sb.RunHidden("git", "-C", tempClone, "add", "login.txt")
		sb.RunHidden("git", "-C", tempClone, "commit", "-m", "fix login redirect")
		sb.RunHidden("git", "-C", tempClone, "push", "origin", "colleague/fix-login")

		sb.RunHidden("git", "-C", apiClone, "fetch", "origin")
	})

	AfterAll(func() {
		sb.Cleanup()
	})

	It("creates the workspace from origin/develop", func() {
		res := sb.RunWtg("new", "my-feature", "api", "--base", "origin/develop")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("created space"))
	})

	It("starts the branch with develop's commits", func() {
		sb.Cd(filepath.Join(sb.SpacesDir, "my-feature", "api"))
		res := sb.RunCmd("git", "log", "--oneline")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("add develop-only change"))
	})

	It("lands into develop, not main", func() {
		worktree := filepath.Join(sb.SpacesDir, "my-feature", "api")
		Expect(os.WriteFile(filepath.Join(worktree, "feature.txt"), []byte("my feature\n"), 0644)).To(Succeed())
		sb.RunHidden("git", "add", "feature.txt")
		sb.RunHidden("git", "commit", "-m", "add feature")

		res := sb.RunWtg("land", "--dry-run", "--strategy", "merge")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("would push my-feature to origin/develop"))
	})

	It("continues a branch that already exists on origin", func() {
		sb.Cd(sb.HomeDir)
		res := sb.RunWtg("new", "review-login", "api", "--branch", "colleague/fix-login")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("on branch colleague/fix-login"))
	})

	It("checks out the colleague's commits as they are", func() {
		sb.Cd(filepath.Join(sb.SpacesDir, "review-login", "api"))
		res := sb.RunCmd("git", "log", "--oneline")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("fix login redirect"))
	})
})
