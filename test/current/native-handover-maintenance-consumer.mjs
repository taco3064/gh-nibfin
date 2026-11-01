import test from 'node:test';
import assert from 'node:assert/strict';
import { join } from 'node:path';
import { withCurrentCore } from '../../src/current-core.mjs';
import { nativeCycleFixture } from './native-cycle-fixture.mjs';
import { initializeFileHostStore } from '../../src/host-store.mjs';
import { nativePoolStore } from '../../src/native-pool-store.mjs';
import { withNativeHandoverMaintenance } from '../../src/native-handover-maintenance.mjs';

async function fixture(core, context, name) {
  const f = await nativeCycleFixture(core, context, { name });
  await nativePoolStore(core, f).initialize();
  const store = await initializeFileHostStore(core, {
    directory: join(f.root, 'state'),
    installation: {
      packageVersion: context.identity.packageVersion,
      pins: context.identity.pins,
    },
    repositories: [{ repository: 'example/project', repositoryId: '100' }],
  });
  const state = await store.read();
  await store.updateWork({
    expected: state.identity,
    work: {
      ...state.work,
      paused: false,
      receipts: [{ kind: 'historical-test-data', reference: 'original-receipt' }],
    },
  });
  return {
    ...f,
    store,
    options: {
      root: f.root,
      configuration: f.configuration,
      request: core.contentIdentity({ purpose: name }),
      observeResources: async () => ({ status: 'unused' }),
    },
  };
}

test(
  'handover pause preserves used-host evidence; ' + 'read-only checks do not pause',
  async () =>
    withCurrentCore(async (core, context) => {
      for (const checkOnly of [true, false]) {
        const f = await fixture(core, context, 'handover-pause-' + checkOnly);
        try {
          const before = (await f.store.read()).work;
          const result = await withNativeHandoverMaintenance(
            core,
            { ...f.options, checkOnly },
            async () => {
              assert.equal((await f.store.read()).work.paused, !checkOnly);
              return 'verified-data-publication';
            },
          );
          assert.equal(result, 'verified-data-publication');
          assert.deepEqual((await f.store.read()).work, before);
        } finally {
          await f.store.close();
        }
      }
    }),
);

test('failed handover restores only its own pause and retains original cause', async () =>
  withCurrentCore(async (core, context) => {
    const f = await fixture(core, context, 'handover-original-failure');
    try {
      const before = (await f.store.read()).work;
      await assert.rejects(
        withNativeHandoverMaintenance(core, f.options, async () => {
          throw Error('original-custody-failure');
        }),
        /original-custody-failure/,
      );
      assert.deepEqual((await f.store.read()).work, before);
    } finally {
      await f.store.close();
    }
  }));

test('a later Owner hold survives maintenance and prevents pause restoration', async () =>
  withCurrentCore(async (core, context) => {
    const f = await fixture(core, context, 'handover-new-owner-hold');
    try {
      await assert.rejects(
        withNativeHandoverMaintenance(core, f.options, async () => {
          const state = await f.store.read();
          await f.store.updateWork({
            expected: state.identity,
            work: { ...state.work, hold: { reason: 'Owner requested hold' } },
          });
        }),
        /pause-drift/,
      );
      const state = await f.store.read();
      assert.equal(state.work.paused, true);
      assert.deepEqual(state.work.hold, { reason: 'Owner requested hold' });
    } finally {
      await f.store.close();
    }
  }));

test('unknown writes and intentional pauses cannot authorize import', async () =>
  withCurrentCore(async (core, context) => {
    for (const restriction of [{ unknownWrite: true }, { paused: true }]) {
      const f = await fixture(
        core,
        context,
        'handover-restricted-' + (restriction.paused ? 'pause' : 'write'),
      );
      try {
        const state = await f.store.read();
        await f.store.updateWork({
          expected: state.identity,
          work: { ...state.work, ...restriction },
        });
        let invoked = false;
        await assert.rejects(
          withNativeHandoverMaintenance(core, f.options, async () => {
            invoked = true;
          }),
          /restriction/,
        );
        assert.equal(invoked, false);
      } finally {
        await f.store.close();
      }
    }
  }));
