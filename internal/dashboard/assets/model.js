export function text(value) {
  return String(value ?? 'Unknown').replace(/[\u0000-\u001f\u007f-\u009f]/g,
    c => `\\u${c.charCodeAt(0).toString(16).padStart(4, '0')}`);
}
const positive = n => Number.isSafeInteger(n) && n > 0;
export function repository(r) {
  return r && Number.isSafeInteger(r.id) && r.id >= 0 &&
    /^[A-Za-z0-9-]+\/[A-Za-z0-9_.-]+$/.test(r.name) &&
    !r.name.split('/').some(s => s === '.' || s === '..');
}
export function identity(i) {
  return i && repository(i.repository) && positive(i.id) && positive(i.number);
}
export function key(i) {
  return JSON.stringify([i.repository.name.toLowerCase(), i.repository.id,
    i.id, i.number]);
}
export function label(i) {
  return `${i.repository.name}#${i.number}`;
}
export function link(url, repo, path) {
  if (!repository(repo) || typeof url !== 'string') return null;
  const expected = `https://github.com/${repo.name}${path}`;
  return url === expected ? expected : null;
}
export function checkLink(c, issue) {
  if (!positive(c.id) || typeof c.url !== 'string') return null;
  const repo = issue.repository;
  const exact = [`/runs/${c.id}`,
    `/pull/${issue.number}/checks?check_run_id=${c.id}`];
  for (const path of exact) {
    const safe = link(c.url, repo, path);
    if (safe) return safe;
  }
  const prefix = `https://github.com/${repo.name}/actions/runs/`;
  if (!c.url.startsWith(prefix)) return null;
  const suffix = c.url.slice(prefix.length);
  const match = suffix.match(/^([1-9][0-9]*)\/job\/([1-9][0-9]*)$/);
  return match && Number.isSafeInteger(Number(match[1])) &&
    Number(match[2]) === c.id ? c.url : null;
}
const reasons = new Set(['none', 'unavailable', 'permission_denied',
  'not_found_or_concealed', 'rate_limited', 'invalid_response', 'collection_limit',
  'inconsistent_collection', 'stale_observation', 'invalid_repository',
  'no_supported_reader']);
function status(s) {
  return s && ['known', 'unknown', 'denied'].includes(s.availability) &&
    typeof s.complete === 'boolean' && typeof s.observed_at === 'string';
}
function section(s) {
  return s && status(s.status) && (s.items === null || Array.isArray(s.items)) &&
    !(s.status.availability === 'known' && s.status.complete && s.items === null);
}
function validCore(core) {
  return core && ['completion', 'pause', 'execution'].every(n => {
    const c = core[n];
    return c && ['known', 'unknown', 'denied'].includes(c.availability) &&
      typeof c.verification === 'string';
  });
}
export function parse(s) {
  if (!s || s.schema_version !== 1 || !validCore(s.core) ||
      !['repository', 'issues', 'pull_requests', 'runs'].every(n => section(s[n]))) {
    throw Error('Unusable snapshot');
  }
  const seen = new Set(), numbers = new Set();
  if ((s.repository.items || []).length > 1) throw Error("Invalid repository list");
  for (const r of s.repository.items || []) {
    if (!repository(r)) throw Error('Invalid repository');
  }
  for (const i of s.issues.items || []) {
    if (!identity(i.identity) || i.identity.repository.id === 0 ||
        !section(i.blocked_by) ||
        !Array.isArray(i.progress) || !i.progress.every(p => typeof p === "string") ||
        !Array.isArray(i.assignees) || !i.assignees.every(a =>
          a && positive(a.id) && typeof a.login === "string") || !validCore(i.core) ||
        typeof i.title !== 'string' || !['open', 'closed'].includes(i.state)) {
      throw Error('Invalid issue');
    }
    const selected = s.repository.items?.[0];
    if (selected && (i.identity.repository.id !== selected.id ||
        i.identity.repository.name !== selected.name)) throw Error("Foreign node");
    const k = key(i.identity);
    const qualified = `${i.identity.repository.name.toLowerCase()}#` +
      i.identity.number;
    if (seen.has(k) || numbers.has(qualified)) throw Error('Duplicate issue');
    seen.add(k);
    numbers.add(qualified);
    for (const e of i.blocked_by.items || []) {
      if (!identity(e.from) || key(e.from) !== k || !identity(e.to?.identity) ||
          !status(e.to.resolution)) throw Error('Invalid dependency');
    }
  }
  for (const p of s.pull_requests.items || []) {
    if (!identity(p.identity) || !section(p.checks)) throw Error('Invalid PR');
    for (const c of p.checks.items || []) {
      if (!positive(c.id) || typeof c.name !== 'string' || c.head !== p.head) {
        throw Error('Invalid check');
      }
    }
  }
  for (const r of s.runs.items || []) {
    if (!repository(r.repository) || !positive(r.id) ||
        !Array.isArray(r.pull_requests) || !r.pull_requests.every(identity)) {
      throw Error('Invalid run');
    }
  }
  return s;
}
function instant(t) {
  const pattern = /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d/;
  if (typeof t !== 'string' || !pattern.test(t) ||
      !/(Z|[+-]\d\d:\d\d)$/.test(t)) return NaN;
  const wall = new Date(t.slice(0, 19) + 'Z');
  if (!Number.isFinite(+wall) || wall.toISOString().slice(0, 19) !== t.slice(0, 19)) {
    return NaN;
  }
  const n = Date.parse(t);
  return t.startsWith('0001-') ? NaN : n;
}
export function current(s, now = Date.now()) {
  const start = instant(s.started_at), end = instant(s.finished_at);
  const expires = instant(s.expires_at);
  return Number.isFinite(start + end + expires) && start <= end &&
    end < expires && expires - start <= 300000 &&
    start <= now && end <= now && now < expires;
}
export function statusInfo(s, snapshot) {
  const observed = instant(s.observed_at);
  const fresh = current(snapshot) && Number.isFinite(observed) &&
    observed >= instant(snapshot.started_at) && observed <= instant(snapshot.finished_at);
  const availability = s.availability === 'denied' ? 'denied' :
    fresh ? s.availability : 'unknown';
  const complete = fresh && availability === 'known' && s.complete;
  const reason = !fresh && availability !== 'denied' ? 'stale_observation' :
    reasons.has(s.reason) ? s.reason : 'unavailable';
  return {availability, complete, reason};
}
export function fact(s, snapshot) {
  const {availability, complete, reason} = statusInfo(s, snapshot);
  return `${availability}; ${complete ? 'complete' : 'incomplete'}; ${reason}; ` +
    `observed ${text(s.observed_at)}; source ${safeSource(s.source)}; ` +
    `expires ${text(snapshot.expires_at)}`;
}
function safeSource(source) {
  return typeof source === 'string' &&
    /^(repos\/[A-Za-z0-9_./?=&-]+|none)$/.test(source) ? source : 'unavailable';
}
export function claims(core) {
  return ['completion', 'pause', 'execution'].map(n => {
    const c = core?.[n];
    const reason = reasons.has(c?.reason) ? c.reason : 'no_supported_reader';
    return `Core ${n}: Unknown / unverified; ${reason}`;
  }).join('. ');
}
