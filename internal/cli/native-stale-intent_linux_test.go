package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func historicalNativePlan(plan InitPlan, pins Producers) InitPlan {
	plan.Files = append([]Change(nil), plan.Files...)
	files := templatesForRecipe(pins, plan.Member.ID, recipeFixture(), "issue")
	for index, file := range plan.Files {
		if body, ok := files[file.Path]; ok {
			plan.Files[index].Content = body
		}
	}
	return plan
}

func saveNativeFixtureIntent(t *testing.T, intent managedInstallIntent) string {
	t.Helper()
	path, err := managedIntentPath(intent.Plan.Repository)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := installIntentLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := saveManagedIntent(path, intent); err != nil {
		t.Fatal(err)
	}
	return path
}

func configureNativeFixture(api *nativeWakeAPI, plan InitPlan) *nativeWakeSetup {
	value, _ := json.Marshal(plan.Execution)
	api.data["repos/example/project/actions/variables/"+sharedDestinationVariable] =
		actionsVariable{Name: sharedDestinationVariable, Value: string(value)}
	_ = api.publishNativeWakeSecret(plan.Repository.FullName)
	api.secretWrites = 0
	secret, _, _ := nativeWakeSecretMetadata(api, plan.Repository.FullName)
	return &nativeWakeSetup{SecretState: "confirmed", Secret: secret}
}

func TestNativeStaleIntentReusesVerifiedCurrentInstallation(t *testing.T) {
	for _, pool := range []string{"false", "true"} {
		t.Run(pool, func(t *testing.T) {
			base, plan, merge := managedResumeFixture(t)
			plan.NativeManaged = true
			merge()
			api := &nativeWakeAPI{fakeAPI: base}
			host := nativeFixtureHost(t, api)
			state := configureNativeFixture(api, plan)
			api.data["repos/example/project/actions/variables/"+sharedPoolVariable] =
				actionsVariable{Name: sharedPoolVariable, Value: pool}
			old := Pins()
			old.Core.Commit, old.Action.Commit = strings.Repeat("d", 40), strings.Repeat("e", 40)
			saveNativeFixtureIntent(t, managedInstallIntent{
				Plan: historicalNativePlan(plan, old), Pins: old, Native: state})
			completions := 0
			ops := managedHostOperations{
				discover: func() (*sharedHost, error) { return host, nil },
				cleanup:  func(InitPlan, []sharedRegistration) error { return nil },
				native: func(plan InitPlan, runners []sharedRegistration) (
					nativeContinuation, error) {
					assertNativeInstallLockHeld(t, plan)
					return activateNativeRoute(api, plan, runners, nativeActivationOperations{
						discover: func() (*sharedHost, error) { return host, nil },
						recheck:  func(sharedHost) error { return nil },
						complete: func() ([]byte, error) {
							completions++
							return []byte(`{"status":"paused","results":[]}`), nil
						},
					})
				},
				prepare: func(InitPlan) error { t.Fatal("repeated installation"); return nil },
				credentials: func(int64, []byte) error {
					t.Fatal("repeated credential setup")
					return nil
				},
			}
			var out bytes.Buffer
			if err := (App{API: api}).resumeManagedInit(context.Background(), plan, nil,
				&out, ops); err != nil || api.secretWrites != 0 || completions != 1 {
				t.Fatal(err, api.secretWrites, completions)
			}
			assertNativeInstallLockReleased(t, plan)
			writes := 0
			if pool == "false" {
				writes = 1
			}
			if len(api.writes) != writes {
				t.Fatal("only unfinished pool activation may write", api.writes)
			}
		})
	}
}

func TestNativeStaleIntentAgentSelectionAndIdentityUseTheSameRules(t *testing.T) {
	_, plan, _ := managedResumeFixture(t)
	plan.NativeManaged = true
	plan.Agent = &agentBinding{"codex", "fixture-model", "fixture-profile", ""}
	old := Pins()
	old.Artifact.ID++
	intent := managedInstallIntent{Plan: historicalNativePlan(plan, old), Pins: old}
	path := saveNativeFixtureIntent(t, intent)
	selection := plan
	selection.Agent = nil
	if agent, err := managedIntentAgent(selection); err != nil ||
		agent == nil || *agent != *plan.Agent {
		t.Fatal("stale producer bookkeeping blocked no-host agent selection", agent, err)
	}
	for _, change := range []string{"member", "destination", "agent", "recipe", "origin",
		"json", "unknown", "duplicate", "permissions"} {
		t.Run(change, func(t *testing.T) {
			copy := intent
			copy.Plan.Agent = &agentBinding{"codex", "fixture-model", "fixture-profile", ""}
			copy.Plan.Execution = &SharedExecution{plan.Execution.Repository,
				plan.Execution.RepositoryID}
			switch change {
			case "member":
				copy.Plan.Member.ID++
			case "destination":
				copy.Plan.Execution.RepositoryID++
			case "agent":
				copy.Plan.Agent.Model = "invalid model"
			case "recipe":
				copy.Plan.IssueRecipe = "issue-managed"
			case "origin":
				copy.Pins.Core.Repository = "unrelated/core"
			}
			data, _ := json.Marshal(copy)
			switch change {
			case "json":
				data = append(data, []byte(" {}")...)
			case "unknown":
				data = append(data[:len(data)-1], []byte(`,"unknown":true}`)...)
			case "duplicate":
				data = append(data[:len(data)-1], []byte(`,"pins":{}}`)...)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if change == "permissions" {
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			}
			defer os.Chmod(path, 0600)
			if _, err := loadManagedIntent(path, plan); err == nil {
				t.Fatal("invalid intent became an empty or accepted intent", change)
			}
			if _, err := managedIntentAgent(selection); err == nil {
				t.Fatal("agent selection bypassed common intent checks", change)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(data, after) {
				t.Fatal("failed read replaced partial intent")
			}
		})
	}
}

func TestLegacyIntentKeepsItsExistingProducerAndRouteChecks(t *testing.T) {
	_, plan, _ := managedResumeFixture(t)
	plan.IssueRecipe = "issue-managed"
	old := Pins()
	old.Artifact.ID++
	path := saveNativeFixtureIntent(t, managedInstallIntent{Plan: plan, Pins: old})
	if _, err := loadManagedIntent(path, plan); err == nil {
		t.Fatal("legacy artifact installation was silently migrated")
	}
	if _, err := managedIntentAgent(plan); err == nil {
		t.Fatal("legacy agent selection bypassed its current producer check")
	}
}

func TestNativeStaleFailedProviderReadPreservesTheOriginalIntent(t *testing.T) {
	base, plan, merge := managedResumeFixture(t)
	plan.NativeManaged = true
	merge()
	api := &nativeWakeAPI{fakeAPI: base}
	host := nativeFixtureHost(t, api)
	state := configureNativeFixture(api, plan)
	old := Pins()
	old.Artifact.ID++
	path := saveNativeFixtureIntent(t, managedInstallIntent{
		Plan: historicalNativePlan(plan, old), Pins: old, Native: state})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	api.readFailure = "repos/example/project/actions/secrets/" + nativeWakeSecret
	ops := managedHostOperations{discover: func() (*sharedHost, error) { return host, nil }}
	err = (App{API: api}).resumeManagedInit(context.Background(), plan, nil,
		new(bytes.Buffer), ops)
	after, readErr := os.ReadFile(path)
	if err == nil || readErr != nil || !bytes.Equal(before, after) ||
		api.secretWrites != 0 || len(api.writes) != 0 {
		t.Fatal("a failed observation discarded intent or repeated a write", err, readErr)
	}
}

func TestNativeStaleUnfinishedPRIsReadAndReusedBeforeHostWork(t *testing.T) {
	base, plan, _ := managedResumeFixture(t)
	plan.NativeManaged = true
	api := &nativeWakeAPI{fakeAPI: base}
	old := Pins()
	old.Artifact.ID++
	state := configureNativeFixture(api, plan)
	saveNativeFixtureIntent(t, managedInstallIntent{
		Plan: historicalNativePlan(plan, old), Pins: old, Native: state})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	app := App{API: api}
	for range 2 {
		err := app.resumeManagedInit(ctx, plan, nil, &out, managedHostOperations{})
		var pending *CommandError
		if !errors.As(err, &pending) || pending.Code != "MANAGED_INSTALL_AWAITING_PR_MERGE" ||
			len(api.writes) != 0 || api.secretWrites != 0 {
			t.Fatal("unfinished original PR was recreated or host work started", err, api.writes)
		}
	}
}
