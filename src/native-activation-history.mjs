import fs from 'node:fs/promises';
import { join } from 'node:path';
import { readPrivate, ensurePrivateDirectory } from './local-state.mjs';

const need = (ok, reason) => {
  if (!ok) throw Error('native-activation-history:' + reason);
};

// Preserve exact completed records before reusing the next transaction's fixed
// paths. Move the referenced activation first, so interrupted retirement retries
// still have the original journal as their authoritative index.
export async function archiveCompletedActivation(
  core,
  { root, previous, names = ['activation-upgrade'] },
) {
  const parkedNames = {
    'activation-upgrade': 'activation-previous',
    'runtime-repair': 'runtime-repair-activation',
    'completed-upgrade': 'completed-upgrade-activation',
  };
  for (const name of names) {
    need(Object.hasOwn(parkedNames, name), 'journal-kind');
    const source = join(root, name + '.json');
    const journal = await readPrivate(source, { missing: true, limit: 134217728 });
    if (!journal) continue;
    const { identity, ...body } = journal;
    need(
      identity === core.contentIdentity(body) &&
        journal.phase === 'completed' &&
        journal.next === core.contentIdentity(previous),
      'unfinished-activation-upgrade',
    );
    const directory = join(root, 'activation-history', identity);
    await ensurePrivateDirectory(directory);
    const parked = parkedNames[name] + '.json';
    const oldPath = join(root, parked),
      newPath = join(directory, parked);
    const original = await readPrivate(oldPath, { missing: true });
    const activation = original ?? (await readPrivate(newPath));
    const { identity: activationId, ...activationBody } = activation;
    const expected =
      typeof journal.activation === 'string'
        ? journal.activation
        : journal.activation.identity;
    need(
      activationId === core.contentIdentity(activationBody) &&
        activationId === expected,
      'activation-custody',
    );
    if (original) {
      const existing = await readPrivate(newPath, { missing: true });
      need(
        !existing || core.canonicalJSON(existing) === core.canonicalJSON(original),
        'activation-conflict',
      );
      await fs.rename(oldPath, newPath);
    }
    const destination = join(directory, name + '.json');
    const existing = await readPrivate(destination, {
      missing: true,
      limit: 134217728,
    });
    need(
      !existing || core.canonicalJSON(existing) === core.canonicalJSON(journal),
      'journal-conflict',
    );
    await fs.rename(source, destination);
  }
}
