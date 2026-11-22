package cli

import (
	"encoding/json"
	"errors"
	"slices"
)

// The Claude request vector and result image keys Core advertises at
// consumerContract.managedResourcePreparation.agents.claude. Rebuild sends
// exactly this vector, so a Core advertising anything else is refused.
var (
	claudePreparationArguments = []string{"--consent-resource-preparation",
		"--agent=claude"}
	claudePreparationImageKeys = []string{"agent", "hooks", "claudeAgent"}
)

// Each Claude credential kind Core accepts, with the fixed prefix that names it.
// Core's intake remains the authority for the full value; this local check only
// names the kind before anything leaves the terminal.
var claudeCredentialKinds = []struct{ kind, prefix string }{
	{"claude-subscription-token", "sk-ant-oat"},
	{"anthropic-api-key", "sk-ant-api"},
}

func knownClaudeCredentialKind(kind string) bool {
	return slices.ContainsFunc(claudeCredentialKinds,
		func(k struct{ kind, prefix string }) bool { return k.kind == kind })
}

// requireClaudeContract checks the additive Claude advertisement in a decoded
// consumerContract. The embedded-archive probe and the installed Core both use it.
func requireClaudeContract(output []byte) error {
	var contract struct {
		Version     int `json:"schemaVersion"`
		Preparation struct {
			Kind       string `json:"kind"`
			Executable string `json:"executable"`
			Agents     struct {
				Claude struct {
					Arguments []string `json:"arguments"`
					ImageKeys []string `json:"imageKeys"`
				} `json:"claude"`
			} `json:"agents"`
		} `json:"managedResourcePreparation"`
		Credentials struct {
			Claude struct {
				EnvelopeKey string   `json:"envelopeKey"`
				Kinds       []string `json:"credentialKinds"`
			} `json:"claude"`
		} `json:"managedCredentials"`
	}
	if len(output) > 65536 || json.Unmarshal(output, &contract) != nil {
		return errors.New("CLAUDE_CORE_ADVERTISEMENT_REQUIRED")
	}
	p, c := contract.Preparation, contract.Credentials.Claude
	// The advertised kinds must be exactly the kinds this CLI can name.
	kinds := make([]string, 0, len(claudeCredentialKinds))
	for _, k := range claudeCredentialKinds {
		kinds = append(kinds, k.kind)
	}
	slices.Sort(kinds)
	if contract.Version != 1 || p.Kind != "linux-managed-resources-v1" ||
		p.Executable != "/opt/nibfin/core/bin/nibfin-prepare-shared-host.mjs" ||
		!slices.Equal(p.Agents.Claude.Arguments, claudePreparationArguments) ||
		!slices.Equal(p.Agents.Claude.ImageKeys, claudePreparationImageKeys) ||
		c.EnvelopeKey != "claude" ||
		!slices.Equal(slices.Sorted(slices.Values(c.Kinds)), kinds) {
		return errors.New("CLAUDE_CORE_ADVERTISEMENT_REQUIRED")
	}
	return nil
}
