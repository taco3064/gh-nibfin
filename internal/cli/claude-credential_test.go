package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

// Synthetic values only: they match the published shapes, never a real account.
var (
	fixtureClaudeToken = "sk-ant-oat01-" + strings.Repeat("a", 40)
	fixtureClaudeKey   = "sk-ant-api03-" + strings.Repeat("b", 40)
)

type fixtureTerminal struct {
	in  *bytes.Reader
	out bytes.Buffer
}

func (t *fixtureTerminal) Read(p []byte) (int, error)  { return t.in.Read(p) }
func (t *fixtureTerminal) Write(p []byte) (int, error) { return t.out.Write(p) }

func newFixtureTerminal(input string) *fixtureTerminal {
	return &fixtureTerminal{in: bytes.NewReader([]byte(input))}
}

func TestClaudeCredentialKindsAndMaskedSummary(t *testing.T) {
	for value, kind := range map[string]string{
		fixtureClaudeToken: "claude-subscription-token",
		fixtureClaudeKey:   "anthropic-api-key",
	} {
		got, err := classifyClaudeCredential([]byte(value))
		if err != nil || got != kind {
			t.Fatal(value[:12], got, err)
		}
		summary := claudeCredentialSummary(claudeCredential{kind, []byte(value)})
		if !strings.Contains(summary, kind) || !strings.Contains(summary, value[:12]) ||
			!strings.Contains(summary, "53 characters") ||
			strings.Contains(summary, value[13:20]) {
			t.Fatal("summary must show only kind, prefix and length", summary)
		}
	}
	for _, value := range []string{"", "sk-ant-oat01-short", "sk-ant-xyz01-" +
		strings.Repeat("a", 40), "sk-ant-oat1-" + strings.Repeat("a", 40),
		fixtureClaudeToken + " x", "ghp_" + strings.Repeat("a", 40)} {
		if _, err := classifyClaudeCredential([]byte(value)); err == nil {
			t.Fatal("unrecognized credential accepted", value)
		}
	}
}

func TestClaudeCredentialPromptIsHiddenAndConfirmed(t *testing.T) {
	// A typo erased with backspace and surrounding spaces are not part of it.
	tty := newFixtureTerminal(" " + fixtureClaudeToken + "x\x7f \ry\r")
	credential, err := promptClaudeCredential(tty)
	if err != nil || credential.kind != "claude-subscription-token" ||
		string(credential.value) != fixtureClaudeToken {
		t.Fatal(err)
	}
	if strings.Contains(tty.out.String(), fixtureClaudeToken) ||
		!strings.Contains(tty.out.String(), "claude-subscription-token sk-ant-oat01") {
		t.Fatal("terminal output must carry only the masked summary", tty.out.String())
	}
	value := credential.value
	credential.clear()
	if !bytes.Equal(value, make([]byte, len(value))) {
		t.Fatal("cleared credential retained its value")
	}
	for name, input := range map[string]string{
		"declined":     fixtureClaudeKey + "\rn\r",
		"interrupted":  fixtureClaudeKey + "\x03",
		"end of input": fixtureClaudeKey,
		"unknown kind": "not-a-claude-credential\ry\r",
		"oversized":    strings.Repeat("a", claudeCredentialLimit+1) + "\ry\r",
	} {
		if _, err := promptClaudeCredential(newFixtureTerminal(input)); err == nil {
			t.Fatal("prompt accepted", name)
		}
	}
}

func TestClaudeCredentialEnvelopeCarriesOnlyClaude(t *testing.T) {
	token := []byte("gho_" + strings.Repeat("c", 36))
	data, err := claudeCredentialEnvelope(token,
		claudeCredential{"anthropic-api-key", []byte(fixtureClaudeKey)})
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if json.Unmarshal(data, &envelope) != nil || len(envelope) != 3 ||
		envelope["schemaVersion"] != float64(1) ||
		envelope["githubToken"] != string(token) || envelope["claude"] != fixtureClaudeKey {
		t.Fatal(string(data))
	}
	_, err = claudeCredentialEnvelope(token,
		claudeCredential{"claude-subscription-token", []byte(fixtureClaudeKey)})
	if err == nil {
		t.Fatal("kind and value disagree")
	}
}

type fixtureCredentialAdmin struct {
	results map[string]string
	pending map[string]bool
	calls   []string
	input   []byte
}

func (f *fixtureCredentialAdmin) run(input []byte, operation string) ([]byte, error) {
	f.calls = append(f.calls, operation)
	if input != nil {
		f.input = slices.Clone(input)
	}
	if f.pending[operation] {
		return []byte(f.results[operation]), errors.New("CORE_PENDING_RESULT")
	}
	return []byte(f.results[operation]), nil
}

func TestClaudeCredentialTransferRenewsOrSwitches(t *testing.T) {
	active := `{"status":"active","memberId":7}`
	cases := []struct {
		name, status, disconnect string
		pending                  bool
		calls                    string
		code                     string
	}{
		{"fresh", `{"status":"manual"}`, "", false, "status,intake", ""},
		{"disconnected", `{"status":"disconnected","memberId":7}`, "", false,
			"status,intake", ""},
		{"same kind", `{"status":"active","memberId":7,` +
			`"credentialKind":"claude-subscription-token"}`, "", false,
			"status,replace", ""},
		{"codex host", `{"status":"active","phase":"active","memberId":7}`,
			`{"status":"disconnected"}`, false, "status,disconnect,intake", ""},
		{"other claude kind", `{"status":"active","memberId":7,` +
			`"credentialKind":"anthropic-api-key"}`, `{"status":"disconnected"}`,
			false, "status,disconnect,intake", ""},
		{"resumed disconnect", `{"status":"pending","phase":"disconnecting",` +
			`"memberId":7}`, `{"status":"disconnected"}`, false,
			"status,disconnect,intake", ""},
		{"settlement pending", active, `{"status":"pending",` +
			`"code":"MANAGED_CREDENTIAL_SETTLEMENT_PENDING"}`, true,
			"status,disconnect", "MANAGED_CREDENTIAL_SETTLEMENT_PENDING_RESUME_SAME_COMMAND"},
		{"installing", `{"status":"pending","phase":"installing","memberId":7}`, "",
			false, "status", "MANAGED_CREDENTIAL_STATUS_REQUIRES_READBACK"},
		{"other member", `{"status":"active","memberId":8}`, "", false, "status",
			"MANAGED_CREDENTIAL_STATUS_REQUIRES_READBACK"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			admin := &fixtureCredentialAdmin{results: map[string]string{
				"status": c.status, "disconnect": c.disconnect,
				"intake": active, "replace": active},
				pending: map[string]bool{"disconnect": c.pending}}
			plan := InitPlan{Member: User{ID: 7}, Agent: &agentBinding{"claude",
				"fixture-model", "fixture-profile", "claude-subscription-token"}}
			err := transferClaudeCredential(admin.run, plan, []byte("gho_fixture"),
				claudeCredential{"claude-subscription-token", []byte(fixtureClaudeToken)})
			if strings.Join(admin.calls, ",") != c.calls ||
				c.code == "" && err != nil || c.code != "" && (err == nil ||
				err.Error() != c.code) {
				t.Fatal(admin.calls, err)
			}
			if c.code == "" && (!bytes.Contains(admin.input, []byte(fixtureClaudeToken)) ||
				bytes.Contains(admin.input, []byte(`"codex"`))) {
				t.Fatal("transfer must use the Claude envelope only")
			}
		})
	}
}

func TestClaudeCredentialTransferRefusesBeforeHostEffects(t *testing.T) {
	admin := &fixtureCredentialAdmin{results: map[string]string{}}
	credential := claudeCredential{"anthropic-api-key", []byte(fixtureClaudeKey)}
	for _, agent := range []*agentBinding{nil,
		{"codex", "gpt-6.1", "linux-rootful-docker-v1", ""},
		{"claude", "fixture-model", "fixture-profile", "claude-subscription-token"}} {
		plan := InitPlan{Member: User{ID: 7}, Agent: agent}
		if transferClaudeCredential(admin.run, plan, nil, credential) == nil {
			t.Fatal("plan and credential kind must agree", agent)
		}
	}
	if len(admin.calls) != 0 {
		t.Fatal(admin.calls)
	}
	admin.results = map[string]string{"status": `{"status":"manual"}`,
		"intake": `{"status":"denied","code":"MANAGED_CLAUDE_CREDENTIAL_INVALID"}`}
	plan := InitPlan{Member: User{ID: 7}, Agent: &agentBinding{"claude",
		"fixture-model", "fixture-profile", "anthropic-api-key"}}
	err := transferClaudeCredential(admin.run, plan, nil, credential)
	if err == nil || err.Error() != "MANAGED_CLAUDE_CREDENTIAL_INVALID" {
		t.Fatal(err)
	}
}

func fixtureClaudeContract() map[string]any {
	return map[string]any{"schemaVersion": 1,
		"managedResourcePreparation": map[string]any{
			"kind":       "linux-managed-resources-v1",
			"executable": "/opt/nibfin/core/bin/nibfin-prepare-shared-host.mjs",
			"arguments":  []string{"--consent-resource-preparation"},
			"agents": map[string]any{"claude": map[string]any{
				"arguments": []string{"--consent-resource-preparation", "--agent=claude"},
				"imageKeys": []string{"agent", "hooks", "claudeAgent"}}}},
		"managedCredentials": map[string]any{"claude": map[string]any{
			"envelopeKey": "claude", "credentialKinds": []string{
				"claude-subscription-token", "anthropic-api-key"}}},
	}
}

func TestClaudeContractRequiresExactAdvertisement(t *testing.T) {
	if err := requireClaudeContract(jsonRelay(fixtureClaudeContract())); err != nil {
		t.Fatal(err)
	}
	claude := func(c map[string]any) map[string]any {
		preparation := c["managedResourcePreparation"].(map[string]any)
		return preparation["agents"].(map[string]any)["claude"].(map[string]any)
	}
	credentials := func(c map[string]any) map[string]any {
		return c["managedCredentials"].(map[string]any)["claude"].(map[string]any)
	}
	for name, change := range map[string]func(map[string]any){
		"version": func(c map[string]any) { c["schemaVersion"] = 2 },
		"absent":  func(c map[string]any) { delete(c, "managedCredentials") },
		"reversed": func(c map[string]any) {
			claude(c)["arguments"] = []string{"--agent=claude",
				"--consent-resource-preparation"}
		},
		"images": func(c map[string]any) {
			claude(c)["imageKeys"] = []string{"agent", "hooks"}
		},
		"envelope": func(c map[string]any) { credentials(c)["envelopeKey"] = "codex" },
		"extra kind": func(c map[string]any) {
			credentials(c)["credentialKinds"] = []string{"claude-subscription-token",
				"anthropic-api-key", "unknown"}
		},
		"duplicate kind": func(c map[string]any) {
			credentials(c)["credentialKinds"] = []string{"anthropic-api-key",
				"anthropic-api-key"}
		},
	} {
		contract := fixtureClaudeContract()
		change(contract)
		if requireClaudeContract(jsonRelay(contract)) == nil {
			t.Fatal("incomplete Claude advertisement accepted", name)
		}
	}
}

func TestAgentBindingKindAndProductionInitRefusal(t *testing.T) {
	for binding, valid := range map[agentBinding]bool{
		{"claude", "m", "p", "anthropic-api-key"}: true,
		{"claude", "m", "p", ""}:                  true,
		{"claude", "m", "p", "unknown"}:           false,
		{"codex", "m", "p", "anthropic-api-key"}:  false,
	} {
		if binding.valid() != valid || binding.composition().CredentialKind != "" {
			t.Fatal(binding)
		}
	}
	if _, err := (App{}).initAgentCapability("claude"); err == nil {
		t.Fatal("production init must keep refusing Claude")
	}
}
