import fs from 'node:fs/promises';
import { join, resolve, parse } from 'node:path';
import { userInfo } from 'node:os';
import { randomUUID } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { withCore } from './core.mjs';

class Diagnostic extends Error {}
const fail = message => { throw new Diagnostic(message); };
const safeName = /^[a-z][a-z0-9-]{0,47}$/;
export function options(args) {
  const values = {};
  for (let i = 0; i < args.length; i += 2) {
    const key = args[i];
    if (!['--name', '--agent', '--count'].includes(key) || key in values ||
        !args[i + 1] || args[i + 1].startsWith('--')) fail('Use --name NAME --agent AGENT [--count N]; each option exactly once.');
    values[key] = args[i + 1];
  }
  if (!safeName.test(values['--name'] ?? '')) fail('Name must be 1–48 lowercase letters/digits/hyphens, starting with a letter.');
  if (!['codex', 'claude', 'gemini'].includes(values['--agent'])) fail('Agent must be codex, claude or gemini; changing Agent requires another environment.');
  const text = values['--count'] ?? '2';
  const count = Number(text);
  if (!/^[1-9][0-9]*$/.test(text) || !Number.isSafeInteger(count) || count > 128) fail('Count must be a positive integer from 1 to 128 (total instances in this environment).');
  return { name: values['--name'], agent: values['--agent'], count };
}

// No inherited Actions tokens, provider keys, alternate login homes or Node injection.
export function processEnvironment(loginSpace, env = process.env) {
  const home = userInfo().homedir;
  return { PATH: env.PATH ?? '/usr/bin:/bin', HOME: home, LANG: 'C',
    GH_CONFIG_DIR: join(home, '.config/gh'), CODEX_HOME: loginSpace,
    CLAUDE_CONFIG_DIR: loginSpace, GEMINI_CLI_HOME: loginSpace };
}
export function observeProcess(command, args, environment) {
  const result = spawnSync(command, args, { env: environment, encoding: 'utf8', timeout: 15000, maxBuffer: 65536 });
  // Never return raw provider output to diagnostics or records.
  return { ok: !result.error && result.status === 0,
    missing: result.error?.code === 'ENOENT',
    http: /\b403\b/.test(result.stderr ?? '') ? 403 : /\b401\b/.test(result.stderr ?? '') ? 401 : undefined,
    identity: command === 'gh' && args[0] === 'api' && result.status === 0 && /^[1-9][0-9]*\s*$/.test(result.stdout ?? '') ? result.stdout.trim() : undefined };
}
export async function githubObservation({ run, environment, account }) {
  for (const tool of ['git', 'gh']) {
    if (!(await run(tool, ['--version'], environment)).ok) fail(`Missing required tool ${tool}; install it for the execution account.`);
  }
  const auth = await run('gh', ['auth', 'status', '--hostname', 'github.com'], environment);
  if (!auth.ok) fail(`gh login unavailable${auth.http ? ` (HTTP ${auth.http})` : ''}; run gh auth login as the execution/service account.`);
  const identity = await run('gh', ['api', 'user', '--jq', '.id'], environment);
  if (!identity.ok || !identity.identity) fail(`gh identity unavailable${identity.http ? ` (HTTP ${identity.http})` : ''}; verify this execution account's gh login and permissions.`);
  if (account.uid !== userInfo().uid) fail('Execution/service account mismatch; run create as the intended service account.');
  return identity.identity;
}
async function capability() {
  return withCore(core => {
    if (typeof core.createAgentEnvironments !== 'function' || typeof core.createLinuxCodexBackend !== 'function')
      fail('Core backend capability unavailable in the pinned N01 identity package; install a reviewed immutable lifecycle/backend handoff through the maintained loader. No environment was created.');
    return core;
  });
}
async function directoryChain(path, io) {
  const absolute = resolve(path);
  if (absolute === parse(absolute).root) fail('Storage root must be a non-root directory.');
  let current = parse(absolute).root;
  for (const part of absolute.slice(current.length).split('/')) {
    current = join(current, part);
    try { await io.mkdir(current, { mode: 0o700 }); }
    catch (error) { if (error.code !== 'EEXIST') throw error; }
    const stat = await io.lstat(current);
    if (!stat.isDirectory() || stat.isSymbolicLink()) fail('Storage path contains a link or non-directory; choose a private real directory.');
  }
  return io.realpath(absolute);
}
export async function createEnvironment(input, dependencies = {}) {
  const { name, agent, count } = options(input);
  const io = dependencies.io ?? fs;
  const run = dependencies.run ?? observeProcess;
  const account = dependencies.account ?? userInfo();
  const root = await directoryChain(dependencies.root ?? join(userInfo().homedir, '.local/share/nibfin/environments'), io);
  const destination = join(root, name);
  try { await io.lstat(destination); fail('Environment name already exists; choose another name. Existing Agent and count cannot be changed.'); }
  catch (error) { if (error.code !== 'ENOENT') throw error; }
  const loginSpace = join(destination, 'login');
  const environment = processEnvironment(loginSpace);
  const githubId = await githubObservation({ run, environment, account });
  let core;
  try { core = await (dependencies.core ?? capability)(); }
  catch (error) {
    if (!dependencies.core) throw error;
    fail('Core capability failed; verify the reviewed backend installation.');
  }
  if (typeof core.createAgentEnvironments !== 'function' || typeof core.createLinuxCodexBackend !== 'function')
    fail('Core backend capability unavailable; install the reviewed backend package.');
  if (agent !== 'codex') fail('Selected Agent backend unavailable at the reviewed Linux/Codex boundary; install a reviewed matching Agent adapter before creation.');
  // The private allocation is not a published environment until environment.json exists.
  // mkdir without recursive is the exclusive name reservation; never remove a collision.
  let owned = false;
  try {
    await io.mkdir(destination, { mode: 0o700 }); owned = true;
    await io.mkdir(loginSpace, { mode: 0o700 });
    const instances = [];
    for (let i = 0; i < count; i++) {
      const id = (dependencies.id ?? randomUUID)();
      const base = join(destination, `instance-${i + 1}`);
      await io.mkdir(base, { mode: 0o700 });
      const instance = { id, workspace: join(base, 'workspace'), registration: join(base, 'registration'), service: join(base, 'service') };
      for (const path of [instance.workspace, instance.registration, instance.service]) await io.mkdir(path, { mode: 0o700 });
      instances.push(instance);
    }
    let descriptor;
    try { descriptor = core.createAgentEnvironments().describe({ id: name, agent, loginSpace, instances }); }
    catch { fail('Core rejected the environment descriptor; verify distinct IDs and canonical independent paths.'); }
    for (const instance of descriptor.instances) {
      let readiness;
      try {
        const backend = core.createLinuxCodexBackend({ root: destination, environment: descriptor, instanceId: instance.id,
          task: { role: 'readiness' }, agent, process: { environment, account } });
        readiness = await backend.readiness();
      } catch { fail('Agent/backend readiness failed; verify tools, environment login and supported isolation configuration.'); }
      // Core readiness is a report, never infer readiness from construction or truthiness.
      if (readiness?.ready !== true) fail('Agent/backend not ready; check Agent login, tools and isolation/read/write availability. Unknown readiness is not supported.');
    }
    const record = { schemaVersion: 1, name, agent, loginSpace, githubId, account: { uid: account.uid, username: account.username },
      instances: descriptor.instances.map(instance => ({ ...instance, loginSpace, status: 'unregistered', serviceStatus: 'not-installed' })) };
    await io.writeFile(join(destination, 'environment.pending'), JSON.stringify(record, null, 2) + '\n', { flag: 'wx', mode: 0o600 });
    await io.rename(join(destination, 'environment.pending'), join(destination, 'environment.json'));
    return record;
  } catch (error) {
    if (owned) {
      try { await io.rm(destination, { recursive: true, force: true }); }
      catch { fail(`Creation failed; cleanup incomplete for environment ${name}. Inspect its directory under the configured storage root before retrying; preserve login data.`); }
    }
    if (error instanceof Diagnostic) throw error;
    fail('Creation failed; verify tools, Agent login, isolation and distinct descriptor IDs/paths, then retry. No complete environment was published.');
  }
}
