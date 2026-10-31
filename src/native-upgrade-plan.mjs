import fs from 'node:fs/promises';
import { join } from 'node:path';
import { readPrivate, writePrivate, sha } from './local-state.mjs';
const need = (ok, reason) => {
  if (!ok) throw Error('native-upgrade-plan:' + reason);
};
const absent = async (path) => {
  try {
    await fs.lstat(path);
    return false;
  } catch (error) {
    if (error.code === 'ENOENT') return true;
    throw error;
  }
};

export async function publishNativeUpgradePlan(
  core,
  { root, previous, next, current, destination, stage, assertLease },
) {
  await stage();
  assertLease();
  const nextPath = join(root, 'native-installation-next.json'),
    bytes = JSON.stringify(next, null, 2) + '\n';
  if (await absent(nextPath)) await writePrivate(nextPath, next, { exclusive: true });
  else
    need(
      (await fs.readFile(nextPath)).equals(Buffer.from(bytes)),
      'next-metadata-conflict',
    );
  const inventory = (record) =>
    record.runtimeInventory.map((f) => ({ kind: 'file', ...f, mode: 0o600 }));
  const metadata = (bytes) => [
    {
      kind: 'file',
      path: 'native-installation.json',
      mode: 0o600,
      sha256: sha(bytes),
    },
  ];
  const body = {
    version: 2,
    current,
    next: destination,
    resources: [
      { name: 'runtime', current: inventory(previous), next: inventory(next) },
      {
        name: 'installation',
        current: metadata(await fs.readFile(join(root, 'native-installation.json'))),
        next: metadata(bytes),
      },
    ],
  };
  const plan = { ...body, identity: core.contentIdentity(body) },
    oldPlan = await readPrivate(join(root, 'upgrade-plan.json'), {
      missing: true,
      limit: 4000000,
    });
  need(
    !oldPlan || core.canonicalJSON(oldPlan) === core.canonicalJSON(plan),
    'original-plan-conflict',
  );
  if (!oldPlan)
    await writePrivate(join(root, 'upgrade-plan.json'), plan, { exclusive: true });

  return plan;
}
