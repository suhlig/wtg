package acceptance

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Scenario 14: Archiving and restoring retired repositories", Ordered, func() {
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
})
