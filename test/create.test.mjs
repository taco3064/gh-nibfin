import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { join, resolve, sep } from 'node:path';
import { tmpdir, userInfo } from 'node:os';
import { spawnSync } from 'node:child_process';
import { pathToFileURL } from 'node:url';
import { createEnvironment, options, processEnvironment, githubObservation, observeProcess } from '../src/create.mjs';

const checkout = resolve(import.meta.dirname, '..');
const sentinel = 'credential-sentinel';
async function scratch(run) {
  const root = await fs.mkdtemp(join(tmpdir(), 'create proof '));
  try { return await run(root); } finally { await fs.rm(root, { recursive: true, force: true }); }
}
// Contract double only: these observations do not measure native isolation/login.
const contractSource = `
export function contract(state = {}) {
 return {
  createAgentEnvironments: () => ({ describe(value) {
   const ids = [value.id, ...value.instances.map(i => i.id)];
   if (new Set(ids).size !== ids.length) throw Error('aliased ID');
   const paths = [value.loginSpace, ...value.instances.flatMap(i => [i.workspace, i.registration, i.service])];
   for (const p of paths) {
    if (!p.startsWith('/') || p === '/' || p.includes('/../')) throw Error('invalid path');
    if (paths.some(q => p !== q && p.startsWith(q + '/'))) throw Error('overlap');
   }
   if (new Set(paths).size !== paths.length) throw Error('aliased path');
   return Object.freeze(value);
  }}),
  createLinuxCodexBackend(args) {
   state.calls?.push(args);
   const forbidden = () => { throw Error('must never start/register/install'); };
   return { readiness: async () => { if (state.error) throw Error('credential-sentinel'); return state.readiness ?? { ready: true }; },
    start: forbidden, collect: forbidden, stop: forbidden, termination: forbidden, cleanup: forbidden };
  }
 };
}
`;
const { contract } = await import('data:text/javascript,' + encodeURIComponent(contractSource));
function run(command, args, environment) {
  assert.ok(['git', 'gh'].includes(command));
  assert.ok(['--version', 'auth', 'api'].includes(args[0]));
  assert.equal(environment.GH_TOKEN, undefined);
  return { ok: true, identity: command === 'gh' && args[0] === 'api' ? '42' : undefined };
}
const args = (name = 'sample', count) => ['--name', name, '--agent', 'codex', ...(count === undefined ? [] : ['--count', String(count)])];
const deps = root => ({ root: join(root, 'storage'), run, core: async () => contract() });
async function absent(path) { await assert.rejects(fs.stat(path), { code: 'ENOENT' }); }

test('executable-level local contract proof: unrelated cwd, spaces, default/1/4; exact persisted allocation', () => scratch(async root => {
  const copy = join(root, 'extension with spaces');
  await fs.cp(checkout, copy, { recursive: true });
  const harness = join(root, 'local proof.mjs');
  await fs.writeFile(harness, `import {main} from ${JSON.stringify(pathToFileURL(join(copy, 'src/cli.mjs')).href)};\n${contractSource}\nprocess.exitCode = await main(process.argv.slice(2), {root: ${JSON.stringify(join(root, 'storage'))}, core: async () => contract(), run: async (c,a) => ({ok:true, identity:c==='gh'&&a[0]==='api'?'42':undefined})});`);
  for (const [name, count, expected] of [['default', undefined, 2], ['one', 1, 1], ['four', 4, 4]]) {
    const result = spawnSync(process.execPath, [harness, 'runner', 'create', ...args(name, count)], { cwd: tmpdir(), encoding: 'utf8', env: { PATH: process.env.PATH, HOME: '/nonexistent' } });
    assert.equal(result.status, 0, result.stderr);
    const base = join(root, 'storage', name);
    const record = JSON.parse(await fs.readFile(join(base, 'environment.json')));
    assert.equal(record.instances.length, expected);
    assert.equal(record.agent, 'codex');
    assert.equal(new Set(record.instances.map(i => i.id)).size, expected);
    assert.equal((await fs.readdir(base)).length, expected + 2);
    for (const instance of record.instances) {
      assert.equal(instance.loginSpace, record.loginSpace);
      assert.equal(instance.status, 'unregistered');
      assert.equal(instance.serviceStatus, 'not-installed');
      for (const path of [instance.workspace, instance.registration, instance.service]) assert.deepEqual(await fs.readdir(path), []);
      for (const key of ['runnerId', 'token', 'binding']) assert.equal(instance[key], undefined);
    }
  }
  const records = await Promise.all(['default', 'one', 'four'].map(async name => JSON.parse(await fs.readFile(join(root, 'storage', name, 'environment.json')))));
  assert.equal(new Set(records.map(r => r.loginSpace)).size, 3);
}));

test('strict options reject invalid counts, unsafe names, duplicate flags and operands before any filesystem activity', async () => {
  for (const value of ['0', '-1', '1.2', 'NaN', '9007199254740992', '129', '01', '']) assert.throws(() => options(args('sample', value)), /Count|Use/);
  for (const value of ['../escape', '/absolute', 'a/b', 'A', 'x\n', '..']) assert.throws(() => options(args(value)));
  for (const extra of [['--count'], ['extra'], ['--repo', 'a/b'], ['--name', 'other'], ['--agent', 'claude']]) assert.throws(() => options([...args(), ...extra]));
  await assert.rejects(createEnvironment(args('sample', 0), { io: new Proxy({}, { get() { assert.fail('allocation before validation'); } }) }));
});

test('Core descriptor boundary rejects aliases and never executes model methods; failed descriptors clean up', () => scratch(async root => {
  const calls = [];
  const d = { ...deps(root), core: async () => contract({ calls }) };
  await createEnvironment(args(), d);
  assert.equal(calls.length, 2);
  for (const call of calls) {
    assert.equal(call.environment.loginSpace, call.process.environment.CODEX_HOME);
    assert.equal(call.process.account.uid, userInfo().uid);
  }
  await assert.rejects(createEnvironment(args('aliased'), { ...d, id: () => 'same' }), /descriptor/);
  await absent(join(d.root, 'aliased'));
  await assert.rejects(createEnvironment(['--name','claude-env','--agent','claude'], d), /Selected Agent backend unavailable/);
  const core = contract();
  for (const instances of [[{id:'a',workspace:'/x',registration:'/x',service:'/s'}], [{id:'a',workspace:'/x',registration:'/x/y',service:'/s'}]])
    assert.throws(() => core.createAgentEnvironments().describe({id:'env',loginSpace:'/login',instances}));
}));

test('existing names and symlink escapes preserve existing bytes and login', () => scratch(async root => {
  const d = deps(root);
  await createEnvironment(args(), d);
  const file = join(d.root, 'sample/environment.json');
  const before = await fs.readFile(file);
  await fs.writeFile(join(d.root, 'sample/login/sentinel'), sentinel);
  await assert.rejects(createEnvironment(['--name','sample','--agent','claude'], d), /already exists/);
  assert.deepEqual(await fs.readFile(file), before);
  assert.equal(await fs.readFile(join(d.root, 'sample/login/sentinel'), 'utf8'), sentinel);
  const outside = join(root, 'outside'); await fs.mkdir(outside);
  await fs.writeFile(join(outside, 'sentinel'), sentinel);
  await fs.symlink(outside, join(d.root, 'linked'));
  await assert.rejects(createEnvironment(args('linked'), d), /already exists/);
  await assert.rejects(createEnvironment(args('escape'), { ...d, root: join(d.root, 'linked/child') }), /link/);
  assert.deepEqual(await fs.readdir(outside), ['sentinel']);
}));

test('credential-free process diagnostics: tools, gh login/401/403, identity and service account', () => scratch(async root => {
  const environment = processEnvironment(join(root, 'login'), { PATH: process.env.PATH, HOME: '/host-login', GH_TOKEN: sentinel, GITHUB_TOKEN: sentinel, CODEX_HOME: '/host', NODE_OPTIONS: sentinel });
  assert.equal(environment.HOME, userInfo().homedir);
  assert.equal(environment.CODEX_HOME, join(root, 'login'));
  for (const key of ['GH_TOKEN', 'GITHUB_TOKEN', 'NODE_OPTIONS', 'OPENAI_API_KEY']) assert.equal(environment[key], undefined);
  for (const [command, first, http] of [['git','--version'], ['gh','--version'], ['gh','auth'], ['gh','api',401], ['gh','api',403]]) {
    const result = async (c,a,e) => c === command && a[0] === first ? {ok:false,http,raw:sentinel} : run(c,a,e);
    await assert.rejects(githubObservation({ run: result, environment, account: userInfo() }), error => !error.message.includes(sentinel) && /tool|gh/.test(error.message));
    await assert.rejects(createEnvironment(args('failed'), { ...deps(root), run: result }));
    await absent(join(root, 'storage/failed'));
  }
  await assert.rejects(githubObservation({run, environment, account:{uid:-1}}), /account mismatch/);
  const executable = join(root, 'process tool');
  await fs.writeFile(executable, '#!/bin/sh\nprintf credential-sentinel >&2\nexit 1\n', {mode:0o700});
  assert.deepEqual(observeProcess(executable, [], environment), {ok:false,missing:false,http:undefined,identity:undefined});
  assert.equal(observeProcess(join(root,'missing'), [], environment).missing, true);
}));

test('Agent login/isolation unavailable or unknown, backend exceptions and missing capabilities fail without sensitive output', () => scratch(async root => {
  for (const readiness of [{ready:false,reason:'Agent login',raw:sentinel}, {ready:false,reason:'isolation'}, {ready:'unknown'}, {}, null]) {
    await assert.rejects(createEnvironment(args(), {...deps(root),core:async()=>contract({readiness:readiness ?? {}})}), /not ready/);
    await absent(join(root,'storage/sample'));
  }
  await assert.rejects(createEnvironment(args(), {...deps(root),core:async()=>contract({error:true})}), error => /readiness failed/.test(error.message) && !error.message.includes(sentinel));
  await assert.rejects(createEnvironment(args(), {...deps(root),core:async()=>({})}), /capability unavailable/);
  // Real pinned production package, injected process observations only.
  const { core, ...production } = deps(root);
  await assert.rejects(createEnvironment(args(), production), /pinned N01 identity package/);
  await absent(join(root,'storage/sample'));
}));

test('partial allocation/publication failures clean only owned resources, retry; cleanup failure gives recovery information', () => scratch(async root => {
  const d = deps(root);
  await fs.mkdir(d.root); await fs.writeFile(join(d.root,'sentinel'), sentinel);
  for (const method of ['mkdir', 'writeFile', 'rename']) {
    const io = Object.create(fs);
    let triggered = false;
    io[method] = async (...values) => {
      if (String(values[0]).includes(`sample${sep}`) && (method !== 'mkdir' || String(values[0]).endsWith('registration'))) { triggered = true; throw Error(sentinel); }
      return fs[method](...values);
    };
    await assert.rejects(createEnvironment(args(), {...d,io}), error => !error.message.includes(sentinel));
    assert.ok(triggered);
    await absent(join(d.root,'sample'));
  }
  const io = Object.create(fs);
  io.writeFile = async () => { throw Error(sentinel); };
  io.rm = async () => { throw Error(sentinel); };
  await assert.rejects(createEnvironment(args(), {...d,io}), /cleanup incomplete.*sample/);
  await absent(join(d.root,'sample/environment.json'));
  await fs.rm(join(d.root,'sample'),{recursive:true});
  await createEnvironment(args(),d);
  assert.equal(await fs.readFile(join(d.root,'sentinel'),'utf8'),sentinel);
}));

test('real diagnostic subprocesses use execution account and environment login; output is sanitized', () => scratch(async root => {
  const bin = join(root, 'bin'); await fs.mkdir(bin);
  const log = join(root, 'observations');
  const script = `#!/bin/sh
printf '%s|%s|%s|%s|%s|%s\\n' "$(id -u)" "$GH_CONFIG_DIR" "$CODEX_HOME" "$GH_TOKEN" "$GITHUB_TOKEN" "$*" >> '${log}'
case "$*" in
 '--version') exit 0;;
 'auth status --hostname github.com') printf credential-sentinel >&2; exit 0;;
 'api user --jq .id') printf '42\\n'; exit 0;;
 *) exit 99;;
esac
`;
  for (const tool of ['git', 'gh']) await fs.writeFile(join(bin, tool), script, {mode:0o700});
  const calls = [];
  const d = { ...deps(root), core:async()=>contract({calls}),
    run: (c,a,e) => observeProcess(c,a,{...e,PATH:bin + ':' + process.env.PATH}) };
  const record = await createEnvironment(args(), d);
  const lines = (await fs.readFile(log,'utf8')).trim().split('\n');
  assert.equal(lines.length,4);
  for (const line of lines) {
    const [uid,config,login,token,actions] = line.split('|');
    assert.equal(uid,String(userInfo().uid));
    assert.equal(config,processEnvironment(record.loginSpace).GH_CONFIG_DIR);
    assert.equal(login,record.loginSpace);
    assert.equal(token,''); assert.equal(actions,'');
  }
  assert.equal(calls.length,2);
  assert.doesNotMatch(await fs.readFile(join(d.root,'sample/environment.json'),'utf8'),/credential-sentinel/);
  // An actual process returning sensitive stderr and HTTP 403 cannot publish.
  await fs.writeFile(join(bin,'gh'), '#!/bin/sh\nprintf "HTTP 403 credential-sentinel" >&2\nexit 1\n', {mode:0o700});
  await assert.rejects(createEnvironment(args('rejected'),d), error => !error.message.includes(sentinel));
  await absent(join(d.root,'rejected'));
}));
