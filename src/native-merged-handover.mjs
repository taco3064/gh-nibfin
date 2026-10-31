import fs from 'node:fs/promises';
import { join, dirname } from 'node:path';
import { currentPin, currentProducer } from './current-core.mjs';
import { openNativeHandoverArchive } from './native-handover-archive.mjs';
import { verifyNativeHandoverCustody } from './native-handover-custody.mjs';
import {
  observeNativeMergedHandover,
  readNativeMergedContinuation,
} from './native-handover-context.mjs';
import { nativeOwnerSigner } from './native-owner-policy.mjs';
import { withUnusedNativeMaintenance } from './native-unused-maintenance.mjs';
import { authorizeNativeContinuationRepair } from './native-continuation-repair.mjs';
import { nativeCycleDirectory, openNativeCycle } from './native-delivery-store.mjs';
import { verifyNativeRuntime } from './native-control-service.mjs';
import {
  ensurePrivateDirectory,
  readPrivate,
  writePrivate,
  privatePath,
} from './local-state.mjs';

const need = (ok, reason) => {
  if (!ok) throw Error('native-handover:' + reason);
};
const absent = async (path) => {
  try {
    await fs.lstat(path);
    return false;
  } catch (error) {
    if (error.code === 'ENOENT') return true;
    throw error;
  }
};
const same = (core, a, b) => core.canonicalJSON(a) === core.canonicalJSON(b);

async function putExact(path, bytes) {
  if (await absent(path)) {
    const handle = await fs.open(path, 'wx', 0o600);
    try {
      await handle.writeFile(bytes);
      await handle.sync();
    } finally {
      await handle.close();
    }
  }
  await privatePath(path);
  need((await fs.readFile(path)).equals(bytes), 'staging-bytes');
}

async function existingContinuation(core, { root, state, archive }) {
  let store;
  try {
    store = await openNativeCycle(core, { root, target: state.target });
  } catch (error) {
    if (error.code === 'ENOENT') return null;
    throw error;
  }
  const value = await store.read();
  need(
    value.continuation?.parent.state === state.identity &&
      value.continuation.parent.archive === archive.identity,
    'original-cycle-conflict',
  );
  const proof = await readNativeMergedContinuation(core, {
    root,
    store,
    state: value,
  });
  if (value.completion)
    core.verifyCompletion(value.completion, value.completionCurrent);
  return {
    outcome: 'existing',
    target: state.target,
    approval: proof.identity,
    next:
      value.completion && value.fanout?.outcome === 'discovered'
        ? null
        : core.nextDeliveryStage(value.receipts, null),
  };
}

async function publishCycle(
  core,
  { root, staging, archive, cycle, prepared, custody },
) {
  const { state } = cycle;
  await ensurePrivateDirectory(staging);
  await ensurePrivateDirectory(join(staging, 'records'));
  const source = {
    version: 1,
    kind: 'native-original-cycle',
    path: cycle.base + 'state.json',
    bytes: (await archive.readBytes(cycle.base + 'state.json')).toString('base64'),
    manifest: (await archive.readManifest()).toString('base64'),
  };
  const originalBody = { version: 1, kind: 'retained', payload: source };
  const originalId = core.contentIdentity(originalBody);
  const originalRecord = { ...originalBody, identity: originalId };
  const recordNames = new Set([
    ...cycle.records.map((path) => path.slice((cycle.base + 'records/').length)),
    originalId + '.json',
  ]);
  need(
    (await fs.readdir(staging)).every((name) =>
      ['state.json', 'records'].includes(name),
    ),
    'staging-inventory',
  );
  need(
    (await fs.readdir(join(staging, 'records'))).every((name) =>
      recordNames.has(name),
    ),
    'staging-records',
  );
  for (const path of cycle.records) {
    const name = path.slice((cycle.base + 'records/').length);
    await putExact(join(staging, 'records', name), await archive.readBytes(path));
  }
  await putExact(
    join(staging, 'records', originalId + '.json'),
    Buffer.from(JSON.stringify(originalRecord, null, 2) + '\n'),
  );
  const { identity: oldIdentity, ...original } = state;
  const body = {
    ...original,
    revision: state.revision + 1,
    records: { ...state.records, [originalId]: 'native-original-cycle' },
    // The original completed dispatch stays in the exact retained original
    // record. It cannot masquerade as an active dispatch in this new host.
    dispatch: null,
    continuation: {
      version: 1,
      original: 'native-record:' + originalId,
      parent: prepared.parent,
      execution: prepared.execution,
      approval: prepared.approval,
      custody,
    },
  };
  const value = { ...body, identity: core.contentIdentity(body) };
  await putExact(
    join(staging, 'state.json'),
    Buffer.from(JSON.stringify(value, null, 2) + '\n'),
  );
  const target = nativeCycleDirectory(root, state.target);
  await ensurePrivateDirectory(dirname(target));
  if (await absent(target)) {
    try {
      await fs.rename(staging, target);
    } catch {
      /* Read back this exact original local publication below. */
    }
  }
  need(await absent(staging), 'original-publication-unconfirmed');
  const observed = await openNativeCycle(core, { root, target: state.target });
  need((await observed.read()).identity === value.identity, 'publication-readback');
  return {
    outcome: 'imported',
    target: state.target,
    approval: core.contentIdentity(prepared.approval),
    next: 'final',
  };
}

/** Official Owner init imports only independently verified merged native work.
 * Legacy UNACCEPTED input remains in the private archive, without a new Accept,
 * reset budget, rewritten Issue or active copy of the retired private key.
 */
export async function importNativeMergedWork(
  core,
  {
    root,
    configuration,
    repository,
    directory,
    provider,
    context,
    checkOnly = false,
    observeResources,
  },
) {
  need(core.distributionAPI?.mergedContinuation === 1, 'compatible-core-required');
  need(
    core.distributionIdentity().components.core.sha256 === currentPin.coreSha256 &&
      context.identity.components.core.sha256 === currentPin.coreSha256,
    'installed-core-identity',
  );
  const installation = await readPrivate(join(root, 'native-installation.json'));
  await verifyNativeRuntime(root, installation);
  const archive = await openNativeHandoverArchive(core, {
    directory,
    hostRoot: root,
  });
  const retained = Object.entries(archive.metadata)
    .filter(([, value]) => value.status === 'UNACCEPTED')
    .map(([number]) => ({ number, status: 'UNACCEPTED', reason: 'not-imported' }));
  const repaired = await authorizeNativeContinuationRepair(core, {
    root,
    configuration,
    repository,
    directory,
    provider,
    checkOnly,
  });
  if (repaired)
    return {
      status: retained.length ? 'partial' : 'ready',
      archive: archive.identity,
      effects: repaired.effects,
      results: [repaired],
      retained,
    };
  need(archive.candidates.length <= 128, 'cycle-limit');
  const results = [];
  for (const path of archive.candidates) {
    const cycle = await archive.readCycle(path),
      { state } = cycle;
    if (state.target?.repository !== repository) continue;
    const existing = await existingContinuation(core, { root, state, archive });
    if (existing) {
      results.push(existing);
      continue;
    }
    const activation = await readPrivate(join(root, 'activation.json'));
    const key = core.contentIdentity({
      kind: 'native-merged-handover',
      archive: archive.identity,
      target: state.target,
      core: currentPin.coreSha256,
      source: currentProducer,
      activation: activation.identity,
    });
    const journalDirectory = join(root, 'handovers', key);
    const journalPath = join(journalDirectory, 'intent.json');
    let journal = await readPrivate(journalPath, {
      missing: true,
      limit: 8 * 1024 * 1024,
    });
    if (journal) {
      const { identity, ...body } = journal;
      need(
        identity === core.contentIdentity(body) &&
          journal.request === key &&
          journal.archive === archive.identity &&
          journal.state === state.identity &&
          ['approved', 'installed'].includes(journal.phase),
        'original-handover-intent',
      );
    }
    const inputs = {
      root,
      configuration,
      repository,
      state,
      archive,
      provider,
      ...(journal ? { execution: journal.execution } : {}),
    };
    const observation = await observeNativeMergedHandover(core, inputs);
    const custody = await verifyNativeHandoverCustody(core, { archive, cycle });
    if (checkOnly) {
      results.push({
        outcome: 'verified-not-authorized',
        target: state.target,
        next: 'final',
        custody,
      });
      continue;
    }
    const result = await withUnusedNativeMaintenance(
      core,
      {
        root,
        configuration,
        memberId: state.member.id,
        reason: 'merged-work-import',
        identity: key,
        ...(observeResources ? { observeResources } : {}),
      },
      async ({ assertLease }) => {
        await ensurePrivateDirectory(journalDirectory);
        const latest = await observeNativeMergedHandover(core, {
          ...inputs,
          execution: observation.execution,
        });
        const signer = await nativeOwnerSigner(core, { root });
        const approval = journal?.approval ?? signer.sign(latest.draft);
        core.verifyMergedContinuation(approval, latest);
        const body = {
          version: 1,
          request: key,
          archive: archive.identity,
          state: state.identity,
          phase: 'approved',
          parent: latest.parent,
          execution: latest.execution,
          approval,
        };
        journal = { ...body, identity: core.contentIdentity(body) };
        await writePrivate(journalPath, journal);
        assertLease();
        const published = await publishCycle(core, {
          root,
          staging: join(journalDirectory, 'cycle-next'),
          archive,
          cycle,
          prepared: { ...latest, approval },
          custody,
        });
        const complete = { ...body, phase: 'installed', result: published };
        await writePrivate(journalPath, {
          ...complete,
          identity: core.contentIdentity(complete),
        });
        return published;
      },
    );
    results.push(result);
  }
  need(results.length > 0, 'no-matching-merged-work');
  return {
    status: retained.length ? 'partial' : 'ready',
    archive: archive.identity,
    effects: checkOnly ? 'none' : 'merged-work-continuation-only',
    results,
    retained,
  };
}
