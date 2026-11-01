import { inspectLegacyNativeCustody } from './native-legacy-custody.mjs';
import { readNativeOwnerPolicy } from './native-owner-policy.mjs';
import { prepareNativeCycle } from './native-cycle-policy.mjs';
import { withNativeHandoverMaintenance } from './native-handover-maintenance.mjs';
import { verifyNativeLegacyHandover } from './native-legacy-handover.mjs';

const need = (ok, reason) => {
  if (!ok) throw Error('native-legacy-import:' + reason);
};

export async function importNativeUnacceptedWork(
  core,
  { root, archive, repository, configuration, provider, context, checkOnly },
) {
  const owner = await readNativeOwnerPolicy(core, { root });
  const results = [];
  for (const [number, metadata] of Object.entries(archive.metadata)) {
    if (metadata.status !== 'UNACCEPTED') continue;
    need(/^[1-9][0-9]*$/.test(number), 'issue-number');
    const prefix = 'g' + number;
    const source = await archive.readJSON(prefix + '/state.json');
    if (source.payload?.pending?.repository !== repository) continue;
    const issueId = source.payload.pending.issueId;
    const original = await inspectLegacyNativeCustody(core, {
      archive,
      prefix,
      repository,
      issueId,
      number,
      memberId: owner.memberId,
    });
    const inputs = {
      root,
      configuration,
      repository,
      number,
      provider,
      context,
      handover: original,
    };
    const checked = await prepareNativeCycle(core, { ...inputs, checkOnly: true });
    if (typeof checked.read === 'function') {
      const state = await checked.read();
      const proof = await verifyNativeLegacyHandover(core, {
        root,
        store: checked,
        state,
      });
      need(proof !== null, 'existing-handover-required');
      results.push({
        outcome: 'existing-unaccepted',
        target: state.target,
        next: core.nextDeliveryStage(state.receipts, null),
        status: 'UNACCEPTED',
      });
      continue;
    }
    if (checkOnly) {
      results.push({ ...checked, status: 'UNACCEPTED' });
      continue;
    }
    const request = core.contentIdentity({
      kind: 'native-unaccepted-handover-maintenance',
      owner: owner.identity,
      archive: archive.identity,
      state: original.state,
      current: checked.handover,
    });
    const imported = await withNativeHandoverMaintenance(
      core,
      { root, configuration, request },
      () => prepareNativeCycle(core, inputs),
    );
    const state = await imported.read();
    const verified = await verifyNativeLegacyHandover(core, {
      root,
      store: imported,
      state,
    });
    need(verified !== null, 'publication-readback');
    results.push({
      outcome: 'imported-unaccepted',
      target: state.target,
      status: 'UNACCEPTED',
      originalSession: verified.plan.session,
      originalRepairCount: verified.plan.repairs,
      next: core.nextDeliveryStage(state.receipts, null),
    });
  }
  return results;
}
