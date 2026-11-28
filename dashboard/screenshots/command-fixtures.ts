// Fictional entries of GET /api/commands for the screenshot specs, in its
// shape (the argument schema of #347).

const providerArg = { name: 'provider', required: true, repeatable: false, allowed: ['claude', 'codex'], param: 'provider' }
const yes = { name: 'yes', shorthand: 'y', type: 'bool', default: 'false', usage: 'apply without asking for confirmation', required: false }
const json = { name: 'json', shorthand: '', type: 'bool', default: 'false', usage: 'print the result as JSON', required: false }
const repo = {
  name: 'repo',
  shorthand: 'R',
  type: 'string',
  default: '',
  usage: 'the repository as [HOST/]OWNER/REPO (default: the current directory)',
  required: false,
  param: 'repo',
}

export const commands = [
  {
    path: 'runner add',
    api: 'runner-add',
    short: 'Add Runners for a provider on this host',
    long: 'Add Runners for a provider on this host.\n\nThe machine starts when it is stopped, and the provider login is created when it is missing.',
    flags: [{ name: 'count', shorthand: '', type: 'int', default: '2', usage: 'number of Runners', required: false, param: 'count' }, yes, json],
    args: [providerArg],
  },
  {
    path: 'runner remove',
    api: 'runner-remove',
    short: 'Remove Runners for a provider on this host',
    long: '',
    flags: [
      { name: 'count', shorthand: '', type: 'int', default: '0', usage: 'number of Runners (default: all)', required: false, param: 'count' },
      {
        name: 'delete-login',
        shorthand: '',
        type: 'bool',
        default: 'false',
        usage: 'also delete the provider login when no Runner of it remains',
        required: false,
        param: 'deleteLogin',
      },
      yes,
      json,
    ],
    args: [providerArg],
  },
  {
    path: 'login',
    api: 'login',
    short: 'Log in to a provider for the Runners',
    long: '',
    flags: [json],
    args: [{ ...providerArg, required: false }],
  },
  { path: 'init', api: 'init', short: 'Set up a repository for Nibfin', long: '', flags: [repo, yes, json], args: [] },
  { path: 'upgrade', api: 'upgrade', short: 'Upgrade the Nibfin workflows of a repository', long: '', flags: [repo, yes, json], args: [] },
  {
    path: 'dashboard',
    short: 'Start the local Dashboard',
    long: '',
    flags: [{ name: 'port', shorthand: '', type: 'int', default: '0', usage: 'port', required: false }],
    args: [],
  },
]

// resume with its --option flag, as the CLI defines it
// (src/app/data/commands.json). The Issue element spec adds it to the
// schema; the operation dialog harness lists only commands.
export const resumeCommand = {
  path: 'resume',
  api: 'resume',
  short: 'Resume Nibfin work on Issues',
  long: 'Resume Nibfin work on Issues.\n\nWith --option N it answers the options of the repair record on one pending Issue.',
  flags: [
    {
      name: 'option',
      shorthand: '',
      type: 'int',
      default: '0',
      usage: "answer the repair record's options with option N (one Issue)",
      required: false,
      param: 'option',
    },
    repo,
  ],
  args: [{ name: 'issue', required: true, repeatable: true, param: 'issues' }],
}
