const value = label => ({ label });
const flag = { label: null };
export const commands = Object.freeze({
  init: { repo: value('OWNER/REPO'), 'auto-register': flag, check: flag },
  'runner create': { name: value('NAME'), agent: value('AGENT'), count: value('N') },
  'runner register': { runner: value('NAME'), repo: value('OWNER/REPO'), org: value('ORG'), group: value('NAME'), method: value('auto|manual') },
  'runner unregister': { runner: value('NAME'), repo: value('OWNER/REPO'), org: value('ORG') },
  'runner list': { runner: value('NAME'), json: flag },
  'runner remove': { runner: value('NAME'), yes: flag },
  upgrade: { runner: value('NAME'), repo: value('OWNER/REPO'), check: flag, version: value('VERSION') }
});
const invalid = message => { throw new Error(message); };
export function help(command = '') {
  if (!command || command === 'runner') {
    return `Usage: gh nibfin ${command ? 'runner ' : ''}<command> [options]\n\n` +
      Object.keys(commands).filter(k => !command || k.startsWith('runner ')).join('\n') +
      '\n\nUse --help on an operation to see its options.\n';
  }
  return `Usage: gh nibfin ${command} [options]\n\n` +
    Object.entries(commands[command]).map(([k, v]) => `--${k}${v.label ? ` ${v.label}` : ''}`).join('\n') + '\n--help\n';
}
export function parse(args) {
  let command = '', offset = 0;
  if (args[0] && !args[0].startsWith('-')) {
    command = args[0]; offset = 1;
    if (command === 'runner' && args[1] && !args[1].startsWith('-')) {
      command += ` ${args[1]}`; offset = 2;
    }
  }
  if (command && command !== 'runner' && !Object.hasOwn(commands, command)) invalid('Unknown command. Use --help.');
  const schema = commands[command] ?? {};
  const options = Object.create(null);
  let wantsHelp = false;
  for (let i = offset; i < args.length; i++) {
    const token = args[i];
    if (token === '--help' || token === '-h') {
      if (wantsHelp) invalid('Repeated help option.');
      wantsHelp = true; continue;
    }
    if (!token.startsWith('--') || !Object.hasOwn(schema, token.slice(2))) invalid('Unknown option or unexpected positional argument.');
    const key = token.slice(2);
    if (Object.hasOwn(options, key)) invalid('Repeated option.');
    if (schema[key].label) {
      const next = args[++i];
      if (!next || !next.trim() || next.startsWith('--') || next === '-h') invalid('Missing option value.');
      options[key] = next;
    } else options[key] = true;
  }
  if (wantsHelp || !command || command === 'runner') return { help: help(command) };
  const requireOption = key => { if (!options[key]) invalid(`Required option --${key} is missing.`); };
  if (options.repo && !/^[A-Za-z0-9][A-Za-z0-9-]*\/[A-Za-z0-9_.-]+$/.test(options.repo)) invalid('Repository must be OWNER/REPO.');
  if (options.org && !/^[A-Za-z0-9][A-Za-z0-9-]*$/.test(options.org)) invalid('Invalid organization selector.');
  if (command === 'init') requireOption('repo');
  if (command === 'runner create') {
    requireOption('name'); requireOption('agent');
    const count = options.count ?? '2';
    if (!/^[1-9][0-9]*$/.test(count) || !Number.isSafeInteger(Number(count))) invalid('Count must be a positive safe integer.');
    options.count = Number(count);
  }
  if (['runner register', 'runner unregister'].includes(command)) {
    requireOption('runner');
    if (Boolean(options.repo) === Boolean(options.org)) invalid('Select exactly one of --repo or --org.');
    if (options.group && !options.org) invalid('--group requires --org.');
  }
  if (command === 'runner register') {
    options.method ??= 'manual';
    if (!['auto', 'manual'].includes(options.method)) invalid('Method must be auto or manual.');
  }
  if (command === 'runner remove') requireOption('runner');
  return { command, options };
}
export async function run(args, { handlers = {}, stdout = s => process.stdout.write(s), stderr = s => process.stderr.write(s) } = {}) {
  let request;
  try { request = parse(args); }
  catch (error) { stderr(`Invalid input: ${error.message}\n`); return 2; }
  if (request.help) { stdout(request.help); return 0; }
  const handler = Object.hasOwn(handlers, request.command) ? handlers[request.command] : null;
  if (!handler) {
    stderr(`Capability unavailable: ${request.command}. This baseline has no approved backend; consult README.md before installation or Runner management.\n`);
    return 1;
  }
  try { await handler(request.options); return 0; }
  catch { stderr('Operation failed. Consult the trusted installation configuration.\n'); return 1; }
}
