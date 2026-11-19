package cli

import (
	"strings"
	"testing"
)

func TestNativeMainSourceTransitionEntersNormalRootOperationBeforeCleanup(t *testing.T) {
	api, host, ops, retirements := nativeConsumersFixture(t)
	target := upgradeSource{Kind: "installed-current", Producers: api.first.old,
		Execution: &host.SharedExecution, maintained: true, nativeLatest: true}
	ops.selected = &target
	adopted, err := ops.bindings()
	if err != nil {
		t.Fatal(err)
	}
	previous := adopted.Pin
	previous.Commit = strings.Repeat("9", 40)
	adopted.Pin = previous
	ops.bindings = func() (relayAdoptions, error) { return adopted, nil }
	held, sourceOperations := false, 0
	ops.complete = func() ([]byte, error) {
		return []byte(`{"status":"paused","results":[]}`), nil
	}
	_, continuation, err := nativeUpgradeLifecycle(nativeUpgradeOperations{
		prepare: func() error { return nil },
		lock: func() (func(), error) {
			held = true
			return func() { held = false }, nil
		},
		host: func() (sharedHost, error) { return host, nil },
		migrate: func(current sharedHost) ([]nativeMigrationResult,
			*nativeContinuation, error) {
			return migrateNativeConsumers(api, current, "latest", ops, true)
		},
		maintain: func(pins Producers) error {
			if held || *retirements != 0 || !sameProducerSources(pins, target.Producers) {
				t.Fatal("source transition skipped its original root boundary")
			}
			sourceOperations++
			adopted.Pin = pins.Core
			return nil
		},
	})
	if err != nil || continuation == nil || continuation.Status != "paused" ||
		sourceOperations != 1 || *retirements != 1 || held {
		t.Fatal("source transition did not resume after normal root operation", err)
	}
	for name, fixture := range api.apis {
		if fixture.secretWrites != 0 || len(fixture.writes) != 0 {
			t.Fatal("confirmed main publication was replayed", name, fixture.writes)
		}
	}
}
