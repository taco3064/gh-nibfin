package cli

import (
	"errors"
	"testing"
)

func TestNativeMigrationRepairsMissingSecretWithoutDisablingNativePool(t *testing.T) {
	for _, previous := range []string{"", "confirmed", "observed", "published", "pending"} {
		t.Run(previous, func(t *testing.T) {
			f, host, ops, _ := readyMigrationFixture(t)
			state := &nativeWakeSetup{SecretState: previous}
			ops.state = func(InitPlan) (*nativeWakeSetup, func() error, func(), error) {
				return state, func() error { return nil }, func() {}, nil
			}
			delete(f.data, upgradeRepo+"/actions/secrets/"+nativeWakeSecret)
			result, err := migrateNativeConsumer(f, host, "example/project", "latest", ops)
			pool, _, poolErr := managedPool(f, "example/project")
			if err != nil || result.Status != "ready" || f.secretWrites != 1 ||
				state.SecretState != "observed" || poolErr != nil || pool.Value != "true" ||
				len(f.writes) != 0 {
				t.Fatal("native repair changed route or replayed writes", err, result,
					f.secretWrites, state, pool, f.writes)
			}
		})
	}
}

func TestNativeMigrationMissingSecretRequiresFreshCompleteNativeAuthority(t *testing.T) {
	for _, changed := range []string{"read", "host", "adoptions", "provider", "runners",
		"destination", "permission", "caller"} {
		t.Run(changed, func(t *testing.T) {
			f, host, ops, _ := readyMigrationFixture(t)
			state := &nativeWakeSetup{SecretState: "confirmed"}
			ops.state = func(InitPlan) (*nativeWakeSetup, func() error, func(), error) {
				return state, func() error { return nil }, func() {}, nil
			}
			delete(f.data, upgradeRepo+"/actions/secrets/"+nativeWakeSecret)
			switch changed {
			case "read":
				f.denyRead = upgradeRepo + "/actions/secrets/" + nativeWakeSecret
			case "destination":
				delete(f.data, upgradeRepo+"/actions/variables/"+sharedDestinationVariable)
			case "permission":
				repo := f.data[upgradeRepo].(Repository)
				repo.Permissions = map[string]bool{"push": true}
				f.data[upgradeRepo] = repo
			case "caller":
				f.denyRead = upgradeRepo + "/git/ref/heads/main"
			default:
				original := ops.installed
				ops.installed = func(plan InitPlan, pins Producers) (
					*sharedHost, []sharedRegistration, error) {
					actual, runners, err := original(plan, pins)
					copy := *actual
					switch changed {
					case "host":
						copy.RepositoryID++
					case "adoptions":
						copy.AdoptedRepositoryIDs = []int64{10, 11}
					case "provider":
						copy.Agent = &agentBinding{"codex", "changed", "changed"}
					case "runners":
						runners[0].ID++
					}
					return &copy, runners, err
				}
			}
			_, err := migrateNativeConsumer(f, host, "example/project", "latest", ops)
			if err == nil || f.secretWrites != 0 || len(f.writes) != 0 ||
				state.SecretState != "confirmed" {
				t.Fatal("incomplete authority permitted repair", err, state, f.secretWrites)
			}
		})
	}
}

func TestNativeMigrationRepairUsesNormalUnknownWriteAndReadbackRecovery(t *testing.T) {
	for _, unknownWrite := range []bool{true, false} {
		f, host, ops, _ := readyMigrationFixture(t)
		state := &nativeWakeSetup{SecretState: "observed"}
		ops.state = func(InitPlan) (*nativeWakeSetup, func() error, func(), error) {
			return state, func() error { return nil }, func() {}, nil
		}
		delete(f.data, upgradeRepo+"/actions/secrets/"+nativeWakeSecret)
		f.lost, f.metadataFail = unknownWrite, !unknownWrite
		_, err := migrateNativeConsumer(f, host, "example/project", "latest", ops)
		if err == nil || f.secretWrites != 1 {
			t.Fatal("uncertain repair claimed completion", err, f.secretWrites)
		}
		f.lost, f.metadataFail, f.denyRead = false, false, ""
		_, err = migrateNativeConsumer(f, host, "example/project", "latest", ops)
		want := 1
		if unknownWrite {
			want++
		}
		if err != nil || f.secretWrites != want {
			t.Fatal("repair resumed wrong operation", err, f.secretWrites, want)
		}
	}
}

func TestNativeMigrationDoesNotResetProgressAfterFailedObservation(t *testing.T) {
	f, host, ops, _ := readyMigrationFixture(t)
	state := &nativeWakeSetup{SecretState: "published"}
	f.denyRead = upgradeRepo + "/actions/secrets/" + nativeWakeSecret
	plan, err := nativeMigrationPlan(f, host, fixtureUpgradePlan(t, f.upgradeAPI))
	if err != nil {
		t.Fatal(err)
	}
	err = prepareMigrationNativeWake(f, host, plan, "latest", ops, state,
		func() error { return errors.New("save must not run") })
	if err == nil || state.SecretState != "published" || f.secretWrites != 0 {
		t.Fatal("failed read reset known progress", err, state, f.secretWrites)
	}
}
