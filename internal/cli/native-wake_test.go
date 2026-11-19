package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type nativeWakeAPI struct {
	*fakeAPI
	secretWrites          int
	writeLost             bool
	readFailure           string
	readAfterWriteFailure bool
}

func (a *nativeWakeAPI) Call(method, path string, input, output any) error {
	if method == "GET" && path == a.readFailure {
		return errors.New("MANAGED_GITHUB_RESPONSE_UNKNOWN")
	}
	if (method == "POST" || method == "PATCH") &&
		strings.Contains(path, "/actions/variables") {
		variable := input.(actionsVariable)
		base := "repos/example/project/actions/variables/"
		a.data[base+variable.Name] = variable
		a.writes = append(a.writes, method+" "+path)
		return nil
	}
	return a.fakeAPI.Call(method, path, input, output)
}

func (a *nativeWakeAPI) publishNativeWakeSecret(repo string) error {
	a.secretWrites++
	a.data["repos/"+repo+"/actions/secrets/"+nativeWakeSecret] = nativeSecretMetadata{
		Name: nativeWakeSecret, CreatedAt: "2026-11-20T00:00:00Z",
		UpdatedAt: "2026-11-20T00:00:00Z",
	}
	if a.writeLost {
		return errors.New("MANAGED_GITHUB_RESPONSE_UNKNOWN")
	}
	if a.readAfterWriteFailure {
		a.readFailure = "repos/" + repo + "/actions/secrets/" + nativeWakeSecret
	}
	return nil
}

func nativeWakeFixture() (*nativeWakeAPI, InitPlan) {
	api := &nativeWakeAPI{fakeAPI: initFixture()}
	repo := api.data["repos/example/project"].(Repository)
	plan := InitPlan{NativeManaged: true, IssueRecipe: "issue", Repository: repo,
		Member: api.data["user"].(User), Execution: &SharedExecution{repo.FullName, repo.ID}}
	return api, plan
}

func TestNativeWakePreparesAndReusesConfirmedProviderEffects(t *testing.T) {
	api, plan := nativeWakeFixture()
	state := &nativeWakeSetup{}
	var snapshots []nativeWakeSetup
	save := func() error { snapshots = append(snapshots, *state); return nil }
	for range 2 {
		if err := prepareNativeWake(api, plan, state, save,
			api.publishNativeWakeSecret); err != nil {
			t.Fatal(err)
		}
	}
	if api.secretWrites != 1 || len(api.writes) != 1 || len(snapshots) != 3 ||
		snapshots[0].SecretState != "pending" || snapshots[1].SecretState != "published" ||
		snapshots[2].SecretState != "confirmed" {
		t.Fatalf("completed configuration repeated: %+v %+v", snapshots, api.writes)
	}
	if err := requireInactiveSharedWake(api, plan.Repository.FullName); err != nil {
		t.Fatal("setup must leave dispatch disabled", err)
	}
	data, _ := json.Marshal(state)
	if strings.Contains(string(data), "githubToken") {
		t.Fatal("intent must never contain the credential")
	}
}

func TestNativeWakePublishedEffectRetriesOnlyMetadataObservation(t *testing.T) {
	api, plan := nativeWakeFixture()
	api.readAfterWriteFailure = true
	state := &nativeWakeSetup{}
	var persisted nativeWakeSetup
	save := func() error { persisted = *state; return nil }
	if err := prepareNativeWake(api, plan, state, save,
		api.publishNativeWakeSecret); err == nil || persisted.SecretState != "published" {
		t.Fatal("successful write was not persisted before metadata GET", err)
	}
	state = &persisted
	api.readFailure = ""
	if err := prepareNativeWake(api, plan, state, save,
		api.publishNativeWakeSecret); err != nil || api.secretWrites != 1 ||
		state.SecretState != "confirmed" {
		t.Fatal("successful write was repeated after failed GET", err, api.secretWrites)
	}
	state.SecretState = "published"
	delete(api.data, "repos/example/project/actions/secrets/"+nativeWakeSecret)
	err := prepareNativeWake(api, plan, state, save, api.publishNativeWakeSecret)
	if err == nil || err.Error() != "NATIVE_WAKE_SECRET_CHANGED" || api.secretWrites != 1 {
		t.Fatal("deleted configuration must be reported as changed, not an unknown write", err)
	}
}

func TestNativeWakePublicationSaveFailureDoesNotClaimDurableConfirmation(t *testing.T) {
	api, plan := nativeWakeFixture()
	state := &nativeWakeSetup{}
	var persisted nativeWakeSetup
	save := func() error {
		if state.SecretState == "published" {
			return errors.New("FIXTURE_DISK_ERROR")
		}
		persisted = *state
		return nil
	}
	err := prepareNativeWake(api, plan, state, save, api.publishNativeWakeSecret)
	if err == nil || err.Error() != "FIXTURE_DISK_ERROR" || api.secretWrites != 1 ||
		persisted.SecretState != "pending" {
		t.Fatal("failed save was presented as durable publication", err, persisted)
	}
}

func TestNativeWakeUnknownWriteReentersOnlyItsAuthorizedUnfinishedSetup(t *testing.T) {
	for _, present := range []bool{true, false} {
		api, plan := nativeWakeFixture()
		api.writeLost = true
		state := &nativeWakeSetup{}
		save := func() error { return nil }
		err := prepareNativeWake(api, plan, state, save, api.publishNativeWakeSecret)
		if err == nil || err.Error() != "NATIVE_WAKE_SECRET_WRITE_UNKNOWN" ||
			state.SecretState != "pending" || api.secretWrites != 1 {
			t.Fatal("lost response must remain unfinished", err, state)
		}
		if !present {
			delete(api.data, "repos/example/project/actions/secrets/"+nativeWakeSecret)
		}
		api.writeLost = false
		for range 2 {
			if err := prepareNativeWake(api, plan, state, save,
				api.publishNativeWakeSecret); err != nil {
				t.Fatal(err)
			}
		}
		if api.secretWrites != 2 || state.SecretState != "confirmed" {
			t.Fatal("new successful response must confirm without repeating it", state)
		}
	}
}

func TestNativeWakePendingWriteRevalidatesItsBoundOperation(t *testing.T) {
	for _, change := range []string{"account", "destination", "enabled", "failed-read"} {
		t.Run(change, func(t *testing.T) {
			api, plan := nativeWakeFixture()
			state := &nativeWakeSetup{}
			api.writeLost = true
			save := func() error { return nil }
			_ = prepareNativeWake(api, plan, state, save, api.publishNativeWakeSecret)
			api.writeLost = false
			switch change {
			case "account":
				plan.Member.ID++
			case "destination":
				api.data["repos/example/project/actions/variables/"+
					sharedDestinationVariable] = actionsVariable{
					Name: sharedDestinationVariable, Value: "unrelated"}
			case "enabled":
				api.data["repos/example/project/actions/variables/"+sharedPoolVariable] =
					actionsVariable{Name: sharedPoolVariable, Value: "true"}
			case "failed-read":
				api.readFailure = "repos/example/project/actions/secrets/" + nativeWakeSecret
			}
			if err := prepareNativeWake(api, plan, state, save,
				api.publishNativeWakeSecret); err == nil || api.secretWrites != 1 ||
				state.SecretState != "pending" {
				t.Fatal("changed or unobserved operation was blindly repeated", err)
			}
		})
	}
}

func TestNativeWakeConflictsAndFailedObservationsPreserveConfiguration(t *testing.T) {
	for _, scenario := range []string{"secret", "destination", "permission", "account",
		"destination-identity", "failed-read", "save-failure"} {
		t.Run(scenario, func(t *testing.T) {
			api, plan := nativeWakeFixture()
			save := func() error { return nil }
			switch scenario {
			case "secret":
				_ = api.publishNativeWakeSecret(plan.Repository.FullName)
				api.secretWrites = 0
			case "destination":
				api.data["repos/example/project/actions/variables/"+
					sharedDestinationVariable] = actionsVariable{
					Name: sharedDestinationVariable, Value: "unrelated"}
			case "permission":
				plan.Repository.Permissions["admin"] = false
			case "account":
				plan.Member.ID++
			case "destination-identity":
				plan.Execution.RepositoryID++
			case "failed-read":
				api.readFailure = "repos/example/project/actions/secrets/" + nativeWakeSecret
			case "save-failure":
				save = func() error { return errors.New("FIXTURE_DISK_ERROR") }
			}
			if err := prepareNativeWake(api, plan, &nativeWakeSetup{}, save,
				api.publishNativeWakeSecret); err == nil || api.secretWrites != 0 {
				t.Fatal("unsafe secret publication", err, api.secretWrites)
			}
		})
	}
}

func TestNativeWakeChangedMetadataIsNotCredentialEquality(t *testing.T) {
	api, plan := nativeWakeFixture()
	state := &nativeWakeSetup{}
	save := func() error { return nil }
	if err := prepareNativeWake(api, plan, state, save,
		api.publishNativeWakeSecret); err != nil {
		t.Fatal(err)
	}
	path := "repos/example/project/actions/secrets/" + nativeWakeSecret
	changed := state.Secret
	changed.UpdatedAt = "2026-11-20T01:00:00Z"
	api.data[path] = changed
	if err := prepareNativeWake(api, plan, state, save,
		api.publishNativeWakeSecret); err == nil || api.secretWrites != 1 {
		t.Fatal("external secret change was overwritten", err)
	}
}
