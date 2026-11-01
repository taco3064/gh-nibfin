import fs from 'node:fs/promises';
import { currentPin } from './current-core.mjs';
import { nativeCycleDirectory, openNativeCycle } from './native-delivery-store.mjs';
import { readNativeMergedContinuation } from './native-handover-context.mjs';
import { verifyNativeLegacyHandover } from './native-legacy-handover.mjs';

const need = (ok, code) => {
  if (!ok) throw Error('native-cycle:' + code);
};

export async function inspectExistingNativeCycle(
  core,
  { root, target, scopeHash, issue, handover },
) {
  try {
    await fs.lstat(nativeCycleDirectory(root, target));
  } catch (error) {
    if (error.code === 'ENOENT') return null;
    throw error;
  }
  const store = await openNativeCycle(core, { root, target });
  const state = await store.read();
  need(state.scopeHash === scopeHash, 'original-cycle-reconciliation-required');
  const refresh = state.core !== currentPin.coreSha256 && !state.continuation;
  if (state.continuation)
    await readNativeMergedContinuation(core, {
      root,
      store,
      state,
      operation: 'readiness',
    });
  let proof;
  if (state.legacyHandover) {
    proof = await verifyNativeLegacyHandover(core, {
      root,
      store,
      state,
      now: refresh ? state.grant.payload.validFrom : Date.now(),
    });
    if (handover)
      need(
        core.contentIdentity(proof.original) === core.contentIdentity(handover),
        'original-handover-drift',
      );
  } else need(!handover, 'original-cycle-has-no-handover');
  core.nextDeliveryStage(state.receipts, null, issue.labels);
  need(
    issue.state === 'open' || Boolean(state.receipts.closure),
    'original-closure-required',
  );
  if (refresh) {
    need(
      handover && proof && core.distributionAPI.unacceptedHandoverRevision === 1,
      'original-cycle-reconciliation-required',
    );
    core.verifyUnacceptedHandoverPrefix({ ...proof, state });
    return { store, previous: { ...proof, state } };
  }
  return { store, previous: null };
}
