import {unregisterInstance} from './unregister.mjs';
import {listEnvironments, listText} from './list.mjs';
import { registerInstance } from './register.mjs';
import { createEnvironment } from './create.mjs';
import { pathToFileURL } from 'node:url';
import { withCore, producer } from './core.mjs';

export const version = '0.1.0-baseline';
const commands = new Map([
  ['init', 'Initialize repository workflows/labels; optionally enable automatic registration.'],
  ['runner create', 'Create a fixed-Agent environment with N unregistered instances (default 2; positive integer total).'],
  ['runner register', 'Register existing instances with GitHub; never implicitly create instances.'],
  ['runner unregister', 'Unregister one instance while retaining its environment and other registrations.'],
  ['runner list', 'List local environments, identities, registrations and service state without secrets.'],
  ['runner remove', 'Member-controlled removal of a local environment; report remote cleanup failures.'],
  ['upgrade', 'Check/update Nibfin versions; preserve compatibility and authorization boundaries.'],
]);
const usage = `Usage: gh nibfin [--help | --version | COMMAND]\n\n${
  [...commands].map(([name, purpose]) => `  ${name}\n    ${purpose}`).join('\n')
}\n\nCreate: runner create --name NAME --agent AGENT [--count N] (1–128 total). Register: runner register --runner NAME (--repo OWNER/REPO | --org ORG) [--group NAME] [--method manual]. List: runner list [--runner NAME] [--json] (read-only; unknown remote is not offline; exit 2 for incomplete local inventory). Unregister: runner unregister --runner NAME (--repo OWNER/REPO | --org ORG) [--instance ID]. Omit instance only for one matching registration. --org removes the shared Org registration, not a single Repo binding. Requires Linux non-root user systemd; incomplete outcomes require reconciliation before reuse. Other business operations are not implemented.\nPublic repositories may have personal or Organization owners.\nRepo ownership, Runner registration level and auto-registration policy are separate.\nPrivate repositories are outside product scope. Validation confers no authorization.\n`;

// Maintained consumer API: real producer exports, without an eighth CLI command.
export { withCore };
export async function main(args, dependencies) {
  if (process.versions.node.split('.')[0] !== '22') throw new Error('Install Node.js 22.x and retry.');
  if (!args.length || (args.length === 1 && args[0] === '--help')) {
    process.stdout.write(usage);
    return 0;
  }
  if (args.length === 1 && args[0] === '--version') {
    await withCore(core => {
      // Exercise the public producer entry, without replicating identity logic.
      core.githubId('42');
      process.stdout.write(`gh-nibfin ${version}; @nibfin/core@0.1.0; producer ${producer}\nLocal baseline only; platform installation and complete runtime unverified.\n`);
    });
    return 0;
  }
  if (args[0] === 'runner' && args[1] === 'create') {
    try {
      const record = await createEnvironment(args.slice(2), dependencies);
      process.stdout.write(`Created ${record.name}: ${record.instances.length} unregistered ${record.agent} instances.\n`);
      return 0;
    } catch (error) { process.stderr.write(`runner create: ${error.message}\n`); return 2; }
  }
  if (args[0] === 'runner' && args[1] === 'register') {
    try {
      const result = await registerInstance(args.slice(2), dependencies);
      process.stdout.write(`Registered instance ${result.instanceId}: Runner ${result.runnerId}, ${result.label}; ${result.status}, busy=${result.busy}; service ${result.service.status}.\n`);
      return 0;
    } catch (error) { process.stderr.write(`runner register: ${error.message}\n`); return 2; }
  }
  if (args[0] === 'runner' && args[1] === 'unregister') {
    try {
      const r=await unregisterInstance(args.slice(2),dependencies);
      process.stdout.write(`Unregister ${r.instanceId}: local=${r.local}; service=${r.service}; remote=${r.remote}; ${r.complete ? (r.already ? 'already unregistered' : 'complete') : 'incomplete; reconcile private unregister.json, active work and remote identity before retry/reuse'}.\n`);
      return r.complete ? 0 : 2;
    } catch {process.stderr.write('runner unregister: Use --runner NAME (--repo OWNER/REPO | --org ORG) [--instance ID]. Verify private records, Linux user-systemd/account prerequisites; reconcile uncertain work/removal before retry.\n');return 2;}
  }
  if (args[0] === 'runner' && args[1] === 'list') {
    const result = await listEnvironments(args.slice(2), dependencies);
    process.stdout.write(args.includes('--json') ? JSON.stringify(result)+'\n' : listText(result));
    return result.complete ? 0 : 2;
  }
  const command = args.join(' ');
  if (commands.has(command) && JSON.stringify(args) === JSON.stringify(command.split(' '))) {
    process.stderr.write(`${command}: not implemented; this baseline exposes responsibilities only. See --help and the later command implementation Issues.\n`);
    return 2;
  }
  process.stderr.write(`Invalid command or options: ${JSON.stringify(args)}. Use gh nibfin --help.\n`);
  return 2;
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try { process.exitCode = await main(process.argv.slice(2)); }
  catch (error) {
    process.stderr.write(`gh-nibfin: ${error.code === 'ENOENT' ? 'Required Core handoff missing; restore the reviewed handoff.' : error.message} Retry with Node 22.x and the intact reviewed package.\n`);
    process.exitCode = 1;
  }
}
