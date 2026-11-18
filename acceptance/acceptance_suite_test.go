package acceptance

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
)

var (
	wtgBinPath    string
	fakeGHBinPath string
)

func TestAcceptance(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "wtg Acceptance Suite")
}

var _ = BeforeSuite(func() {
	var err error
	wtgBinPath, err = gexec.Build("github.com/suhlig/wtg")
	Expect(err).NotTo(HaveOccurred())

	fakeGHBinPath, err = gexec.Build("github.com/suhlig/wtg/acceptance/cmd/fakegh")
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	// Save recorded transcripts if output dir is requested or default to site/data/transcripts
	transcriptsDir := os.Getenv("WTG_TRANSCRIPTS_DIR")
	if transcriptsDir == "" {
		transcriptsDir = "../site/data/transcripts"
	}
	_ = GlobalTranscripts.SaveAll(transcriptsDir)

	gexec.CleanupBuildArtifacts()
})
