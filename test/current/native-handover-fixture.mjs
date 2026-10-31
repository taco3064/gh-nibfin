// Synthetic GitHub/model fixture, real private custody and Owner signatures.
// This is source-contract evidence, never live platform or model certification.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { join, dirname } from 'node:path';
import { nativeCycleFixture } from './native-cycle-fixture.mjs';
import { nativeControlHost } from '../../src/native-host.mjs';
import { openFileHostStore } from '../../src/host-store.mjs';
import { openNativeCycle } from '../../src/native-delivery-store.mjs';
import { importNativeMergedWork } from '../../src/native-merged-handover.mjs';
import { readPrivate, writePrivate, sha } from '../../src/local-state.mjs';

export async function handoverMergedFixture(core, { context, fixture, target }) {
  assert.equal(core.distributionAPI.mergedContinuation, 1);
  const prior = await openNativeCycle(core, { root: fixture.root, target });
  let state = await prior.read();
  const session = '00000000-0000-4000-8000-000000000076';
  const directory = await fs.mkdtemp(join(context.root, 'writer-session-'));
  await fs.writeFile(
    join(directory, session + '.jsonl'),
    JSON.stringify({ type: 'session_meta', payload: { id: session } }) + '\n',
    { mode: 0o600 },
  );
  const metadata = {
    id: session,
    memberId: state.member.id,
    role: 'deliver',
    execution: state.receipts.deliver.execution.id,
    adapter: 'codex',
  };
  const snapshot = core.captureRescueSession({
    directory,
    memberId: metadata.memberId,
    role: metadata.role,
    session,
    execution: metadata.execution,
  });
  await prior.retain({ version: 1, kind: 'native-role-session', metadata, snapshot });
  await prior.update((value) => {
    value.dispatch = { id: 'retired-original-dispatch', stage: 'merge' };
    return value;
  });
  state = await prior.read();
  const originalBytes = await fs.readFile(join(prior.directory, 'state.json'));
  const archive = await fs.mkdtemp(join(context.root, 'retained-work-'));
  const files = [];
  async function copy(path, source) {
    const bytes = await fs.readFile(source),
      stat = await fs.stat(source);
    await fs.mkdir(dirname(join(archive, path)), { recursive: true, mode: 0o700 });
    await fs.writeFile(join(archive, path), bytes, { mode: 0o600 });
    files.push({
      path,
      bytes: bytes.length,
      sha256: sha(bytes),
      sourceMode: stat.mode & 0o777,
    });
  }
  await copy('original/cycle/state.json', join(prior.directory, 'state.json'));
  for (const name of await fs.readdir(join(prior.directory, 'records')))
    await copy(
      'original/cycle/records/' + name,
      join(prior.directory, 'records', name),
    );
  await copy(
    'historical-authority/owner-policy.json',
    join(fixture.root, 'owner-policy.json'),
  );
  await writePrivate(join(archive, 'manifest.json'), {
    format: 1,
    createdAt: new Date().toISOString(),
    authority: 'historical-data-only',
    activationAuthorized: false,
    issues: { [target.number]: { status: 'merged-pending-final' } },
    files,
  });
  const fresh = await nativeCycleFixture(core, context, {
    name: 'fresh-native-fixture',
  });
  const observed = { status: 'running', stageEntry: 'configured', diagnostics: [] };
  const host = nativeControlHost({
    root: fresh.root,
    service: { check: async () => observed, install: async () => observed },
    prepare: (path) =>
      fs.writeFile(join(path, 'fixture'), 'application', { mode: 0o600 }),
    verify: async () => {},
  });
  await host.prepare({
    core: { ...core, createGithubProvider: () => fixture.provider },
    context,
    executionPool: fresh.configuration,
    memberId: state.member.id,
  });
  const installation = await readPrivate(
    join(fresh.root, 'native-installation.json'),
  );
  const activation = {
    version: 1,
    phase: 'active',
    installation: core.contentIdentity(installation),
    memberId: state.member.id,
    projects: [],
  };
  await writePrivate(join(fresh.root, 'activation.json'), {
    ...activation,
    identity: core.contentIdentity(activation),
  });
  const ownerStore = await openFileHostStore(core, {
    directory: join(fresh.root, 'state'),
  });
  try {
    const value = await ownerStore.read();
    await ownerStore.updateWork({
      expected: value.identity,
      work: { ...value.work, paused: false },
    });
  } finally {
    await ownerStore.close();
  }
  const options = {
    root: fresh.root,
    configuration: fresh.configuration,
    repository: target.repository,
    directory: archive,
    provider: fixture.provider,
    context,
    observeResources: async () => ({ status: 'unused' }),
  };
  const before = await fs.readFile(join(fresh.root, 'state/store.json'));
  const checked = await importNativeMergedWork(core, {
    ...options,
    checkOnly: true,
  });
  assert.equal(checked.effects, 'none');
  assert.equal(checked.results[0].outcome, 'verified-not-authorized');
  await assert.rejects(fs.access(join(fresh.root, 'handovers')));
  const afterCheck = await fs.readFile(join(fresh.root, 'state/store.json'));
  assert.deepEqual(afterCheck, before);
  const result = await importNativeMergedWork(core, options);
  assert.equal(result.status, 'ready');
  const repeated = await importNativeMergedWork(core, options);
  assert.equal(repeated.results[0].outcome, 'existing');
  assert.equal(repeated.results[0].approval, result.results[0].approval);
  const imported = await openNativeCycle(core, { root: fresh.root, target });
  const after = await imported.read();
  const owner = await readPrivate(join(fresh.root, 'owner-policy.json'));
  assert.notEqual(owner.keyId, state.ownerKey.id);
  assert.equal(after.continuation.approval.keyId, owner.keyId);
  assert.equal(after.dispatch, null);
  assert.deepEqual(after.receipts, state.receipts);
  assert.deepEqual(after.attempts, state.attempts);
  assert(
    (await fs.readFile(join(prior.directory, 'state.json'))).equals(originalBytes),
  );
  return {
    root: fresh.root,
    archive,
    originalBytes,
    prior,
    repeat: () => importNativeMergedWork(core, options),
  };
}
