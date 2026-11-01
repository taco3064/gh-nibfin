import fs from 'node:fs/promises';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { readPrivate, writePrivate } from './local-state.mjs';
import { openFileHostStore } from './host-store.mjs';
import { readNativeOwnerPolicy } from './native-owner-policy.mjs';
import { verifyNativeRuntime } from './native-control-service.mjs';
import { nativeInstallationSelection } from './managed-upgrade.mjs';
import { observeCompletedNativeWork } from './native-completed-work.mjs';
import { requireDrainedNativeWork } from './native-drained-work.mjs';
import { archiveCompletedNativeUpgrade } from './native-upgrade-history.mjs';
import { archiveCompletedActivation } from './native-activation-history.mjs';
import { publishNativeUpgradePlan } from './native-upgrade-plan.mjs';
import { observeUnusedNativePaths } from './native-staged-transition.mjs';
import {
  withNativeHandoverMaintenance,
  releaseNativeHandoverMaintenance,
} from './native-handover-maintenance.mjs';

const need = (ok, reason) => {
  if (!ok) throw Error('native-completed-upgrade:' + reason);
};
const same = (core, a, b) => core.canonicalJSON(a) === core.canonicalJSON(b);
const seal = (core, body) => ({ ...body, identity: core.contentIdentity(body) });
const journalPath = (root) => join(root, 'completed-upgrade.json');

export async function readCompletedNativeUpgrade(core, root) {
  const record = await readPrivate(journalPath(root), { missing: true });
  if (!record) return null;
  const { identity, ...body } = record;
  need(
    identity === core.contentIdentity(body) &&
      record.version === 1 &&
      record.kind === 'native-completed-work-upgrade' &&
      ['intent', 'staged', 'completed'].includes(record.phase),
    'journal',
  );
  return record;
}

// A used, idle host with only verified terminal cycles enters the existing
// sealed upgrade owner. Unaccepted or uncertain cycles require their own bound
// continuation; this profile never clears their history to manufacture idleness.
export async function stageCompletedNativeUpgrade(core, options) {
  const {
    root,
    previous,
    next,
    context,
    memberId,
    provider,
    verifyRunner,
    control,
    stage,
    observeResources,
  } = options;
  const store = await openFileHostStore(core, { directory: join(root, 'state') });
  try {
    if ((await store.read()).work.pending.length !== 0)
      return { status: 'not-applicable' };
  } finally {
    await store.close();
  }
  need(same(core, previous.executionPool, next.executionPool), 'pool-membership');
  const owner = await readNativeOwnerPolicy(core, { root });
  need(owner.memberId === memberId, 'owner');
  let journal = await readCompletedNativeUpgrade(core, root);
  const active = await readPrivate(join(root, 'activation.json'), { missing: true });
  const activation =
    active ??
    (await readPrivate(join(root, 'completed-upgrade-activation.json'), {
      missing: true,
    }));
  need(
    activation?.phase === 'active' &&
      activation.memberId === memberId &&
      activation.installation === core.contentIdentity(previous),
    'original-activation',
  );
  const { identity: activationId, ...activationBody } = activation;
  need(activationId === core.contentIdentity(activationBody), 'activation-integrity');
  const request = core.contentIdentity({
    kind: 'native-completed-work-upgrade',
    previous: core.contentIdentity(previous),
    next: core.contentIdentity(next),
    activation: activationId,
    owner: owner.identity,
  });
  if (journal && journal.phase !== 'completed')
    need(
      journal.previous === core.contentIdentity(previous) &&
        journal.next === core.contentIdentity(next) &&
        journal.maintenance === request &&
        journal.owner === owner.identity,
      'original-request',
    );
  await verifyNativeRuntime(root, previous);
  const oldCore = await import(
    pathToFileURL(join(root, 'runtime/node_modules/@nibfin/core/src/index.mjs')).href
  );
  const current = nativeInstallationSelection(
    oldCore,
    oldCore.distributionIdentity(),
    previous,
  );
  const destination = nativeInstallationSelection(core, context.identity, next);
  return withNativeHandoverMaintenance(
    core,
    {
      root,
      configuration: previous.executionPool,
      request,
      keepPaused: true,
      restoreOnFailure: false,
      observeResources,
    },
    async ({ assertLease }) => {
      const host = await openFileHostStore(core, { directory: join(root, 'state') });
      try {
        need(
          same(core, (await host.read()).installation, current),
          'installation-drift',
        );
      } finally {
        await host.close();
      }
      const drained = () =>
        requireDrainedNativeWork(oldCore, {
          root,
          configuration: previous.executionPool,
          memberId,
          provider,
          verifyRunner,
          observeResources,
        });
      await drained();
      const cycles = await observeCompletedNativeWork(oldCore, {
        root,
        configuration: previous.executionPool,
      });
      if (!journal || journal.phase === 'completed') {
        await archiveCompletedActivation(core, {
          root,
          previous,
          names: ['activation-upgrade', 'runtime-repair', 'completed-upgrade'],
        });
        const body = {
          version: 1,
          kind: 'native-completed-work-upgrade',
          phase: 'intent',
          previous: core.contentIdentity(previous),
          next: core.contentIdentity(next),
          activation: activationId,
          maintenance: request,
          owner: owner.identity,
          cycles,
        };
        await writePrivate(journalPath(root), seal(core, body), { exclusive: true });
        journal = seal(core, body);
      } else need(same(core, journal.cycles, cycles), 'cycle-drift');
      if (active) {
        const parked = join(root, 'completed-upgrade-activation.json');
        need(
          (await readPrivate(parked, { missing: true })) === null,
          'activation-park-conflict',
        );
        await fs.rename(join(root, 'activation.json'), parked);
      }
      await control.stageUnit();
      const stopped = await drained();
      need(
        (
          await (observeResources ?? observeUnusedNativePaths)([
            join(root, 'runtime'),
            ...stopped.resources,
          ])
        ).status === 'unused',
        'runtime-resources',
      );
      if (await readPrivate(join(root, 'upgrade.json'), { missing: true }))
        await archiveCompletedNativeUpgrade(core, {
          root,
          current,
          installation: previous,
        });
      assertLease();
      const plan = await publishNativeUpgradePlan(core, {
        root,
        previous,
        next,
        current,
        destination,
        stage,
        assertLease,
      });
      need(
        same(
          core,
          cycles,
          await observeCompletedNativeWork(oldCore, {
            root,
            configuration: previous.executionPool,
          }),
        ),
        'cycle-drift',
      );
      const { identity, ...body } = journal;
      await writePrivate(journalPath(root), seal(core, { ...body, phase: 'staged' }));
      return { status: 'staged', plan };
    },
  );
}

export async function completeCompletedNativeUpgrade(
  core,
  { root, installation, activation },
) {
  const journal = await readCompletedNativeUpgrade(core, root);
  if (!journal) return;
  const owner = await readNativeOwnerPolicy(core, { root });
  need(
    ['staged', 'completed'].includes(journal.phase) &&
      journal.next === core.contentIdentity(installation) &&
      journal.owner === owner.identity &&
      activation.installation === journal.next &&
      activation.phase === 'prepared',
    'actual-new-installation',
  );
  await verifyNativeRuntime(root, installation);
  const cycles = await observeCompletedNativeWork(core, {
    root,
    configuration: installation.executionPool,
  });
  need(same(core, cycles, journal.cycles), 'historical-work-drift');
  const active = { ...activation, phase: 'active' };
  const nextActivation = core.contentIdentity(active);
  if (journal.phase === 'completed')
    need(journal.nextActivation === nextActivation, 'activation-drift');
  await releaseNativeHandoverMaintenance(core, {
    root,
    request: journal.maintenance,
  });
  const { identity, ...body } = journal;
  await writePrivate(
    journalPath(root),
    seal(core, { ...body, phase: 'completed', nextActivation }),
  );
}
