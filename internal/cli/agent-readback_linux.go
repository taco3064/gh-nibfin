package cli

import (
	"errors"
	"slices"
)

// Fixed read-only bridge. Core owns all provider and adoption validation.
// The CLI never receives the host document or provider credential paths.
const installedAgentScript = `
import { installedProvider } from '/opt/nibfin/core/src/provider-binding.mjs';
import { inspectSharedHost } from '/opt/nibfin/core/src/shared-host-inspect.mjs';
import { boundedHostFile, hostFile } from '/opt/nibfin/core/src/host-adoption.mjs';
import { rootOwnedPath } from '/opt/nibfin/core/src/host-paths.mjs';
try {
  await rootOwnedPath(hostFile);
  const before = await boundedHostFile(hostFile, 262144);
  const host = JSON.parse(before);
  const binding = installedProvider(host);
  const pool = await inspectSharedHost();
  if (await boundedHostFile(hostFile, 262144) !== before) throw new Error();
  process.stdout.write(JSON.stringify({ ...pool, agent: {` +
	installedAgentProjection + ` } }));
} catch { process.exitCode = 1; }
`

// The non-secret binding fields both host read-backs project. A Claude pool adds
// its stored credential kind; the Codex projection keeps its original fields.
const installedAgentProjection = `
    provider: binding.provider, model: binding.modelProfile.model,
    profile: binding.components.hostProfile,
    ...binding.provider === 'claude' && { credentialKind: binding.credentialKind }`

func readInstalledAgent(process Process, host *sharedHost) error {
	node, _, err := trustedSharedCore()
	if err != nil {
		return err
	}
	const env = "/usr/bin/env"
	if err := requireRootOwnedHostPath(env); err != nil {
		return err
	}
	output, err := runSharedAdministrator(process, env, []string{
		"-i", "PATH=/usr/bin:/bin", "LANG=C.UTF-8", node,
		"--input-type=module", "--eval", installedAgentScript,
	}, nil)
	if err != nil || len(output) > 16384 {
		return errors.New("INSTALLED_AGENT_READBACK_REQUIRED")
	}
	observed, err := decodeSharedHost(output)
	if err != nil || observed.Agent == nil || !observed.Agent.valid() ||
		observed.SharedExecution != host.SharedExecution ||
		observed.MemberID != host.MemberID ||
		!slices.Equal(observed.Runners, host.Runners) ||
		!slices.Equal(observed.AdoptedRepositoryIDs, host.AdoptedRepositoryIDs) {
		return errors.New("INSTALLED_AGENT_READBACK_REQUIRED")
	}
	host.Agent = observed.Agent
	return nil
}

func verifyPlannedAgent(plan InitPlan, host *sharedHost) error {
	if plan.Agent == nil {
		return nil
	}
	if host == nil || !sameAgent(plan.Agent, host.Agent) ||
		host.RepositoryID != plan.Execution.RepositoryID || host.MemberID != plan.Member.ID {
		return errors.New("INSTALLED_AGENT_PLAN_MISMATCH")
	}
	return nil
}
