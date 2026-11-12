import {items, status, identity, key, progress, stale, validate, githubURL}
  from './model.js';
import {progressLabels, issueStates} from './model.js';
import {node, line, link, claims} from './dom.js';
import {activity} from './activity.js';
import {graph, details} from './graph.js';

const get = id => document.getElementById(id);
let snapshot;
let selected;
let timer;
let failed = false;
const filters = get('filters');
for (const [id, values] of [['state', issueStates], ['progress', progressLabels]]) {
  for (const value of values) get(id).append(node('option', value));
}
filters.addEventListener('submit', event => event.preventDefault());
filters.addEventListener('input', renderGraph);
filters.addEventListener('reset', event => {
  event.preventDefault();
  for (const control of filters.querySelectorAll('input, select')) control.value = '';
  renderGraph();
});
function freshness() {
  const expired = stale(snapshot);
  const message = failed ? 'Refresh failed; retained observations are not current.' :
    expired ? 'Stale observations — reopen or Read snapshot to refresh.' :
      'Current observation window; collection limitations still apply.';
  get('freshness').textContent = `${message} ` +
    `Observed: ${snapshot.finished_at}; expires: ${snapshot.expires_at}`;
  get('dashboard').classList.toggle('stale', failed || expired);
}
function renderGraph() {
  if (!snapshot) return;
  const issues = items(snapshot.issues);
  const query = get('search').value.toLowerCase().trim();
  const stateFilter = get('state').value;
  const label = get('progress').value;
  const filtered = (issues ?? []).filter(issue =>
    `${identity(issue.identity)} ${issue.title}`.toLowerCase().includes(query) &&
    (!stateFilter || issue.state === stateFilter) &&
    (!label || progress(issue).includes(label)));
  const all = new Map((issues ?? []).map(issue => [key(issue.identity), issue]));
  const context = {all, selected,
    visible: new Set(filtered.map(issue => key(issue.identity))),
    choose(value) {
      selected = value;
      renderGraph();
      get('details').focus();
    }};
  const state = issues === null ? 'Issue observations unavailable; not known empty.' :
    !issues.length ? (snapshot.issues.status?.complete
      ? 'Known empty Issue collection.' : 'No Issues observed; collection incomplete.') :
      !filtered.length ? 'No Issues match these filters.' :
        `${filtered.length} of ${issues.length} collected Issues shown.`;
  get('issue-status').textContent = `${state} ${status(snapshot.issues.status)}`;
  graph(get('graph'), get('relations'), filtered, context);
  details(get('details'), all.get(selected), context);
}
export function display(value) {
  const next = validate(value);
  clearInterval(timer);
  snapshot = next;
  failed = false;
  const parent = get('repository');
  parent.replaceChildren();
  line(parent, `Repository observations: ${status(snapshot.repository.status)}`);
  const repositories = items(snapshot.repository);
  if (repositories === null) line(parent, 'Repository identity unavailable.');
  else if (!repositories.length) line(parent, 'No repository identity observed.');
  for (const repository of repositories ?? []) {
    const heading = node('h3');
    link(heading, repository.name, githubURL(repository.url, repository));
    parent.append(heading);
  }
  const issues = items(snapshot.issues);
  if (issues !== null) {
    const open = issues.filter(issue => issue.state === 'open').length;
    const closed = issues.filter(issue => issue.state === 'closed').length;
    line(parent, `Observed Issues: ${issues.length}; open: ${open}; closed: ${closed}. ` +
      'Counts describe collected facts, not verified completion.');
  }
  claims(parent, snapshot.core);
  activity(get('activity'), snapshot);
  get('dashboard').hidden = false;
  renderGraph();
  freshness();
  timer = setInterval(freshness, 500);
}
export function invalidate() {
  failed = true;
  if (snapshot) freshness();
}
