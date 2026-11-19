package acceptance

import (
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// No step prints a duration: `go test` reports one for a package that has
// tests, so the packages here have none.
var _ = Describe("Running a command in every repo", Ordered, func() {
	var sb *Sandbox

	BeforeAll(func() {
		sb = NewSandbox(
			"exec",
			"Running a command in every repo",
			"Run the tests, or any other command, in every repo of the workspace",
			wtgBinPath,
			fakeGHBinPath,
		)

		sb.RunHidden(wtgBinPath, "config", "init")

		sb.InitRepo("api", map[string]string{
			"go.mod": "module example.com/api\n\ngo 1.22\n",
			"api.go": "package api\n",
		})
		sb.InitRepo("web", map[string]string{
			"go.mod": "module example.com/web\n\ngo 1.22\n",
			"web.go": "package web\n",
		})

		sb.RunHidden(wtgBinPath, "new", "my-feature", "api", "web")
		sb.Cd(filepath.Join(sb.SpacesDir, "my-feature"))
	})

	AfterAll(func() {
		sb.Cleanup()
	})

	It("runs the command in each repo, one after the other", func() {
		res := sb.RunWtg("exec", "--", "go", "test", "./...")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("example.com/api"))
		Expect(res.OutputPlain).To(ContainSubstring("example.com/web"))
	})

	It("runs it in all repos at once with --parallel", func() {
		res := sb.RunWtg("exec", "--parallel", "--", "go", "test", "./...")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("example.com/api"))
		Expect(res.OutputPlain).To(ContainSubstring("example.com/web"))
	})

	It("carries on after a failure and reports it at the end", func() {
		// web.go exists only in web.
		res := sb.RunWtg("exec", "--", "test", "-f", "web.go")
		Expect(res.ExitCode).NotTo(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("command failed in: api"))
	})

	It("stops a command that runs too long with --timeout", func() {
		res := sb.RunWtg("exec", "--timeout", "1s", "--", "sleep", "30")
		Expect(res.ExitCode).NotTo(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("timed out after 1s"))
		Expect(res.OutputPlain).To(ContainSubstring("command failed in: api, web"))
	})
})
