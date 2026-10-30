import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { spawnSync } from 'node:child_process';
import { gzipSync, gunzipSync } from 'node:zlib';
import { createHash } from 'node:crypto';
import { verifyCore, parseArchive, validateMetadata } from '../src/core.mjs';
import { withCore } from '../src/cli.mjs';
const checkout = resolve(import.meta.dirname, '..');
const handoff = join(checkout, 'docs/handoffs/n01');
const archive = await fs.readFile(join(handoff, 'nibfin-core.tgz'));
const environment = { PATH: process.env.PATH, HOME: '/nonexistent', LANG: 'C', TMPDIR: tmpdir() };
async function scratch(run) {
  const root = await fs.mkdtemp(join(tmpdir(), 'nibfin test '));
  try { return await run(root); }
  finally { await fs.rm(root, { recursive: true, force: true }); }
}
function cli(args, cwd = tmpdir(), executable = join(checkout, 'gh-nibfin'), env = environment) {
  const result = spawnSync(executable, args, { cwd, env, encoding: 'utf8' });
  assert.ifError(result.error);
  assert.doesNotMatch(result.stdout + result.stderr, /credential-sentinel/);
  return result;
}
function checksum(header) {
  header.fill(32, 148, 156);
  const sum = header.reduce((a, b) => a + b, 0).toString(8).padStart(6, '0');
  header.write(`${sum}\0 `, 148, 'ascii');
}
function tarFixture(entries) {
  const parts = [];
  for (const { name, bytes = Buffer.from('x'), type = 48, size = bytes.length } of entries) {
    const header = Buffer.alloc(512);
    header.write(name);
    header.write(size.toString(8).padStart(11, '0') + '\0', 124);
    header[156] = type;
    checksum(header);
    parts.push(header, bytes, Buffer.alloc((512 - bytes.length % 512) % 512));
  }
  return gzipSync(Buffer.concat([...parts, Buffer.alloc(1024)]));
}
const validFiles = await verifyCore();
const entries = [...validFiles].map(([name, bytes]) => ({ name: `package/${name}`, bytes }));

test('executable diagnostics, exact seven paths and strict grammar', async () => {
  assert.ok((await fs.stat(join(checkout, 'gh-nibfin'))).mode & 0o111);
  for (const cwd of [checkout, tmpdir()]) {
    for (const args of [[], ['--help'], ['--version']]) {
      const result = cli(args, cwd);
      assert.equal(result.status, 0, result.stderr);
      assert.match(result.stdout, args[0] === '--version' ? /@nibfin\/core@0.1.0; producer aee2bb305be1e384399a06682e2bd54d135f2f24/ : /default 2; positive integer total/);
    }
  }
  const paths = ['upgrade'];
  for (const path of paths) {
    const result = cli(path.split(' '));
    assert.equal(result.status, 2);
    assert.match(result.stderr, /upgrade: .*preserve the original native installation/s);
    assert.match(cli(['--help']).stdout, new RegExp(path));
  }
  const init=cli(['init']);assert.equal(init.status,2);assert.match(init.stderr,/Select --repo|Use init/);
  const removal = cli(['runner','remove']);
  assert.equal(removal.status, 2);
  assert.match(removal.stderr, /Use --runner NAME \[--yes\]/);
  assert.match(cli(['--help']).stdout, /runner remove --runner NAME/);
  for (const args of [['runner'], ['help'], ['version'], ['status'], ['runner', 'unknown'], ['--unknown'], ['init', 'extra'], ['init', '--help'], ['--version', 'extra'], ['runner create'], ['init', 'a b', '$(false)']]) {
    const result = cli(args);
    assert.equal(result.status, 2);
    if(args[0]==='init')assert.match(result.stderr,/Use init/);
    else {assert.match(result.stderr,/Use gh nibfin --help/);assert.ok(result.stderr.includes(JSON.stringify(args)),'argument boundaries preserved');}
  }
});

test('checkout with spaces; missing and incompatible Node diagnostics', () => scratch(async root => {
  const copy = join(root, 'extension with spaces');
  await fs.cp(checkout, copy, { recursive: true });
  const entry = join(copy, 'gh-nibfin');
  assert.equal(cli(['--version'], root, entry).status, 0);
  const bin = join(root, 'bin');
  await fs.mkdir(bin);
  const missing = cli(['--help'], root, entry, { PATH: bin });
  assert.equal(missing.status, 1);
  assert.match(missing.stderr, /install Node.js 22.x/);
  // Real maintained entry with a simulated unsupported runtime version.
  const preload = join(root, 'version.cjs');
  await fs.writeFile(preload, "Object.defineProperty(process.versions, 'node', {value:'20.0.0'});");
  const incompatible = cli(['--help'], root, entry, { ...environment, NODE_OPTIONS: `--require=${JSON.stringify(preload)}` });
  assert.equal(incompatible.status, 1);
  assert.match(incompatible.stderr, /Install Node.js 22.x/);
}));

test('actual public producer exports preserve identity and reject invalid inputs', () => scratch(async root => {
  await withCore(core => {
    assert.equal(core.githubId('9007199254740993'), '9007199254740993');
    assert.equal(core.memberRunnerLabel('42'), 'nibfin-user-42');
    assert.deepEqual(core.repositoryCoordinates('Example-Org', 'Core'), { owner: 'example-org', repository: 'core', key: 'example-org/core' });
    assert.equal(core.repositoryCoordinates('Person', 'repo').key, 'person/repo');
    for (const value of [42, 0, '0', '01', '-1', '+1', '1.2', ' 1', 'a', undefined]) {
      assert.throws(() => core.githubId(value), TypeError);
      assert.throws(() => core.memberRunnerLabel(value), TypeError);
    }
    for (const pair of [['a--b', 'repo'], ['-a', 'repo'], ['a/', 'repo'], ['a', '../repo'], ['a', '..'], ['a', 'x\\y'], ['', 'x'], ['a', 'x\n'], [undefined, 'x']]) {
      assert.throws(() => core.repositoryCoordinates(...pair), TypeError);
    }
  }, { temporaryRoot: root });
  assert.deepEqual(await fs.readdir(root), []);
}));

test('production trust pins reject changed, truncated, missing and self-consistent substitutes before execution', () => scratch(async root => {
  const directory = pathToFileURL(root + '/');
  const provenance = await fs.readFile(join(handoff, 'provenance.json'));
  await fs.writeFile(join(root, 'provenance.json'), provenance);
  await assert.rejects(withCore(() => assert.fail('must not execute'), { directory }), /ENOENT/);
  for (const bytes of [archive.subarray(0, 100), Buffer.alloc(16385), Buffer.from(archive).fill(0, 100, 101), tarFixture([{ name: 'package/package.json', bytes: Buffer.from('{"scripts":{"install":"touch marker"}}') }])]) {
    await fs.writeFile(join(root, 'nibfin-core.tgz'), bytes);
    await assert.rejects(withCore(() => assert.fail('must not execute'), { directory }), /unapproved archive|input too large/);
  }
  const replacement = tarFixture([{ name: 'package/src/index.mjs', bytes: Buffer.from("throw new Error('executed payload');") }]);
  await fs.writeFile(join(root, 'nibfin-core.tgz'), replacement);
  const altered = JSON.parse(provenance);
  altered.artifact.sha256 = createHash('sha256').update(replacement).digest('hex');
  altered.artifact.bytes = replacement.length;
  await fs.writeFile(join(root, 'provenance.json'), JSON.stringify(altered));
  await assert.rejects(withCore(() => assert.fail('must not execute'), { directory }), /unapproved provenance/);
  assert.deepEqual((await fs.readdir(root)).sort(), ['nibfin-core.tgz', 'provenance.json']);
}));

test('structural rejection without any production digest override', () => {
  assert.equal(parseArchive(tarFixture(entries)).size, 4);
  const badChecksum = gunzipSync(archive);
  badChecksum[0] ^= 1;
  const malformed = [Buffer.from('not gzip'), gzipSync(Buffer.alloc(17)), gzipSync(badChecksum), gzipSync(Buffer.alloc(33000)), Buffer.alloc(16385)];
  for (const name of ['../escape', '/absolute', 'package/../escape', 'package/extra']) malformed.push(tarFixture([{ name }]));
  for (const type of [49, 50, 53, 120, 103]) malformed.push(tarFixture([{ ...entries[0], type }]));
  malformed.push(tarFixture([...entries, entries[0]]), tarFixture(entries.slice(1)), tarFixture([{ ...entries[0], size: 9000 }]), tarFixture([{ ...entries[0], size: 8000 }]));
  const nonzeroTail = gunzipSync(tarFixture(entries));
  nonzeroTail[nonzeroTail.length - 1] = 1;
  malformed.push(gzipSync(nonzeroTail), gzipSync(gunzipSync(archive).subarray(0, 512)));
  for (const bytes of malformed) assert.throws(() => parseArchive(bytes), /Core verification/);
  for (const change of [{ name: 'evil' }, { exports: { '.': './evil.mjs' } }, { scripts: { install: 'touch marker' } }, { dependencies: { evil: '*' } }, { bin: 'evil' }, { type: 'commonjs' }]) {
    const files = new Map(validFiles);
    files.set('package.json', Buffer.from(JSON.stringify({ ...JSON.parse(files.get('package.json')), ...change })));
    assert.throws(() => validateMetadata(files), /unapproved package/);
  }
  const files = new Map(validFiles);
  files.set('package.json', Buffer.from('{'));
  assert.throws(() => validateMetadata(files), /invalid package/);
  files.set('package.json', validFiles.get('package.json'));
  files.delete('src/index.mjs');
  assert.throws(() => validateMetadata(files), /entry/);
});

test('consumer cleanup on write, import and callback failures; no caller mutation', () => scratch(async root => {
  const before = await fs.readFile(join(checkout, 'README.md'));
  await fs.writeFile(join(root, 'caller-marker'), 'untouched');
  const original = fs.writeFile;
  for (const failure of ['installation', 'import']) {
    fs.writeFile = async function (path, bytes, options) {
      if (failure === 'installation' && String(path).endsWith('package.json')) throw new Error('installation failure');
      return original.call(this, path, failure === 'import' && String(path).endsWith('consumer.mjs') ? 'invalid syntax !' : bytes, options);
    };
    try { await assert.rejects(withCore(() => assert.fail('unreachable'), { temporaryRoot: root })); }
    finally { fs.writeFile = original; }
    assert.deepEqual(await fs.readdir(root), ['caller-marker']);
  }
  await assert.rejects(withCore(() => { throw new Error('callback failure'); }, { temporaryRoot: root }), /callback failure/);
  assert.deepEqual(await fs.readdir(root), ['caller-marker']);
  assert.equal(await fs.readFile(join(root, 'caller-marker'), 'utf8'), 'untouched');
  assert.deepEqual(await fs.readFile(join(checkout, 'README.md')), before);
}));

test('credential sentinels are not read or exposed by diagnostics and refusals', () => scratch(async root => {
  await fs.mkdir(join(root, '.config/gh'), { recursive: true });
  await fs.writeFile(join(root, '.config/gh/hosts.yml'), 'credential-sentinel');
  await fs.mkdir(join(root, '.codex'));
  await fs.writeFile(join(root, '.codex/auth.json'), 'credential-sentinel');
  const env = { ...environment, HOME: root, GH_TOKEN: 'credential-sentinel', GITHUB_TOKEN: 'credential-sentinel', OPENAI_API_KEY: 'credential-sentinel' };
  for (const args of [['--help'], ['--version'], ['init']]) {
    const result = cli(args, root, undefined, env);
    assert.equal(result.status, args[0] === 'init' ? 2 : 0);
  }
  assert.equal(await fs.readFile(join(root, '.codex/auth.json'), 'utf8'), 'credential-sentinel');
}));
