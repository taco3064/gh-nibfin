import fs from 'node:fs/promises';
import { join } from 'node:path';
import { openFileHostStore } from './host-store.mjs';
import { nativePoolStore } from './native-pool-store.mjs';
import { readNativeOwnerPolicy } from './native-owner-policy.mjs';
import { observeUnusedNativePaths } from './native-staged-transition.mjs';
import {
  readPrivate,
  writePrivate,
  ensurePrivateDirectory,
  privatePath,
} from './local-state.mjs';
import { maintenanceLock } from './upgrade.mjs';

const need = (ok, reason) => {
  if (!ok) throw Error('native-unused-maintenance:' + reason);
};
const reasons = new Set(['activation-upgrade', 'merged-work-import']);
const idleDirectories = [
  'jobs',
  'pool-intake',
  'pool-blockages',
  'dispatches',
  'stage-records',
  'original-work',
];

/** An explicit installer/importer boundary for a pool that has never executed.
 * Core owns pin-switch and work restrictions. This adapter atomically records an
 * Owner pause, proves unused local resources, and never clears another hold.
 */
export async function withUnusedNativeMaintenance(core, options, operation) {
  const {
    root,
    configuration,
    memberId,
    reason,
    identity,
    keepPaused = false,
    observeResources = observeUnusedNativePaths,
  } = options;
  need(
    reasons.has(reason) &&
      /^[a-f0-9]{64}$/.test(identity) &&
      typeof operation === 'function',
    'request',
  );
  return maintenanceLock(root, async (assertLease) => {
    const owner = await readNativeOwnerPolicy(core, { root });
    need(owner.memberId === memberId, 'owner');
    const selected = core.validateExecutionPool(configuration);
    const pool = nativePoolStore(core, { root, configuration: selected });
    async function unused() {
      const status = await pool.status();
      const ledger = await readPrivate(join(root, 'pool-state/ledger.json'), {
        limit: 32 * 1024 * 1024,
      });
      need(
        status.occupied === 0 &&
          status.pending === 0 &&
          Object.keys(ledger.jobs).length === 0 &&
          Object.keys(ledger.capacity.completed).length === 0,
        'previous-execution',
      );
      for (const name of idleDirectories) {
        const path = join(root, name);
        try {
          await privatePath(path, { directory: true });
          need(
            (await fs.readdir(path)).every((n) => n === 'maintenance.lock'),
            'original-work',
          );
        } catch (error) {
          if (error.code !== 'ENOENT') throw error;
        }
      }
      const paths = [
        join(root, 'jobs'),
        ...Array.from({ length: selected.capacity }, (_, i) =>
          join(root, 'runner-' + (i + 1)),
        ),
      ];
      need((await observeResources(paths)).status === 'unused', 'resources');
    }
    await unused();
    const directory = join(root, 'maintenance-events');
    await ensurePrivateDirectory(directory);
    const path = join(directory, identity + '.json');
    let journal = await readPrivate(path, { missing: true });
    const store = await openFileHostStore(core, { directory: join(root, 'state') });
    try {
      let state = await store.read();
      if (journal) {
        const { identity: record, ...body } = journal;
        need(
          record === core.contentIdentity(body) &&
            journal.request === identity &&
            journal.reason === reason &&
            journal.owner === owner.identity &&
            ['intent', 'paused'].includes(journal.phase),
          'original-intent',
        );
        need(
          core.contentIdentity(state.work) === journal.before ||
            core.contentIdentity(state.work) === journal.paused,
          'work-drift',
        );
      } else {
        const work = state.work;
        need(
          core.protectedWorkRestriction(work) === null &&
            !work.paused &&
            !work.active &&
            work.terminated &&
            work.reconciled &&
            ['pending', 'conversations', 'receipts'].every(
              (key) => work[key].length === 0,
            ),
          'owner-restriction-or-original-work',
        );
        journal = {
          version: 1,
          request: identity,
          reason,
          owner: owner.identity,
          phase: 'intent',
          original: structuredClone(work),
          before: core.contentIdentity(work),
          paused: core.contentIdentity({ ...work, paused: true }),
        };
      }
      const save = async (phase) => {
        const { identity: previous, ...body } = { ...journal, phase };
        journal = { ...body, identity: core.contentIdentity(body) };
        await writePrivate(path, journal);
      };
      await save('intent');
      if (core.contentIdentity(state.work) === journal.before) {
        await store.updateWork({
          expected: state.identity,
          work: { ...state.work, paused: true },
        });
      }
      await save('paused');
      await unused();
      assertLease();
      const result = await operation({ assertLease, journal: identity });
      need(
        (await readNativeOwnerPolicy(core, { root })).identity === owner.identity,
        'owner-drift',
      );
      state = await store.read();
      need(core.contentIdentity(state.work) === journal.paused, 'work-drift');
      if (!keepPaused) {
        await unused();
        await store.updateWork({ expected: state.identity, work: journal.original });
        await save('completed');
      }
      return result;
    } finally {
      await store.close();
    }
  });
}
