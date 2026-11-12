// Snapshot interpretation is shared by all presentation surfaces.
export const progressLabels = ['scope', 'shaped', 'delivered'];
export const issueStates = ['open', 'closed'];
const availability = new Map([
  ['known', 'Known'], ['unknown', 'Unknown'], ['denied', 'Denied']
]);
const reasons = new Map([
  ['none', ''], ['unavailable', 'unavailable'],
  ['permission_denied', 'permission denied'],
  ['not_found_or_concealed', 'not found or concealed'],
  ['rate_limited', 'rate limited'], ['invalid_response', 'invalid response'],
  ['collection_limit', 'collection limit'],
  ['inconsistent_collection', 'inconsistent collection'],
  ['stale_observation', 'stale observation'],
  ['no_supported_reader', 'no supported evidence reader'],
  ['invalid_repository', 'invalid repository']
]);
export function text(value) {
  return typeof value === 'string' ? value.replace(
    /[\u0000-\u001f\u007f-\u009f\u202a-\u202e\u2066-\u2069]/g, '�') : '';
}
export function items(section) {
  return Array.isArray(section?.items) ? section.items : null;
}
export function status(value) {
  const known = availability.get(value?.availability) ?? 'Unknown';
  const complete = value?.complete === true ? 'complete' : 'incomplete';
  const reason = reasons.get(value?.reason) ?? 'unavailable';
  return `${known}, ${complete}${reason ? ` (${reason})` : ''}`;
}
export function claim(value) {
  const state = availability.get(value?.availability) ?? 'Unknown';
  const verification = ['verified', 'unverified'].includes(value?.verification)
    ? value.verification : 'unverified';
  const reason = reasons.get(value?.reason) ?? 'unavailable';
  return `${state}, ${verification}${reason ? ` (${reason})` : ''}`;
}
export function identity(value) {
  return `${text(value?.repository?.name) || 'Unknown repository'}#` +
    `${Number.isSafeInteger(value?.number) ? value.number : '?'}`;
}
export function key(value) {
  const repo = value?.repository;
  return `${repo?.id || text(repo?.name).toLowerCase()}:${value?.number}`;
}
export function progress(issue) {
  return progressLabels.filter(label => issue.progress?.includes(label));
}
export function state(value) {
  return issueStates.includes(value) ? value : 'Unknown';
}
export function stale(snapshot, now = Date.now()) {
  const start = Date.parse(snapshot.started_at);
  const expiry = Date.parse(snapshot.expires_at);
  return !Number.isFinite(start) || !Number.isFinite(expiry) ||
    now < start || now >= expiry;
}
export function validate(snapshot) {
  if (snapshot?.schema_version !== 1 || !snapshot.issues ||
      !snapshot.repository || !snapshot.pull_requests || !snapshot.runs) {
    throw new Error('Unsupported snapshot');
  }
  return snapshot;
}

// Validate the exact resource, including identity, before creating any link.
export function githubURL(raw, repository, resource = '', number = '') {
  const name = repository?.name;
  if (typeof name !== 'string' ||
      !/^[a-z0-9][a-z0-9-]*\/[a-z0-9_.-]+$/i.test(name)) return null;
  if (name.split('/').some(part => part === '.' || part === '..')) return null;
  if (typeof raw !== 'string' || /[\s\u0000-\u001f\u007f\\]/.test(raw)) return null;
  try {
    const url = new URL(raw);
    const suffix = resource ? `/${resource}/${number}` : '';
    const path = `/${name}${suffix}`;
    if (url.protocol !== 'https:' || url.host !== 'github.com' ||
        url.username || url.password || url.search || url.hash ||
        url.pathname !== path || raw !== `https://github.com${path}`) return null;
    if (resource && (!Number.isSafeInteger(number) || number < 1)) return null;
    return url.href;
  } catch {
    return null;
  }
}
export function checkURL(raw, repository, id, pullNumber) {
  const ending = `/checks?check_run_id=${id}`;
  if (typeof raw === 'string' && raw.endsWith(ending)) {
    const base = githubURL(raw.slice(0, -ending.length),
      repository, 'pull', pullNumber);
    if (base && Number.isSafeInteger(id) && id > 0) return base + ending;
  }
  const direct = githubURL(raw, repository, 'runs', id);
  if (direct) return direct;
  if (typeof raw !== 'string') return null;
  const match = raw.match(/\/actions\/runs\/([1-9][0-9]*)\/job\/([1-9][0-9]*)$/);
  if (!match) return null;
  const run = Number(match[1]);
  const job = Number(match[2]);
  const base = githubURL(raw.slice(0, raw.lastIndexOf('/job/')),
    repository, 'actions/runs', run);
  return base && job === id ? `${base}/job/${id}` : null;
}
