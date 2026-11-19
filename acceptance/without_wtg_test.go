package acceptance

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// ADR 0022: everything wtg created is plain git, so losing wtg's state costs
// convenience, not work. Every shown command after the first is git.
var _ = Describe("Carrying on without wtg", Ordered, func() {
	var sb *Sandbox
	var apiClone string

	BeforeAll(func() {
		sb = NewSandbox(
			"without-wtg",
			"Carrying on without wtg",
			"I stopped using wtg, or lost its state; is my work still there?",
			wtgBinPath,
			fakeGHBinPath,
		)

		sb.RunHidden(wtgBinPath, "config", "init")

		apiClone, _ = sb.InitRepo("api", map[string]string{
			"go.mod": "module example.com/api\n\ngo 1.22\n",
			"api.go": "package api\n",
		})

		sb.RunHidden(wtgBinPath, "new", "my-feature", "api")
		worktree := filepath.Join(sb.SpacesDir, "my-feature", "api")
		Expect(os.WriteFile(filepath.Join(worktree, "api.go"), []byte("package api\n\nfunc Hello() string { return \"world\" }\n"), 0644)).To(Succeed())
		sb.RunHidden("git", "-C", worktree, "add", "api.go")
		sb.RunHidden("git", "-C", worktree, "commit", "-m", "implement Hello")

		// wtg's state is lost.
		Expect(os.RemoveAll(sb.DataDir)).To(Succeed())
	})

	AfterAll(func() {
		sb.Cleanup()
	})

	It("no longer knows the workspace", func() {
		sb.Cd(sb.HomeDir)
		res := sb.RunWtg("status", "my-feature")
		Expect(res.ExitCode).NotTo(Equal(0))
	})

	It("is still listed by git", func() {
		sb.Cd(apiClone)
		res := sb.RunCmd("git", "worktree", "list")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("~/spaces/my-feature/api"))
		Expect(res.OutputPlain).To(ContainSubstring("[my-feature]"))
	})

	It("keeps the commit on an ordinary branch", func() {
		res := sb.RunCmd("git", "log", "--oneline", "main..my-feature")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("implement Hello"))
	})

	It("pushes the branch with git", func() {
		sb.Cd(filepath.Join(sb.SpacesDir, "my-feature", "api"))
		res := sb.RunCmd("git", "push", "--set-upstream", "origin", "my-feature")
		Expect(res.ExitCode).To(Equal(0))
	})

	It("removes the worktree and the branch with git", func() {
		sb.Cd(apiClone)
		res := sb.RunCmd("git", "worktree", "remove", "../../spaces/my-feature/api")
		Expect(res.ExitCode).To(Equal(0))

		res = sb.RunCmd("git", "branch", "--delete", "my-feature")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("Deleted branch my-feature"))
	})
})
