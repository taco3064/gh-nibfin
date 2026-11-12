import {items, status, identity, githubURL, checkURL, text, state} from './model.js';
import {node, line, link} from './dom.js';

function section(parent, title, data, render) {
  parent.append(node('h3', title));
  line(parent, status(data?.status));
  const entries = items(data);
  if (entries === null) return line(parent, 'Observations unavailable.');
  if (!entries.length) {
    return line(parent, data?.status?.complete
      ? 'Known empty collection.' : 'No observations; collection incomplete.');
  }
  const list = node('ul');
  for (const entry of entries) {
    const row = node('li');
    render(row, entry);
    list.append(row);
  }
  parent.append(list);
}
export function activity(parent, snapshot) {
  parent.replaceChildren();
  section(parent, 'Pull requests', snapshot.pull_requests, (row, pr) => {
    link(row, identity(pr.identity), githubURL(pr.identity?.url,
      pr.identity?.repository, 'pull', pr.identity?.number));
    line(row, `State: ${state(pr.state)}; merged: ${pr.merged_at ? 'yes' : 'Unknown'}`);
    section(row, 'Checks', pr.checks, (target, check) => {
      link(target, text(check.name), checkURL(check.url,
        pr.identity?.repository, check.id, pr.identity?.number));
      line(target, `Status: ${text(check.status) || 'Unknown'}; ` +
        `conclusion: ${text(check.conclusion) || 'Unknown'}`);
    });
  });
  section(parent, 'Actions runs', snapshot.runs, (row, run) => {
    link(row, `${text(run.repository?.name)} run ${run.id}`,
      githubURL(run.url, run.repository, 'actions/runs', run.id));
    line(row, `Status: ${text(run.status) || 'Unknown'}; ` +
      `conclusion: ${text(run.conclusion) || 'Unknown'}`);
    const references = node('ul');
    for (const ref of run.pull_requests ?? []) {
      const item = node('li');
      link(item, `Native PR reference: ${identity(ref)}`,
        githubURL(ref.url, ref.repository, 'pull', ref.number));
      references.append(item);
    }
    row.append(references);
  });
}
