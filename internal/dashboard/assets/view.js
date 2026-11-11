import {
  items, text, parse, key, label, coreText, resourceURL, statusText, collectionText,
  nativeLabels, instant
} from './model.js';
import {visibleNodes, targetNode, svgOverview} from './graph.js';

let snapshot;
let selected;
let timer;
let expiryTimer;
const statusReaders = new WeakMap();
const byId = id => document.getElementById(id);
export function element(tag, content, parent) {
  const node = document.createElement(tag);
  if (content !== undefined) node.textContent = text(content);
  if (parent) parent.append(node);
  return node;
}
function observe(parent, reader, tag = 'p') {
  const node = element(tag, reader(Date.now()), parent);
  node.dataset.observation = '';
  statusReaders.set(node, reader);
  return node;
}
function link(parent, title, url) {
  if (!url) return element('span', `${title} (link unavailable)`, parent);
  const anchor = element('a', title, parent);
  anchor.href = url;
  anchor.target = '_blank';
  anchor.rel = 'noopener noreferrer';
  return anchor;
}
function section(parent, title, value, now) {
  element('h3', title, parent);
  observe(parent, time => collectionText(value, snapshot, time));
}
function controls(parent) {
  element('p', 'Control outcomes (requested/applied), control eligibility and ' +
    'supported execution evidence are unavailable in this read-only schema. ' +
    'An absence of running activity does not establish that work never started.', parent);
}
function repository(now) {
  const panel = byId('repository');
  panel.replaceChildren();
  section(panel, 'Selected repository', snapshot.repository, now);
  for (const repo of items(snapshot.repository)) {
    link(panel, repo.name, resourceURL(repo, 'repository'));
  }
  element('p', coreText(snapshot.core), panel);
  controls(panel);
  section(panel, 'Pull requests · repository activity', snapshot.pull_requests, now);
  for (const pull of items(snapshot.pull_requests)) {
    const block = element('div', undefined, panel);
    link(block, `PR ${label(pull.identity)}`, resourceURL(pull.identity, 'pull'));
    const merged = Number.isFinite(instant(pull.merged_at)) ?
      pull.merged_at : 'Unknown / no value';
    element('p', `${pull.state}; merged: ${merged}; ` +
      `head: ${pull.head}`, block);
    observe(block, time => `Head checks: ${collectionText(pull.checks, snapshot, time)}`);
    for (const check of items(pull.checks)) {
      const row = element('p', undefined, block);
      link(row, check.name, resourceURL(pull.identity, 'check', check));
      element('span', ` · ${check.status}; ${check.conclusion ?? 'Unknown / no value'}` +
        `; head ${check.head}`, row);
    }
  }
  section(panel, 'Runs · repository activity', snapshot.runs, now);
  for (const run of items(snapshot.runs)) {
    const block = element('p', undefined, panel);
    link(block, `Run ${run.id}`, resourceURL(run, 'run'));
    element('span', ` · attempt ${run.attempt}; ${run.status}; ` +
      `${run.conclusion ?? 'Unknown / no value'}; head ${run.head}`, block);
    for (const pull of run.pull_requests) {
      link(block, ` · Native PR association ${label(pull)}`, resourceURL(pull, 'pull'));
    }
  }
  element('p', 'PRs and runs are repository activity. No Issue association is inferred.',
    panel);
}
function select(value, moveFocus = true) {
  selected = value;
  renderGraph(Date.now());
  if (moveFocus) byId('details').focus();
}
function dependency(parent, edge, visible, now) {
  const row = element('li', undefined, parent);
  row.dataset.from = key(edge.from);
  row.dataset.to = key(edge.to.identity);
  const target = targetNode(snapshot, edge.to);
  const hidden = target && !visible.some(n => key(n.identity) === key(target.identity));
  const warning = target ? (hidden ? ' · hidden by filters' : ' · collected Issue') :
    ' · external or unresolved reference, not a collected node';
  const button = element('button', `→ ${label(edge.to.identity)}${warning}`, row);
  button.type = 'button';
  button.addEventListener('click', () => select(target || edge.to));
  observe(row, time => `Endpoint resolution: ${statusText(
    edge.to.resolution, snapshot, time)}`);
}
function details(visible, now) {
  const panel = byId('detail-content');
  panel.replaceChildren();
  if (!selected) {
    element('p', 'Select an Issue.', panel);
    return;
  }
  const id = selected.identity;
  element('h4', label(id), panel);
  link(panel, 'Original Issue', resourceURL(id, 'issue'));
  if (!('title' in selected)) {
    element('p', 'Reference only. No collected Issue details or recursive fetch.', panel);
    const reference = selected;
    observe(panel, time => `Endpoint resolution: ${statusText(
      reference.resolution, snapshot, time)}`);
    return;
  }
  const hidden = !visible.some(node => key(node.identity) === key(id));
  if (hidden) element('p', 'Selected Issue is hidden by filters; facts retained.', panel);
  element('p', selected.title, panel);
  element('p', `GitHub: ${selected.state}; native labels: ` +
    `${nativeLabels(selected).join(', ') || 'none'}`, panel);
  element('p', `Assignees: ${selected.assignees.map(x => text(x.login)).join(', ') ||
    'none observed'}`, panel);
  element('p', coreText(selected.core), panel);
  controls(panel);
  observe(panel, time => `Issue provenance: ${collectionText(
    snapshot.issues, snapshot, time)}`);
  element('h4', 'Blocked by (dependent → blocker)', panel);
  const blockedBy = selected.blocked_by;
  observe(panel, time => collectionText(blockedBy, snapshot, time));
  const list = element('ul', undefined, panel);
  for (const edge of items(selected.blocked_by)) dependency(list, edge, visible, now);
}
function renderGraph(now) {
  const visible = visibleNodes(snapshot);
  byId('issue-status').textContent = collectionText(snapshot.issues, snapshot, now);
  byId('count').textContent = `${visible.length} of ${items(snapshot.issues).length} ` +
    'retained Issues visible. Dependency lists retain hidden and external targets.';
  byId('map').replaceChildren(svgOverview(visible));
  const nodes = byId('nodes');
  nodes.replaceChildren();
  for (const node of visible) {
    const card = element('div', undefined, nodes);
    card.className = 'node';
    card.dataset.identity = key(node.identity);
    const labels = nativeLabels(node);
    const button = element('button', `#${node.identity.number} ${node.title} · ` +
      `${node.state} · ${labels.join(', ') || 'no native labels'}`, card);
    button.type = 'button';
    button.setAttribute('aria-pressed', String(selected &&
      key(selected.identity) === key(node.identity) || false));
    button.addEventListener('click', () => select(node));
    observe(card, time => collectionText(node.blocked_by, snapshot, time), 'small');
    const list = element('ul', undefined, card);
    for (const edge of items(node.blocked_by)) dependency(list, edge, visible, now);
  }
  details(visible, now);
}
function updateStatuses() {
  const now = Date.now();
  byId('issue-status').textContent = collectionText(snapshot.issues, snapshot, now);
  for (const node of document.querySelectorAll('[data-observation]')) {
    const next = statusReaders.get(node)(now);
    if (node.textContent !== next) node.textContent = next;
  }
}
export function load(value) {
  const next = parse(value);
  snapshot = next;
  selected = items(next.issues).find(node => selected &&
    key(node.identity) === key(selected.identity));
  byId('display').hidden = false;
  repository(Date.now());
  renderGraph(Date.now());
  clearInterval(timer);
  clearTimeout(expiryTimer);
  timer = setInterval(updateStatuses, 500);
  const remaining = instant(snapshot.expires_at) - Date.now();
  if (remaining > 0 && remaining <= 2147483647) {
    expiryTimer = setTimeout(updateStatuses, remaining);
  }
}
byId('filters').addEventListener('submit', event => event.preventDefault());
byId('filters').addEventListener('input', () => {
  if (snapshot) renderGraph(Date.now());
});
byId('filters').addEventListener('reset', event => {
  event.preventDefault();
  for (const id of ['query', 'state', 'progress']) byId(id).value = '';
  if (snapshot) renderGraph(Date.now());
});
document.addEventListener('visibilitychange', () => {
  if (snapshot) updateStatuses();
});
