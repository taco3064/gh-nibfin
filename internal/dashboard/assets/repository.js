import {append, original} from './dom.js';
import {items, statusText, claimText, link, identityLink,
  checkLink, observation} from './model.js';
export function core(parent, claims) {
  for (const name of ['completion', 'pause', 'execution']) {
    append(parent, 'p', `Core ${name}: ${claimText(claims[name])}`);
  }
  append(parent, 'p', 'Controls unavailable: requested/applied outcomes ' +
    'and eligibility ' +
    'cannot be established. No running activity does not mean never started.');
}
export function repositoryPanel(parent, snapshot) {
  parent.replaceChildren();
  append(parent, 'h2', 'Repository');
  for (const repo of items(snapshot.repository)) {
    append(parent, 'h3', repo.name);
    original(parent, link(repo.url, repo), 'Repository on GitHub');
  }
  append(parent, 'p', statusText(snapshot, snapshot.repository.status,
    snapshot.repository.items));
  append(parent, 'p', `Snapshot ${observation(snapshot.started_at)} → ` +
    `${observation(snapshot.finished_at)}; expires ${observation(snapshot.expires_at)}`);
  core(parent, snapshot.core);
  append(parent, 'h3', 'Repository activity');
  append(parent, 'p', 'PRs, runs and checks are repository activity. ' +
    'They establish no Issue association or Core completion.');
  append(parent, 'p', 'Pull requests: ' + statusText(snapshot,
    snapshot.pull_requests.status, snapshot.pull_requests.items));
  for (const pr of items(snapshot.pull_requests)) {
    const row = append(parent, 'details');
    append(row, 'summary', `PR #${pr.identity.number}; ${pr.state}; ` +
      `merged ${pr.merged_at ?? 'not recorded'}; head ${pr.head}`);
    original(row, identityLink(pr.identity, 'pull'));
    append(row, 'p', 'Checks: ' + statusText(snapshot,
      pr.checks.status, pr.checks.items));
    for (const check of items(pr.checks)) {
      const fact = append(row, 'p', `${check.name}; ${check.status}; ` +
        `${check.conclusion ?? 'no conclusion'}; head ${check.head} `);
      original(fact, checkLink(check, pr), 'Check on GitHub');
    }
  }
  append(parent, 'p', 'Runs: ' + statusText(snapshot, snapshot.runs.status,
    snapshot.runs.items));
  for (const run of items(snapshot.runs)) {
    const row = append(parent, 'details');
    append(row, 'summary', `Run ${run.id}, attempt ${run.attempt}; ${run.status}; ` +
      `${run.conclusion ?? 'no conclusion'}`);
    append(row, 'p', `Head ${run.head}`);
    original(row, link(run.url, run.repository, `/actions/runs/${run.id}`));
    append(row, 'p', 'Explicit native PR references: ' +
      (run.pull_requests.length ? '' : 'none recorded'));
    for (const pr of run.pull_requests) {
      original(row, identityLink(pr, 'pull'), `${pr.repository.name} PR #${pr.number}`);
    }
  }
}
