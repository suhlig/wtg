package acceptance

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/gomega"
)

const (
	PinnedAuthorName     = "Jane Doe"
	PinnedAuthorEmail    = "jane@example.com"
	PinnedAuthorDate     = "2026-11-18T10:00:00Z"
	PinnedCommitterName  = "Jane Doe"
	PinnedCommitterEmail = "jane@example.com"
	PinnedCommitterDate  = "2026-11-18T10:00:00Z"
)

type StepResult struct {
	Command     string
	Cwd         string
	ExitCode    int
	StdoutRaw   string
	StderrRaw   string
	CombinedRaw string
	OutputPlain string
	OutputHTML  string
}

type Sandbox struct {
	HomeDir         string
	ConfigDir       string
	DataDir         string
	GitConfigGlobal string
	DiscoveryDir    string
	SpacesDir       string
	ArchiveDir      string
	BinDir          string
	BareOriginDir   string

	Cwd           string
	WtgBinPath    string
	FakeGHBinPath string

	ScenarioID    string
	ScenarioTitle string
	ScenarioDesc  string
}

// sshShimScript stands in for ssh. git runs `ssh [options] git@github.com
// "git-upload-pack 'example/api.git'"`; the last argument is the remote command.
// It is served from the bare repository of the same name in $WTG_ACCEPTANCE_REMOTES.
const sshShimScript = `#!/bin/sh
for cmd; do :; done
prog=${cmd%% *}
path=${cmd#* }
path=${path#\'}
path=${path%\'}
exec git "${prog#git-}" "$WTG_ACCEPTANCE_REMOTES/$(basename "$path")"
`

func NewSandbox(scenarioID, title, desc, wtgBin, fakeGHBin string) *Sandbox {
	// The sandbox path must have a fixed length: wtg pads table cells to the
	// widest value, and that width is measured before the path is replaced by
	// "~" in the transcript. os.MkdirTemp's numeric suffix varies in length,
	// which made the padding, and so the transcript, differ between runs.
	suffix := make([]byte, 4)
	_, err := rand.Read(suffix)
	Expect(err).NotTo(HaveOccurred())
	tmpDir := filepath.Join(os.TempDir(), "wtg-acceptance-"+hex.EncodeToString(suffix))
	Expect(os.MkdirAll(tmpDir, 0755)).To(Succeed())
	tmpDir, err = filepath.EvalSymlinks(tmpDir)
	Expect(err).NotTo(HaveOccurred())

	homeDir := filepath.Join(tmpDir, "home")
	configDir := filepath.Join(homeDir, ".config", "wtg")
	dataDir := filepath.Join(homeDir, ".local", "share", "wtg")
	discoveryDir := filepath.Join(homeDir, "repos")
	spacesDir := filepath.Join(homeDir, "spaces")
	archiveDir := filepath.Join(homeDir, "repos-archived")
	binDir := filepath.Join(homeDir, "bin")
	bareOriginDir := filepath.Join(tmpDir, "remotes")
	gitConfigGlobal := filepath.Join(homeDir, ".gitconfig")

	Expect(os.MkdirAll(configDir, 0755)).To(Succeed())
	Expect(os.MkdirAll(dataDir, 0755)).To(Succeed())
	Expect(os.MkdirAll(discoveryDir, 0755)).To(Succeed())
	Expect(os.MkdirAll(spacesDir, 0755)).To(Succeed())
	Expect(os.MkdirAll(archiveDir, 0755)).To(Succeed())
	Expect(os.MkdirAll(binDir, 0755)).To(Succeed())
	Expect(os.MkdirAll(bareOriginDir, 0755)).To(Succeed())

	// Write global gitconfig
	gitConfigContent := fmt.Sprintf(`[user]
	name = %s
	email = %s
[init]
	defaultBranch = main
`, PinnedAuthorName, PinnedAuthorEmail)
	Expect(os.WriteFile(gitConfigGlobal, []byte(gitConfigContent), 0644)).To(Succeed())

	// ADR 0027 D7: the fixtures' origin is a real github.com URL, so wtg sees
	// what a user sees (no insteadOf rewrite, no seam in product code). git
	// reaches it over ssh, and wtg runs plain `ssh` from PATH, so a shim named ssh
	// in the sandbox bin directory (first on PATH) answers that call from the local bare
	// repositories.
	sshShim := filepath.Join(binDir, "ssh")
	Expect(os.WriteFile(sshShim, []byte(sshShimScript), 0755)).To(Succeed())

	// Copy fake gh if provided
	if fakeGHBin != "" {
		ghDest := filepath.Join(binDir, "gh")
		data, err := os.ReadFile(fakeGHBin)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(ghDest, data, 0755)).To(Succeed())
	}

	sb := &Sandbox{
		HomeDir:         homeDir,
		ConfigDir:       configDir,
		DataDir:         dataDir,
		GitConfigGlobal: gitConfigGlobal,
		DiscoveryDir:    discoveryDir,
		SpacesDir:       spacesDir,
		ArchiveDir:      archiveDir,
		BinDir:          binDir,
		BareOriginDir:   bareOriginDir,
		Cwd:             homeDir,
		WtgBinPath:      wtgBin,
		FakeGHBinPath:   fakeGHBin,
		ScenarioID:      scenarioID,
		ScenarioTitle:   title,
		ScenarioDesc:    desc,
	}

	GlobalTranscripts.AddScenario(scenarioID, title, desc)
	return sb
}

func (s *Sandbox) Cleanup() {
	parent := filepath.Dir(s.HomeDir)
	_ = os.RemoveAll(parent)
}

func (s *Sandbox) Env() []string {
	pathEnv := s.BinDir + string(os.PathListSeparator) + os.Getenv("PATH")
	return []string{
		"HOME=" + s.HomeDir,
		"XDG_CONFIG_HOME=" + filepath.Join(s.HomeDir, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(s.HomeDir, ".local", "share"),
		"GIT_CONFIG_GLOBAL=" + s.GitConfigGlobal,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=" + PinnedAuthorName,
		"GIT_AUTHOR_EMAIL=" + PinnedAuthorEmail,
		"GIT_AUTHOR_DATE=" + PinnedAuthorDate,
		"GIT_COMMITTER_NAME=" + PinnedCommitterName,
		"GIT_COMMITTER_EMAIL=" + PinnedCommitterEmail,
		"GIT_COMMITTER_DATE=" + PinnedCommitterDate,
		"WTG_ACCEPTANCE_REMOTES=" + s.BareOriginDir,
		"PATH=" + pathEnv,
		"FAKE_GH_STATE_DIR=" + s.HomeDir,
	}
}

func (s *Sandbox) Cd(dir string) {
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(s.Cwd, dir)
	}
	s.Cwd = dir
}

func (s *Sandbox) WriteConfig(content string) {
	configPath := filepath.Join(s.ConfigDir, "config.toml")
	Expect(os.WriteFile(configPath, []byte(content), 0644)).To(Succeed())
}

func (s *Sandbox) InitRepo(shortName string, files map[string]string) (clonePath, barePath string) {
	// shortName may be nested (github.com/example/api) to mirror a real
	// discovery layout; the origin is always example/<base>.git.
	base := filepath.Base(shortName)
	barePath = filepath.Join(s.BareOriginDir, base+".git")
	Expect(os.MkdirAll(barePath, 0755)).To(Succeed())

	cmd := exec.Command("git", "init", "--bare", "-b", "main", barePath)
	cmd.Env = s.Env()
	out, err := cmd.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), string(out))

	clonePath = filepath.Join(s.DiscoveryDir, shortName)
	Expect(os.MkdirAll(clonePath, 0755)).To(Succeed())

	s.runGit(clonePath, "init", "-b", "main")
	remoteURL := fmt.Sprintf("git@github.com:example/%s.git", base)
	s.runGit(clonePath, "remote", "add", "origin", remoteURL)

	for relPath, content := range files {
		fullPath := filepath.Join(clonePath, relPath)
		Expect(os.MkdirAll(filepath.Dir(fullPath), 0755)).To(Succeed())
		Expect(os.WriteFile(fullPath, []byte(content), 0644)).To(Succeed())
		s.runGit(clonePath, "add", relPath)
	}

	if len(files) == 0 {
		fullPath := filepath.Join(clonePath, "README.md")
		Expect(os.WriteFile(fullPath, []byte("# "+shortName+"\n"), 0644)).To(Succeed())
		s.runGit(clonePath, "add", "README.md")
	}

	s.runGit(clonePath, "commit", "-m", "initial commit")
	s.runGit(clonePath, "push", "-u", "origin", "main")
	s.runGit(clonePath, "remote", "set-head", "origin", "--auto")

	return clonePath, barePath
}

func (s *Sandbox) runGit(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = s.Env()
	out, err := cmd.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("git %v failed in %s: %s", args, dir, string(out)))
	return string(out)
}

func (s *Sandbox) RunHidden(name string, args ...string) StepResult {
	return s.execute(false, name, args...)
}

func (s *Sandbox) RunCmd(name string, args ...string) StepResult {
	return s.execute(true, name, args...)
}

func (s *Sandbox) RunWtg(args ...string) StepResult {
	fullArgs := append([]string{}, args...)
	return s.execute(true, s.WtgBinPath, fullArgs...)
}

func (s *Sandbox) execute(record bool, name string, args ...string) StepResult {
	// exec.Command resolves a bare name against this process's PATH, not
	// cmd.Env, so a real gh (or ssh) would win over the sandbox's. Resolve
	// bare names in the sandbox bin directory first (ADR 0027 E7).
	exe := name
	if !strings.ContainsRune(name, filepath.Separator) {
		if _, err := os.Stat(filepath.Join(s.BinDir, name)); err == nil {
			exe = filepath.Join(s.BinDir, name)
		}
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = s.Cwd
	cmd.Env = s.Env()

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	rawOutput := buf.String()
	normRaw := s.Normalize(rawOutput)
	plain := StripANSI(normRaw)
	htmlOutput := AnsiToHTML(normRaw)

	cmdDisplay := name
	if name == s.WtgBinPath {
		cmdDisplay = "wtg"
	} else if filepath.IsAbs(cmdDisplay) {
		cmdDisplay = filepath.Base(cmdDisplay)
	}
	if len(args) > 0 {
		cmdDisplay += " " + strings.Join(args, " ")
	}

	res := StepResult{
		Command:     cmdDisplay,
		Cwd:         s.Normalize(s.Cwd),
		ExitCode:    exitCode,
		CombinedRaw: normRaw,
		OutputPlain: plain,
		OutputHTML:  htmlOutput,
	}

	if record {
		step := StepTranscript{
			ID:          "step",
			Command:     cmdDisplay,
			Cwd:         res.Cwd,
			ExitCode:    exitCode,
			OutputRaw:   normRaw,
			OutputPlain: plain,
			OutputHTML:  htmlOutput,
		}
		GlobalTranscripts.RecordStep(s.ScenarioID, s.ScenarioTitle, s.ScenarioDesc, step)
	}

	return res
}

func (s *Sandbox) Normalize(str string) string {
	if str == "" {
		return ""
	}
	home := s.HomeDir
	homePrivate := "/private" + home
	str = strings.ReplaceAll(str, homePrivate, "~")
	str = strings.ReplaceAll(str, home, "~")

	parent := filepath.Dir(s.HomeDir)
	parentPrivate := "/private" + parent
	str = strings.ReplaceAll(str, parentPrivate, "~")
	str = strings.ReplaceAll(str, parent, "~")

	return str
}
