import fs from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { createHash } from 'node:crypto';
import { privatePath } from './local-state.mjs';

const need = (ok, reason) => {
  if (!ok) throw Error('native-handover-archive:' + reason);
};
const hash = (bytes) => createHash('sha256').update(bytes).digest('hex');
const fields = (value, names) =>
  need(
    value &&
      Object.getPrototypeOf(value) === Object.prototype &&
      Object.keys(value).sort().join(' ') === names.split(' ').sort().join(' '),
    'schema',
  );
const safePath = (value) =>
  typeof value === 'string' &&
  value.length <= 1024 &&
  !/[\\\x00-\x1f\x7f]/.test(value) &&
  value.split('/').every((part) => part && !['.', '..'].includes(part));
const maxFile = 128 * 1024 * 1024;
const maxTotal = 1024 * 1024 * 1024;

/** Read-only custody verification. Manifest metadata is data, never permission.
 * No executable, private signing key, login or host configuration is imported.
 */
export async function openNativeHandoverArchive(core, { directory, hostRoot }) {
  const root = resolve(directory),
    host = resolve(hostRoot);
  need(
    root !== host && !root.startsWith(host + '/') && !host.startsWith(root + '/'),
    'outside-installation-required',
  );
  await privatePath(root, { directory: true });
  need((await fs.realpath(root)) === root, 'canonical-directory');
  const manifestPath = join(root, 'manifest.json');
  await privatePath(manifestPath);
  const manifestBytes = await fs.readFile(manifestPath);
  need(manifestBytes.length <= 32 * 1024 * 1024, 'manifest-limit');
  const manifest = JSON.parse(manifestBytes);
  fields(manifest, 'format createdAt authority activationAuthorized issues files');
  need(
    manifest.format === 1 &&
      manifest.authority === 'historical-data-only' &&
      manifest.activationAuthorized === false &&
      Number.isFinite(Date.parse(manifest.createdAt)) &&
      manifest.issues &&
      !Array.isArray(manifest.issues) &&
      Array.isArray(manifest.files) &&
      manifest.files.length > 0 &&
      manifest.files.length <= 100000,
    'data-only-manifest',
  );
  const files = new Map();
  let total = 0;
  for (const entry of manifest.files) {
    fields(entry, 'path bytes sha256 sourceMode');
    need(
      safePath(entry.path) &&
        entry.path !== 'manifest.json' &&
        !files.has(entry.path) &&
        Number.isSafeInteger(entry.bytes) &&
        entry.bytes >= 0 &&
        entry.bytes <= maxFile &&
        /^[a-f0-9]{64}$/.test(entry.sha256) &&
        Number.isInteger(entry.sourceMode) &&
        entry.sourceMode >= 0 &&
        entry.sourceMode <= 0o777,
      'file-inventory',
    );
    total += entry.bytes;
    need(total <= maxTotal, 'archive-limit');
    files.set(entry.path, Object.freeze({ ...entry }));
  }
  async function readBytes(path) {
    const entry = files.get(path);
    need(entry, 'unlisted-file');
    const absolute = join(root, path);
    await privatePath(absolute);
    const before = await fs.lstat(absolute);
    const handle = await fs.open(
      absolute,
      fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW,
    );
    try {
      const opened = await handle.stat();
      need(
        opened.ino === before.ino &&
          opened.dev === before.dev &&
          opened.size === entry.bytes &&
          opened.isFile() &&
          opened.nlink === 1 &&
          (opened.mode & 0o077) === 0,
        'file-changed',
      );
      const bytes = await handle.readFile();
      const after = await handle.stat();
      need(
        bytes.length === entry.bytes &&
          hash(bytes) === entry.sha256 &&
          after.size === opened.size &&
          after.mtimeMs === opened.mtimeMs,
        'file-bytes',
      );
      return bytes;
    } finally {
      await handle.close();
    }
  }
  const seen = new Set();
  let directories = 0;
  async function walk(relative = '', depth = 0) {
    need(++directories <= 100000 && depth <= 64, 'directory-limit');
    const absolute = join(root, relative);
    await privatePath(absolute, { directory: true });
    for (const name of await fs.readdir(absolute)) {
      const path = relative ? relative + '/' + name : name;
      need(safePath(path), 'file-path');
      const stat = await fs.lstat(join(root, path));
      if (stat.isDirectory()) {
        await walk(path, depth + 1);
      } else if (path === 'manifest.json') {
        need(hash(await fs.readFile(manifestPath)) === hash(manifestBytes), 'drift');
      } else {
        await readBytes(path);
        seen.add(path);
      }
    }
  }
  await walk();
  need(seen.size === files.size, 'incomplete-archive');
  const candidates = [...files.keys()].filter((path) =>
    /(?:^|\/)cycle\/state\.json$/.test(path),
  );
  return Object.freeze({
    directory: root,
    identity: hash(manifestBytes),
    files,
    bytes: total,
    candidates,
    metadata: structuredClone(manifest.issues),
    readBytes,
    async readManifest() {
      await privatePath(manifestPath);
      const bytes = await fs.readFile(manifestPath);
      need(hash(bytes) === hash(manifestBytes), 'manifest-drift');
      return bytes;
    },
    async readJSON(path) {
      return JSON.parse((await readBytes(path)).toString('utf8'));
    },
    async readCycle(path) {
      need(candidates.includes(path), 'cycle-path');
      const state = JSON.parse((await readBytes(path)).toString('utf8'));
      const { identity, ...body } = state;
      need(state.version === 1 && core.contentIdentity(body) === identity, 'state');
      const base = path.slice(0, -'state.json'.length);
      const records = [...files.keys()].filter((p) => p.startsWith(base));
      need(
        records.every(
          (p) =>
            p === path ||
            /^records\/[a-f0-9]{64}\.json$/.test(p.slice(base.length)) ||
            (p === base + 'records/maintenance.lock' && files.get(p).bytes === 0),
        ),
        'cycle-inventory',
      );
      return {
        state,
        base,
        records: records.filter((p) =>
          /^records\/[a-f0-9]{64}\.json$/.test(p.slice(base.length)),
        ),
      };
    },
  });
}
