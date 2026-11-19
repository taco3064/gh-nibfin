package cli

import (
	"errors"
	"testing"
)

func TestNativeMigrationRebuildsStaleMetadataWithoutSecretWrites(t *testing.T) {
	for _, previous := range []string{"", "confirmed", "observed"} {
		for _, changedAtReady := range []bool{false, true} {
			f, host, ops, _ := readyMigrationFixture(t)
			state := &nativeWakeSetup{SecretState: previous,
				Secret: nativeSecretMetadata{Name: nativeWakeSecret,
					CreatedAt: "old-created", UpdatedAt: "old-updated"}}
			ops.state = func(InitPlan) (*nativeWakeSetup, func() error, func(), error) {
				return state, func() error { return nil }, func() {}, nil
			}
			if changedAtReady {
				refresh := ops.refresh
				ops.refresh = func(plan InitPlan, host sharedHost) error {
					f.data[upgradeRepo+"/actions/secrets/"+nativeWakeSecret] =
						nativeSecretMetadata{Name: nativeWakeSecret,
							CreatedAt: "recreated", UpdatedAt: "new-after-prepare"}
					return refresh(plan, host)
				}
			}
			result, err := migrateNativeConsumer(f, host, "example/project", "latest", ops)
			if err != nil || result.Status != "ready" || f.secretWrites != 0 ||
				len(f.writes) != 0 || state.SecretState != "observed" ||
				state.Secret.CreatedAt == "old-created" ||
				changedAtReady && state.Secret.UpdatedAt != "new-after-prepare" {
				t.Fatal("stale metadata gated current configuration", err, state, f.writes)
			}
		}
	}
}

func TestNativeMigrationReadyMetadataFailureDoesNotOverwriteOrSave(t *testing.T) {
	for _, changedRoute := range []bool{false, true} {
		f, host, ops, _ := readyMigrationFixture(t)
		state := &nativeWakeSetup{SecretState: "observed"}
		saves, afterPreparation := 0, false
		ops.state = func(InitPlan) (*nativeWakeSetup, func() error, func(), error) {
			return state, func() error {
				if afterPreparation {
					t.Fatal("failed fresh observation saved metadata")
				}
				saves++
				return nil
			}, func() {}, nil
		}
		bindings := ops.bindings
		ops.bindings = func() (relayAdoptions, error) {
			if afterPreparation && changedRoute {
				return relayAdoptions{}, errors.New("ACTUAL_ROUTE_CHANGED")
			}
			return bindings()
		}
		ops.refresh = func(InitPlan, sharedHost) error {
			afterPreparation = true
			if !changedRoute {
				f.denyRead = upgradeRepo + "/actions/secrets/" + nativeWakeSecret
			}
			return nil
		}
		_, err := migrateNativeConsumer(f, host, "example/project", "latest", ops)
		if err == nil || saves != 1 || f.secretWrites != 0 || len(f.writes) != 0 {
			t.Fatal("failed ready observation altered credential", err, saves, f.writes)
		}
	}
}
