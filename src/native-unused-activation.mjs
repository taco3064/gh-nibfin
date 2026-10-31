import fs from 'node:fs/promises';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { withUnusedNativeMaintenance } from './native-unused-maintenance.mjs';
import { verifyNativeRuntime } from './native-control-service.mjs';
import { nativeInstallationSelection } from './managed-upgrade.mjs';
import { openFileHostStore } from './host-store.mjs';
import { readNativeOwnerPolicy } from './native-owner-policy.mjs';
import { readPrivate, writePrivate } from './local-state.mjs';

const need = (ok, reason) => {
  if (!ok) throw Error('native-unused-activation:' + reason);
};
const seal = (core, body) => ({ ...body, identity: core.contentIdentity(body) });
function verify(core, record) {
  const { identity, ...body } = record;
  need(identity === core.contentIdentity(body), 'record');
  return record;
}

/** An activated pool with zero execution history may return to the installer's
 * existing safe upgrade boundary. Preserve the exact activation and Owner pause;
 * an occupied, previously used, held or uncertain host cannot enter this path.
 */
export async function prepareUnusedActivationUpgrade(core, options) {
  const { root, previous, next, memberId, observeResources } = options;
  const path = join(root, 'activation-upgrade.json');
  const parkedPath = join(root, 'activation-previous.json');
  const retained = await readPrivate(path, { missing: true });
  const active = await readPrivate(join(root, 'activation.json'), { missing: true });
  if (!active && !retained) return { status: 'unactivated' };
  if (retained) {
    verify(core, retained);
    need(
      ['intent', 'prepared'].includes(retained.phase) &&
        retained.previous === core.contentIdentity(previous) &&
        retained.next === core.contentIdentity(next),
      'original-upgrade',
    );
  }
  const original = active ?? (await readPrivate(parkedPath));
  verify(core, original);
  need(
    original.version === 1 &&
      original.phase === 'active' &&
      original.installation === core.contentIdentity(previous) &&
      original.memberId === memberId &&
      (!retained || retained.activation === original.identity),
    'original-activation',
  );
  await verifyNativeRuntime(root, previous);
  const oldCore = await import(
    pathToFileURL(join(root, 'runtime/node_modules/@nibfin/core/src/index.mjs')).href
  );
  const expected = nativeInstallationSelection(
    oldCore,
    oldCore.distributionIdentity(),
    previous,
  );
  const store = await openFileHostStore(core, { directory: join(root, 'state') });
  try {
    need(
      core.canonicalJSON((await store.read()).installation) ===
        core.canonicalJSON(expected),
      'installation',
    );
  } finally {
    await store.close();
  }
  const request = core.contentIdentity({
    kind: 'unused-activation-upgrade',
    previous: core.contentIdentity(previous),
    next: core.contentIdentity(next),
    activation: original.identity,
  });
  return withUnusedNativeMaintenance(
    core,
    {
      root,
      configuration: previous.executionPool,
      memberId,
      reason: 'activation-upgrade',
      identity: request,
      keepPaused: true,
      ...(observeResources ? { observeResources } : {}),
    },
    async ({ assertLease }) => {
      const owner = await readNativeOwnerPolicy(core, { root });
      const body = {
        version: 1,
        phase: 'intent',
        previous: core.contentIdentity(previous),
        next: core.contentIdentity(next),
        activation: original.identity,
        owner: owner.identity,
        maintenance: request,
      };
      await writePrivate(path, seal(core, body));
      assertLease();
      const current = await readPrivate(join(root, 'activation.json'), {
        missing: true,
      });
      if (current) {
        need(current.identity === original.identity, 'activation-drift');
        need(
          (await readPrivate(parkedPath, { missing: true })) === null,
          'previous-activation-conflict',
        );
        await fs.rename(join(root, 'activation.json'), parkedPath);
      }
      need(
        (await readPrivate(parkedPath)).identity === original.identity,
        'activation-readback',
      );
      await writePrivate(path, seal(core, { ...body, phase: 'prepared' }));
      return { status: 'prepared', maintenance: request };
    },
  );
}

/** Finish the installer-created pause before publishing the new active marker.
 * User holds and actual execution history cannot be cleared by this receipt.
 */
export async function completeUnusedActivationUpgrade(
  core,
  { root, installation, activation },
) {
  const path = join(root, 'activation-upgrade.json');
  const upgrade = await readPrivate(path, { missing: true });
  if (!upgrade) return;
  verify(core, upgrade);
  const owner = await readNativeOwnerPolicy(core, { root });
  need(
    core.canonicalJSON(await readPrivate(join(root, 'native-installation.json'))) ===
      core.canonicalJSON(installation),
    'installation-drift',
  );
  await verifyNativeRuntime(root, installation);
  need(
    ['prepared', 'completed'].includes(upgrade.phase) &&
      upgrade.next === core.contentIdentity(installation) &&
      activation.installation === upgrade.next &&
      activation.phase === 'prepared' &&
      upgrade.owner === owner.identity,
    'new-activation',
  );
  const maintenancePath = join(
    root,
    'maintenance-events',
    upgrade.maintenance + '.json',
  );
  const journal = verify(core, await readPrivate(maintenancePath));
  const store = await openFileHostStore(core, { directory: join(root, 'state') });
  try {
    const state = await store.read();
    need(
      core.canonicalJSON(state.installation) ===
        core.canonicalJSON(
          nativeInstallationSelection(
            core,
            core.distributionIdentity(),
            installation,
          ),
        ),
      'installed-selection',
    );
    need(core.contentIdentity(state.work) === journal.before, 'work-drift');
  } finally {
    await store.close();
  }
  const { identity: oldJournal, ...journalBody } = journal;
  await writePrivate(
    maintenancePath,
    seal(core, { ...journalBody, phase: 'completed' }),
  );
  const { identity: oldUpgrade, ...upgradeBody } = upgrade;
  await writePrivate(
    path,
    seal(core, {
      ...upgradeBody,
      phase: 'completed',
      nextActivation: core.contentIdentity(activation),
    }),
  );
}

export async function verifyNativeProjectUpgrade(
  core,
  { root, previous, context, installation },
) {
  const journal = await readPrivate(join(root, 'upgrade.json'), { missing: true });
  if (!journal || journal.phase !== 'complete') return false;
  const oldRecord = await readPrivate(
    join(root, 'native-installation-previous.json'),
  );
  await verifyNativeRuntime(root, oldRecord, {
    directory: join(root, 'runtime-previous'),
  });
  const oldCore = await import(
    pathToFileURL(
      join(root, 'runtime-previous/node_modules/@nibfin/core/src/index.mjs'),
    ).href
  );
  const oldIdentity = oldCore.distributionIdentity();
  return (
    core.canonicalJSON(previous.installation) ===
      core.canonicalJSON({
        packageVersion: oldIdentity.packageVersion,
        pins: oldIdentity.pins,
      }) &&
    core.canonicalJSON(journal.current) ===
      core.canonicalJSON(
        nativeInstallationSelection(oldCore, oldIdentity, oldRecord),
      ) &&
    core.canonicalJSON(journal.next) ===
      core.canonicalJSON(
        nativeInstallationSelection(core, context.identity, installation),
      )
  );
}
