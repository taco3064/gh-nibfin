package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestNativeUpgradeLifecycleReleasesGateForSourceAndReadsFreshHost(t *testing.T) {
	held, updated, selections, sourceWrites := false, false, 0, 0
	var calls []string
	ops := nativeUpgradeOperations{
		prepare: func() error {
			if held {
				t.Fatal("root preparation inside host gate")
			}
			selections++
			calls = append(calls, "prepare")
			return nil
		},
		lock: func() (func(), error) {
			if held {
				t.Fatal("recursive host admission")
			}
			held = true
			return func() { held = false }, nil
		},
		host: func() (sharedHost, error) {
			if !held {
				t.Fatal("host facts read without normal admission")
			}
			current := sharedHost{MemberID: 42, AdoptedRepositoryIDs: []int64{10}}
			if updated {
				current.AdoptedRepositoryIDs = []int64{10, 11}
			}
			return current, nil
		},
		migrate: func(host sharedHost) ([]nativeMigrationResult, *nativeContinuation, error) {
			if !held {
				t.Fatal("migration outside normal admission")
			}
			if !updated {
				return nil, nil, &nativeSourceRequired{Producers: Pins()}
			}
			if len(host.AdoptedRepositoryIDs) != 2 || host.MemberID != 42 {
				t.Fatal("stale host was reused after source update")
			}
			return []nativeMigrationResult{{Repository: "example/project", Status: "ready"}},
				&nativeContinuation{Status: "paused", Results: []json.RawMessage{}}, nil
		},
		maintain: func(Producers) error {
			if held {
				t.Fatal("root transaction self-locked against CLI")
			}
			updated = true
			sourceWrites++
			return nil
		},
	}
	for range 2 {
		_, continuation, err := nativeUpgradeLifecycle(ops)
		if err != nil || continuation.Status != "paused" || held {
			t.Fatal("source/continuation did not complete", continuation, err, calls)
		}
	}
	if sourceWrites != 1 || selections != 2 {
		t.Fatal("completed source update replayed", sourceWrites, selections)
	}
}

func TestNativeInitLifecycleBootstrapsAfterAdoptionOutsideGate(t *testing.T) {
	for _, pending := range []bool{false, true} {
		held, adopted, installed, finishes := false, false, false, 0
		var calls []string
		ops := nativeInitOperations{
			prepare: func() error {
				if held {
					t.Fatal("bootstrap self-locked against CLI")
				}
				if adopted && !installed {
					installed = true
					calls = append(calls, "bootstrap")
				}
				return nil
			},
			lock: func() (func(), error) {
				if held {
					t.Fatal("recursive host admission")
				}
				held = true
				return func() { held = false }, nil
			},
			resume: func() error {
				if !held {
					t.Fatal("adoption outside normal admission")
				}
				adopted = true
				calls = append(calls, "adopt")
				if pending {
					return errors.New("NATIVE_CONTINUATION_PENDING")
				}
				return nil
			},
			verify: func() error {
				if !held || !installed {
					t.Fatal("post-bootstrap admission was not reacquired")
				}
				calls = append(calls, "fresh")
				return nil
			},
			finish: func() error { finishes++; return nil },
		}
		err := nativeInitLifecycle(ops)
		if (err != nil) != pending || held || !installed ||
			!strings.HasPrefix(strings.Join(calls, ","), "adopt,bootstrap") ||
			pending && finishes != 0 || !pending && finishes != 1 {
			t.Fatal("initialization lifecycle order changed", err, calls, finishes)
		}
	}
}
