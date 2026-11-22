package cli

import (
	"encoding/json"
	"errors"
)

// The Claude counterpart of agentCapabilityScript. It reads the pinned archive's
// consumer contract and runs Core's finite Claude factory once per advertised
// credential kind. It never invokes the executor or sees a credential.
const claudeCapabilityScript = `
import { pathToFileURL } from 'node:url';
const root = pathToFileURL(process.argv[1] + '/');
try {
  const { consumerContract: c } = await import(new URL('src/public-api.mjs', root));
  const { managedModels } = await import(new URL('src/managed-model.mjs', root));
  const { createProviderBinding } = await import(
    new URL('src/provider-binding.mjs', root));
  const { installedProviderExecutor } = await import(
    new URL('src/installed-provider.mjs', root));
  const kinds = c.managedCredentials?.claude?.credentialKinds;
  if (!Array.isArray(kinds) || kinds.length === 0) throw new Error();
  const image = 'sha256:' + '0'.repeat(64);
  const isolation = { kind: 'linux-rootful-docker-v1',
    agent: { image }, hooks: { image } };
  const host = { schemaVersion: 1, kind: 'single-host-linux-v1',
    memberId: 1, model: managedModels.claude, authFile: '/nonexistent',
    installation: { pin: { repository: 'taco3064/nibfin-core',
      commit: '0'.repeat(40), manifestHash: '0'.repeat(64) } },
    repository: { isolation }, sharedRunner: {
      runner: { repository: 'fixture/pool', repositoryId: 1,
        runnerId: 1, runnerName: 'one' },
      additionalRunner: { id: 2, name: 'two', isolation } } };
  let binding;
  for (const credentialKind of kinds) {
    host.providerBinding = createProviderBinding(host, 'claude', { credentialKind });
    if (typeof installedProviderExecutor(host) !== 'function') throw new Error();
    binding = host.providerBinding;
  }
  process.stdout.write(JSON.stringify({ binding: { provider: binding.provider,
    model: binding.modelProfile.model, profile: binding.components.hostProfile },
    contract: { schemaVersion: c.schemaVersion,
      managedResourcePreparation: c.managedResourcePreparation,
      managedCredentials: c.managedCredentials } }));
} catch { process.exitCode = 1; }
`

func (a App) claudeCapability() (agentBinding, error) {
	output, err := a.inspectSelectedProducer(claudeCapabilityScript)
	var probe struct {
		Binding  agentBinding    `json:"binding"`
		Contract json.RawMessage `json:"contract"`
	}
	// The output is about 1 KB; the bound is the installed agent read-back's.
	if err != nil || len(output) > 16384 || json.Unmarshal(output, &probe) != nil ||
		requireClaudeContract(probe.Contract) != nil || !probe.Binding.valid() ||
		probe.Binding.Provider != "claude" || probe.Binding.CredentialKind != "" {
		return agentBinding{}, errors.New("AGENT_COMPOSITION_UNAVAILABLE")
	}
	return probe.Binding, nil
}
