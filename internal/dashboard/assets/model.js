// Schema interpretation and trust decisions live here, separate from rendering.
const reasons = new Set([
  'none', 'unavailable', 'permission_denied', 'not_found_or_concealed',
  'rate_limited', 'invalid_response', 'collection_limit',
  'inconsistent_collection', 'stale_observation', 'invalid_repository',
  'no_supported_reader'
]);
export const items = section => section.items || [];
export const progressLabels = ['scope', 'shaped', 'delivered'];
export const nativeLabels = node => node.progress.filter(x => progressLabels.includes(x));
export const text = value => String(value ?? 'Unknown').replace(
  /[\p{Cc}\p{Cf}\p{Zl}\p{Zp}]/gu,
  char => `[U+${char.codePointAt(0).toString(16).padStart(4, '0')}]`
);
const object = value => value && typeof value === 'object' && !Array.isArray(value);
const positive = value => Number.isSafeInteger(value) && value > 0;
const repoName = name => typeof name === 'string' &&
  /^[A-Za-z0-9][A-Za-z0-9-]*\/[A-Za-z0-9_.-]+$/.test(name) &&
  !['.', '..'].includes(name.split('/')[1]);
export function repository(value, reference = false) {
  return object(value) && repoName(value.name) &&
    (positive(value.id) || (reference && value.id === 0));
}
export function identity(value, reference = false) {
  return object(value) && repository(value.repository, reference) &&
    positive(value.id) && positive(value.number);
}
export const key = id => JSON.stringify([
  id.repository.name.toLowerCase(), id.repository.id, id.id, id.number
]);
export const label = id => `${id.repository.name}#${id.number}`;
export function same(a, b) {
  return key(a) === key(b);
}
function status(value) {
  return object(value) && ['known', 'unknown', 'denied'].includes(value.availability) &&
    typeof value.complete === 'boolean';
}
function section(value, valid) {
  return object(value) && status(value.status) &&
    (value.items === null || (Array.isArray(value.items) && value.items.every(valid)));
}
function edge(value) {
  return object(value) && identity(value.from) && object(value.to) &&
    identity(value.to.identity, true) && status(value.to.resolution);
}
function issue(value) {
  return object(value) && identity(value.identity) && typeof value.title === 'string' &&
    ['open', 'closed'].includes(value.state) && Array.isArray(value.progress) &&
    value.progress.every(x => typeof x === 'string') &&
    Array.isArray(value.assignees) && value.assignees.every(x =>
      object(x) && positive(x.id) && typeof x.login === 'string') &&
    section(value.blocked_by, edge) &&
    items(value.blocked_by).every(e => same(e.from, value.identity));
}
function check(value) {
  return object(value) && positive(value.id) && typeof value.name === 'string' &&
    typeof value.status === 'string' && typeof value.head === 'string';
}
function pull(value) {
  return object(value) && identity(value.identity) &&
    typeof value.state === 'string' && typeof value.head === 'string' &&
    section(value.checks, check);
}
function run(value) {
  return object(value) && repository(value.repository) && positive(value.id) &&
    positive(value.attempt) && typeof value.status === 'string' &&
    typeof value.head === 'string' && Array.isArray(value.pull_requests) &&
    value.pull_requests.every(x => identity(x));
}
export function parse(value) {
  if (!object(value) || value.schema_version !== 1 ||
      !section(value.repository, x => repository(x)) ||
      !section(value.issues, issue) || !section(value.pull_requests, pull) ||
      !section(value.runs, run)) throw new Error('Unusable snapshot');
  const repos = items(value.repository);
  if (repos.length > 1) throw new Error('Ambiguous repository');
  const ids = new Set();
  const numbers = new Set();
  for (const node of items(value.issues)) {
    const id = node.identity;
    const qualified = `${id.repository.name.toLowerCase()}#${id.number}`;
    if (ids.has(id.id) || numbers.has(qualified) || !repos.length ||
        id.repository.id !== repos[0].id ||
        id.repository.name !== repos[0].name) throw new Error('Ambiguous node');
    ids.add(id.id);
    numbers.add(qualified);
  }
  return value;
}
// Strict RFC3339 with a real calendar date. Invalid clocks never establish freshness.
export function instant(value) {
  if (typeof value !== 'string' || !/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?Z$/
    .test(value)) return NaN;
  const parsed = Date.parse(value);
  if (!Number.isFinite(parsed) || value.startsWith('0000-')) return NaN;
  if (new Date(parsed).toISOString().slice(0, 19) !== value.slice(0, 19)) return NaN;
  return parsed;
}
export function current(snapshot, now) {
  const start = instant(snapshot.started_at);
  const finish = instant(snapshot.finished_at);
  const expiry = instant(snapshot.expires_at);
  return start <= finish && finish < expiry && expiry - start <= 300000 &&
    finish <= now && now < expiry;
}
function provenance(source) {
  if (typeof source !== 'string') return 'unavailable';
  const parts = source.match(/^repos\/([^/]+\/[^/?]+)(.*)$/);
  if (!parts || !repoName(parts[1])) return 'unavailable';
  const tail = parts[2];
  const allowed = ['', '/issues?state=all&sort=created&direction=asc',
    '/pulls?state=all&sort=created&direction=asc', '/actions/runs'];
  const endpoint = /^\/issues\/[1-9]\d*(?:\/dependencies\/blocked_by)?$/;
  const checks = /^\/commits\/[a-f0-9]{40}\/check-runs\?filter=all$/;
  return allowed.includes(tail) || endpoint.test(tail) || checks.test(tail) ?
    source : 'unavailable';
}
export function statusText(value, snapshot, now, available = true) {
  const observed = instant(value.observed_at);
  const fresh = current(snapshot, now) &&
    observed >= instant(snapshot.started_at) && observed <= instant(snapshot.finished_at);
  const availability = value.availability === 'denied' ? 'denied' :
    (fresh && available ? value.availability : 'unknown');
  const complete = fresh && available && availability === 'known' &&
    value.complete && value.reason === 'none';
  const reason = reasons.has(value.reason) ? value.reason : 'unavailable';
  const source = provenance(value.source);
  return `${availability}; ${complete ? 'complete' : 'incomplete'}; ` +
    `${reason}; ${fresh ? 'current observation' : 'stale or invalid observation'}; ` +
    `observed ${Number.isFinite(observed) ? text(value.observed_at) : 'Unknown'}; ` +
    `expires ${Number.isFinite(instant(snapshot.expires_at)) ?
      text(snapshot.expires_at) : 'Unknown'}; source ${source}`;
}
export function collectionText(section, snapshot, now) {
  const status = statusText(section.status, snapshot, now, section.items !== null);
  const count = section.items === null ? 'Unavailable list' :
    section.items.length ? `${section.items.length} retained facts` :
      (status.startsWith('known; complete') ? 'Known empty' : 'No retained facts');
  return `${count}. ${status}`;
}
// The schema has no supported Core evidence reader or control outcome source.
export function coreText(core) {
  return ['completion', 'pause', 'execution'].map(name => {
    const claim = core?.[name];
    const reason = reasons.has(claim?.reason) ? claim.reason : 'no_supported_reader';
    return `${name}: Unknown / unverified (${reason})`;
  }).join('; ');
}
export function resourceURL(value, kind, extra) {
  const repo = kind === 'repository' ? value : value.repository;
  if (!repository(repo, true)) return '';
  let path = `/${repo.name}`;
  if (kind === 'issue' || kind === 'pull') {
    if (!identity(value, true)) return '';
    path += `/${kind === 'issue' ? 'issues' : 'pull'}/${value.number}`;
  } else if (kind === 'run') {
    if (!positive(value.id)) return '';
    path += `/actions/runs/${value.id}`;
  } else if (kind === 'check') {
    if (!positive(extra.id)) return '';
    const prefix = `https://github.com${path}`;
    const escaped = String(extra.id);
    const allowed = [
      `${prefix}/runs/${escaped}`,
      `${prefix}/pull/${value.number}/checks?check_run_id=${escaped}`
    ];
    // A job URL is retained only when its final component matches this check.
    const match = typeof extra.url === 'string' && extra.url.startsWith(prefix) &&
      extra.url.slice(prefix.length).match(
        /^\/actions\/runs\/([1-9]\d*)\/job\/([1-9]\d*)$/
      );
    if (match && match[2] === escaped) {
      allowed.push(extra.url);
    }
    return allowed.includes(extra.url) ? extra.url : '';
  }
  const expected = `https://github.com${path}`;
  return value.url === expected ? expected : '';
}
