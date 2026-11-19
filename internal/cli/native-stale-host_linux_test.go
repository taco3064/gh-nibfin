package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeCompletedPRUsesCurrentPlanWhenOnlyRetrievalMetadataChanged(t *testing.T) {
	base, plan, merge := managedResumeFixture(t)
	plan.NativeManaged = true
	merge()
	api := &nativeWakeAPI{fakeAPI: base}
	state := configureNativeFixture(api, plan)
	current, old := Pins(), Pins()
	old.Artifact.ID++
	old.Artifact.Digest = "sha256:" + strings.Repeat("f", 64)
	saveNativeFixtureIntent(t, managedInstallIntent{
		Plan: historicalNativePlan(plan, old), Pins: old, Native: state,
		PR: 7, Head: strings.Repeat("b", 40)})
	var installed *sharedHost
	preparations, credentials, registrations := 0, 0, 0
	ops := managedHostOperations{
		discover: func() (*sharedHost, error) { return installed, nil },
		prepare: func(observed InitPlan) error {
			assertNativeInstallLockHeld(t, observed)
			preparations++
			if Pins() != current || !observed.NativeManaged || len(observed.Files) != 0 ||
				observed.Head != strings.Repeat("c", 40) {
				t.Fatal("host work must receive the current authorized plan", observed)
			}
			return nil
		},
		credentials: func(int64, []byte) error { credentials++; return nil },
		stage:       func(InitPlan) ([]string, error) { return []string{"one", "two"}, nil },
		register: func(InitPlan, []string) ([]sharedRegistration, error) {
			registrations++
			return []sharedRegistration{{ID: 25, Name: "slot-1", Status: "online"},
				{ID: 26, Name: "slot-2", Status: "online"}}, nil
		},
		adopt: func(InitPlan, []sharedRegistration) (*sharedHost, error) {
			installed = nativeFixtureHost(t, api)
			return installed, nil
		},
		start:   func([]string) error { return nil },
		cleanup: func(InitPlan, []sharedRegistration) error { return nil },
		native: func(plan InitPlan, runners []sharedRegistration) (nativeContinuation, error) {
			assertNativeInstallLockHeld(t, plan)
			return activateNativeRoute(api, plan, runners, nativeActivationOperations{
				discover: func() (*sharedHost, error) { return installed, nil },
				recheck:  func(sharedHost) error { return nil },
				complete: func() ([]byte, error) {
					return []byte(`{"status":"dispatched","results":[]}`), nil
				},
			})
		},
	}
	var out bytes.Buffer
	for range 2 {
		if err := (App{API: api}).resumeManagedInit(context.Background(), plan, nil,
			&out, ops); err != nil {
			t.Fatal("retrieval metadata blocked the unfinished host stage", err)
		}
		assertNativeInstallLockReleased(t, plan)
	}
	if preparations != 1 || credentials != 1 || registrations != 1 ||
		api.secretWrites != 0 || len(api.writes) != 1 {
		t.Fatal("completed effects repeated", preparations, credentials, registrations,
			api.secretWrites, api.writes)
	}
}

func TestNativeActualSourceTransitionKeepsTheUpgradeBoundary(t *testing.T) {
	base, plan, merge := managedResumeFixture(t)
	plan.NativeManaged = true
	merge()
	api := &nativeWakeAPI{fakeAPI: base}
	old := fixtureProducers(t, base, strings.Repeat("d", 40),
		strings.Repeat("e", 40), 103, true, true)
	current := Pins()
	oldSource := "repos/" + old.Core.Repository + "/contents/"
	for _, path := range []string{"src/shared-runner-contract.json", sharedRunnerWorkflow} {
		base.data[oldSource+path+"?ref="+old.Core.Commit] =
			base.data[oldSource+path+"?ref="+current.Core.Commit]
	}
	execution := plan.Repository
	execution.ID, execution.FullName = 20, "example/execution"
	plan.Execution = &SharedExecution{execution.FullName, execution.ID}
	root := "repos/" + execution.FullName
	base.data[root] = execution
	head := strings.Repeat("c", 40)
	base.data[root+"/git/ref/heads/main"] = map[string]any{
		"ref": "refs/heads/main", "object": map[string]string{"sha": head}}
	base.data[root+"/branches/main"] = map[string]any{
		"name": "main", "protected": true, "commit": map[string]string{"sha": head}}
	base.data[root+"/actions/workflows/shared-runner.yml"] = map[string]any{
		"id": 42, "path": sharedRunnerWorkflow, "state": "active"}
	base.data[root+"/contents/"+sharedRunnerWorkflow+"?ref="+head] =
		encoded([]byte("runner"))
	data, _ := json.Marshal(current)
	base.data[root+"/contents/.nibfin/producer-pins.json?ref="+head] = encoded(data)
	recipes, err := callerRecipes(api, old.Core)
	if err != nil {
		t.Fatal(err)
	}
	oldPlan := plan
	oldPlan.Sources, oldPlan.Files = &old, []Change{}
	for path, text := range templatesForRecipe(old, plan.Member.ID, recipes, "issue") {
		oldPlan.Files = append(oldPlan.Files, Change{path, text})
	}
	saveNativeFixtureIntent(t, managedInstallIntent{Plan: oldPlan, Pins: old})
	for _, file := range oldPlan.Files {
		base.data["repos/example/project/contents/"+file.Path+"?ref="+
			strings.Repeat("c", 40)] = encoded([]byte(file.Content))
	}
	ops := managedHostOperations{prepare: func(InitPlan) error {
		t.Fatal("an actual source transition cannot silently install a root package")
		return nil
	}}
	err = (App{API: api}).resumeManagedInit(context.Background(), plan, nil,
		new(bytes.Buffer), ops)
	if err == nil || !strings.Contains(err.Error(), "EXISTING_FILE_CONFLICT") ||
		len(api.writes) != 0 || api.secretWrites != 0 {
		t.Fatal("actual caller/source transition bypassed upgrade", err, api.writes)
	}
}
