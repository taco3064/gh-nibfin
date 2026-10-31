// Command names and public help text share one maintained catalog.
export const commands = new Map([
  [
    'init',
    'Prepare a shared native pool and project-local workflows; ' +
      'activate after all selected projects verify. Optional --auto-register.',
  ],
  [
    'runner create',
    'Create a fixed-Agent environment with N unregistered ' +
      'instances (default 2; positive integer total).',
  ],
  [
    'runner register',
    'Register existing instances with GitHub; never ' +
      'implicitly create instances.',
  ],
  [
    'runner unregister',
    'Unregister one instance while retaining its environment ' +
      'and other registrations.',
  ],
  [
    'runner list',
    'List local environments, identities, registrations and ' +
      'service state without secrets.',
  ],
  [
    'runner remove',
    'Member-controlled removal of a local environment; ' +
      'report remote cleanup failures.',
  ],
  [
    'upgrade',
    'Check/update Nibfin versions; preserve compatibility ' +
      'and authorization boundaries.',
  ],
]);
const details =
  'Create: runner create --name NAME --agent AGENT ' +
  '[--count N] (1–128 total). Register: runner register ' +
  '--runner NAME (--repo OWNER/REPO | --org ORG) [--group ' +
  'NAME] [--method auto|manual]. List: runner list ' +
  '[--runner NAME] [--json] (read-only; unknown remote is ' +
  'not offline; exit 2 for incomplete local inventory). ' +
  'Unregister: runner unregister --runner NAME (--repo ' +
  'OWNER/REPO | --org ORG) [--instance ID]. Omit instance ' +
  'only for one matching registration. --org removes the ' +
  'shared Org registration, not a single Repo binding. ' +
  'Requires Linux non-root user systemd; incomplete ' +
  'outcomes require reconciliation before reuse. Remove: ' +
  'runner remove --runner NAME [--yes]. Removes the ENTIRE ' +
  'environment, Agent credentials and all instances; ' +
  'prompts unless --yes. Org removal affects its entire ' +
  'shared registration. Local and remote outcomes are ' +
  'independent; exit 2 requires private .removals ' +
  'reconciliation. Linux non-root user-systemd only. Init: ' +
  'init --repo OWNER/REPO [--pool-repos OWNER/REPO,...] ' +
  '[--runner-count N] [--image sha256:DIGEST] ' +
  '[--runtime-profile PROFILE] [--check] [--auto-register] ' +
  '[--handover ARCHIVE]. ' +
  'Physical capacity defaults to 2, shared across selected projects. ' +
  'Initial runtime preparation requires reviewed image and profile inputs. ' +
  '--check observes managed sources and the existing ' +
  'authorized host without installing/starting services or ' +
  'invoking admission. Upgrade: upgrade ' +
  '[--check|--apply|--rollback] consumes the native ' +
  "installer's sealed generation plan; checks " +
  'are read-only and all transitions preserve pause and ' +
  'original work. Owner init --handover verifies the complete ' +
  'private data-only archive and creates fresh bounded approval for ' +
  'retained merged work. --check does not sign or import; retained ' +
  'UNACCEPTED work is reported separately. Missing onboarding or staged ' +
  'evidence returns exit 2.\nPublic repositories may have ' +
  'personal or Organization owners.\nRepo ownership, Runner ' +
  'registration level and auto-registration policy are ' +
  'separate.\nPrivate repositories are outside product ' +
  'scope. Validation confers no authorization.\n';
export const usage =
  `Usage: gh nibfin [--help | --version | COMMAND]\n\n${[...commands]
    .map(([name, purpose]) => `  ${name}\n    ${purpose}`)
    .join('\n')}\n\n` + details;
