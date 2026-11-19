package acceptance

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Archiving and restoring retired repositories", Ordered, func() {
	var sb *Sandbox

	BeforeAll(func() {
		sb = NewSandbox(
			"archive-and-unarchive",
			"Archiving and restoring retired repositories",
			"Retire old clones out of discovery area into archive storage without losing uncommitted work or provenance",
			wtgBinPath,
			fakeGHBinPath,
		)

		sb.RunHidden(wtgBinPath, "config", "init")

		sb.InitRepo("old-service", map[string]string{
			"go.mod": "module example.com/old-service\n\ngo 1.22\n",
		})
		spike, _ := sb.InitRepo("spike-repo", map[string]string{"README.md": "# spike\n"})
		Expect(os.WriteFile(filepath.Join(spike, "notes.txt"), []byte("work in progress\n"), 0644)).To(Succeed())
	})

	AfterAll(func() {
		sb.Cleanup()
	})

	It("archives a repository clone", func() {
		res := sb.RunWtg("repo", "archive", "old-service")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("old-service"))
		Expect(res.OutputPlain).To(ContainSubstring("moved to ~/repos-archived/old-service"))
	})

	It("restores an archived repository clone", func() {
		res := sb.RunWtg("repo", "unarchive", "old-service")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("old-service"))
		Expect(res.OutputPlain).To(ContainSubstring("restored to ~/repos/old-service"))
	})

	It("refuses a clone with pending work", func() {
		res := sb.RunWtg("repo", "archive", "spike-repo")
		Expect(res.ExitCode).NotTo(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("Refusing to archive with pending work"))
	})

	It("previews the archive of a clone with pending work, changing nothing", func() {
		res := sb.RunWtg("repo", "archive", "spike-repo", "--force", "--dry-run")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("DRY RUN"))
	})

	It("archives locally and on GitHub with --remote", func() {
		res := sb.RunWtg("repo", "archive", "old-service", "--remote")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("Archived on GitHub"))
	})

	It("restores locally and on GitHub with --remote", func() {
		res := sb.RunWtg("repo", "unarchive", "old-service", "--remote")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("Unarchived on GitHub"))
	})
})
