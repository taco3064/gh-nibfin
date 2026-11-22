package cli

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func applyClaudeDriver(d *rebuildDriver, kind string, out *bytes.Buffer,
	setup func(InitPlan)) error {
	ops := d.operations()
	ops.credentialKind = kind
	fixture := ops.host.setupAgent
	ops.host.setupAgent = func(plan InitPlan, token []byte) error {
		setup(plan)
		return fixture(plan, token)
	}
	return (App{API: d}).applyRebuild(
		d.api.data["repos/example/project"].(Repository),
		d.api.data["user"].(User), "claude", []byte("fixture-secret"),
		json.NewEncoder(out), ops)
}

func TestClaudeRebuildBindsCredentialKindThroughReadback(t *testing.T) {
	d := newRebuildDriver(t)
	const kind = "claude-subscription-token"
	d.failure = "start"
	var out bytes.Buffer
	var planned []string
	record := func(plan InitPlan) { planned = append(planned, plan.Agent.CredentialKind) }
	if err := applyClaudeDriver(d, kind, &out, record); err == nil {
		t.Fatal("interrupted replacement completed")
	}
	path, _ := sharedMutationPath()
	saved, err := readRebuildIntent(path)
	if err != nil || saved == nil || saved.Agent.CredentialKind != kind ||
		d.host.Agent == nil || d.host.Agent.CredentialKind != kind {
		t.Fatal("credential kind must enter the intent and the adopted binding", err)
	}
	// A different kind cannot continue a replacement started with another one.
	out.Reset()
	err = applyClaudeDriver(d, "anthropic-api-key", &out, record)
	if err == nil || err.Error() != "REBUILD_CLAUDE_CREDENTIAL_KIND_CHANGED" {
		t.Fatal(err)
	}
	d.failure = ""
	out.Reset()
	if err := applyClaudeDriver(d, kind, &out, record); err != nil {
		t.Fatal(err, d.calls, out.String())
	}
	if !slices.Equal(planned, []string{kind}) ||
		!strings.Contains(out.String(), `"credentialKind":"`+kind+`"`) {
		t.Fatal(planned, out.String())
	}
	// Read-back rejects an adopted binding whose kind differs from the plan.
	plan := InitPlan{Repository: Repository{ID: 10}, Member: User{ID: 7},
		Agent: &agentBinding{"claude", "fixture-model", "fixture-profile", kind}}
	host := d.old
	host.Agent = &agentBinding{"claude", "fixture-model", "fixture-profile",
		"anthropic-api-key"}
	if verifyPlannedAgent(plan, &host) == nil {
		t.Fatal("mismatched credential kind passed read-back")
	}
}

func TestClaudeRebuildSetupRequiresCredentialAndKeepsCodex(t *testing.T) {
	codex := 0
	setup := rebuildAgentSetup(func(InitPlan, []byte) error {
		codex++
		return nil
	}, nil, nil)
	claude := InitPlan{Agent: &agentBinding{"claude", "m", "p", "anthropic-api-key"}}
	if err := setup(claude, nil); err == nil ||
		err.Error() != "REBUILD_CLAUDE_CREDENTIAL_REQUIRED_RERUN_SAME_COMMAND" {
		t.Fatal(err)
	}
	plan := InitPlan{Agent: &agentBinding{"codex", "gpt-6.1", "linux-rootful-docker-v1",
		""}}
	if err := setup(plan, nil); err != nil || codex != 1 {
		t.Fatal("Codex setup must stay the shared managed setup", err)
	}
	intent := rebuildIntent{Agent: agentBinding{"codex", "m", "p", ""}}
	if bindRebuildCredentialKind(&intent, "anthropic-api-key") == nil ||
		bindRebuildCredentialKind(&intent, "") != nil || intent.Agent.CredentialKind != "" {
		t.Fatal("a Codex intent never records a Claude credential kind")
	}
}

func TestClaudeRebuildPreparationAndInstalledAdvertisement(t *testing.T) {
	if !slices.Equal(replacementPreparationArguments("codex"),
		[]string{"--consent-resource-preparation"}) ||
		!slices.Equal(replacementPreparationArguments("claude"),
			[]string{"--consent-resource-preparation", "--agent=claude"}) {
		t.Fatal("preparation vectors changed")
	}
	image := "sha256:" + strings.Repeat("a", 64)
	receipt := map[string]any{"status": "prepared", "code": "MANAGED_RESOURCES_PREPARED",
		"profile": "/etc/nibfin/shared-host-profile.json", "adoption": false,
		"servicesReady": false, "images": map[string]string{"agent": image, "hooks": image}}
	if decodeReplacementPreparation(jsonRelay(receipt), "codex") != nil ||
		decodeReplacementPreparation(jsonRelay(receipt), "claude") == nil {
		t.Fatal("a Claude preparation must return its Claude Agent image")
	}
	receipt["images"] = map[string]string{"agent": image, "hooks": image,
		"claudeAgent": image}
	if decodeReplacementPreparation(jsonRelay(receipt), "claude") != nil {
		t.Fatal("complete Claude preparation refused")
	}
	contract := fixtureClaudeContract()
	contract["managedPoolReplacement"] = map[string]string{
		"kind":              "managed-pool-replacement-v1",
		"activationCommand": "activate-shared-replacement"}
	if requireRebuildCapability(jsonRelay(contract), "claude") != nil {
		t.Fatal("advertised installed Core refused")
	}
	delete(contract, "managedCredentials")
	if requireRebuildCapability(jsonRelay(contract), "claude") == nil ||
		requireRebuildCapability(jsonRelay(contract), "codex") != nil {
		t.Fatal("only a Claude rebuild requires the installed Claude advertisement")
	}
}
