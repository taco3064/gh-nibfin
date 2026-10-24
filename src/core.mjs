import { createHash } from 'node:crypto';
import { gunzipSync } from 'node:zlib';
import fs from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

export const producer = '0a2e276ad5b37d94b485bbcf8c7288743b9d158f';
export const archiveDigest = '956b12fb80620e18576212d371989b0448cf39878533b813e8cba838a40c1759';
const provenanceDigest = 'deab63b7275276c84af916f7a27fc6fa76e1af3f5857ea3439c76c320aaab1e4';
const handoff = new URL('../docs/handoffs/n01/', import.meta.url);
const names = ['package.json', 'README.md', 'docs/package.md', 'src/index.mjs'];
const sha = bytes => createHash('sha256').update(bytes).digest('hex');
const reject = message => { throw new Error(`Core verification: ${message}`); };
const zero = bytes => bytes.every(byte => byte === 0);
function field(bytes) {
  const end = bytes.indexOf(0);
  if (end >= 0 && !zero(bytes.subarray(end))) reject('invalid tar field');
  return bytes.subarray(0, end < 0 ? bytes.length : end).toString('ascii');
}
function octal(bytes) {
  const text = bytes.toString('ascii').replace(/[\0 ]+$/, '').trim();
  if (!/^[0-7]+$/.test(text)) reject('invalid tar number');
  return parseInt(text, 8);
}

// Structural inspection is exported for adversarial tests, never a trust override.
export function parseArchive(bytes) {
  if (bytes.length > 16384) reject('archive too large');
  let tar;
  try { tar = gunzipSync(bytes, { maxOutputLength: 32768 }); }
  catch { reject('malformed or oversized gzip'); }
  if (tar.length % 512) reject('truncated tar');
  const files = new Map();
  let offset = 0;
  while (offset < tar.length) {
    const header = tar.subarray(offset, offset + 512);
    if (zero(header)) {
      if (tar.length - offset < 1024 || !zero(tar.subarray(offset))) reject('invalid tar terminator');
      if (files.size !== names.length) reject('missing package files');
      return files;
    }
    let checksum = 0;
    for (let i = 0; i < 512; i++) checksum += i >= 148 && i < 156 ? 32 : header[i];
    if (checksum !== octal(header.subarray(148, 156))) reject('bad tar checksum');
    if (header.subarray(0, 100).some(byte => byte > 127)) reject('non-ASCII tar path');
    const name = field(header.subarray(0, 100));
    if (!names.some(item => name === `package/${item}`) || !zero(header.subarray(345, 500))) reject('unsafe or unexpected path');
    if (![0, 48].includes(header[156]) || !zero(header.subarray(157, 257))) reject('unsupported tar type or link');
    if (files.has(name.slice(8))) reject('duplicate package file');
    const size = octal(header.subarray(124, 136));
    if (size > 8192) reject('oversized package file');
    offset += 512;
    const padded = Math.ceil(size / 512) * 512;
    if (offset + padded > tar.length || !zero(tar.subarray(offset + size, offset + padded))) reject('truncated or invalid tar payload');
    files.set(name.slice(8), tar.subarray(offset, offset + size));
    offset += padded;
  }
  reject('missing tar terminator');
}

export function validateMetadata(files) {
  let pkg;
  try { pkg = JSON.parse(files.get('package.json').toString()); }
  catch { reject('invalid package metadata'); }
  if (pkg.name !== '@nibfin/core' || pkg.version !== '0.1.0' || pkg.type !== 'module' ||
      pkg.exports?.['.'] !== './src/index.mjs' || Object.keys(pkg.exports).length !== 1 ||
      pkg.bin || Object.keys(pkg.dependencies ?? {}).length ||
      ['preinstall', 'install', 'postinstall', 'prepare'].some(key => pkg.scripts?.[key]) ||
      pkg.engines?.node !== '>=22 <23' || pkg.engines?.npm !== '>=10 <11' ||
      !files.get('src/index.mjs')?.length) reject('unapproved package metadata or entry');
}

async function readBounded(url) {
  const handle = await fs.open(url, 'r');
  try {
    const bytes = Buffer.alloc(16385);
    let total = 0;
    while (total < bytes.length) {
      const result = await handle.read(bytes, total, bytes.length - total, null);
      if (!result.bytesRead) break;
      total += result.bytesRead;
    }
    if (total > 16384) reject('handoff input too large');
    return bytes.subarray(0, total);
  } finally { await handle.close(); }
}

export async function verifyCore(directory = handoff) {
  // Expected hashes are compiled from the reviewed handoff, not supplied by input.
  const provenance = await readBounded(new URL('provenance.json', directory));
  if (sha(provenance) !== provenanceDigest) reject('unapproved provenance');
  const bytes = await readBounded(new URL('nibfin-core.tgz', directory));
  if (bytes.length !== 3980 || sha(bytes) !== archiveDigest) reject('unapproved archive');
  const files = parseArchive(bytes);
  const expected = JSON.parse(provenance).files;
  for (const name of names) {
    const value = files.get(name);
    if (value.length !== expected[name].bytes || sha(value) !== expected[name].sha256) reject(`changed ${name}`);
  }
  validateMetadata(files);
  return files;
}

export async function withCore(consume, { directory = handoff, temporaryRoot = tmpdir() } = {}) {
  const files = await verifyCore(directory);
  const root = await fs.mkdtemp(join(temporaryRoot, 'nibfin consumer '));
  try {
    // No dependency manager, scripts, cache, network, or caller package resolution.
    const target = join(root, 'node_modules/@nibfin/core');
    for (const [name, bytes] of files) {
      const destination = join(target, name);
      await fs.mkdir(join(destination, '..'), { recursive: true });
      await fs.writeFile(destination, bytes, { flag: 'wx' });
    }
    const bridge = join(root, 'consumer.mjs');
    await fs.writeFile(bridge, "export * from '@nibfin/core';\n", { flag: 'wx' });
    const core = await import(pathToFileURL(bridge).href);
    return await consume(core);
  } finally {
    await fs.rm(root, { recursive: true, force: true });
  }
}
