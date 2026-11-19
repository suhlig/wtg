package acceptance

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Scenario 13 backs the FAQ: the refusals and errors people ask about, with the
// real text wtg prints. Every step here exits non-zero on purpose or changes
// nothing, so it is safe to run in any order.
var _ = Describe("Scenario 13: When wtg refuses", Ordered, func() {
	var sb *Sandbox

	BeforeAll(func() {
		sb = NewSandbox(
			"refusals",
			"When wtg refuses",
			"delete refuses; a repo name is ambiguous; a branch has diverged",
			wtgBinPath,
			fakeGHBinPath,
		)

		sb.RunHidden(wtgBinPath, "config", "init")

		// Two repos named "api" under different owners make the name ambiguous.
		for _, owner := range []string{"example-org", "client"} {
			dir := filepath.Join(sb.DiscoveryDir, owner, "api")
			Expect(os.MkdirAll(dir, 0755)).To(Succeed())
			sb.RunHidden("git", "-C", dir, "init", "-b", "main")
			Expect(os.WriteFile(filepath.Join(dir, "README.md"), []byte("# api\n"), 0644)).To(Succeed())
			sb.RunHidden("git", "-C", dir, "add", "README.md")
			sb.RunHidden("git", "-C", dir, "commit", "-m", "initial commit")
		}

		sb.InitRepo("web", map[string]string{
			"go.mod": "module example.com/web\n\ngo 1.22\n",
			"web.go": "package web\n",
		})
		sb.RunHidden(wtgBinPath, "new", "my-feature", "web")

		// A local commit that exists on no remote.
		worktree := filepath.Join(sb.SpacesDir, "my-feature", "web")
		Expect(os.WriteFile(filepath.Join(worktree, "web.go"), []byte("package web\n\nfunc Hello() {}\n"), 0644)).To(Succeed())
		sb.RunHidden("git", "-C", worktree, "add", "web.go")
		sb.RunHidden("git", "-C", worktree, "commit", "-m", "add Hello")
	})

	AfterAll(func() {
		sb.Cleanup()
	})

	It("refuses an ambiguous repo name and lists the candidates", func() {
		sb.Cd(sb.HomeDir)
		res := sb.RunWtg("new", "other-feature", "api")
		Expect(res.ExitCode).NotTo(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("is ambiguous, matches:"))
	})

	It("refuses to delete without a terminal unless --yes is given", func() {
		sb.Cd(sb.HomeDir)
		res := sb.RunWtg("delete", "my-feature")
		Expect(res.ExitCode).NotTo(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("pass --yes to delete anyway"))
	})

	It("names the unpushed commits it would lose", func() {
		sb.Cd(sb.HomeDir)
		res := sb.RunWtg("delete", "my-feature", "--dry-run")
		Expect(res.OutputPlain).To(ContainSubstring("unpushed commit"))
	})
})
