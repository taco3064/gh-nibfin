package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLiveOfficialRunnerPackage(t *testing.T) {
	if os.Getenv("NIBFIN_TEST_LIVE_RUNNER_SOURCE") != "1" {
		t.Skip("explicit official package download only; no Runner registration")
	}
	api := GitHub{Process: NativeProcess{}}
	repo, user, err := repository(api, "taco3064/gh-nibfin", true)
	if err != nil {
		t.Fatal(err)
	}
	root := privateRunnerTestRoot(t)
	path := filepath.Join(root, "official-runner")
	record, err := createRunner(api, OfficialRunnerDownload, repo, user,
		"nibfin-package-verification", path)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != "created" || record.RunnerID != 0 {
		t.Fatal("package validation must not register a Runner")
	}
	t.Logf("official package verified: %s sha256=%s", record.Application.Filename,
		record.Application.Hash)
}
