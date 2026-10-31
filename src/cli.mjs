import {
  versionText,
  upgradeFailureText,
  createdText,
  registeredText,
  unregisteredText,
  unregisterUsage,
  removedText,
  removalInstanceText,
  retainedRemovalText,
  removeUsage,
  unimplementedText,
  fatalText,
} from './cli-output.mjs';
import { removeEnvironment } from './remove.mjs';
import { unregisterInstance } from './unregister.mjs';
import { listEnvironments, listText } from './list.mjs';
import { registerInstance } from './register.mjs';
import { createEnvironment } from './create.mjs';
import { pathToFileURL } from 'node:url';
import { withCore } from './core.mjs';
import { withCurrentCore, currentProducer, currentPin } from './current-core.mjs';
import { initializeRepository } from './installation.mjs';
import { upgradeManagedInstallation } from './managed-upgrade.mjs';

import { version } from './version.mjs';
export { version };
import { commands, usage } from './cli-help.mjs';

// Maintained consumer API: real producer exports, without an eighth CLI command.
export { withCore };
export async function main(args, dependencies) {
  if (process.versions.node.split('.')[0] !== '22')
    throw new Error('Install Node.js 22.x and retry.');
  if (!args.length || (args.length === 1 && args[0] === '--help')) {
    process.stdout.write(usage);
    return 0;
  }
  if (args.length === 1 && args[0] === '--version') {
    await withCurrentCore((core) => {
      // Exercise the public producer entry, without replicating identity logic.
      core.githubId('42');
      process.stdout.write(
        versionText(version, currentPin.packageVersion, currentProducer),
      );
    });
    return 0;
  }
  if (args[0] === 'init') {
    try {
      const result = await initializeRepository(args.slice(1), dependencies);
      process.stdout.write(JSON.stringify(result) + '\n');
      return result.status === 'ready' ? 0 : 2;
    } catch (error) {
      process.stderr.write('init: ' + error.message + '\n');
      return 2;
    }
  }
  if (args[0] === 'upgrade') {
    try {
      const result = await upgradeManagedInstallation(args.slice(1), dependencies);
      process.stdout.write(JSON.stringify(result) + '\n');
      return ['checked', 'unchanged', 'switched', 'rolled-back'].includes(
        result.status,
      )
        ? 0
        : 2;
    } catch (error) {
      process.stderr.write(upgradeFailureText(error));
      return 2;
    }
  }
  if (args[0] === 'runner' && args[1] === 'create') {
    try {
      const record = await createEnvironment(args.slice(2), dependencies);
      process.stdout.write(createdText(record));
      return 0;
    } catch (error) {
      process.stderr.write(`runner create: ${error.message}\n`);
      return 2;
    }
  }
  if (args[0] === 'runner' && args[1] === 'register') {
    try {
      const result = await registerInstance(args.slice(2), dependencies);
      if (result.cancelled) {
        process.stdout.write('Registration cancelled; no request submitted.\n');
        return 2;
      }
      process.stdout.write(registeredText(result));
      return 0;
    } catch (error) {
      process.stderr.write(`runner register: ${error.message}\n`);
      return 2;
    }
  }
  if (args[0] === 'runner' && args[1] === 'unregister') {
    try {
      const r = await unregisterInstance(args.slice(2), dependencies);
      process.stdout.write(unregisteredText(r));
      return r.complete ? 0 : 2;
    } catch {
      process.stderr.write(unregisterUsage);
      return 2;
    }
  }
  if (args[0] === 'runner' && args[1] === 'remove') {
    try {
      const r = await removeEnvironment(args.slice(2), dependencies);
      if (r.cancelled) {
        process.stdout.write('Removal cancelled; no resources changed.\n');
        return 2;
      }
      process.stdout.write(removedText(r));
      for (const i of r.instances) process.stdout.write(removalInstanceText(i));
      for (const i of r.instances)
        if (i.retainedSources?.length) process.stdout.write(retainedRemovalText(i));
      if (!r.complete)
        process.stdout.write(`Private reconciliation record: ${r.residue}\n`);
      return r.complete ? 0 : 2;
    } catch {
      process.stderr.write(removeUsage);
      return 2;
    }
  }
  if (args[0] === 'runner' && args[1] === 'list') {
    const result = await listEnvironments(args.slice(2), dependencies);
    process.stdout.write(
      args.includes('--json') ? JSON.stringify(result) + '\n' : listText(result),
    );
    return result.complete ? 0 : 2;
  }
  const command = args.join(' ');
  if (
    commands.has(command) &&
    JSON.stringify(args) === JSON.stringify(command.split(' '))
  ) {
    process.stderr.write(unimplementedText(command));
    return 2;
  }
  process.stderr.write(
    `Invalid command or options: ${JSON.stringify(args)}. Use gh nibfin --help.\n`,
  );
  return 2;
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    process.exitCode = await main(process.argv.slice(2));
  } catch (error) {
    process.stderr.write(fatalText(error));
    process.exitCode = 1;
  }
}
