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

func TestControlledClaudeSetupUsesPrivateProviderBoundary(t *testing.T) {
	api, plan, _ := managedResumeFixture(t)
	binding := agentBinding{"claude", "fixture-model", "fixture-profile", ""}
	plan.Agent = &binding
	var adopted *sharedHost
	host, err := decodeSharedHost([]byte(`{
  "schemaVersion":1,"repositoryId":10,"repository":"example/project","memberId":7,
  "adoptedRepositoryIds":[10],
  "runners":[{"id":25,"name":"slot-1"},{"id":26,"name":"slot-2"}]
 }`))
	if err != nil {
		t.Fatal(err)
	}
	host.Agent = &binding
	var setup, registrations int
	secret := []byte("synthetic-private-fixture-key")
	labels := []struct {
		Name string `json:"name"`
	}{
		{"nibfin-user-7"}, {"nibfin-shared"},
	}
	slots := []sharedRegistration{
		{ID: 25, Name: "slot-1", Status: "online"},
		{ID: 26, Name: "slot-2", Status: "online"},
	}
	api.data["repos/example/project/actions/runners?per_page=100&page=1"] =
		map[string]any{"total_count": 2, "runners": []RunnerInfo{
			{ID: 25, Name: "slot-1", OS: "Linux", Status: "online", Labels: labels},
			{ID: 26, Name: "slot-2", OS: "Linux", Status: "online", Labels: labels},
		}}
	ops := managedHostOperations{
		discover: func() (*sharedHost, error) { return adopted, nil },
		prepare: func(p InitPlan) error {
			if !sameAgent(p.Agent, &binding) {
				t.Fatal("plan lost provider")
			}
			return nil
		},
		credentials: func(int64, []byte) error {
			t.Fatal("Claude must never use Codex credential transfer")
			return nil
		},
		setupAgent: func(p InitPlan, input []byte) error {
			if !bytes.Equal(input, secret) || !sameAgent(p.Agent, &binding) {
				return errors.New("FIXTURE_SETUP_BINDING_CHANGED")
			}
			setup++
			return nil
		},
		readAgent: verifyPlannedAgent,
		stage:     func(InitPlan) ([]string, error) { return []string{"one", "two"}, nil },
		register: func(InitPlan, []string) ([]sharedRegistration, error) {
			registrations++
			return slots, nil
		},
		adopt: func(InitPlan, []sharedRegistration) (*sharedHost, error) {
			adopted = host
			return host, nil
		},
		start:   func([]string) error { return nil },
		cleanup: func(InitPlan, []sharedRegistration) error { return nil },
	}
	var output bytes.Buffer
	err = (App{API: api}).finishManagedInit(context.Background(), plan, secret,
		json.NewEncoder(&output), ops)
	if err != nil || setup != 1 || registrations != 1 {
		t.Fatal(err, setup, registrations)
	}
	for _, expected := range []string{"claude", "fixture-model", "fixture-profile",
		`"id":25`, `"id":26`} {
		if !strings.Contains(output.String(), expected) {
			t.Fatal(output.String())
		}
	}
	path, err := managedIntentPath(plan.Repository)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := installIntentLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	intent := managedInstallIntent{Plan: plan, Pins: Pins()}
	if err := saveManagedIntent(path, intent); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil || bytes.Contains(saved, secret) ||
		bytes.Contains(output.Bytes(), secret) {
		t.Fatal("secret escaped the private setup boundary", err)
	}
	inherited, err := managedIntentAgent(plan)
	if err != nil || inherited == nil || *inherited != binding {
		t.Fatal(inherited, err)
	}
	output.Reset()
	wrong := binding
	wrong.Provider = "codex"
	host.Agent = &wrong
	err = publishManagedRoute(json.NewEncoder(&output), plan, slots, ops)
	if err == nil || output.Len() != 0 {
		t.Fatal("mismatched installed provider completed")
	}
	host.Agent = &binding
	slots[1].ID = 27
	err = publishManagedRoute(json.NewEncoder(&output), plan, slots, ops)
	if err == nil || output.Len() != 0 {
		t.Fatal("mismatched native ID completed")
	}
}

func TestLegacyIntentRejectsExplicitClaudeBeforeEffects(t *testing.T) {
	api, plan, _ := managedResumeFixture(t)
	path, _ := managedIntentPath(plan.Repository)
	unlock, err := installIntentLock(path)
	if err != nil {
		t.Fatal(err)
	}
	intent := managedInstallIntent{Plan: plan, Pins: Pins()}
	if err := saveManagedIntent(path, intent); err != nil {
		t.Fatal(err)
	}
	unlock()
	a := App{API: api, agents: &agentOperations{
		capability: func(string) (agentBinding, error) {
			t.Fatal("legacy Codex intent must reject Claude before capability/setup")
			return agentBinding{}, nil
		},
	}}
	err = a.runInit([]string{"--repo", "example/project", "--agent", "claude"},
		new(bytes.Buffer))
	if err == nil || !strings.Contains(err.Error(), "AGENT_MISMATCH") ||
		len(api.writes) != 0 {
		t.Fatal(err, api.writes)
	}
}

func TestActiveSuccessorSupersedesOldInstallationIntent(t *testing.T) {
	api, _, _ := managedResumeFixture(t)
	recipes := recipeFixture()
	recipes["issue-managed"] = []string{"name: managed CORE_COMMIT", ""}
	pin := Pins().Core
	api.data["repos/"+pin.Repository+"/contents/src/caller-recipes.json?ref="+
		pin.Commit] = encoded(jsonRelay(recipes))
	plan, err := planInitRecipe(api, "example/project", "example/project", "issue-managed")
	if err != nil {
		t.Fatal(err)
	}
	plan.Agent = &agentBinding{"codex", "gpt-6.1", "linux-rootful-docker-v1", ""}
	path, _ := managedIntentPath(plan.Repository)
	unlock, err := installIntentLock(path)
	if err != nil {
		t.Fatal(err)
	}
	intent := managedInstallIntent{Plan: plan, Pins: Pins()}
	if err := saveManagedIntent(path, intent); err != nil {
		t.Fatal(err)
	}
	unlock()
	managedResumePull(api, plan)()
	binding := agentBinding{"claude", "fixture-model", "fixture-profile", ""}
	plan.Agent = &binding
	host, err := decodeSharedHost([]byte(`{
 "schemaVersion":1,"repository":"example/project","repositoryId":10,"memberId":7,
 "adoptedRepositoryIds":[10],
 "runners":[{"id":31,"name":"fresh-one"},{"id":32,"name":"fresh-two"}]
}`))
	if err != nil {
		t.Fatal(err)
	}
	host.Agent = &binding
	labels := []struct {
		Name string `json:"name"`
	}{{"nibfin-user-7"}, {sharedRunnerLabel}}
	api.data["repos/example/project/actions/runners?per_page=100&page=1"] =
		map[string]any{"total_count": 2, "runners": []RunnerInfo{
			{ID: 31, Name: "fresh-one", OS: "Linux", Status: "online", Labels: labels},
			{ID: 32, Name: "fresh-two", OS: "Linux", Status: "online", Labels: labels},
		}}
	destination, _ := json.Marshal(*plan.Execution)
	api.data["repos/example/project/actions/variables/"+sharedDestinationVariable] =
		actionsVariable{sharedDestinationVariable, string(destination)}
	ops := managedHostOperations{
		discover:  func() (*sharedHost, error) { return host, nil },
		readAgent: verifyPlannedAgent,
		cleanup:   func(InitPlan, []sharedRegistration) error { return nil },
		activate: func(InitPlan, []sharedRegistration) (managedRelayResult, error) {
			return managedRelayResult{Github: "verified"}, nil
		},
	}
	var out bytes.Buffer
	err = (App{API: api}).resumeManagedInit(context.Background(), plan, nil, &out, ops)
	if err != nil || len(api.writes) != 0 || !strings.Contains(out.String(), "claude") {
		t.Fatal("obsolete installation intent replaced active successor", err, api.writes)
	}
}
