import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { join } from 'node:path';
import { readPrivate, writePrivate, sha } from '../../src/local-state.mjs';
import { openFileHostStore } from '../../src/host-store.mjs';
import { nativePoolStore } from '../../src/native-pool-store.mjs';
import { managedUpgradeLayout } from '../../src/managed-upgrade.mjs';
import { coordinateUpgrade } from '../../src/upgrade.mjs';
import {
  stageCompletedNativeUpgrade,
  completeCompletedNativeUpgrade,
} from '../../src/native-completed-upgrade.mjs';

// Real completed receipts, runtime bytes, sealed filesystem/pin switch and CAS.
// Provider, OS resource observations and services are explicit fixture doubles.
export async function completedUpgradeFixture(core, context, { root, state }) {
  const previous = await readPrivate(join(root, 'native-installation.json'));
  const source = join(root, 'runtime');
  const versionPath = 'client/version.mjs';
  const bytes = Buffer.concat([
    await fs.readFile(join(source, versionPath)),
    Buffer.from('\n// Next CLI fixture revision.\n'),
  ]);
  const next = {
    ...previous,
    runtimeInventory: previous.runtimeInventory.map((file) =>
      file.path === versionPath ? { ...file, sha256: sha(bytes) } : file,
    ),
  };
  const configuration = previous.executionPool;
  const pool = nativePoolStore(core, { root, configuration });
  const id = 'native-00000000-0000-4000-8000-000000000010';
  const request = {
    id,
    repository: 'example/project',
    repositoryId: '100',
    runId: '5000',
    jobId: '5001',
    attempt: '1',
  };
  const job = { request, phase: 'released' };
  assert.equal(
    (
      await pool.jobs.write({
        id,
        expected: null,
        value: { ...job, identity: core.contentIdentity(job) },
      })
    ).status,
    'retained',
  );
  await fs.mkdir(join(root, 'dispatches'), { recursive: true, mode: 0o700 });
  const claim = {
    id,
    repository: request.repository,
    repositoryId: '100',
    phase: 'completed',
    runId: '5000',
    conclusion: 'success',
    sourceCommit: 'b'.repeat(40),
  };
  await writePrivate(join(root, 'dispatches', id + '.json'), {
    ...claim,
    identity: core.contentIdentity(claim),
  });
  const provider = {
    request: async (input) => {
      assert.equal(input.endpoint, '/repos/example/project/actions/runs/5000');
      assert.equal(input.target, '100');
      return {
        outcome: 'read',
        data: {
          id: '5000',
          status: 'completed',
          conclusion: 'success',
          run_attempt: 1,
          repository: { id: '100' },
          head_repository: { id: '100' },
          actor: { id: '7' },
          triggering_actor: { id: '7' },
          head_sha: 'b'.repeat(40),
        },
      };
    },
  };
  const store = await openFileHostStore(core, { directory: join(root, 'state') });
  const cyclePath = join(
    root,
    'delivery',
    state.target.repositoryId,
    state.target.databaseId,
    'state.json',
  );
  const originalCycle = await fs.readFile(cyclePath);
  const work = (await store.read()).work;
  let staged = 0;
  const options = {
    root,
    previous,
    next,
    context,
    memberId: '7',
    provider,
    observeResources: async () => ({ status: 'unused' }),
    verifyRunner: async () => {},
    control: {
      stageUnit: async () => {
        staged++;
      },
    },
    stage: async () => {
      await fs.cp(source, join(root, 'runtime-next'), { recursive: true });
      await fs.writeFile(join(root, 'runtime-next', versionPath), bytes);
    },
  };
  try {
    const before = await store.read();
    await store.updateWork({
      expected: before.identity,
      work: { ...before.work, hold: { reason: 'Owner hold' } },
    });
    await assert.rejects(stageCompletedNativeUpgrade(core, options), /restriction/);
    assert.equal(staged, 0);
    const held = await store.read();
    await store.updateWork({ expected: held.identity, work });
    assert.equal((await stageCompletedNativeUpgrade(core, options)).status, 'staged');
    assert.equal(staged, 1);
    assert.deepEqual((await store.read()).work, { ...work, paused: true });
    assert.deepEqual(await fs.readFile(cyclePath), originalCycle);
    const plan = await readPrivate(join(root, 'upgrade-plan.json'), {
      limit: 4000000,
    });
    const layout = managedUpgradeLayout(core, root, plan);
    const services = new Map();
    const result = await coordinateUpgrade({
      core,
      directory: root,
      store,
      current: plan.current,
      next: plan.next,
      ...layout,
      manager: {
        stop: async (unit) => services.set(unit, false),
        start: async (unit) => services.set(unit, true),
        reload: async () => {},
        observe: async (unit) => ({
          active: services.get(unit) === true,
          stopped: services.get(unit) !== true,
        }),
      },
    });
    assert.equal(result.status, 'switched');
    const active = await readPrivate(join(root, 'completed-upgrade-activation.json'));
    const { identity, ...originalActivation } = active;
    const activation = {
      ...originalActivation,
      phase: 'prepared',
      installation: core.contentIdentity(next),
    };
    await completeCompletedNativeUpgrade(core, {
      root,
      installation: next,
      activation,
    });
    assert.deepEqual((await store.read()).work, work);
    assert.deepEqual(await fs.readFile(cyclePath), originalCycle);
    assert.equal(
      (await readPrivate(join(root, 'completed-upgrade.json'))).phase,
      'completed',
    );
    await completeCompletedNativeUpgrade(core, {
      root,
      installation: next,
      activation,
    });
    assert.deepEqual((await store.read()).work, work);
  } finally {
    await store.close();
  }
}
