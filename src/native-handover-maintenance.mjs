import { join } from 'node:path';
import { openFileHostStore } from './host-store.mjs';
import { nativePoolStore } from './native-pool-store.mjs';
import { readNativeOwnerPolicy } from './native-owner-policy.mjs';
import { readPrivate, writePrivate, ensurePrivateDirectory } from './local-state.mjs';
import { maintenanceLock } from './upgrade.mjs';
import { observeUnusedNativePaths } from './native-staged-transition.mjs';

const need = (ok, reason) => {
  if (!ok) throw Error('native-handover-maintenance:' + reason);
};

async function restoreOwnedPause(core, { store, journal, path }) {
  const state = await store.read();
  const current = core.contentIdentity(state.work);
  need(['paused', 'completed'].includes(journal.phase), 'pause-journal');
  if (journal.phase === 'completed') need(current === journal.before, 'pause-drift');
  else {
    need([journal.before, journal.paused].includes(current), 'pause-drift');
    if (current === journal.paused)
      await store.updateWork({ expected: state.identity, work: journal.original });
    const { identity, ...body } = journal;
    const completed = { ...body, phase: 'completed' };
    await writePrivate(path, {
      ...completed,
      identity: core.contentIdentity(completed),
    });
  }
}

export async function releaseNativeHandoverMaintenance(core, { root, request }) {
  need(/^[a-f0-9]{64}$/.test(request), 'request');
  return maintenanceLock(root, async () => {
    const path = join(root, 'maintenance-events', request + '.json');
    const journal = await readPrivate(path);
    const { identity, ...body } = journal;
    const owner = await readNativeOwnerPolicy(core, { root });
    need(
      identity === core.contentIdentity(body) &&
        journal.request === request &&
        journal.owner === owner.identity &&
        journal.kind === 'native-maintenance',
      'original-journal',
    );
    const store = await openFileHostStore(core, { directory: join(root, 'state') });
    try {
      await restoreOwnedPause(core, { store, journal, path });
    } finally {
      await store.close();
    }
  });
}

/** Own one reversible pause while publishing a data-only handover. Existing
 * work, credentials, holds and historical receipts never get replaced. The
 * durable journal identifies our pause across a crash; CAS protects later edits.
 */
export async function withNativeHandoverMaintenance(
  core,
  {
    root,
    configuration,
    request,
    checkOnly = false,
    keepPaused = false,
    restoreOnFailure = true,
    observeResources = observeUnusedNativePaths,
  },
  operation,
) {
  need(/^[a-f0-9]{64}$/.test(request), 'request');
  const pool = nativePoolStore(core, { root, configuration });
  const store = await openFileHostStore(core, { directory: join(root, 'state') });
  const idle = async () => {
    const status = await pool.status();
    need(status.occupied === 0 && status.pending === 0, 'live-runner-work');
    const resources = await observeResources([
      join(root, 'jobs'),
      ...Array.from({ length: configuration.capacity }, (_, i) =>
        join(root, 'runner-' + (i + 1)),
      ),
    ]);
    need(resources.status === 'unused', 'live-native-resources');
  };
  try {
    if (checkOnly) {
      const state = await store.read();
      need(core.protectedWorkRestriction(state.work) === null, 'restriction');
      need(
        !state.work.active &&
          state.work.terminated &&
          state.work.reconciled &&
          state.work.pending.length === 0,
        'unreconciled-work',
      );
      await idle();
      return operation();
    }
    return await maintenanceLock(root, async (assertLease) => {
      const owner = await readNativeOwnerPolicy(core, { root });
      const directory = join(root, 'maintenance-events');
      await ensurePrivateDirectory(directory);
      const path = join(directory, request + '.json');
      let journal = await readPrivate(path, { missing: true });
      let state = await store.read();
      if (journal) {
        const { identity, ...body } = journal;
        need(
          identity === core.contentIdentity(body) &&
            journal.kind === 'native-maintenance' &&
            journal.owner === owner.identity &&
            journal.request === request &&
            ['intent', 'paused', 'completed'].includes(journal.phase),
          'original-journal',
        );
        need(
          [journal.before, journal.paused].includes(core.contentIdentity(state.work)),
          'pause-drift',
        );
      } else {
        need(core.protectedWorkRestriction(state.work) === null, 'restriction');
        need(
          !state.work.active &&
            state.work.terminated &&
            state.work.reconciled &&
            state.work.pending.length === 0,
          'original-work',
        );
        await idle();
        journal = {
          kind: 'native-maintenance',
          request,
          owner: owner.identity,
          phase: 'intent',
          original: state.work,
          before: core.contentIdentity(state.work),
          paused: core.contentIdentity({ ...state.work, paused: true }),
        };
      }
      const save = async (phase) => {
        const { identity, ...body } = { ...journal, phase };
        journal = { ...body, identity: core.contentIdentity(body) };
        await writePrivate(path, journal);
      };
      await save('intent');
      if (core.contentIdentity(state.work) === journal.before)
        await store.updateWork({
          expected: state.identity,
          work: { ...state.work, paused: true },
        });
      await save('paused');
      let result;
      let failure;
      try {
        await idle();
        assertLease();
        result = await operation({ assertLease });
      } catch (error) {
        failure = error;
      }
      if (keepPaused && (!failure || !restoreOnFailure)) {
        if (failure) throw failure;
        return result;
      }
      try {
        await restoreOwnedPause(core, { store, journal, path });
      } catch (error) {
        if (failure)
          throw new AggregateError(
            [failure, error],
            'native-handover-maintenance:recovery-required',
          );
        throw error;
      }
      if (failure) throw failure;
      return result;
    });
  } finally {
    await store.close();
  }
}
