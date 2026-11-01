import { observeNativeCycles } from './native-cycle-inventory.mjs';
import { readCompletedNativeCycle } from './native-completed-cycle.mjs';

export function observeCompletedNativeWork(core, options) {
  return observeNativeCycles(core, options, async ({ root, store, state }) => {
    const completed = await readCompletedNativeCycle(core, { root, store, state });
    return { completion: completed.identity };
  });
}
