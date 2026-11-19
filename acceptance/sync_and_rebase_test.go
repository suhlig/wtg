package acceptance

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Keeping a feature branch up to date", Ordered, func() {
	var sb *Sandbox
	var apiBare string

	BeforeAll(func() {
		sb = NewSandbox(
			"sync-and-rebase",
			"Keeping a feature branch up to date",
			"I am mid-feature; how do I pick up changes in main?",
			wtgBinPath,
			fakeGHBinPath,
		)

		sb.RunHidden(wtgBinPath, "config", "init")

		var apiClone string
		apiClone, apiBare = sb.InitRepo("api", map[string]string{
			"go.mod": "module example.com/api\n\ngo 1.22\n",
			"api.go": "package api\n\nfunc Version() string { return \"1.0.0\" }\n",
		})
		_, _ = sb.InitRepo("web", map[string]string{
			"go.mod": "module example.com/web\n\ngo 1.22\n",
			"web.go": "package web\n",
		})

		_ = apiClone

		// Create workspace
		sb.RunHidden(wtgBinPath, "new", "my-feature", "api", "web")

		// Make a feature commit in api
		apiWorktree := filepath.Join(sb.SpacesDir, "my-feature", "api")
		Expect(os.WriteFile(filepath.Join(apiWorktree, "feature.txt"), []byte("my feature code\n"), 0644)).To(Succeed())
		sb.RunHidden("git", "-C", apiWorktree, "add", "feature.txt")
		sb.RunHidden("git", "-C", apiWorktree, "commit", "-m", "add feature")

		// Push initial branch so origin has the pre-rebase commit
		spaceDir := filepath.Join(sb.SpacesDir, "my-feature")
		sb.Cd(spaceDir)
		sb.RunHidden(wtgBinPath, "push")

		// Simulate main moving ahead in upstream origin
		tempClone, err := os.MkdirTemp("", "temp-main-*")
		Expect(err).NotTo(HaveOccurred())
		sb.RunHidden("git", "clone", apiBare, tempClone)
		Expect(os.WriteFile(filepath.Join(tempClone, "upstream.txt"), []byte("new feature on main\n"), 0644)).To(Succeed())
		sb.RunHidden("git", "-C", tempClone, "add", "upstream.txt")
		sb.RunHidden("git", "-C", tempClone, "commit", "-m", "upstream commit on main")
		sb.RunHidden("git", "-C", tempClone, "push", "origin", "main")
		_ = os.RemoveAll(tempClone)
	})

	AfterAll(func() {
		sb.Cleanup()
	})

	It("syncs main repo clones with remote updates", func() {
		sb.Cd(sb.HomeDir)
		res := sb.RunWtg("repo", "sync")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("api"))
		Expect(res.OutputPlain).To(ContainSubstring("fast-forwarded"))
	})

	It("rebases workspace feature branches onto updated main", func() {
		spaceDir := filepath.Join(sb.SpacesDir, "my-feature")
		sb.Cd(spaceDir)

		res := sb.RunWtg("rebase")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("api"))
		Expect(res.OutputPlain).To(ContainSubstring("rebased"))
	})

	It("pushes the rebased feature branch", func() {
		spaceDir := filepath.Join(sb.SpacesDir, "my-feature")
		sb.Cd(spaceDir)

		res := sb.RunWtg("push")
		// Since rebase rewritten history and remote has old commit, standard push shows diverged hint
		Expect(res.ExitCode).NotTo(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("force-with-lease"))
	})
})
