import fs from 'node:fs/promises';
import { join } from 'node:path';
import { nativePoolStore } from './native-pool-store.mjs';
import { readPrivate } from './local-state.mjs';
import { observeUnusedNativePaths } from './native-staged-transition.mjs';
const need = (ok, reason) => {
  if (!ok) throw Error('native-drained-work:' + reason);
};

// Actual project Run readback and physical resources, shared by used-host owners.
export async function requireDrainedNativeWork(
  core,
  {
    root,
    configuration,
    memberId,
    provider,
    verifyRunner,
    observeResources = observeUnusedNativePaths,
  },
) {
  const pool = nativePoolStore(core, { root, configuration });
  const resources = [
    join(root, 'jobs'),
    ...Array.from({ length: configuration.capacity }, (_, i) =>
      join(root, 'runner-' + (i + 1)),
    ),
  ];

  const status = await pool.status();
  const ledger = await readPrivate(join(root, 'pool-state/ledger.json'), {
    limit: 33554432,
  });
  need(
    status.occupied === 0 &&
      status.pending === 0 &&
      Object.values(ledger.jobs).every((job) => job.phase === 'released'),
    'original-jobs-not-released',
  );
  const dispatches = join(root, 'dispatches');
  for (const name of await fs.readdir(dispatches)) {
    need(
      /^(?:native-[a-f0-9-]{36}|rescue-[a-f0-9]{64})\.json$/.test(name),
      'dispatch-inventory',
    );
    const claim = await readPrivate(join(dispatches, name));
    const { identity, ...body } = claim;
    const target = configuration.targets.find(
      (t) =>
        t.repositoryId === claim.repositoryId && t.repository === claim.repository,
    );
    need(
      identity === core.contentIdentity(body) &&
        target &&
        claim.phase === 'completed' &&
        claim.runId,
      'dispatch-not-completed',
    );
    const result = await provider.request({
      endpoint: '/repos/' + target.repository + '/actions/runs/' + claim.runId,
      target: target.repositoryId,
      operation: 'native-repair-read',
      correlation: claim.id,
      signal: AbortSignal.timeout(30000),
    });
    const run = result.data;
    need(
      result.outcome === 'read' &&
        run.id === claim.runId &&
        run.status === 'completed' &&
        run.conclusion === claim.conclusion &&
        run.repository?.id === target.repositoryId &&
        run.head_repository?.id === target.repositoryId &&
        run.actor?.id === memberId &&
        run.triggering_actor?.id === memberId &&
        run.head_sha === claim.sourceCommit &&
        run.run_attempt === 1,
      'dispatch-official-readback',
    );
  }
  for (let i = 1; i <= configuration.capacity; i++)
    await verifyRunner(join(root, 'runner-' + i), { unregistered: true });
  need((await observeResources(resources)).status === 'unused', 'resources');

  return { status: 'drained', resources };
}
