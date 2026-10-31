export const versionText = (version, coreVersion, producer) =>
  `gh-nibfin ${version}; @nibfin/core@${coreVersion}; producer ${producer}\n` +
  'Platform installation and complete runtime verification are separate.\n';

export const upgradeFailureText = (error) =>
  'upgrade: ' +
  error.message +
  '; preserve the original native installation, ' +
  'staged generations and maintenance journal for reconciliation.\n';

export const createdText = (record) =>
  `Created ${record.name}: ${record.instances.length} unregistered ` +
  `${record.agent} instances.\n`;

export function registeredText(result) {
  const status =
    result.method === 'auto'
      ? 'configuration completed; remote unknown (eligibility unproven)'
      : result.status + ', busy=' + result.busy;
  return (
    `Registered instance ${result.instanceId}: Runner ${result.runnerId}, ` +
    `${result.label}; ${status}; service ${result.service.status}.\n`
  );
}

export function unregisteredText(result) {
  const status = result.complete
    ? result.already
      ? 'already unregistered'
      : 'complete'
    : 'incomplete; reconcile private unregister.json, active work and ' +
      'remote identity before retry/reuse';
  return (
    `Unregister ${result.instanceId}: local=${result.local}; ` +
    `service=${result.service}; remote=${result.remote}; ${status}.\n`
  );
}

export const unregisterUsage =
  'runner unregister: Use --runner NAME (--repo OWNER/REPO | --org ORG) ' +
  '[--instance ID]. Verify private records, Linux user-systemd/account ' +
  'prerequisites; reconcile uncertain work/removal before retry.\n';

export function removedText(result) {
  const status = result.complete
    ? 'complete'
    : 'incomplete; reconcile remote/effect/service residue';
  return `Remove ${result.name}: local=${result.local}; ${status}.\n`;
}

export const removalInstanceText = (instance) =>
  `${instance.id}: service=${instance.service}; local=${instance.local}; ` +
  `remote=${instance.remote}; effects=${instance.effects}.\n`;

export const retainedRemovalText = (instance) =>
  `${instance.id}: original operation evidence retained in service/` +
  instance.retainedSources.join(', service/') +
  '; owning runtime must ' +
  'reconcile the unrecognized record before deletion.\n';

export const removeUsage =
  'runner remove: Use --runner NAME [--yes]. Verify owned paths/account and ' +
  'user-systemd; inspect retained resources and private .removals records ' +
  'before retrying.\n';

export const unimplementedText = (command) =>
  `${command}: not implemented; this baseline exposes responsibilities only. ` +
  'See --help and the later command implementation Issues.\n';

export function fatalText(error) {
  const reason =
    error.code === 'ENOENT'
      ? 'Required Core handoff missing; restore the reviewed handoff.'
      : error.message;
  return `gh-nibfin: ${reason} Retry with Node 22.x and the intact reviewed package.\n`;
}
