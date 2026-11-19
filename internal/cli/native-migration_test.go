package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type migrationAPI struct {
	*upgradeAPI
	secretWrites int
	lost         bool
	metadataFail bool
}

func (f *migrationAPI) publishNativeWakeSecret(repo string) error {
	f.secretWrites++
	f.data["repos/"+repo+"/actions/secrets/"+nativeWakeSecret] = nativeSecretMetadata{
		Name: nativeWakeSecret, CreatedAt: "2026-11-20", UpdatedAt: "2026-11-20",
	}
	if f.lost {
		return errors.New("MANAGED_GITHUB_RESPONSE_UNKNOWN")
	}
	if f.metadataFail {
		f.denyRead = "repos/" + repo + "/actions/secrets/" + nativeWakeSecret
	}
	return nil
}

func migrationFixture(t *testing.T) (*migrationAPI, sharedHost,
	*relayAdoptions, *nativeWakeSetup, nativeMigrationOperations) {
	t.Helper()
	f := &migrationAPI{upgradeAPI: upgradeFixture(t, true, true, true)}
	f.data["user"] = User{ID: 42, Login: "operator", Type: "User"}
	f.data[pullListPath(upgradeBranchName)] = []upgradePull{}
	host := sharedHost{SchemaVersion: 1, MemberID: 42,
		SharedExecution:      SharedExecution{"example/project", 10},
		AdoptedRepositoryIDs: []int64{10}}
	body := f.installed[managedIssueWorkflow]
	var bindings relayAdoptions
	data, _ := json.Marshal(map[string]any{"memberId": 42, "pin": f.old.Core,
		"origins": []any{map[string]any{"repository": "example/project",
			"repositoryId": 10, "event": map[string]any{
				"repository": "example/project", "repositoryId": 10, "memberId": 42,
				"workflowId": 99, "workflowPath": managedIssueWorkflow,
				"sourceCommit": strings.Repeat("a", 40), "workflowBlob": blobHash([]byte(body)),
				"reusable": map[string]any{"repository": f.old.Core.Repository,
					"path": ".github/workflows/issue.yml", "commit": f.old.Core.Commit}}}}})
	if err := json.Unmarshal(data, &bindings); err != nil {
		t.Fatal(err)
	}
	f.data[upgradeRepo+"/branches/main"] = map[string]any{"name": "main",
		"commit": map[string]any{"sha": strings.Repeat("a", 40)}}
	f.data[upgradeRepo+"/actions/workflows/99"] = map[string]any{
		"id": 99, "path": managedIssueWorkflow, "state": "active"}
	f.data[upgradeRepo+"/actions/variables/"+sharedPoolVariable] =
		actionsVariable{Name: sharedPoolVariable, Value: "true"}
	destination, _ := json.Marshal(host.SharedExecution)
	f.data[upgradeRepo+"/actions/variables/"+sharedDestinationVariable] =
		actionsVariable{Name: sharedDestinationVariable, Value: string(destination)}
	state := &nativeWakeSetup{}
	ops := nativeMigrationOperations{
		bindings: func() (relayAdoptions, error) { return bindings, nil },
		state: func(InitPlan) (*nativeWakeSetup, func() error, func(), error) {
			return state, func() error { return nil }, func() {}, nil
		},
		refresh: func(InitPlan, sharedHost) error {
			t.Fatal("unmerged caller refreshed")
			return nil
		},
		retire: func() ([]byte, error) {
			t.Fatal("unmerged peer retired working route")
			return nil, nil
		},
	}
	return f, host, &bindings, state, ops
}

func TestNativeMigrationPreparesActiveArtifactRouteAndReusesProposal(t *testing.T) {
	f, host, _, state, ops := migrationFixture(t)
	for range 2 {
		result, err := migrateNativeConsumer(f, host, "example/project", "latest", ops)
		if err != nil || result.Status != "pending" || result.Proposal == nil {
			t.Fatal("existing proposal did not resume", err, result)
		}
	}
	if f.secretWrites != 1 || state.SecretState != "confirmed" {
		t.Fatal("confirmed credential repeated", f.secretWrites, state)
	}
	pool, _, _ := managedPool(f, "example/project")
	if pool.Value != "true" {
		t.Fatal("working route was disabled")
	}
	if countWrite(f.writes, "POST "+upgradeRepo+"/pulls") != 1 {
		t.Fatal("upgrade proposal duplicated", f.writes)
	}
}

func TestNativeMigrationRejectsChangedAuthorityBeforeCredentialWrite(t *testing.T) {
	for _, changed := range []string{"member", "repository", "caller", "destination"} {
		t.Run(changed, func(t *testing.T) {
			f, host, bindings, _, ops := migrationFixture(t)
			switch changed {
			case "member":
				bindings.MemberID++
			case "repository":
				bindings.Origins[0].Event.RepositoryID++
			case "caller":
				bindings.Origins[0].Event.Reusable.Path = ".github/workflows/other.yml"
			case "destination":
				delete(f.data, upgradeRepo+"/actions/variables/"+sharedDestinationVariable)
			}
			_, err := migrateNativeConsumer(f, host, "example/project", "latest", ops)
			if err == nil || f.secretWrites != 0 || len(f.writes) != 0 {
				t.Fatal("unproven artifact route mutated", err, f.secretWrites, f.writes)
			}
		})
	}
}

func TestNativeMigrationUnknownWriteAndPublishedMetadataRecovery(t *testing.T) {
	for _, lost := range []bool{false, true} {
		f, host, _, state, ops := migrationFixture(t)
		f.lost, f.metadataFail = lost, !lost
		_, err := migrateNativeConsumer(f, host, "example/project", "latest", ops)
		if err == nil {
			t.Fatal("lost observation claimed success")
		}
		if lost && state.SecretState != "pending" ||
			!lost && state.SecretState != "published" {
			t.Fatal("incorrect durable stage", state)
		}
		f.lost, f.metadataFail, f.denyRead = false, false, ""
		_, err = migrateNativeConsumer(f, host, "example/project", "latest", ops)
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if lost {
			want = 2
		}
		if f.secretWrites != want {
			t.Fatal("wrong retry operation", f.secretWrites, want)
		}
	}
}

func TestNativeMigrationDoesNotAdoptAnUnrelatedSecretByMetadata(t *testing.T) {
	f, host, _, _, ops := migrationFixture(t)
	if err := f.publishNativeWakeSecret("example/project"); err != nil {
		t.Fatal(err)
	}
	f.secretWrites = 0
	_, err := migrateNativeConsumer(f, host, "example/project", "latest", ops)
	if err == nil || err.Error() != "NATIVE_WAKE_EXISTING_SECRET_CONFLICT" ||
		f.secretWrites != 0 || len(f.writes) != 0 {
		t.Fatal("metadata granted credential ownership", err, f.secretWrites, f.writes)
	}
}

func countWrite(writes []string, expected string) int {
	count := 0
	for _, write := range writes {
		if write == expected {
			count++
		}
	}
	return count
}
