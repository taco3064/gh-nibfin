import test from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readdirSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { commands, parse, run } from '../src/cli.mjs';
import { verifyCoreInstallation } from '../src/core.mjs';
const cases = [
  ['init', '--repo', 'owner/repo'],
  ['runner', 'create', '--name', 'local', '--agent', 'opaque'],
  ['runner', 'register', '--runner', 'local', '--repo', 'owner/repo'],
  ['runner', 'unregister', '--runner', 'local', '--org', 'owner'],
  ['runner', 'list'], ['runner', 'remove', '--runner', 'local'], ['upgrade']
];
const leaves = ['init', 'runner create', 'runner register', 'runner unregister', 'runner list', 'runner remove', 'upgrade'];
const executable = resolve('gh-nibfin');
function processRun(args, cwd) {
  return spawnSync(executable, args, { cwd, encoding: 'utf8', env: { PATH: resolve(process.execPath, '..'), HOME: cwd ?? '/nonexistent' } });
}
test('complete seven-leaf routing and help', async () => {
  assert.deepEqual(Object.keys(commands), leaves);
  for (const args of [[], ['--help'], ['runner', '--help'], ...leaves.map(k => [...k.split(' '), '--help'])]) {
    const result = processRun(args);
    assert.equal(result.status, 0); assert.equal(result.stderr, '');
    assert.match(result.stdout, /Usage: gh nibfin/);
  }
  for (const [i, args] of cases.entries()) {
    let calls = 0;
    const handlers = Object.fromEntries(leaves.map(leaf => [leaf, () => { assert.equal(leaf, leaves[i]); calls++; }]));
    assert.equal(await run(args, { handlers }), 0); assert.equal(calls, 1);
    for (const option of Object.keys(commands[leaves[i]])) {
      assert.match(processRun([...leaves[i].split(' '), '--help']).stdout, new RegExp(`--${option}`));
    }
  }
});
test('invalid inputs fail at process boundary and never dispatch', async () => {
  const bad = [
    ['status'], ['runner', 'sign'], ['init', '--secret-value'], ['upgrade', 'extra'],
    ['upgrade', '--check', '--check'], ['init', '--repo'], ['init'],
    ['runner', 'create', '--name', 'a'], ['runner', 'remove'],
    ['runner', 'register', '--runner', 'a'], ['init', '--repo', 'bad'],
    [...cases[2], '--org', 'org'], [...cases[2], '--group', 'g'],
    [...cases[2], '--method', 'wrong'], ['upgrade', '--version', ''],
    ['--help', '--unknown'], ['runner', '--help', '--unknown'],
    ['upgrade', '--help', '--unknown'], ['upgrade', '--handlers', 'success']
  ];
  for (const count of ['0', '-1', '1.5', 'oops', '9007199254740992']) bad.push([...cases[1], '--count', count]);
  for (const args of bad) {
    const result = processRun(args);
    assert.equal(result.status, 2, JSON.stringify(args));
    assert.equal(result.stdout, ''); assert.match(result.stderr, /^Invalid input:/);
    assert.ok(!result.stderr.includes('secret-value'));
    let called = false;
    assert.equal(await run(args, { handlers: Object.fromEntries(leaves.map(k => [k, () => { called = true; }])), stderr() {} }), 2);
    assert.equal(called, false);
  }
});
test('count represents parsed local total; register selects without creation', () => {
  assert.equal(parse(cases[1]).options.count, 2);
  for (const n of [1, 4]) assert.equal(parse([...cases[1], '--count', String(n)]).options.count, n);
  assert.deepEqual({ ...parse(cases[2]).options }, { runner: 'local', repo: 'owner/repo', method: 'manual' });
  const intent = '$(touch secret); arbitrary agent';
  assert.equal(parse(['runner', 'create', '--name', 'a', '--agent', intent]).options.agent, intent);
});
test('credential-free production leaves are unavailable and leave no local state', () => {
  const directory = mkdtempSync(join(tmpdir(), 'nibfin-cli-'));
  try {
    for (const args of cases) {
      const result = processRun(args, directory);
      assert.equal(result.status, 1); assert.equal(result.stdout, '');
      assert.match(result.stderr, /Capability unavailable:.*no approved backend/);
    }
    assert.deepEqual(readdirSync(directory), []);
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
test('missing Core configuration and package fail safely', async () => {
  assert.equal((await verifyCoreInstallation()).ok, false);
  assert.equal((await verifyCoreInstallation({ modulePath: '/nonexistent/core.mjs', expected: { name: 'core', version: '1', contentDigest: 'abc' } })).ok, false);
});
