package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Read the maintained package interfaces from an authenticated producer archive.
// This is a disposable source inspection, never a host or candidate installation.
const agentCapabilityScript = `
import { pathToFileURL } from 'node:url';
const root = pathToFileURL(process.argv[1] + '/');
try {
  const { consumerContract: c } = await import(new URL('src/public-api.mjs', root));
  const { managedModel } = await import(new URL('src/managed-model.mjs', root));
  const { createProviderBinding } = await import(
    new URL('src/provider-binding.mjs', root));
  const { installedProviderExecutor } = await import(
    new URL('src/installed-provider.mjs', root));
  // This adapter supports the existing finite Codex setup protocol only.
  // claudeModel describes a component and is deliberately not consulted.
  if (c.schemaVersion !== 1 ||
      c.managedResourcePreparation?.kind !== 'linux-managed-resources-v1' ||
      c.managedResourcePreparation.executable !==
        '/opt/nibfin/core/bin/nibfin-prepare-shared-host.mjs' ||
      JSON.stringify(c.managedResourcePreparation.arguments) !==
        JSON.stringify(['--consent-resource-preparation']))
    throw new Error();
  // A structural capability probe calls Core's validation and finite factory.
  // It never invokes the returned executor or supplies live credentials.
  const image = 'sha256:' + '0'.repeat(64);
  const isolation = { kind: 'linux-rootful-docker-v1',
    agent: { image }, hooks: { image } };
  const host = { schemaVersion: 1, kind: 'single-host-linux-v1',
    memberId: 1, model: managedModel, authFile: '/nonexistent',
    installation: { pin: { repository: 'taco3064/nibfin-core',
      commit: '0'.repeat(40), manifestHash: '0'.repeat(64) } },
    repository: { isolation }, sharedRunner: {
      runner: { repository: 'fixture/pool', repositoryId: 1,
        runnerId: 1, runnerName: 'one' },
      additionalRunner: { id: 2, name: 'two', isolation } } };
  host.providerBinding = createProviderBinding(host, 'codex');
  if (typeof installedProviderExecutor(host) !== 'function') throw new Error();
  const binding = host.providerBinding;
  process.stdout.write(JSON.stringify({ provider: binding.provider,
    model: binding.modelProfile.model, profile: binding.components.hostProfile }));
} catch { process.exitCode = 1; }
`

func (a App) agentCapability(provider string) (binding agentBinding, err error) {
	if a.agents != nil {
		return a.agents.capability(provider)
	}
	// No installed Claude composition/setup interface exists in the producer.
	// In particular a component advertisement cannot authorize credential intake.
	if provider != "codex" {
		return binding, errors.New("AGENT_COMPOSITION_UNAVAILABLE")
	}
	output, err := a.inspectSelectedProducer(agentCapabilityScript)
	if err != nil || len(output) > 1024 || json.Unmarshal(output, &binding) != nil ||
		!binding.valid() || binding.Provider != provider {
		return agentBinding{}, errors.New("AGENT_COMPOSITION_UNAVAILABLE")
	}
	return binding, nil
}

func (a App) inspectSelectedProducer(script string) (output []byte, err error) {
	archive, err := candidateArchive(a.API, Pins())
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "nibfin-provider-")
	if err != nil {
		return nil, errors.New("AGENT_SOURCE_UNAVAILABLE")
	}
	defer func() {
		if os.RemoveAll(dir) != nil {
			err = errors.New("AGENT_SOURCE_CLEANUP_INCOMPLETE")
		}
	}()
	if err = unpackCore(archive, dir); err != nil {
		return nil, err
	}
	version, err := os.ReadFile(filepath.Join(dir, ".node-version"))
	if err != nil || a.Process == nil {
		return nil, errors.New("AGENT_SOURCE_UNAVAILABLE")
	}
	actual, err := a.Process.Run("node", []string{"--version"}, nil)
	if err != nil || strings.TrimSpace(string(actual)) !=
		"v"+strings.TrimSpace(string(version)) {
		return nil, errors.New("CORE_PINNED_NODE_REQUIRED")
	}
	return a.Process.Run("node", []string{"--input-type=module", "--eval",
		script, dir}, nil)
}
