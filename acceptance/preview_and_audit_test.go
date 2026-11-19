package acceptance

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Previewing and auditing operations", Ordered, func() {
	var sb *Sandbox

	BeforeAll(func() {
		sb = NewSandbox(
			"preview-and-audit",
			"Previewing and auditing operations",
			"Preview and audit what wtg does with --dry-run and --summary",
			wtgBinPath,
			fakeGHBinPath,
		)

		sb.RunHidden(wtgBinPath, "config", "init")

		sb.InitRepo("api", map[string]string{
			"go.mod": "module example.com/api\n\ngo 1.22\n",
		})
		sb.InitRepo("web", map[string]string{
			"go.mod": "module example.com/web\n\ngo 1.22\n",
		})
	})

	AfterAll(func() {
		sb.Cleanup()
	})

	It("previews workspace creation with dry-run", func() {
		res := sb.RunWtg("--dry-run", "new", "my-feature", "api", "web")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("DRY RUN"))
		Expect(res.OutputPlain).To(ContainSubstring("Would run"))
	})

	It("audits commands executed using summary", func() {
		res := sb.RunWtg("--summary", "new", "my-feature", "api", "web")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("Commands run"))
	})
})
