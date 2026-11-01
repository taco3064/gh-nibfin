import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { join } from 'node:path';
import { withCurrentCore } from '../../src/current-core.mjs';
import { prepareNativeCycle } from '../../src/native-cycle-policy.mjs';
import { verifyNativeLegacyHandover } from '../../src/native-legacy-handover.mjs';
import {
  nativeReapprovalFixture,
  prepareReapprovalHostFixture,
} from './native-reapproval-fixture.mjs';
import { completedUpgradeFixture } from './native-completed-upgrade-fixture.mjs';

test('engine reapproval preserves signed history and session data', async () =>
  withCurrentCore(async (core, context) => {
    const f = await nativeReapprovalFixture(core, context);
    const path = join(f.store.directory, 'state.json');
    const bytes = await fs.readFile(path);
    const records = await fs.readdir(join(f.store.directory, 'records'));
    const inputs = { ...f.options, handover: f.original };
    assert.equal(
      (
        await prepareNativeCycle(core, {
          ...inputs,
          checkOnly: true,
        })
      ).next,
      'deliver',
    );
    assert.deepEqual(await fs.readFile(path), bytes);
    assert.deepEqual(await fs.readdir(join(f.store.directory, 'records')), records);
    const store = await prepareNativeCycle(core, inputs);
    const state = await store.read();
    assert.equal(state.core, core.distributionIdentity().components.core.sha256);
    assert.equal(state.cycle, f.before.cycle);
    assert.equal(state.createdAt, f.before.createdAt);
    assert.equal(state.handoverPreviousIdentity, f.before.identity);
    assert.deepEqual(state.attempts, {});
    const proof = await verifyNativeLegacyHandover(core, {
      root: f.root,
      store,
      state,
    });
    const history = await store.get(proof.proof.previousReference);
    assert.deepEqual(history.state, f.before);
    assert.equal(history.state.grant.signature, f.before.grant.signature);
    assert.equal(proof.plan.session, f.original.session);
    assert.equal(proof.plan.repairs, 0);
    assert.equal(core.nextDeliveryStage(state.receipts), 'deliver');
    const beforeProof = await store.get(f.before.legacyHandover);
    assert.equal(proof.proof.sessionReference, beforeProof.sessionReference);
    assert.equal(proof.proof.workspaceReference, beforeProof.workspaceReference);
    await prepareNativeCycle(core, inputs);
    assert.equal((await store.read()).identity, state.identity);
  }));

test('reapproval cannot erase attempts or changed Scope', async () =>
  withCurrentCore(async (core, context) => {
    const f = await nativeReapprovalFixture(core, context);
    await f.store.update((state) => {
      state.attempts.deliver = { phase: 'unresolved' };
      return state;
    });
    const path = join(f.store.directory, 'state.json');
    const before = await fs.readFile(path);
    await assert.rejects(
      prepareNativeCycle(core, {
        ...f.options,
        handover: f.original,
      }),
      /execution-already-started/,
    );
    assert.deepEqual(await fs.readFile(path), before);
    f.issue.body += '\nChanged requirements';
    await assert.rejects(
      prepareNativeCycle(core, {
        ...f.options,
        handover: f.original,
      }),
      /original-cycle-reconciliation-required/,
    );
    assert.deepEqual(await fs.readFile(path), before);
  }));

test('sealed upgrade preserves the unexecuted handover prefix', async () =>
  withCurrentCore(async (core, context) => {
    const f = await nativeReapprovalFixture(core, context);
    await prepareReapprovalHostFixture(core, context, f);
    await completedUpgradeFixture(core, context, {
      root: f.root,
      state: f.before,
      handover: f.archiveDirectory,
    });
    assert.equal((await f.store.read()).identity, f.before.identity);
    const store = await prepareNativeCycle(core, {
      ...f.options,
      handover: f.original,
    });
    const state = await store.read();
    assert.equal(state.cycle, f.before.cycle);
    assert.equal(state.handoverPreviousIdentity, f.before.identity);
    assert.equal(state.deferredRoles.deliver.target.session, f.original.session);
  }));
