package cli

import (
	"encoding/json"
	"errors"
	"slices"
)

// Read fixed Core-owned files through maintained Core validation. Neither host
// documents nor retirement history/credential locations cross this projection.
const rebuildSnapshotScript = `
import { boundedHostFile, hostFile } from '/opt/nibfin/core/src/host-adoption.mjs';
import { rootOwnedPath } from '/opt/nibfin/core/src/host-paths.mjs';
import { inspectSharedHost } from '/opt/nibfin/core/src/shared-host-inspect.mjs';
import { installedProvider } from '/opt/nibfin/core/src/provider-binding.mjs';
import { sharedRepository } from '/opt/nibfin/core/src/shared-runner.mjs';
import { readRetirementFile } from '/opt/nibfin/core/src/retirement-files.mjs';
import { retirementPath, drainPath } from '/opt/nibfin/core/src/retirement-admission.mjs';
try {
  await rootOwnedPath(hostFile);
  const bytes = await boundedHostFile(hostFile, 262144);
  const host = JSON.parse(bytes), pool = await inspectSharedHost();
  const binding = installedProvider(host);
  const peers = new Map();
  const read = async path => {
    await rootOwnedPath(path);
    const bytes = await boundedHostFile(path, 262144);
    peers.set(path, bytes);
    return JSON.parse(bytes);
  };
  const repositories = [];
  for (const id of pool.adoptedRepositoryIds) {
    const peer = await sharedRepository(host, id, read);
    repositories.push({ repository: peer.repository.approval.repository,
      repositoryId: id });
  }
  const state = await readRetirementFile(retirementPath);
  const project = value => value ? { schemaVersion: 1,
    repository: value.repository, repositoryId: value.repositoryId,
    memberId: value.memberId,
    runners: value.runners.map(r => ({ id: r.id, name: r.name })) } : null;
  const retirement = state ? { id: state.id, phase: state.phase,
    settled: state.pending === null,
    pool: project(state.binding.pool), repositories: state.binding.repositories.map(r =>
      ({ repository: r.repository, repositoryId: r.repositoryId })),
    replacement: project(state.replacement?.pool),
    release: state.replacement?.phase ?? '' } : null;
  for (const [path, before] of peers)
    if (await boundedHostFile(path, 262144) !== before) throw new Error();
  if (await boundedHostFile(hostFile, 262144) !== bytes) throw new Error();
  if (JSON.stringify(await readRetirementFile(retirementPath)) !==
      JSON.stringify(state)) throw new Error();
  const draining = await readRetirementFile(drainPath) !== null;
  process.stdout.write(JSON.stringify({ draining, host: { ...pool, agent: {` +
	installedAgentProjection + ` } }, repositories, retirement }));
} catch { process.exitCode = 1; }
`

func readRebuildSnapshot(process Process) (rebuildSnapshot, error) {
	var snapshot rebuildSnapshot
	node, _, err := trustedSharedCore()
	if err != nil {
		return snapshot, err
	}
	const env = "/usr/bin/env"
	if err := requireRootOwnedHostPath(env); err != nil {
		return snapshot, err
	}
	output, err := runSharedAdministrator(process, env, []string{
		"-i", "PATH=/usr/bin:/bin", "LANG=C.UTF-8", node,
		"--input-type=module", "--eval", rebuildSnapshotScript,
	}, nil)
	if err != nil || len(output) > 65536 ||
		json.Unmarshal(output, &snapshot) != nil || !validRebuildSnapshot(snapshot) {
		return rebuildSnapshot{}, errors.New("REBUILD_SNAPSHOT_UNVERIFIED")
	}
	return snapshot, nil
}

func validRebuildSnapshot(s rebuildSnapshot) bool {
	if !validRebuildPool(s.Host) || s.Host.Agent == nil || !s.Host.Agent.valid() ||
		!validRebuildRepositories(s.Repositories, s.Host.RepositoryID) ||
		len(s.Host.AdoptedRepositoryIDs) != len(s.Repositories) {
		return false
	}
	for _, repo := range s.Repositories {
		if !slices.Contains(s.Host.AdoptedRepositoryIDs, repo.ID) {
			return false
		}
	}
	if r := s.Retirement; r != nil {
		if !rebuildID.MatchString(r.ID) || !validRebuildPool(r.Pool) ||
			!validRebuildRepositories(r.Repositories, r.Pool.RepositoryID) ||
			!slices.Contains([]string{"draining", "settling", "removal", "cleanup",
				"retired"}, r.Phase) {
			return false
		}
		if !slices.Contains([]string{"", "publishing", "prepared", "released"}, r.Release) ||
			r.Replacement != nil && !validRebuildPool(r.Replacement) ||
			r.Release != "" && r.Replacement == nil {
			return false
		}
	}
	return true
}

func validRebuildPool(pool *sharedHost) bool {
	if pool == nil {
		return false
	}
	bytes, err := json.Marshal(pool)
	_, invalid := decodeSharedHost(bytes)
	return err == nil && invalid == nil
}

func validRebuildRepositories(repos []preparationRepository, primary int64) bool {
	if len(repos) == 0 || len(repos) > 10 {
		return false
	}
	ids, names := map[int64]bool{}, map[string]bool{}
	for _, repo := range repos {
		if repo.ID <= 0 || !repoPattern.MatchString(repo.Repository) ||
			ids[repo.ID] || names[repo.Repository] {
			return false
		}
		ids[repo.ID], names[repo.Repository] = true, true
	}
	return ids[primary]
}

func sameRebuildPool(a, b *sharedHost) bool {
	return a != nil && b != nil && a.SharedExecution == b.SharedExecution &&
		a.MemberID == b.MemberID && slices.Equal(a.Runners, b.Runners)
}

func sameRebuildRepositories(a, b []preparationRepository) bool {
	if len(a) != len(b) {
		return false
	}
	for _, repo := range a {
		if !slices.Contains(b, repo) {
			return false
		}
	}
	return true
}
