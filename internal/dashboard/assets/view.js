import {drawGraph} from './graph.js';
import {text, key, label, link, checkLink, fact, claims, statusInfo}
  from './model.js';
export function el(tag, value, parent) {
  const node = document.createElement(tag);
  if (value !== undefined) node.textContent = text(value);
  if (parent) parent.append(node);
  return node;
}
function original(parent, title, url) {
  if (!url) return el('span', `${title}: link unavailable`, parent);
  const a = el('a', title, parent);
  a.href = url;
  a.target = '_blank';
  a.rel = 'noopener noreferrer';
  return a;
}
function collection(parent, title, section, snapshot) {
  el('h3', title, parent);
  el('p', fact(section.status, snapshot), parent);
  if (section.items === null) el('p', 'Unavailable collection', parent);
  else if (!section.items.length) {
    const known = statusInfo(section.status, snapshot).complete;
    el('p', known ? 'Known empty collection' : 'No retained items; incomplete', parent);
  }
}
export function repositoryView(s) {
  const root = document.getElementById('repository');
  root.replaceChildren();
  el('h2', 'Repository', root);
  collection(root, 'Repository facts', s.repository, s);
  for (const r of s.repository.items || []) {
    original(root, r.name, link(r.url, r, ''));
  }
  el('p', claims(s.core), root);
  el('p', 'Controls, requested/applied outcomes and eligibility unavailable: ' +
    'no supported control or evidence reader. ' +
    'Activity cannot establish permission.', root);
  collection(root, 'Repository pull requests (not Issue activity)', s.pull_requests, s);
  for (const p of s.pull_requests.items || []) {
    const row = el('article', undefined, root);
    original(row, `PR ${label(p.identity)}`,
      link(p.identity.url, p.identity.repository, `/pull/${p.identity.number}`));
    el('p', `${p.state}; merged ${p.merged_at ?? 'Unknown'}; head ${p.head}`, row);
    collection(row, 'Head checks', p.checks, s);
    for (const c of p.checks.items || []) {
      el('p', `${c.name}: ${c.status}; ${c.conclusion ?? 'Unknown'}; ` +
        `head ${c.head}`, row);
      const url = checkLink(c, p.identity);
      original(row, 'Original check', url);
    }
  }
  collection(root, 'Repository runs (not Issue activity)', s.runs, s);
  for (const r of s.runs.items || []) {
    const row = el('article', undefined, root);
    original(row, `Run ${r.id}`, link(r.url, r.repository, `/actions/runs/${r.id}`));
    el('p', `${r.status}; ${r.conclusion ?? 'Unknown'}; attempt ${r.attempt}; ` +
      `head ${r.head}`, row);
    el('p', 'Explicit PR references: ' +
      (r.pull_requests.map(label).join(', ') || 'none supplied'), row);
  }
}
export function graphView(s, state) {
  const nodes = document.getElementById('nodes');
  nodes.replaceChildren();
  const issues = s.issues.items || [];
  const visible = issues.filter(i =>
    `${label(i.identity)} ${i.title}`.toLowerCase().includes(state.query.toLowerCase()) &&
    (!state.state || i.state === state.state) &&
    (!state.progress || i.progress.includes(state.progress)));
  document.getElementById('graph-status').textContent =
    `${visible.length} of ${issues.length} retained Issues. ` + fact(s.issues.status, s);
  if (s.issues.items === null) el('p', 'Issue collection unavailable', nodes);
  if (!issues.length && s.issues.items !== null) {
    el('p', statusInfo(s.issues.status, s).complete ? 'Known empty Issue collection' :
      'No retained Issues; incomplete', nodes);
  }
  drawGraph(visible, issues, state.select);
  const visibleKeys = new Set(visible.map(i => key(i.identity)));
  for (const i of visible) {
    const card = el('li', undefined, nodes);
    const progress = i.progress.filter(p =>
      ['scope', 'shaped', 'delivered'].includes(p)).join(', ');
    const button = el('button', `${label(i.identity)} ${i.title} — ${i.state}; ` +
      progress, card);
    button.type = 'button';
    button.dataset.identity = key(i.identity);
    button.setAttribute('aria-pressed', String(state.selected === key(i.identity)));
    button.onclick = () => state.select(i.identity);
    for (const edge of i.blocked_by.items || []) {
      const target = edge.to.identity;
      el('p', `→ blocked by ${label(target)}; ` +
        (visibleKeys.has(key(target)) ? 'visible node' :
          'hidden node or reference'), card);
    }
    el('p', 'Dependencies: ' + fact(i.blocked_by.status, s), card);
  }
}
export function detailsView(s, state, reference) {
  const root = document.getElementById('details');
  root.replaceChildren();
  el('h3', 'Selected Issue details', root);
  const i = (s.issues.items || []).find(i => key(i.identity) === state.selected);
  if (!i) {
    if (reference) {
      el('p', `External or unresolved reference ${label(reference.identity)}`, root);
      el('p', fact(reference.resolution, s), root);
      original(root, 'Original Issue', link(reference.identity.url,
        reference.identity.repository, `/issues/${reference.identity.number}`));
    } else el('p', 'Select an Issue to read its facts.', root);
    return;
  }
  if (reference) el('p', 'Navigation endpoint: ' + fact(reference.resolution, s), root);
  el('h4', `${label(i.identity)} ${i.title}`, root);
  el('p', `GitHub ${i.state}; native labels: ${i.progress.join(', ') || 'none'}`, root);
  el('p', 'Assignees: ' + (i.assignees.map(a => a.login).join(', ') || 'none'), root);
  el('p', claims(i.core), root);
  el('p', 'Issue provenance: ' + fact(s.issues.status, s), root);
  original(root, 'Original Issue',
    link(i.identity.url, i.identity.repository, `/issues/${i.identity.number}`));
  collection(root, 'Blocked by (dependent → blocker)', i.blocked_by, s);
  for (const e of i.blocked_by.items || []) {
    const row = el('div', undefined, root);
    el('p', `${label(e.from)} → ${label(e.to.identity)}`, row);
    el('p', 'Endpoint resolution: ' + fact(e.to.resolution, s), row);
    const b = el('button', `Inspect blocker ${label(e.to.identity)}`, row);
    b.type = 'button';
    b.onclick = () => state.select(e.to.identity, e.to);
  }
}
