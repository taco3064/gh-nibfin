import {append, action, original} from './dom.js';
import {items, key, qualified, statusText, identityLink} from './model.js';
import {core} from './repository.js';

export function graphCards(parent, snapshot, visible, selected, select) {
  parent.replaceChildren();
  const visibleKeys = new Set(visible.map(issue => key(issue.identity)));
  for (const issue of visible) {
    const card = append(parent, 'li', undefined, 'node');
    card.dataset.identity = key(issue.identity);
    const button = action(card, `${qualified(issue.identity)} ${issue.title}`, () =>
      select({issue}));
    const chosen = selected?.issue?.identity || selected?.reference?.identity;
    button.setAttribute('aria-pressed', String(chosen &&
      key(chosen) === key(issue.identity) || false));
    append(card, 'p', `GitHub ${issue.state}; native labels: ` +
      (issue.progress.join(', ') || 'none'));
    append(card, 'p', 'Blocked by: ' + statusText(snapshot, issue.blocked_by.status,
      issue.blocked_by.items));
    const edges = append(card, 'ul');
    for (const edge of items(issue.blocked_by)) {
      const target = edge.to;
      const hidden = !visibleKeys.has(key(target.identity));
      const row = append(edges, 'li');
      action(row, `→ ${qualified(target.identity)}${hidden ? ' (outside view)' : ''}`,
        () => select({reference: target}));
    }
  }
}
export function details(parent, snapshot, selection, all, visible, select) {
  parent.replaceChildren();
  const heading = append(parent, 'h3', 'Issue details');
  heading.tabIndex = -1;
  if (!selection) {
    append(parent, 'p', 'Select an Issue or dependency to read its facts.');
    return heading;
  }
  let issue = selection.issue;
  const ref = selection.reference;
  if (ref) {
    append(parent, 'h4', `Dependency reference: ${qualified(ref.identity)}`);
    append(parent, 'p', `Provider Issue ID ${ref.identity.id}; ` +
      `repository ID ${ref.identity.repository.id || 'unavailable'}`);
    append(parent, 'p', 'Resolution: ' + statusText(snapshot, ref.resolution));
    original(parent, identityLink(ref.identity));
    issue = all.find(node => key(node.identity) === key(ref.identity));
    if (!issue) {
      append(parent, 'p', 'External or unresolved reference; not a collected node. ' +
        'No additional data is fetched. Resolution warnings remain applicable.');
      return heading;
    }
  }
  append(parent, 'h4', `${qualified(issue.identity)} ${issue.title}`);
  if (!visible.includes(issue)) {
    append(parent, 'p', 'This collected Issue is hidden by the current filters.');
  }
  append(parent, 'p', `Provider Issue ID ${issue.identity.id}; ` +
    `repository ID ${issue.identity.repository.id}; GitHub ${issue.state}`);
  append(parent, 'p', `Native labels: ${issue.progress.join(', ') || 'none'}`);
  append(parent, 'p', 'Assignees: ' + (issue.assignees.map(person =>
    `${person.login} (ID ${person.id})`).join(', ') || 'none'));
  original(parent, identityLink(issue.identity));
  core(parent, issue.core);
  append(parent, 'p', 'Issue collection: ' + statusText(snapshot, snapshot.issues.status,
    snapshot.issues.items));
  append(parent, 'h4', 'Native blockers (dependent → blocker)');
  append(parent, 'p', statusText(snapshot, issue.blocked_by.status,
    issue.blocked_by.items));
  for (const edge of items(issue.blocked_by)) {
    const row = append(parent, 'div', undefined, 'dependency');
    action(row, 'Follow dependency → ' + qualified(edge.to.identity), () =>
      select({reference: edge.to}));
    append(row, 'p', 'Resolution: ' + statusText(snapshot, edge.to.resolution));
    if (!visible.some(node => key(node.identity) === key(edge.to.identity))) {
      append(row, 'p', 'Dependency outside view: filtered, external or unresolved.');
    }
  }
  return heading;
}
// A deterministic overview complements the full keyboard-operable adjacency graph.
export function diagram(parent, visible) {
  parent.replaceChildren();
  const ns = 'http://www.w3.org/2000/svg';
  const svg = document.createElementNS(ns, 'svg');
  const height = Math.max(160, Math.ceil(visible.length / 3) * 90);
  svg.setAttribute('width', '960');
  svg.setAttribute('height', String(height));
  svg.setAttribute('aria-hidden', 'true');
  const positions = new Map(visible.map((issue, i) => [key(issue.identity),
    {x: 15 + (i % 3) * 320, y: 15 + Math.floor(i / 3) * 90}]));
  const defs = document.createElementNS(ns, 'defs');
  const marker = document.createElementNS(ns, 'marker');
  marker.id = 'arrow';
  for (const [name, value] of Object.entries({viewBox: '0 0 10 10', refX: '10',
    refY: '5', markerWidth: '7', markerHeight: '7', orient: 'auto-start-reverse'})) {
    marker.setAttribute(name, value);
  }
  const arrow = document.createElementNS(ns, 'path');
  arrow.setAttribute('d', 'M 0 0 L 10 5 L 0 10 z');
  marker.append(arrow);
  defs.append(marker);
  svg.append(defs);
  for (const issue of visible) {
    const from = positions.get(key(issue.identity));
    for (const edge of items(issue.blocked_by)) {
      const to = positions.get(key(edge.to.identity));
      if (!to) continue;
      const line = document.createElementNS(ns, 'path');
      const d = `M ${from.x + 145} ${from.y + 40} ` +
        `Q ${from.x + 290} ${from.y + 80} ${to.x + 145} ${to.y}`;
      line.setAttribute('d', d);
      line.setAttribute('class', 'edge');
      line.setAttribute('marker-end', 'url(#arrow)');
      svg.append(line);
    }
  }
  for (const issue of visible) {
    const position = positions.get(key(issue.identity));
    const box = document.createElementNS(ns, 'rect');
    box.setAttribute('x', String(position.x));
    box.setAttribute('y', String(position.y));
    box.setAttribute('width', '290');
    box.setAttribute('height', '40');
    svg.append(box);
    const label = document.createElementNS(ns, 'text');
    label.setAttribute('x', String(position.x + 8));
    label.setAttribute('y', String(position.y + 26));
    label.textContent = `#${issue.identity.number} ${issue.state}`;
    svg.append(label);
  }
  parent.append(svg);
}
