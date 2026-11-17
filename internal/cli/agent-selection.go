package cli

import (
	"errors"
	"regexp"
)

// Only bounded, non-secret provider identity crosses the CLI orchestration boundary.
type agentBinding struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Profile  string `json:"profile"`
}

var agentField = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

func (b agentBinding) valid() bool {
	return (b.Provider == "codex" || b.Provider == "claude") &&
		agentField.MatchString(b.Model) && agentField.MatchString(b.Profile)
}

func validateAgentOption(value string, explicit bool) error {
	if explicit && value != "codex" && value != "claude" {
		return errors.New("UNKNOWN_AGENT_USE_CODEX_OR_CLAUDE")
	}
	return nil
}

// The fixture boundary is private and callable only by in-package drivers.
// No flag, environment variable or producer-supplied path can install an adapter.
type agentOperations struct {
	capability func(string) (agentBinding, error)
}

func selectAgent(value string, explicit bool, existing *agentBinding,
	capability func(string) (agentBinding, error)) (agentBinding, error) {
	if err := validateAgentOption(value, explicit); err != nil {
		return agentBinding{}, err
	}
	if existing != nil {
		if !existing.valid() {
			return agentBinding{}, errors.New("INSTALLED_AGENT_READBACK_REQUIRED")
		}
		if explicit && value != existing.Provider {
			return agentBinding{}, errors.New(
				"AGENT_MISMATCH_USE_RUNNER_REBUILD_WITH_AGENT")
		}
		return *existing, nil
	}
	if !explicit {
		value = "codex"
	}
	binding, err := capability(value)
	if err != nil || !binding.valid() || binding.Provider != value {
		return agentBinding{}, errors.New(
			"AGENT_COMPOSITION_UNAVAILABLE_USE_COMPATIBLE_CORE_OR_CODEX")
	}
	return binding, nil
}

func sameAgent(a, b *agentBinding) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func compatibleIntentAgent(saved, planned *agentBinding) bool {
	// Legacy durable intent predates provider selection and can only denote Codex.
	return sameAgent(saved, planned) || saved == nil &&
		planned != nil && planned.Provider == "codex"
}
