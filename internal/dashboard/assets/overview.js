import {node, line, link} from './dom.js';
import {text} from './model.js';
import {selectionChanged} from './controls.js';
import {observeDiagnosis} from './diagnosis.js';
import {connectOverview} from './overview-stream.js';

const get = id => document.getElementById(id);
const recent = time => {
  const age = Date.now() - Date.parse(time);
  return Number.isFinite(age) && age >= -1000 && age < 30000;
};
const usable = field => field?.value != null && recent(field.observedAt) &&
  field.freshness !== 'stale';
const fact = field => usable(field) ? field.value : null;
const coverage = section => {
  const s = section?.status;
  const availability = s?.availability === 'denied' ? 'Access denied' :
    s?.availability === 'known' ? 'Observed' : 'Unavailable or unknown';
  return `${availability}. ${s?.complete ? 'Complete' : 'Partial'} coverage.`;
};
const qualified = row => `${row.identity.repository.name}#${row.identity.number}`;
function jobLink(parent, job) {
  const repo = job?.repository;
  if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repo ?? '') ||
    !Number.isSafeInteger(job?.runId) || !Number.isSafeInteger(job?.jobId)) return;
  const url = `https://github.com/${repo}/actions/runs/${job.runId}/job/${job.jobId}`;
  if (job.url === url) link(parent, 'Native job', url);
}
function runnerCard(runner, value) {
  const card = node('article', undefined, 'runner-card');
  card.dataset.key = runner.key;
  card.append(node('h3', runner.registration.name));
  const current = !runner.retained && recent(runner.busy.observedAt);
  const state = current ? runner.state : runner.retained
    ? 'unknown (retained observation)' : 'unknown (stale observation)';
  const label = node('p', state, 'runner-state');
  if (current && runner.state === 'busy' && runner.busy.value === true) {
    const indicator = node('span', '', 'busy-indicator');
    indicator.setAttribute('aria-hidden', 'true');
    label.prepend(indicator);
  }
  card.append(label);
  line(card, `Registration: ${runner.registration.repository} / ` +
    `${runner.registration.runnerId}`);
  const job = current && recent(runner.association?.observedAt)
    ? runner.association.value : null;
  const work = value.work ?? [];
  const associated = job && work.find(item => item.native?.jobId === job.jobId &&
    item.native?.repositoryId === job.repositoryId &&
    item.native?.runId === job.runId && item.native?.attempt === job.attempt);
  if (runner.state === 'busy' || job) {
    line(card, `Issue: ${associated?.issueObservation?.value === true &&
      recent(associated.issueObservation.observedAt)
      ? associated?.issue?.qualified ?? 'Unknown' : 'Unknown'}`);
    line(card, `Stage: ${fact(associated?.phase) ?? 'Unknown'}`);
  }
  if (job) {
    line(card, `Execution repository: ${job.repository}`);
    const elapsed = Date.now() - Date.parse(job.startedAt);
    if (job.startedAt && Number.isFinite(elapsed) && elapsed >= 0) {
      const clock = node('p', `Elapsed: ${Math.floor(elapsed / 1000)}s`, 'elapsed');
      clock.dataset.started = job.startedAt;
      card.append(clock);
    }
    jobLink(card, job);
  }
  const details = node('details');
  details.append(node('summary', 'Observation evidence'));
  line(details, `Observed: ${runner.busy.observedAt || 'Unknown'}`);
  line(details, `Source: ${runner.busy.source || 'Unknown'}; ` +
    `source freshness: ${runner.busy.freshness || 'unknown'}`);
  line(details, `Routes: ${runner.routes.map(route => route.repository).join(', ')}`);
  const raw = node('pre', JSON.stringify({
    adoption: runner.adoption, online: runner.online, busy: runner.busy,
    association: runner.association, work: associated ?? null
  }, null, 2));
  details.append(raw);
  card.append(details);
  return card;
}
function states(row) {
  if (row.retained) return 'Execution unknown (retained evidence)';
  const values = new Set();
  for (const evidence of row.evidence ?? []) {
    const activity = fact(evidence.activity);
    if (activity) values.add(activity);
    if (fact(evidence.pause) === true) {
      values.add('paused (admitted work may continue)');
    }
    if (fact(evidence.dependencies) > 0) values.add('blocked by native dependencies');
    if (fact(evidence.executionBlocker)) values.add('pending external effect');
    if (fact(evidence.waiting) === 'no-admission-observed') {
      values.add('not yet scheduled (no admission observed)');
    }
  }
  const report = (row.evidence ?? []).flatMap(work =>
    work.diagnosis?.reports ?? []).find(value => value.rootCause);
  if (report) values.add(`${report.confidence} historical explanation: ` +
    report.rootCause);
  return [...values].join('; ') || 'Execution unknown';
}

export function start(request, readRepository) {
  const panel = get('user-overview');
  panel.hidden = false;
  get('page-title').textContent = 'Your local work';
  get('page-intro').textContent = 'Registered Runners and assigned open GitHub Issues.';
  let value;
  let selected;
  let navigating = false;
  let connection = 'Pending first observation…';
  const controller = new AbortController();
  const render = () => {
    get('overview-connection').textContent = connection;
    if (!value) {
      get('runner-coverage').textContent = 'Pending Runner observation…';
      get('assigned-coverage').textContent = 'Pending assigned-Issue inventory…';
      return;
    }
    observeDiagnosis(value, request);
    const elapsed = Date.now() - Date.parse(value.observed_at);
    const age = Math.max(0, Math.floor(elapsed / 1000));
    get('overview-freshness').textContent =
      `${recent(value.observed_at) ? 'Recent read' : 'Stale read'} (${age}s ago). ` +
      `Adoption: ${coverage({status: value.discovery})} Core: ${value.local_status}.`;
    get('overview-freshness').classList.toggle('observation-stale',
      !recent(value.observed_at));
    get('overview-evidence').textContent = JSON.stringify({
      discovery: value.discovery, core_interval: value.local_interval,
      core_status: value.local_status, core_code: value.local_code,
      core_completeness: value.local_completeness, errors: value.errors ?? []
    }, null, 2);
    get('runner-coverage').textContent = coverage(value.runners);
    get('assigned-coverage').textContent = coverage(value.issues);
    const runners = get('runners');
    const oldCards = new Map([...runners.children].map(card => [card.dataset.key, card]));
    const cards = (value.runners.items ?? []).map(runner => {
      let card = oldCards.get(runner.key);
      const current = !runner.retained && recent(runner.busy.observedAt);
      if (!card || card.observation !== runner || card.current !== current) {
        const next = runnerCard(runner, value);
        next.observation = runner;
        next.current = current;
        if (card) {
          next.querySelector('details').open = card.querySelector('details').open;
          card.replaceWith(next);
        }
        card = next;
      }
      return card;
    });
    for (const card of cards) if (!card.isConnected) runners.append(card);
    for (const card of [...runners.children]) {
      if (!cards.includes(card)) card.remove();
    }
    for (const clock of runners.querySelectorAll('.elapsed')) {
      const elapsed = Date.now() - Date.parse(clock.dataset.started);
      clock.textContent = `Elapsed: ${Math.floor(elapsed / 1000)}s`;
    }
    if (!runners.childElementCount) line(runners, value.runners.status.complete
      ? 'No local Runner registrations.' : 'Runner inventory unavailable or partial.');
    const query = get('overview-search').value.trim().toLowerCase();
    const rows = (value.issues.items ?? []).filter(row =>
      `${qualified(row)} ${row.title}`.toLowerCase().includes(query));
    // Preserve keyed row elements so background updates do not steal focus.
    const parent = get('assigned-issues');
    const existing = new Map([...parent.children].map(row => [row.dataset.key, row]));
    const keep = new Set();
    for (const row of rows) {
      keep.add(row.key);
      let item = existing.get(row.key);
      if (!item) {
        item = node('article', undefined, 'assigned-issue');
        item.dataset.key = row.key;
        item.append(node('button'), node('p'), node('details'));
        item.firstChild.type = 'button';
        const details = item.lastChild;
        details.append(node('summary', 'Evidence and freshness'), node('pre'));
      }
      item.firstChild.textContent = text(`${qualified(row)} — ${row.title}`);
      item.firstChild.onclick = async () => {
        if (navigating) return;
        navigating = true;
        selected = row.key;
        render();
        try {
          await readRepository(row.identity.repository.name, row.identity);
        } finally {
          navigating = false;
        }
      };
      item.classList.toggle('selected', selected === row.key);
      item.children[1].textContent = `${row.state}; assigned to you; ${states(row)}` +
        `${row.retained ? '; retained native read' :
          recent(row.observed_at) ? '' : '; stale native read'}`;
      item.querySelector('pre').textContent = JSON.stringify({
        observed_at: row.observed_at, evidence: row.evidence
      }, null, 2);
      if (!item.isConnected) parent.append(item);
    }
    for (const child of [...parent.children]) {
      if (!keep.has(child.dataset.key)) child.remove();
    }
    get('assigned-empty').textContent = rows.length ? '' :
      query ? 'No Issues match this filter.' : value.issues.status.complete
        ? 'No assigned open Issues in adopted repositories.' :
          'No assigned Issues observed; inventory is incomplete.';
  };
  get('refresh-overview').addEventListener('click', async () => {
    connection = 'Refresh requested; waiting for the next bounded read…';
    render();
    try {
      const response = await request('/api/overview');
      if (response.ok) value = (await response.json()).overview;
      else connection = 'Overview pending or unavailable; retained observations remain.';
    } catch {
      connection = 'Disconnected; retaining observations.';
    }
    render();
  });
  get('overview-search').addEventListener('input', render);
  get('back-overview').addEventListener('click', () => {
    selectionChanged();
    get('dashboard').hidden = true;
    panel.hidden = false;
  });
  const timer = setInterval(render, 1000);
  window.addEventListener('pagehide', () => {
    clearInterval(timer);
    controller.abort();
  }, {once: true});
  render();
  void connectOverview(request, next => { value = next; render(); }, message => {
    connection = message;
    render();
  }, controller.signal);
}
