package cli

import (
	"encoding/json"
	"errors"
)

const rebuildCapabilityScript = `
import { pathToFileURL } from 'node:url';
try {
  const root = pathToFileURL(process.argv[1] + '/');
  const { consumerContract } = await import(new URL('src/public-api.mjs', root));
  process.stdout.write(JSON.stringify(consumerContract));
} catch { process.exitCode = 1; }
`

func requireRebuildCapability(output []byte, provider string) error {
	var contract struct {
		Version     int `json:"schemaVersion"`
		Replacement struct {
			Kind    string `json:"kind"`
			Command string `json:"activationCommand"`
		} `json:"managedPoolReplacement"`
	}
	if len(output) > 65536 || json.Unmarshal(output, &contract) != nil ||
		contract.Version != 1 ||
		contract.Replacement.Kind != "managed-pool-replacement-v1" ||
		contract.Replacement.Command != "activate-shared-replacement" {
		return errors.New("REBUILD_COMPATIBLE_CORE_REQUIRED")
	}
	// Only a Claude replacement needs the installed Core's Claude advertisement.
	if provider == "claude" && requireClaudeContract(output) != nil {
		return errors.New("REBUILD_COMPATIBLE_CORE_REQUIRED")
	}
	return nil
}

func (a App) requireRebuildProducers(provider string) error {
	node, entry, err := trustedSharedCore()
	if err != nil {
		return errors.New("REBUILD_INSTALLED_CORE_UNVERIFIED")
	}
	output, err := a.Process.Run(node, []string{entry, "contract"}, nil)
	if err != nil || requireRebuildCapability(output, provider) != nil {
		return errors.New("REBUILD_INSTALLED_CORE_INCOMPATIBLE")
	}
	return nil
}
