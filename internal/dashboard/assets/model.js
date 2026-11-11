// Schema interpretation is shared by every presentation of a fact.
const reasons = {
  none: 'none', unavailable: 'unavailable', permission_denied: 'permission denied',
  not_found_or_concealed: 'not found or concealed', rate_limited: 'rate limited',
  invalid_response: 'invalid response', collection_limit: 'collection limit',
  inconsistent_collection: 'inconsistent collection',
  stale_observation: 'stale observation', invalid_repository: 'invalid repository',
  no_supported_reader: 'no supported reader'
};
const availabilities = ['known', 'unknown', 'denied'];
const progressLabels = ['scope', 'shaped', 'delivered'];
const object = value => value && typeof value === 'object' && !Array.isArray(value);
const positive = value => Number.isSafeInteger(value) && value > 0;
export const text = value => String(value ?? '').replace(
  /[\u0000-\u001f\u007f-\u009f\u202a-\u202e\u2066-\u2069]/gu,
  c => `[U+${c.codePointAt(0).toString(16).toUpperCase().padStart(4, '0')}]`
);
export const reason = value =>
  Object.hasOwn(reasons, value) ? reasons[value] : 'unrecognized reason';
export const items = section => section.items || [];
export const qualified = id => `${id.repository.name}#${id.number}`;
export const key = id => JSON.stringify([
  id.repository.name.toLowerCase(), id.repository.id, id.id, id.number
]);
function repository(repo, reference = false) {
  return object(repo) && typeof repo.name === 'string' &&
    /^[A-Za-z0-9][A-Za-z0-9-]{0,38}\/[A-Za-z0-9_.-]{1,100}$/.test(repo.name) &&
    !['.', '..'].includes(repo.name.split('/')[1]) &&
    (positive(repo.id) || reference && repo.id === 0);
}
function identity(id, reference = false) {
  return object(id) && repository(id.repository, reference) &&
    positive(id.id) && positive(id.number);
}
function status(value) {
  return object(value) && availabilities.includes(value.availability) &&
    typeof value.complete === 'boolean' && typeof value.reason === 'string' &&
    typeof value.source === 'string' && typeof value.observed_at === 'string';
}
function section(value, validate) {
  return object(value) && status(value.status) &&
    (value.items === null || Array.isArray(value.items) && value.items.every(validate));
}
function core(value) {
  return object(value) && ['completion', 'pause', 'execution'].every(name => {
    const claim = value[name];
    return object(claim) && availabilities.includes(claim.availability) &&
      typeof claim.verification === 'string' && typeof claim.reason === 'string';
  });
}
function issue(value) {
  return object(value) && identity(value.identity) && typeof value.title === 'string' &&
    ['open', 'closed'].includes(value.state) && Array.isArray(value.progress) &&
    value.progress.every(label => progressLabels.includes(label)) &&
    Array.isArray(value.assignees) && value.assignees.every(person =>
      object(person) && positive(person.id) && typeof person.login === 'string') &&
    core(value.core) && section(value.blocked_by, edge => object(edge) &&
      identity(edge.from) && key(edge.from) === key(value.identity) &&
      object(edge.to) && identity(edge.to.identity, true) && status(edge.to.resolution));
}
function check(value) {
  return object(value) && positive(value.id) && typeof value.name === 'string' &&
    typeof value.head === 'string' && typeof value.status === 'string' &&
    (value.conclusion === null || typeof value.conclusion === 'string');
}
function pull(value) {
  return object(value) && identity(value.identity) && typeof value.state === 'string' &&
    typeof value.head === 'string' &&
    (value.merged_at === null || typeof value.merged_at === 'string') &&
    section(value.checks, check);
}
function run(value) {
  return object(value) && repository(value.repository) && positive(value.id) &&
    positive(value.attempt) && typeof value.head === 'string' &&
    typeof value.status === 'string' &&
    (value.conclusion === null || typeof value.conclusion === 'string') &&
    Array.isArray(value.pull_requests) && value.pull_requests.every(id => identity(id));
}
export function parse(value) {
  if (!object(value) || value.schema_version !== 1 || !core(value.core) ||
      !section(value.repository, repo => repository(repo)) ||
      !section(value.issues, issue) || !section(value.pull_requests, pull) ||
      !section(value.runs, run)) throw new Error('Unusable snapshot');
  const repos = items(value.repository);
  const issues = items(value.issues);
  const ids = new Set();
  const numbers = new Set();
  if (repos.length > 1 || issues.length && repos.length !== 1) {
    throw new Error('Unusable repository');
  }
  for (const node of issues) {
    const id = node.identity;
    if (id.repository.id !== repos[0].id ||
        id.repository.name.toLowerCase() !== repos[0].name.toLowerCase() ||
        ids.has(id.id) || numbers.has(id.number)) throw new Error('Conflicting identity');
    ids.add(id.id);
    numbers.add(id.number);
  }
  return value;
}
// Strict calendar validation avoids Date.parse's normalization of impossible dates.
export function timestamp(value) {
  if (typeof value !== 'string' ||
      !/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?Z$/.test(value)) return NaN;
  const ms = Date.parse(value);
  if (!Number.isFinite(ms) ||
      !value.startsWith(new Date(ms).toISOString().slice(0, 19))) {
    return NaN;
  }
  return ms;
}
export function fresh(snapshot, now = Date.now()) {
  const start = timestamp(snapshot.started_at);
  const finish = timestamp(snapshot.finished_at);
  const expiry = timestamp(snapshot.expires_at);
  return start > 0 && start <= finish && finish < expiry &&
    expiry - start <= 300000 && now >= finish && now < expiry;
}
export const observation = value =>
  Number.isFinite(timestamp(value)) ? value : 'invalid/unknown';
function provenance(value) {
  const [path, query, extra] = value.split('?');
  const parts = path.split('/');
  const name = parts.slice(1, 3).join('/');
  if (parts[0] !== 'repos' || extra !== undefined ||
      !repository({id: 0, name}, true)) return 'unavailable';
  const tail = parts.slice(3).join('/');
  const valid = !query && (tail === '' || tail === 'actions/runs' ||
    /^issues\/[1-9][0-9]*(?:\/dependencies\/blocked_by)?$/.test(tail)) ||
    ['issues', 'pulls'].includes(tail) &&
      query === 'state=all&sort=created&direction=asc' ||
    /^commits\/[a-fA-F0-9]{40}\/check-runs$/.test(tail) && query === 'filter=all';
  return valid ? value : 'unavailable';
}
export function statusText(snapshot, value, collection, now = Date.now()) {
  const observed = timestamp(value.observed_at);
  const current = fresh(snapshot, now) && observed >= timestamp(snapshot.started_at) &&
    observed <= timestamp(snapshot.finished_at) && observed <= now;
  const denied = value.availability === 'denied';
  const complete = current && value.availability === 'known' && value.complete &&
    value.reason === 'none' && collection !== null;
  const availability = denied ? 'Denied' : complete ? 'Known' : 'Unknown';
  const why = current || denied ? reason(value.reason) : 'stale observation';
  let count = '';
  if (collection !== undefined) {
    count = collection === null ? '; unavailable collection (null)' :
      collection.length === 0 && complete ? '; known empty' :
        `; ${collection.length} retained facts`;
  }
  const source = provenance(value.source);
  const observedText = observation(value.observed_at);
  const expiry = observation(snapshot.expires_at);
  return `${availability}; ${complete ? 'complete, current' : 'incomplete'}; ` +
    `${why}${count}; observed ${observedText}; expires ${expiry}; source ${source}`;
}
// Links must match the exact resource identity, never merely an official host.
export function link(raw, repo, suffix = '') {
  if (!repository(repo, true) || typeof raw !== 'string') return null;
  const expected = `https://github.com/${repo.name}${suffix}`;
  return raw === expected ? expected : null;
}
export function identityLink(id, kind = 'issues') {
  return identity(id, true) ? link(id.url, id.repository, `/${kind}/${id.number}`) : null;
}
export function checkLink(value, pr) {
  if (typeof value.url !== 'string' || !positive(value.id)) return null;
  const base = `https://github.com/${pr.identity.repository.name}`;
  const escaped = base.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const pattern = new RegExp(`^${escaped}/(?:runs/${value.id}|` +
    `actions/runs/[1-9][0-9]*/job/${value.id}|` +
    `pull/${pr.identity.number}/checks\\?check_run_id=${value.id})$`);
  return pattern.test(value.url) ? value.url : null;
}
export function claimText(claim) {
  // Version 1 has no supported Core reader; no binding can authorize a claim.
  const availability = claim.availability === 'denied' ? 'Denied' : 'Unknown';
  return `${availability} / unverified; ${reason(claim.reason)}; ` +
    'no supported evidence reader; source unavailable';
}
