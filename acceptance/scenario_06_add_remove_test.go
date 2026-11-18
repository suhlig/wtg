package acceptance

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Scenario 6: Adding and removing repositories", Ordered, func() {
	var sb *Sandbox

	BeforeAll(func() {
		sb = NewSandbox(
			"add-and-remove",
			"Adding and removing repositories",
			"The feature needs a third repo, or one is no longer needed",
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
		sb.InitRepo("payments", map[string]string{
			"go.mod": "module example.com/payments\n\ngo 1.22\n",
		})

		sb.RunHidden(wtgBinPath, "new", "my-feature", "api", "web")
	})

	AfterAll(func() {
		sb.Cleanup()
	})

	It("adds a third repository to the workspace", func() {
		res := sb.RunWtg("add", "my-feature", "payments")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("payments"))
		Expect(res.OutputPlain).To(ContainSubstring("added"))
	})

	It("removes a repository that is no longer needed", func() {
		res := sb.RunWtg("remove", "my-feature", "web")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("web"))
		Expect(res.OutputPlain).To(ContainSubstring("removed"))
	})
})
