import { archiveCompletedActivation } from './native-activation-history.mjs';
import { requireDrainedNativeWork } from './native-drained-work.mjs';
import fs from 'node:fs/promises';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { maintenanceLock } from './upgrade.mjs';
import { openFileHostStore } from './host-store.mjs';
import { verifyNativeRuntime } from './native-control-service.mjs';
import { observeUnusedNativePaths } from './native-staged-transition.mjs';
import { archiveCompletedNativeUpgrade } from './native-upgrade-history.mjs';
import { nativeInstallationSelection } from './managed-upgrade.mjs';
import { publishNativeUpgradePlan } from './native-upgrade-plan.mjs';
import { observeContinuationRepair } from './native-continuation-repair-proof.mjs';
import { readNativeOwnerPolicy } from './native-owner-policy.mjs';
import { openNativeHandoverArchive } from './native-handover-archive.mjs';
import { openNativeCycle } from './native-delivery-store.mjs';
import { readPrivate, writePrivate, ensurePrivateDirectory } from './local-state.mjs';

const need = (ok, reason) => {
  if (!ok) throw Error('native-runtime-repair:' + reason);
};
const same = (core, a, b) => core.canonicalJSON(a) === core.canonicalJSON(b);
const seal = (core, body) => ({ ...body, identity: core.contentIdentity(body) });
const journalPath = (root) => join(root, 'runtime-repair.json');

export async function readNativeRuntimeRepair(core, root) {
  const record = await readPrivate(journalPath(root), {
    missing: true,
    limit: 134217728,
  });
  if (!record) return null;
  const { identity, ...body } = record;
  need(
    identity === core.contentIdentity(body) &&
      record.version === 1 &&
      ['paused', 'staged', 'activated', 'completed'].includes(record.phase),
    'journal-integrity',
  );
  return record;
}

export function requireRepairWork(core, work, journal) {
  need(
    same(core, work, journal.protectedWork) &&
      work.paused &&
      work.reconciled &&
      work.terminated &&
      !work.active &&
      !work.unknownWrite &&
      work.hold === null,
    'protected-work-drift',
  );
}

export async function nativeRuntimeRepairPending(core, { root, repository }) {
  const journal = await readNativeRuntimeRepair(core, root);
  if (
    !journal ||
    !['activated', 'completed'].includes(journal.phase) ||
    journal.originalState.target.repository !== repository
  )
    return false;
  const active = await readPrivate(join(root, 'activation.json'));
  need(
    active.identity === journal.nextActivation && active.phase === 'active',
    'new-activation',
  );
  const store = await openFileHostStore(core, { directory: join(root, 'state') });
  try {
    const { work } = await store.read();
    if (journal.phase === 'completed' && !work.paused) return false;
    requireRepairWork(core, work, journal);
  } finally {
    await store.close();
  }
  return true;
}

export async function retainRepairActivation(
  core,
  { root, activation, journal, authorizationHost },
) {
  need(
    ['staged', 'activated'].includes(journal.phase) &&
      activation.installation === journal.next &&
      (journal.phase !== 'activated' ||
        journal.nextActivation === activation.identity),
    'activation-installation',
  );
  const { identity, ...body } = journal;
  await writePrivate(
    journalPath(root),
    seal(core, {
      ...body,
      phase: 'activated',
      nextActivation: activation.identity,
      authorizationHost: journal.authorizationHost ?? authorizationHost,
    }),
  );
}

/** A used host enters the normal sealed upgrade only after independent proof
 * that the failed closure submitted no writes and every physical job ended.
 * Pending UNACCEPTED work remains pending; reconciliation concerns effects,
 * never acceptance. The raw work, cycle and old activation stay in this journal.
 */
export async function stageNativeRuntimeRepair(
  core,
  {
    root,
    previous,
    next,
    directory,
    provider,
    context,
    stage,
    control,
    verifyRunner,
    memberId,
    observeResources = observeUnusedNativePaths,
  },
) {
  need(
    directory &&
      previous.core !== next.core &&
      same(core, previous.executionPool, next.executionPool),
    'explicit-handover',
  );
  return maintenanceLock(root, async (assertLease) => {
    await verifyNativeRuntime(root, previous);
    const oldCore = await import(
      pathToFileURL(join(root, 'runtime/node_modules/@nibfin/core/src/index.mjs'))
        .href
    );
    const current = nativeInstallationSelection(
      oldCore,
      oldCore.distributionIdentity(),
      previous,
    );
    const destination = nativeInstallationSelection(core, context.identity, next);
    const owner = await readNativeOwnerPolicy(core, { root });
    need(owner.memberId === memberId, 'owner');
    let journal = await readNativeRuntimeRepair(core, root);
    const active = await readPrivate(join(root, 'activation.json'), {
      missing: true,
    });
    const activation = active ?? journal?.activation;
    need(
      activation?.phase === 'active' &&
        activation.installation === core.contentIdentity(previous) &&
        activation.memberId === memberId,
      'old-activation',
    );
    const { identity: activeId, ...activeBody } = activation;
    need(activeId === core.contentIdentity(activeBody), 'old-activation-integrity');
    if (journal)
      need(
        ['paused', 'staged'].includes(journal.phase) &&
          journal.previous === core.contentIdentity(previous) &&
          journal.next === core.contentIdentity(next) &&
          journal.owner === owner.identity &&
          journal.activation.identity === activation.identity,
        'original-request',
      );
    const drained = () =>
      requireDrainedNativeWork(oldCore, {
        root,
        configuration: previous.executionPool,
        memberId,
        provider,
        verifyRunner,
        observeResources,
      });
    const resources = [
      join(root, 'jobs'),
      ...Array.from({ length: previous.runnerCount }, (_, i) =>
        join(root, 'runner-' + (i + 1)),
      ),
    ];
    await drained();
    const store = await openFileHostStore(core, { directory: join(root, 'state') });
    try {
      const before = await store.read();
      need(same(core, before.installation, current), 'installation-drift');
      const work = before.work;
      if (!journal) {
        need(
          work.restrictionsChecked &&
            !work.paused &&
            !work.active &&
            work.terminated &&
            !work.unknownWrite &&
            work.hold === null &&
            work.conversations.length === 0 &&
            work.pending.length === 1,
          'original-work-boundary',
        );
        const proof = await observeContinuationRepair(core, {
          root,
          directory,
          provider,
          configuration: previous.executionPool,
          execution: {
            // A pure draft proves compatibility; this is never signed or used
            // as execution authority before the actual new activation exists.
            ...(await findContinuation(core, root, directory)).execution,
            core: next.core,
            source: next.producer,
            activation: activation.identity,
            validFrom: Date.now() - 1000,
          },
        });
        const pending = work.pending[0];
        need(
          proof.repair.previous.execution.core === previous.core &&
            proof.repair.previous.execution.source === previous.producer &&
            proof.repair.previous.execution.activation === activation.identity,
          'previous-execution-installation',
        );
        need(
          pending.kind === 'native-stage' &&
            pending.status === 'UNACCEPTED' &&
            pending.cycle === proof.state.cycle &&
            pending.stage === 'closure' &&
            pending.request === proof.state.attempts.closure.request,
          'exact-pending-closure',
        );
        const body = {
          version: 1,
          phase: 'paused',
          previous: core.contentIdentity(previous),
          next: core.contentIdentity(next),
          owner: owner.identity,
          archive: proof.archive.identity,
          directory,
          activation,
          originalWork: work,
          originalState: proof.state,
          protectedWork: { ...work, paused: true, reconciled: true },
        };
        journal = seal(core, body);
        await writePrivate(journalPath(root), journal, { exclusive: true });
      }
      if (same(core, before.work, journal.originalWork))
        await store.updateWork({
          expected: before.identity,
          work: journal.protectedWork,
        });
      requireRepairWork(core, (await store.read()).work, journal);
      await drained();
      assertLease();
      if (active) {
        need(active.identity === journal.activation.identity, 'activation-drift');
        const parked = join(root, 'runtime-repair-activation.json');
        need(!(await readPrivate(parked, { missing: true })), 'park-conflict');
        await fs.rename(join(root, 'activation.json'), parked);
      }
      await control.stageUnit();
      need(
        (await observeResources([join(root, 'runtime'), ...resources])).status ===
          'unused',
        'runtime-resources',
      );
      const upgrade = await readPrivate(join(root, 'upgrade.json'), {
        missing: true,
      });
      if (upgrade)
        await archiveCompletedNativeUpgrade(core, {
          root,
          current,
          installation: previous,
        });
      await archiveCompletedActivation(core, { root, previous });
      const plan = await publishNativeUpgradePlan(core, {
        root,
        previous,
        next,
        current,
        destination,
        stage,
        assertLease,
      });
      requireRepairWork(core, (await store.read()).work, journal);
      const { identity, ...body } = journal;
      await writePrivate(journalPath(root), seal(core, { ...body, phase: 'staged' }));
      return { status: 'staged', plan };
    } finally {
      await store.close();
    }
  });
}

async function findContinuation(core, root, directory) {
  const archive = await openNativeHandoverArchive(core, {
    directory,
    hostRoot: root,
  });
  const values = [];
  for (const path of archive.candidates) {
    const cycle = await archive.readCycle(path);
    try {
      const store = await openNativeCycle(core, { root, target: cycle.state.target });
      const state = await store.read();
      if (state.continuation && !state.completion) values.push(state.continuation);
    } catch (error) {
      if (error.code !== 'ENOENT') throw error;
    }
  }
  need(values.length === 1, 'one-continuation');
  return values[0];
}
