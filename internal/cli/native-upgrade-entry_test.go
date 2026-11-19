package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestManagedLatestNormalEntryResumesSourceBeforeCleanupAndContinuation(t *testing.T) {
	api, host, migration, retired := nativeConsumersFixture(t)
	target := upgradeSource{Kind: "installed-current", Producers: api.first.old,
		Execution: &host.SharedExecution, maintained: true, nativeLatest: true}
	migration.selected = &target
	migration.sourcePending = true
	held, offline, sourceFinishes, nativeEntries := false, true, 0, 0
	installed := migration.installed
	migration.installed = func(plan InitPlan, pins Producers) (
		*sharedHost, []sharedRegistration, error) {
		actual, runners, err := installed(plan, pins)
		if offline {
			for index := range runners {
				runners[index].Status = "offline"
			}
		}
		return actual, runners, err
	}
	migration.complete = func() ([]byte, error) {
		if offline || *retired != 1 {
			t.Fatal("continuation ran before source finish/cleanup")
		}
		return []byte(`{"status":"paused","results":[]}`), nil
	}
	app := App{API: api, Process: upgradeNoProcess{t}, upgrade: &upgradeCommandOperations{
		discover:     func() (*sharedHost, error) { return &host, nil },
		verify:       func(sharedHost) error { return nil },
		selectSource: func(sharedHost, string) (upgradeSource, error) { return target, nil },
		applyNative: func(actual sharedHost, version string, out io.Writer) error {
			nativeEntries++
			_, continuation, err := nativeUpgradeLifecycle(nativeUpgradeOperations{
				prepare: func() error {
					if held {
						t.Fatal("normal entry bootstrap held host gate")
					}
					return nil
				},
				lock: func() (func(), error) {
					if held {
						t.Fatal("normal entry took a second host gate")
					}
					held = true
					return func() { held = false }, nil
				},
				host: func() (sharedHost, error) { return actual, nil },
				migrate: func(current sharedHost) ([]nativeMigrationResult,
					*nativeContinuation, error) {
					return migrateNativeConsumers(api, current, version, migration, true)
				},
				maintain: func(pins Producers) error {
					if held || *retired != 0 || !sameProducerSources(pins, target.Producers) {
						t.Fatal("normal source finish was reordered or reselected")
					}
					sourceFinishes++
					offline, migration.sourcePending = false, false
					return nil
				},
			})
			if err != nil {
				return err
			}
			if continuation.Status != "paused" {
				t.Fatal("Owner pause was discarded")
			}
			return nil
		},
	}}
	err := app.runUpgrade([]string{"--repo", "taco3064/nibfin-core", "--apply"}, io.Discard)
	if err != nil || sourceFinishes != 1 || nativeEntries != 1 || *retired != 1 || held {
		t.Fatal("public latest did not complete its native lifecycle", err, sourceFinishes)
	}
	for name, fixture := range api.apis {
		if fixture.secretWrites != 0 || len(fixture.writes) != 0 {
			t.Fatal("already completed installation was replayed", name, fixture.writes)
		}
	}
}

func TestPinnedUpgradeNormalEntryStaysPROnlyForInstalledHost(t *testing.T) {
	f := upgradeFixture(t, true, true, true)
	f.data[pullListPath(upgradeBranchName)] = []upgradePull{}
	host := sharedHost{AdoptedRepositoryIDs: []int64{10}}
	app := App{API: f, Process: upgradeNoProcess{t}, upgrade: &upgradeCommandOperations{
		discover: func() (*sharedHost, error) { return &host, nil },
		verify:   func(sharedHost) error { return nil },
		recheck:  func(sharedHost) error { return nil },
		selectSource: func(sharedHost, string) (upgradeSource, error) {
			t.Fatal("pinned upgrade selected installed-current authority")
			return upgradeSource{}, nil
		},
		applyNative: func(sharedHost, string, io.Writer) error {
			t.Fatal("pinned proposal deployed or activated a host")
			return nil
		},
	}}
	for range 2 {
		var out bytes.Buffer
		err := app.runUpgrade([]string{"--repo", "example/project", "--version",
			"v1.2.3", "--apply"}, &out)
		if err != nil || !strings.Contains(out.String(), "pull/9") {
			t.Fatal("public pinned PR recovery changed", err, out.String())
		}
	}
	if countWrite(f.writes, "POST "+upgradeRepo+"/pulls") != 1 {
		t.Fatal("pinned PR was published twice", f.writes)
	}
	for _, call := range f.calls {
		if strings.Contains(call, "/actions/secrets") {
			t.Fatal("PR-only entry touched credentials", call)
		}
	}
}

func TestWithdrawnArtifactOptionCannotPublishOrTouchHost(t *testing.T) {
	f := upgradeFixture(t, true, true, true)
	app := App{API: f, Process: upgradeNoProcess{t}, upgrade: &upgradeCommandOperations{
		discover: func() (*sharedHost, error) {
			t.Fatal("withdrawn route reached host administration")
			return nil, nil
		},
	}}
	err := app.runUpgrade([]string{"--repo", "example/project", "--managed-issue",
		"--apply"}, io.Discard)
	if err == nil || err.Error() != "MANAGED_ARTIFACT_ROUTE_RETIRED_USE_NATIVE_UPGRADE" ||
		len(f.writes) != 0 {
		t.Fatal("withdrawn route remained publishable", err, f.writes)
	}
}
