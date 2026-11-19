package acceptance

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Scenario 1: First steps after installation", Ordered, func() {
	var sb *Sandbox

	BeforeAll(func() {
		sb = NewSandbox(
			"first-steps",
			"First steps after installation",
			"I just installed wtg; what are the first steps?",
			wtgBinPath,
			fakeGHBinPath,
		)

		// Set up two repositories under discovery root ~/repos
		sb.InitRepo("api", map[string]string{
			"go.mod":  "module example.com/api\n\ngo 1.22\n",
			"main.go": "package main\n\nfunc main() {}\n",
		})
		sb.InitRepo("web", map[string]string{
			"go.mod": "module example.com/web\n\ngo 1.22\n",
			"app.go": "package main\n\nfunc Run() {}\n",
		})
	})

	AfterAll(func() {
		sb.Cleanup()
	})

	It("scaffolds a fresh configuration file", func() {
		res := sb.RunWtg("config", "init")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("Config written to ~/.config/wtg/config.toml"))
	})

	It("displays the configuration path", func() {
		res := sb.RunWtg("config", "path")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("~/.config/wtg/config.toml"))
	})

	It("shows the status of discovered repositories", func() {
		res := sb.RunWtg("repo", "status")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("api"))
		Expect(res.OutputPlain).To(ContainSubstring("web"))
		Expect(res.OutputPlain).To(ContainSubstring("main"))
		Expect(res.OutputPlain).To(ContainSubstring("clean"))
	})

	It("outputs shell completion script", func() {
		res := sb.RunWtg("completion", "zsh")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("#compdef wtg"))
	})
})
