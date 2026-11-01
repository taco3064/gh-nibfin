import { observeNativeCycles } from './native-cycle-inventory.mjs';
import { readCompletedNativeCycle } from './native-completed-cycle.mjs';
import { verifyNativeLegacyHandover } from './native-legacy-handover.mjs';

export function observePreservedNativeWork(core, options) {
  return observeNativeCycles(core, options, async ({ root, store, state }) => {
    if (state.closed) {
      const completed = await readCompletedNativeCycle(core, { root, store, state });
      return { completion: completed.identity };
    }
    const proof = await verifyNativeLegacyHandover(core, {
      root,
      store,
      state,
      now: state.grant.payload.validFrom,
    });
    if (!proof || core.distributionAPI.unacceptedHandoverRevision !== 1)
      throw Error('native-preserved-work:preexecution-handover-required');
    const prefix = core.verifyUnacceptedHandoverPrefix({ ...proof, state });
    return {
      unaccepted: prefix.previous,
      archive: proof.original.archive,
      session: prefix.session,
      repairs: prefix.repairs,
    };
  });
}
