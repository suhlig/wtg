package acceptance

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Scenario 7: Consuming a library as a read-only member", Ordered, func() {
	var sb *Sandbox

	BeforeAll(func() {
		sb = NewSandbox(
			"read-only-member",
			"Consuming a library as a read-only member",
			"I only consume a library; keep it pinned and separate from the feature branch",
			wtgBinPath,
			fakeGHBinPath,
		)

		sb.RunHidden(wtgBinPath, "config", "init")

		sb.InitRepo("api", map[string]string{
			"go.mod": "module example.com/api\n\ngo 1.22\n",
		})
		sb.InitRepo("config-lib", map[string]string{
			"go.mod": "module example.com/config-lib\n\ngo 1.22\n",
		})

		sb.RunHidden(wtgBinPath, "new", "my-feature", "api")
	})

	AfterAll(func() {
		sb.Cleanup()
	})

	It("adds a library repo as read-only", func() {
		res := sb.RunWtg("add", "my-feature", "--read-only", "config-lib")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("config-lib"))
		Expect(res.OutputPlain).To(ContainSubstring("read-only"))
	})

	It("shows read-only member status", func() {
		res := sb.RunWtg("status", "my-feature")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("config-lib"))
		Expect(res.OutputPlain).To(ContainSubstring("read-only"))
	})
})
