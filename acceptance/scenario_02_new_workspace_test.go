package acceptance

import (
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Scenario 2: Creating a new multi-repo workspace", Ordered, func() {
	var sb *Sandbox

	BeforeAll(func() {
		sb = NewSandbox(
			"new-workspace",
			"Creating a new multi-repo workspace",
			"I want a new workspace with two projects; where is my stuff?",
			wtgBinPath,
			fakeGHBinPath,
		)

		// Create default config
		sb.RunHidden(wtgBinPath, "config", "init")

		// Create two repos
		sb.InitRepo("api", map[string]string{
			"go.mod": "module example.com/api\n\ngo 1.22\n",
			"api.go": "package api\n",
		})
		sb.InitRepo("web", map[string]string{
			"go.mod": "module example.com/web\n\ngo 1.22\n",
			"web.go": "package web\n",
		})
	})

	AfterAll(func() {
		sb.Cleanup()
	})

	It("creates a workspace with two repos", func() {
		res := sb.RunWtg("new", "my-feature", "api", "web")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("api"))
		Expect(res.OutputPlain).To(ContainSubstring("web"))
		Expect(res.OutputPlain).To(ContainSubstring("created"))
	})

	It("reports the path to the new workspace", func() {
		res := sb.RunWtg("path", "my-feature")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("~/spaces/my-feature"))
	})

	It("shows the workspace status", func() {
		res := sb.RunWtg("status", "my-feature")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("my-feature"))
		Expect(res.OutputPlain).To(ContainSubstring("api"))
		Expect(res.OutputPlain).To(ContainSubstring("web"))
		Expect(res.OutputPlain).To(ContainSubstring("[my-feature]"))
		Expect(res.OutputPlain).To(ContainSubstring("clean"))
	})

	It("generates go.work connecting the Go modules", func() {
		spaceDir := filepath.Join(sb.SpacesDir, "my-feature")
		sb.Cd(spaceDir)

		res := sb.RunCmd("cat", "go.work")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("go 1.22"))
		Expect(res.OutputPlain).To(ContainSubstring("./api"))
		Expect(res.OutputPlain).To(ContainSubstring("./web"))
	})
})
