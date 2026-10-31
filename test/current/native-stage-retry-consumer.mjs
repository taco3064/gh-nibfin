import test from 'node:test';
import assert from 'node:assert/strict';
import { nativeStageWork } from '../../src/native-stage-work.mjs';

function fixture(change = () => {}) {
  let value = {
    identity: '0',
    work: {
      restrictionsChecked: true,
      paused: false,
      hold: null,
      unknownWrite: false,
      active: false,
      terminated: true,
      reconciled: true,
      conversations: [],
      pending: [
        {
          kind: 'native-stage',
          request: 'failed',
          cycle: 'original',
          stage: 'closure',
          status: 'UNACCEPTED',
        },
      ],
      receipts: [{ kind: 'native-cycle', reference: 'retained-failure' }],
    },
  };
  change(value.work);
  const store = {
    read: async () => structuredClone(value),
    updateWork: async ({ expected, work }) => {
      assert.equal(expected, value.identity);
      value = {
        identity: String(Number(value.identity) + 1),
        work: structuredClone(work),
      };
      return structuredClone(value);
    },
  };
  return {
    store,
    work: nativeStageWork(store, {
      request: 'retry',
      cycle: 'original',
      stage: 'closure',
      retry: 'failed',
    }),
  };
}

test('closure retry preserves the exact unaccepted predecessor', async () => {
  const f = fixture();
  const before = await f.store.read();
  await f.work.enter();
  const active = await f.store.read();
  assert.deepEqual(active.work.pending[0].history, before.work.pending);
  assert.deepEqual(active.work.receipts, before.work.receipts);
  assert.equal(active.work.active, true);
  await f.work.finish({ completed: false, terminated: true });
  assert.equal((await f.store.read()).work.pending[0].status, 'UNACCEPTED');
  await assert.rejects(f.work.enter(), /retry-boundary|original-pending-retry/);
});

test('retry never adopts other work or clears restrictions', async (t) => {
  const cases = {
    pause: (w) => {
      w.paused = true;
    },
    hold: (w) => {
      w.hold = 'owner';
    },
    uncertain: (w) => {
      w.unknownWrite = true;
    },
    active: (w) => {
      w.active = true;
    },
    live: (w) => {
      w.terminated = false;
    },
    unreconciled: (w) => {
      w.reconciled = false;
    },
    other: (w) => {
      w.pending[0].cycle = 'other';
    },
    unresolved: (w) => {
      w.pending[0].status = 'UNRESOLVED';
    },
  };
  for (const [name, change] of Object.entries(cases))
    await t.test(name, async () => {
      const f = fixture(change);
      const before = await f.store.read();
      await assert.rejects(f.work.enter(), /native-stage-work:/);
      assert.deepEqual(await f.store.read(), before);
    });
});
