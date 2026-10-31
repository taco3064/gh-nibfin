import { join } from 'node:path';
import { readPrivate, writePrivate } from './local-state.mjs';

// Host owns persistence; Core owns response classification and reset deadlines.
export async function nativeGithubRateLimit(root, { now = Date.now() } = {}) {
  const value = await readPrivate(join(root, 'github-rate-limit.json'), {
    missing: true,
  });
  return value?.version === 1 &&
    Number.isSafeInteger(value.resetAt) &&
    value.resetAt > now
    ? value
    : null;
}

let tail = Promise.resolve();
export function retainNativeGithubRateLimit(core, root, response, options = {}) {
  const next = tail.then(() => retain(core, root, response, options));
  tail = next.catch(() => {});
  return next;
}

async function retain(core, root, response, options) {
  const cooldown = core.githubRateLimit(response, options);
  if (!cooldown) return;
  const path = join(root, 'github-rate-limit.json');
  const old = await readPrivate(path, { missing: true });
  if (old?.resetAt >= cooldown.resetAt) return;
  await writePrivate(path, cooldown);
}
