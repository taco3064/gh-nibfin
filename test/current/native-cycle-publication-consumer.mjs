import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { join } from 'node:path';
import { withCurrentCore } from '../../src/current-core.mjs';
import { createNativeCycle } from '../../src/native-delivery-store.mjs';

test(
  'initial custody and state publish together; ' +
    'competing creation cannot replace them',
  async () =>
    withCurrentCore(async (core, context) => {
      const root = await fs.mkdtemp(join(context.root, 'atomic-custody-'));
      const target = {
        repository: 'example/project',
        repositoryId: '100',
        databaseId: '123',
        issueId: 'I_123',
        number: '55',
      };
      const original = {
        kind: 'native-role-session',
        bytes: 'original unaccepted conversation',
      };
      const reference =
        'native-record:' +
        core.contentIdentity({
          version: 1,
          kind: 'retained',
          payload: original,
        });
      const attempts = await Promise.allSettled(
        [1, 2].map((id) =>
          createNativeCycle(core, {
            root,
            target,
            retained: [original],
            value: { cycle: 'original-' + id, legacyHandover: reference },
          }),
        ),
      );
      assert.equal(attempts.filter((r) => r.status === 'fulfilled').length, 1);
      const store = attempts.find((r) => r.status === 'fulfilled').value;
      assert.deepEqual(await store.get(reference), original);
      const state = await store.read();
      assert.equal(state.records[reference.slice(14)], original.kind);
      assert.equal(state.legacyHandover, reference);
      const entries = await fs.readdir(join(root, 'delivery', '100'));
      assert.deepEqual(
        entries.filter((name) => name !== 'maintenance.lock'),
        ['123'],
      );
    }),
);
