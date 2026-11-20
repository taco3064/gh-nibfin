package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type cacheVerificationProcess struct {
	fail   bool
	change func()
}

func (p cacheVerificationProcess) Run(string, []string, []byte) ([]byte, error) {
	if p.change != nil {
		p.change()
	}
	if p.fail {
		return nil, errors.New("simulated verifier refusal")
	}
	return json.Marshal(map[string]any{"status": "verified", "source": Pins().Core})
}

func TestVerificationAndHousekeepingRemainIndependent(t *testing.T) {
	archive := testArchive(t, []string{"bin/nibfin-core.mjs", ".node-version"})
	for _, fail := range []bool{false, true} {
		target := filepath.Join(canonicalCacheFixtureParent(t), "candidate")
		reports := []cacheCleanupReport{}
		process := cacheVerificationProcess{fail: fail, change: func() {
			if err := os.WriteFile(filepath.Join(target, "foreign"), nil, 0600); err != nil {
				t.Fatal(err)
			}
		}}
		directory, err := verifyCache(process, archive, target, "trusted",
			"approved-pin", Pins().Core, func(report cacheCleanupReport) {
				reports = append(reports, report)
			}, unusedCache)
		if fail {
			if err == nil || err.Error() != "CORE_INSTALLATION_READBACK_FAILED" {
				t.Fatal("verification refusal replaced by housekeeping", err)
			}
			if len(reports) != 1 || reports[0].Entries[0].Reason != "foreign_entry" {
				t.Fatal(reports)
			}
		} else if err != nil || directory != target {
			t.Fatal("verified source invalidated", directory, err)
		}
		if _, err := os.Stat(filepath.Join(target, "foreign")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFailedTargetCleanupRetainsActiveConsumer(t *testing.T) {
	target := filepath.Join(canonicalCacheFixtureParent(t), "candidate")
	archive := testArchive(t, []string{"bin/nibfin-core.mjs", ".node-version"})
	var report cacheCleanupReport
	_, err := verifyCache(failingVerifier{}, archive, target, "trusted",
		"approved-pin", Pins().Core, func(result cacheCleanupReport) { report = result },
		func(string) string { return "active_use" })
	if err == nil || err.Error() != "CORE_INSTALLATION_READBACK_FAILED" {
		t.Fatal(err)
	}
	if report.Entries[0].Reason != "active_use" {
		t.Fatal(report)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal(err)
	}
}
