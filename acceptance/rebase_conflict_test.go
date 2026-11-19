package acceptance

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Resolving a rebase conflict", Ordered, func() {
	var sb *Sandbox
	var apiWorktree string

	BeforeAll(func() {
		sb = NewSandbox(
			"rebase-conflict",
			"Resolving a rebase conflict",
			"A rebase conflicts; what happened to my work, and how do I finish by hand?",
			wtgBinPath,
			fakeGHBinPath,
		)

		sb.RunHidden(wtgBinPath, "config", "init")

		_, apiBare := sb.InitRepo("api", map[string]string{
			"go.mod": "module example.com/api\n\ngo 1.22\n",
			"api.go": "package api\n\nfunc Version() string { return \"1.0.0\" }\n",
		})
		sb.InitRepo("web", map[string]string{
			"go.mod": "module example.com/web\n\ngo 1.22\n",
			"web.go": "package web\n",
		})

		sb.RunHidden(wtgBinPath, "new", "my-feature", "api", "web")

		// The feature changes the line that main is about to change too.
		apiWorktree = filepath.Join(sb.SpacesDir, "my-feature", "api")
		Expect(os.WriteFile(filepath.Join(apiWorktree, "api.go"), []byte("package api\n\nfunc Version() string { return \"1.1.0\" }\n"), 0644)).To(Succeed())
		sb.RunHidden("git", "-C", apiWorktree, "add", "api.go")
		sb.RunHidden("git", "-C", apiWorktree, "commit", "-m", "bump version to 1.1.0")

		tempClone, err := os.MkdirTemp("", "temp-main-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(os.RemoveAll, tempClone)
		sb.RunHidden("git", "clone", apiBare, tempClone)
		Expect(os.WriteFile(filepath.Join(tempClone, "api.go"), []byte("package api\n\nfunc Version() string { return \"2.0.0\" }\n"), 0644)).To(Succeed())
		sb.RunHidden("git", "-C", tempClone, "add", "api.go")
		sb.RunHidden("git", "-C", tempClone, "commit", "-m", "bump version to 2.0.0")
		sb.RunHidden("git", "-C", tempClone, "push", "origin", "main")
	})

	AfterAll(func() {
		sb.Cleanup()
	})

	It("aborts the conflicting repo and names the command to retry by hand", func() {
		sb.Cd(filepath.Join(sb.SpacesDir, "my-feature"))
		res := sb.RunWtg("rebase")
		Expect(res.ExitCode).NotTo(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("conflict — rebase aborted"))
		Expect(res.OutputPlain).To(ContainSubstring("to resolve by hand: git -C ~/spaces/my-feature/api rebase --autostash origin/main"))
		// The other repo is not affected.
		Expect(res.OutputPlain).To(ContainSubstring("web"))
	})

	It("stops at the conflict when git is asked to do the same rebase", func() {
		sb.Cd(apiWorktree)
		res := sb.RunCmd("git", "rebase", "--autostash", "origin/main")
		Expect(res.ExitCode).NotTo(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("CONFLICT"))
	})

	It("finishes once the file is resolved", func() {
		// Editing the file is the part only the user can do.
		Expect(os.WriteFile(filepath.Join(apiWorktree, "api.go"), []byte("package api\n\nfunc Version() string { return \"2.1.0\" }\n"), 0644)).To(Succeed())
		sb.RunHidden("git", "add", "api.go")

		res := sb.RunCmd("git", "rebase", "--continue")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("Successfully rebased"))
	})

	It("finds nothing left to rebase", func() {
		sb.Cd(filepath.Join(sb.SpacesDir, "my-feature"))
		res := sb.RunWtg("rebase")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("up to date"))
	})
})
