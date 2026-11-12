import {labelObservation} from './model.js';
import {issueControls} from './controls.js';
import {items, status, identity, key, state, progress, githubURL} from './model.js';
import {node, line, link, claims} from './dom.js';

function issueButton(issue, selected, choose) {
  const button = node('button', `${identity(issue.identity)} — ${issue.title}`);
  button.type = 'button';
  button.className = 'issue-node';
  button.setAttribute('aria-pressed', String(key(issue.identity) === selected));
  button.addEventListener('click', () => choose(key(issue.identity)));
  return button;
}
function blockers(parent, issue, context, textual = false) {
  const section = issue.blocked_by;
  line(parent, `Blocker observations: ${status(section?.status)}`);
  const edges = items(section);
  if (edges === null) return line(parent, 'Blockers unavailable; not known empty.');
  if (!edges.length) {
    return line(parent, section?.status?.complete
      ? 'No native blockers observed (complete collection).'
      : 'No blockers observed; further blockers may be unavailable.');
  }
  const list = node('ul', undefined, 'blockers');
  for (const edge of edges) {
    const target = edge.to?.identity;
    const row = node('li', undefined, 'edge');
    const collected = context.all.get(key(target));
    const resolution = edge.to?.resolution;
    const resolved = resolution?.availability === 'known' && resolution.complete;
    const visible = context.visible.has(key(target));
    const description = `${identity(edge.from)} → ${identity(target)}`;
    const kind = !resolved ? 'unresolved reference' :
      collected ? (visible ? 'collected Issue' : 'collected Issue hidden by filters') :
        'external reference (not collected)';
    row.append(node('span', `${description} — ${kind}; ${status(resolution)}`));
    if (collected && resolved && !textual) {
      row.append(issueButton(collected, context.selected, context.choose));
    }
    list.append(row);
  }
  parent.append(list);
}
export function graph(parent, alternative, issues, context) {
  parent.replaceChildren();
  alternative.replaceChildren();
  const list = node('ul', undefined, 'graph');
  for (const issue of issues) {
    const row = node('li', undefined, 'graph-row');
    const source = node('div', undefined, 'dependent');
    source.append(issueButton(issue, context.selected, context.choose));
    const labels = progress(issue).join(', ') || 'No progress';
    line(source, `${state(issue.state)} · ${labels} · ` +
      `pause label ${labelObservation(issue)}`);
    row.append(source, node('span', '→', 'direction'));
    const targets = node('div');
    blockers(targets, issue, context);
    row.append(targets);
    list.append(row);
    const textRow = node('li');
    textRow.append(node('h3', `${identity(issue.identity)} — ${issue.title}`));
    blockers(textRow, issue, context, true);
    alternative.append(textRow);
  }
  parent.append(list);
}
export function details(parent, issue, context) {
  parent.replaceChildren();
  if (!issue) return line(parent, 'Select an Issue to inspect its details.');
  parent.append(node('h3', `${identity(issue.identity)} — ${issue.title}`));
  link(parent, 'Open native Issue on GitHub', githubURL(issue.identity?.url,
    issue.identity?.repository, 'issues', issue.identity?.number));
  line(parent, `GitHub state: ${state(issue.state)}`);
  line(parent, `Native progress: ${progress(issue).join(', ') || 'None observed'}`);
  line(parent, `Assignees: ${(issue.assignees ?? [])
    .map(person => person.login).join(', ') || 'None observed'}`);
  issueControls(parent, issue);
  claims(parent, issue.core);
  blockers(parent, issue, context);
}
