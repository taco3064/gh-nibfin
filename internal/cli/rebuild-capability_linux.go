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

func requireRebuildCapability(output []byte) error {
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
	return nil
}

func (a App) requireRebuildProducers() error {
	output, err := a.inspectSelectedProducer(rebuildCapabilityScript)
	if err != nil || requireRebuildCapability(output) != nil {
		return errors.New("REBUILD_SELECTED_CORE_INCOMPATIBLE")
	}
	node, entry, err := trustedSharedCore()
	if err != nil {
		return errors.New("REBUILD_INSTALLED_CORE_UNVERIFIED")
	}
	output, err = a.Process.Run(node, []string{entry, "contract"}, nil)
	if err != nil || requireRebuildCapability(output) != nil {
		return errors.New("REBUILD_INSTALLED_CORE_INCOMPATIBLE")
	}
	// Existing adoption and managed resource preparation require the installed
	// source pin. An ordinary producer upgrade belongs to its maintenance route.
	pin, err := installedSharedPin()
	if err != nil || pin != Pins().Core {
		return errors.New("REBUILD_INSTALLED_SOURCE_REQUIRES_UPGRADE")
	}
	return nil
}
