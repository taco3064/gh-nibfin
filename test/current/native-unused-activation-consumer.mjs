import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { join } from 'node:path';
import { withCurrentCore } from '../../src/current-core.mjs';
import { nativeCycleFixture } from './native-cycle-fixture.mjs';
import { nativeControlHost } from '../../src/native-host.mjs';
import { nativeInstallationSelection } from '../../src/managed-upgrade.mjs';
import { verifyNativeRuntime } from '../../src/native-control-service.mjs';
import { openFileHostStore } from '../../src/host-store.mjs';
import { readPrivate, writePrivate, sha } from '../../src/local-state.mjs';
import {
  prepareUnusedActivationUpgrade,
  completeUnusedActivationUpgrade,
} from '../../src/native-unused-activation.mjs';

async function fixture(core, context) {
  const f = await nativeCycleFixture(core, context);
  const observed = { status: 'running', stageEntry: 'configured', diagnostics: [] };
  const host = nativeControlHost({
    root: f.root,
    service: { check: async () => observed, install: async () => observed },
    prepare: (path) =>
      fs.writeFile(join(path, 'application'), 'fixture', { mode: 0o600 }),
    verify: async () => {},
  });
  await host.prepare({
    core: { ...core, createGithubProvider: () => f.provider },
    context,
    executionPool: f.configuration,
    memberId: '7',
  });
  const previous = await readPrivate(join(f.root, 'native-installation.json'));
  const activation = {
    version: 1,
    phase: 'active',
    installation: core.contentIdentity(previous),
    memberId: '7',
    projects: [],
  };
  await writePrivate(join(f.root, 'activation.json'), {
    ...activation,
    identity: core.contentIdentity(activation),
  });
  const store = await openFileHostStore(core, { directory: join(f.root, 'state') });
  const state = await store.read();
  await store.updateWork({
    expected: state.identity,
    work: { ...state.work, paused: false },
  });
  const versionPath = 'client/version.mjs';
  const original = await fs.readFile(join(f.root, 'runtime', versionPath));
  const updated = Buffer.concat([
    original,
    Buffer.from('\n// Fixture CLI revision.\n'),
  ]);
  const next = {
    ...previous,
    runtimeInventory: previous.runtimeInventory.map((entry) =>
      entry.path === versionPath ? { ...entry, sha256: sha(updated) } : entry,
    ),
  };
  return { f, store, previous, next, versionPath, updated, activation };
}

test('unused activation upgrade preserves its original authority and pause', async () =>
  withCurrentCore(async (core, context) => {
    const x = await fixture(core, context),
      { root } = x.f;
    try {
      const before = (await x.store.read()).work;
      // Explicit process-observation seam. Real runtime bytes and Core pin/work
      // transactions are used; this does not certify a live OS upgrade.
      const request = {
        root,
        previous: x.previous,
        next: x.next,
        memberId: '7',
        observeResources: async () => ({ status: 'unused' }),
      };
      assert.equal(
        (await prepareUnusedActivationUpgrade(core, request)).status,
        'prepared',
      );
      assert.equal(
        await readPrivate(join(root, 'activation.json'), { missing: true }),
        null,
      );
      const parked = await readPrivate(join(root, 'activation-previous.json'));
      assert.equal(parked.identity, core.contentIdentity(x.activation));
      assert.deepEqual((await x.store.read()).work, { ...before, paused: true });
      assert.equal(
        (await prepareUnusedActivationUpgrade(core, request)).status,
        'prepared',
      );
      const current = nativeInstallationSelection(core, context.identity, x.previous);
      const next = nativeInstallationSelection(core, context.identity, x.next);
      await fs.writeFile(join(root, 'runtime', x.versionPath), x.updated);
      await writePrivate(join(root, 'native-installation.json'), x.next);
      const capabilities = x.store.transitionCapabilities({
        verifyInstallation: async (installation) => {
          await verifyNativeRuntime(root, x.next);
          return { status: 'verified', installation };
        },
      });
      const switched = await core.applyInstallationTransition({
        ...capabilities,
        current,
        next,
        maintenance: true,
        transaction: 'fixture-upgrade',
      });
      assert.equal(switched.status, 'switched');
      let state = await x.store.read();
      await x.store.updateWork({
        expected: state.identity,
        work: { ...state.work, paused: false },
      });
      const activation = {
        ...x.activation,
        phase: 'prepared',
        installation: core.contentIdentity(x.next),
      };
      await completeUnusedActivationUpgrade(core, {
        root,
        installation: x.next,
        activation,
      });
      assert.equal(
        (await readPrivate(join(root, 'activation-upgrade.json'))).phase,
        'completed',
      );
      assert.deepEqual((await x.store.read()).work, before);
    } finally {
      await x.store.close();
    }
  }));

test('unused activation cannot clear an existing Owner pause', async () =>
  withCurrentCore(async (core, context) => {
    const x = await fixture(core, context);
    try {
      const state = await x.store.read();
      await x.store.updateWork({
        expected: state.identity,
        work: { ...state.work, paused: true },
      });
      await assert.rejects(
        prepareUnusedActivationUpgrade(core, {
          root: x.f.root,
          previous: x.previous,
          next: x.next,
          memberId: '7',
          observeResources: async () => ({ status: 'unused' }),
        }),
        /owner-restriction-or-original-work/,
      );
      assert.equal(
        (await readPrivate(join(x.f.root, 'activation.json'))).phase,
        'active',
      );
      assert.equal((await x.store.read()).work.paused, true);
    } finally {
      await x.store.close();
    }
  }));
