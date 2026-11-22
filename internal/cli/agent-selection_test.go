package cli

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestAgentSelectionPreservesBindingBeforeFreshDefault(t *testing.T) {
	codex := agentBinding{"codex", "gpt-6.1", "linux-rootful-docker-v1", ""}
	claude := agentBinding{"claude", "fixture-model", "fixture-profile", ""}
	calls := 0
	capability := func(provider string) (agentBinding, error) {
		calls++
		if provider == "claude" {
			return claude, nil
		}
		return codex, nil
	}
	fresh, err := selectAgent("", false, nil, capability)
	if err != nil || fresh != codex || calls != 1 {
		t.Fatal(fresh, err, calls)
	}
	inherited, err := selectAgent("", false, &claude, capability)
	if err != nil || inherited != claude || calls != 1 {
		t.Fatal("omission must retain the installed provider without fresh resolution")
	}
	_, err = selectAgent("codex", true, &claude, capability)
	if err == nil || !strings.Contains(err.Error(), "REBUILD") || calls != 1 {
		t.Fatal("explicit conflict must fail before capability/setup effects")
	}
	selected, err := selectAgent("claude", true, nil, capability)
	if err != nil || selected != claude {
		t.Fatal(selected, err)
	}
}

func TestAgentSelectionRejectsIncompleteCapability(t *testing.T) {
	cases := []agentBinding{
		{}, {"claude", "fixture-model", "", ""},
		{"codex", "gpt-6.1", "linux-rootful-docker-v1", ""},
	}
	for _, binding := range cases {
		_, err := selectAgent("claude", true, nil, func(string) (agentBinding, error) {
			return binding, nil
		})
		if err == nil {
			t.Fatal("incomplete or mismatched composition accepted", binding)
		}
	}
	_, err := selectAgent("claude", true, nil, func(string) (agentBinding, error) {
		return agentBinding{}, errors.New("synthetic secret: unavailable producer")
	})
	if err == nil || strings.Contains(err.Error(), "synthetic") {
		t.Fatal("provider diagnostics must remain bounded")
	}
}

func TestAgentOptionRejectsUnknownAndLegacyBeforeEffects(t *testing.T) {
	for _, args := range [][]string{
		{"--agent", "unknown"}, {"--agent="},
		{"--agent", "codex", "--quick", "request"},
		{"--agent", "claude", "--runner-name", "legacy"},
		{"--agent", "codex", "--start-shared-services"},
	} {
		api := initFixture()
		args = append([]string{"init", "--repo", "example/project"}, args...)
		if err := (App{API: api}).Run(args, io.Discard); err == nil {
			t.Fatal("unsupported option combination was accepted", args)
		}
		if len(api.writes) != 0 {
			t.Fatal(api.writes)
		}
	}
}

func TestDesktopAgentValidationAndForwarding(t *testing.T) {
	args := []string{"init", "--repo", "example/project", "--agent=claude"}
	options, err := parseDesktopOptions(args)
	if err != nil || strings.Join(options.Forward, " ") !=
		"init --repo example/project --agent claude" {
		t.Fatal(options, err)
	}
	for _, value := range []string{"", "unknown", "claude;tool"} {
		args[3] = "--agent=" + value
		if _, err := parseDesktopOptions(args); err == nil {
			t.Fatal(value)
		}
	}
	args[0], args[3] = "preflight", "--agent=codex"
	if _, err := parseDesktopOptions(args); err == nil {
		t.Fatal("preflight selection")
	}
}
