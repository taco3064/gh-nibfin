import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { join } from 'node:path';
import { generateKeyPairSync } from 'node:crypto';
import { withCurrentCore } from '../../src/current-core.mjs';
import { sha, writePrivate, readPrivate } from '../../src/local-state.mjs';
import { initializeFileHostStore } from '../../src/host-store.mjs';
import { nativePoolStore } from '../../src/native-pool-store.mjs';
import { withUnusedNativeMaintenance } from '../../src/native-unused-maintenance.mjs';

async function fixture(core, context) {
  const root = await fs.mkdtemp(join(context.root, 'unused-maintenance-'));
  const configuration = {
    version: 2,
    id: 'member-default',
    capacity: 2,
    targets: [
      {
        version: 1,
        repository: 'example/project',
        repositoryId: '100',
        ownerId: '7',
        ownerType: 'User',
      },
    ],
  };
  const publicKey = generateKeyPairSync('ed25519').publicKey.export({
    format: 'pem',
    type: 'spki',
  });
  const policy = {
    version: 1,
    memberId: '7',
    memberLogin: 'owner',
    publicKey,
    keyId: 'native-owner-' + sha(publicKey).slice(0, 24),
    targets: configuration.targets,
    projects: {},
  };
  await writePrivate(join(root, 'owner-policy.json'), {
    ...policy,
    identity: core.contentIdentity(policy),
  });
  await nativePoolStore(core, { root, configuration }).initialize();
  const store = await initializeFileHostStore(core, {
    directory: join(root, 'state'),
    installation: {
      packageVersion: context.identity.packageVersion,
      pins: context.identity.pins,
    },
    repositories: [{ repository: 'example/project', repositoryId: '100' }],
  });
  const state = await store.read();
  await store.updateWork({
    expected: state.identity,
    work: { ...state.work, paused: false },
  });
  return {
    root,
    store,
    options: {
      root,
      configuration,
      memberId: '7',
      reason: 'merged-work-import',
      identity: core.contentIdentity({ root, purpose: 'fixture' }),
      // Explicit resource-observation seam; no real service readiness is claimed.
      observeResources: async () => ({ status: 'unused' }),
    },
  };
}

test('unused maintenance owns only its exact installer pause', async (t) =>
  withCurrentCore(async (core, context) => {
    for (const keepPaused of [false, true])
      await t.test('successful operation keeps pause: ' + keepPaused, async () => {
        const f = await fixture(core, context);
        try {
          const before = (await f.store.read()).work;
          const result = await withUnusedNativeMaintenance(
            core,
            { ...f.options, keepPaused },
            async () => {
              assert.equal((await f.store.read()).work.paused, true);
              return 'actual-operation-result';
            },
          );
          assert.equal(result, 'actual-operation-result');
          assert.deepEqual((await f.store.read()).work, {
            ...before,
            paused: keepPaused,
          });
        } finally {
          await f.store.close();
        }
      });
    await t.test('interruption resumes its exact original pause', async () => {
      const f = await fixture(core, context);
      try {
        await assert.rejects(
          withUnusedNativeMaintenance(core, f.options, async () => {
            throw Error('interrupted');
          }),
          /interrupted/,
        );
        assert.equal((await f.store.read()).work.paused, true);
        await withUnusedNativeMaintenance(core, f.options, async () => 'resumed');
        assert.equal((await f.store.read()).work.paused, false);
        const path = join(f.root, 'maintenance-events', f.options.identity + '.json');
        assert.equal((await readPrivate(path)).phase, 'completed');
      } finally {
        await f.store.close();
      }
    });
    for (const [name, change] of Object.entries({
      'pre-existing Owner pause': (work) => {
        work.paused = true;
      },
      'unknown writes': (work) => {
        work.unknownWrite = true;
      },
      'Owner hold': (work) => {
        work.hold = { reason: 'Owner decision' };
      },
    }))
      await t.test('preserves ' + name, async () => {
        const f = await fixture(core, context);
        try {
          const state = await f.store.read(),
            work = structuredClone(state.work);
          change(work);
          await f.store.updateWork({ expected: state.identity, work });
          await assert.rejects(
            withUnusedNativeMaintenance(core, f.options, async () =>
              assert.fail('must not execute'),
            ),
          );
          assert.deepEqual((await f.store.read()).work, work);
        } finally {
          await f.store.close();
        }
      });
    await t.test('an intervening Owner hold is never restored away', async () => {
      const f = await fixture(core, context);
      try {
        await assert.rejects(
          withUnusedNativeMaintenance(core, f.options, async () => {
            const state = await f.store.read();
            await f.store.updateWork({
              expected: state.identity,
              work: { ...state.work, hold: { reason: 'new Owner hold' } },
            });
          }),
          /work-drift/,
        );
        assert.deepEqual((await f.store.read()).work.hold, {
          reason: 'new Owner hold',
        });
        assert.equal((await f.store.read()).work.paused, true);
      } finally {
        await f.store.close();
      }
    });
  }));
