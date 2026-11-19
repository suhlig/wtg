package acceptance

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Scenario 12: Every workspace gets my shared repos, files and hook", Ordered, func() {
	var sb *Sandbox

	BeforeAll(func() {
		sb = NewSandbox(
			"always",
			"Every workspace gets my shared repos, files and hook",
			"Every workspace gets my shared tooling, my agent file, my .env and a hook",
			wtgBinPath,
			fakeGHBinPath,
		)

		sb.RunHidden(wtgBinPath, "config", "init")

		// api has a local .env that is not on any branch; docs has none.
		apiClone, _ := sb.InitRepo("api", map[string]string{
			"go.mod": "module example.com/api\n\ngo 1.22\n",
			"api.go": "package api\n",
			// .env is local-only, as in a real repo.
			".gitignore": ".env\n",
		})
		sb.InitRepo("docs", map[string]string{"README.md": "# docs\n"})
		sb.InitRepo("shared-tooling", map[string]string{"Makefile": "all:\n"})
		Expect(os.WriteFile(filepath.Join(apiClone, ".env"), []byte("TOKEN=example\n"), 0600)).To(Succeed())

		// The agent file and the hook live in the user's config directory.
		Expect(os.WriteFile(filepath.Join(sb.ConfigDir, "CLAUDE.md"), []byte("# Project notes\n"), 0644)).To(Succeed())
		hook := filepath.Join(sb.ConfigDir, "on-event")
		Expect(os.WriteFile(hook, []byte("#!/bin/sh\necho \"hook: $WTG_SPACE_EVENT $WTG_SPACE_NAME\" >&2\n"), 0755)).To(Succeed())

		sb.WriteConfig(`[always]
repos = ["shared-tooling"]
files = ["~/.config/wtg/CLAUDE.md"]
secrets = [".env"]
run = "~/.config/wtg/on-event"
`)
	})

	AfterAll(func() {
		sb.Cleanup()
	})

	It("creates a workspace that includes the always entries", func() {
		res := sb.RunWtg("new", "my-feature", "api", "docs")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("hook: create my-feature"))
	})

	It("shows what landed in the workspace", func() {
		sb.Cd(filepath.Join(sb.SpacesDir, "my-feature"))
		res := sb.RunCmd("ls", "-A")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("CLAUDE.md"))
		Expect(res.OutputPlain).To(ContainSubstring("shared-tooling"))
	})

	It("copies the secret only where the source repo has it", func() {
		res := sb.RunCmd("ls", "-A", "api")
		Expect(res.OutputPlain).To(ContainSubstring(".env"))
		res = sb.RunCmd("ls", "-A", "docs")
		Expect(res.OutputPlain).NotTo(ContainSubstring(".env"))
	})

	It("runs the hook again on delete", func() {
		sb.Cd(sb.HomeDir)
		res := sb.RunWtg("delete", "my-feature", "--yes")
		Expect(res.ExitCode).To(Equal(0))
		Expect(res.OutputPlain).To(ContainSubstring("hook: delete my-feature"))
	})
})
