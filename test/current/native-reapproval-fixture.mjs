import fs from 'node:fs/promises';
import { join, dirname } from 'node:path';
import { prepareNativeCycle } from '../../src/native-cycle-policy.mjs';
import { openNativeHandoverArchive } from '../../src/native-handover-archive.mjs';
import { nativeControlHost } from '../../src/native-host.mjs';
import { readPrivate, writePrivate } from '../../src/local-state.mjs';
import { openFileHostStore } from '../../src/host-store.mjs';
import { nativeOwnerSigner } from '../../src/native-owner-policy.mjs';
import { inspectLegacyNativeCustody } from '../../src/native-legacy-custody.mjs';
import {
  prepareLegacyNativeHandover,
  legacyHandoverContext,
} from '../../src/native-legacy-handover.mjs';
import { nativeCycleFixture } from './native-cycle-fixture.mjs';
import { legacyCustodyFixture } from './native-legacy-fixture.mjs';

// Synthetic alternate engine bindings, real signatures and private CAS storage.
// This fixture does not claim an old release or an OS/service upgrade execution.
export async function nativeReapprovalFixture(core, context) {
  const f = await nativeCycleFixture(core, context);
  const old = await legacyCustodyFixture(core, context);
  const archiveDirectory = join(context.root, 'reapproval-data-archive');
  await fs.mkdir(archiveDirectory, { mode: 0o700 });
  const files = [];
  for (const [path, metadata] of old.archive.files) {
    await fs.mkdir(dirname(join(archiveDirectory, path)), {
      recursive: true,
      mode: 0o700,
    });
    await fs.writeFile(
      join(archiveDirectory, path),
      await old.archive.readBytes(path),
      { mode: 0o600 },
    );
    files.push({ path, ...metadata });
  }
  await writePrivate(join(archiveDirectory, 'manifest.json'), {
    format: 1,
    createdAt: '2026-11-01T00:00:00Z',
    authority: 'historical-data-only',
    activationAuthorized: false,
    issues: old.archive.metadata,
    files,
  });
  old.input.archive = await openNativeHandoverArchive(core, {
    directory: archiveDirectory,
    hostRoot: f.root,
  });
  const original = await inspectLegacyNativeCustody(core, old.input);
  f.issue.labels.push({ id: '901', name: 'shaped' });
  f.setTree(['docs', 'a.txt']);
  const store = await prepareNativeCycle(core, { ...f.options, handover: original });
  const baseline = await store.read();
  const state = {
    ...baseline,
    core: 'e'.repeat(64),
    policy: { ...baseline.policy, core: 'e'.repeat(64), runtime: 'f'.repeat(64) },
  };
  const retained = prepareLegacyNativeHandover(core, { state, original });
  const signer = await nativeOwnerSigner(core, { root: f.root });
  const grant = signer.sign({
    ...baseline.grant.payload,
    policy: state.policy,
    recordId: retained.recordId,
  });
  const shape = core.createUnacceptedHandoverEvidence({
    original,
    current: retained.current,
    grant,
    context: legacyHandoverContext(core, {
      owner: signer.policy,
      authority: state.authority,
      policy: state.policy,
      work: grant.payload.work,
      target: state.target,
      paths: state.paths,
    }),
  });
  await store.update(async (before) => {
    const records = { ...before.records };
    for (const payload of retained.retained) {
      const reference = await store.retain(payload, { detached: true });
      records[reference.slice(14)] = payload.kind;
    }
    return {
      ...state,
      grant,
      records,
      receipts: shape.receipts,
      expectations: shape.expectations,
      shapeBody: shape.shapeBody,
      legacyHandover: retained.legacyHandover,
      deferredRoles: retained.deferredRoles,
    };
  });
  return { ...f, store, original, archiveDirectory, before: await store.read() };
}

export async function prepareReapprovalHostFixture(core, context, f) {
  const observed = { status: 'running', stageEntry: 'configured', diagnostics: [] };
  const host = nativeControlHost({
    root: f.root,
    service: { check: async () => observed, install: async () => observed },
    prepare: (directory) =>
      fs.writeFile(join(directory, 'fixture'), 'application', { mode: 0o600 }),
    verify: async () => {},
  });
  await host.prepare({
    core: { ...core, createGithubProvider: () => f.provider },
    context,
    repo: 'example/project',
    executionPool: f.configuration,
    memberId: '7',
  });
  const installation = await readPrivate(join(f.root, 'native-installation.json'));
  const activation = {
    version: 1,
    phase: 'active',
    memberId: '7',
    projects: [],
    installation: core.contentIdentity(installation),
  };
  await writePrivate(join(f.root, 'activation.json'), {
    ...activation,
    identity: core.contentIdentity(activation),
  });
  const store = await openFileHostStore(core, { directory: join(f.root, 'state') });
  try {
    const before = await store.read();
    // Complete the fixture's explicit simulated Owner activation.
    await store.updateWork({
      expected: before.identity,
      work: { ...before.work, paused: false },
    });
  } finally {
    await store.close();
  }
}
