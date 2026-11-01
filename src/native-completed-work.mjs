import fs from 'node:fs/promises';
import { join } from 'node:path';
import { privatePath, readPrivate } from './local-state.mjs';
import { openNativeCycle } from './native-delivery-store.mjs';
import { readCompletedNativeCycle } from './native-completed-cycle.mjs';

const need = (ok, reason) => {
  if (!ok) throw Error('native-completed-work:' + reason);
};

// The completed-work upgrade profile cannot reinterpret absent or unfinished
// cycles as an unused host. Every retained cycle must independently verify.
export async function observeCompletedNativeWork(core, { root, configuration }) {
  const directory = join(root, 'delivery');
  await privatePath(directory, { directory: true });
  const repositories = await fs.readdir(directory);
  need(repositories.length > 0 && repositories.length <= 256, 'cycle-inventory');
  const results = [];
  for (const repositoryId of repositories) {
    need(/^[1-9][0-9]*$/.test(repositoryId), 'repository-inventory');
    const selected = configuration.targets.find(
      (t) => t.repositoryId === repositoryId,
    );
    need(selected, 'original-project');
    const parent = join(directory, repositoryId);
    await privatePath(parent, { directory: true });
    for (const databaseId of await fs.readdir(parent)) {
      if (databaseId === 'maintenance.lock') continue;
      need(
        /^[1-9][0-9]*$/.test(databaseId) && results.length < 1000,
        'original-cycle-inventory',
      );
      const stored = await readPrivate(join(parent, databaseId, 'state.json'), {
        limit: 134217728,
      });
      const target = stored.target;
      need(
        target?.repositoryId === repositoryId &&
          target.databaseId === databaseId &&
          target.repository === selected.repository,
        'original-target',
      );
      const store = await openNativeCycle(core, { root, target });
      const state = await store.read();
      const completed = await readCompletedNativeCycle(core, { root, store, state });
      results.push({ target, state: state.identity, completion: completed.identity });
    }
  }
  need(results.length > 0, 'completed-cycle-required');
  return results;
}
