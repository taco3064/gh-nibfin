export const repository = {id: 1, name: 'example/project',
  url: 'https://github.com/example/project'};
export const other = {id: 2, name: 'other/project',
  url: 'https://github.com/other/project'};
export function status(availability = 'known', complete = true, reason = 'none') {
  return {availability, complete, reason, observed_at: new Date().toISOString(),
    source: 'github'};
}
export function section(items, observation = status()) {
  return {items, status: observation};
}
export function core() {
  const claim = {availability: 'unknown', verification: 'unverified',
    reason: 'no_supported_reader', source: 'none', binding: null};
  return {completion: {...claim}, pause: {...claim}, execution: {...claim}};
}
export function identity(number, repo = repository, resource = 'issues') {
  return {repository: repo, id: repo.id * 1000 + number, number,
    url: `${repo.url}/${resource}/${number}`};
}
export function issue(number, repo = repository) {
  return {identity: identity(number, repo), title: `Issue ${number}`,
    state: 'open', progress: ['scope'], assignees: [{id: 7, login: 'reader'}],
    pause_label: {present: false, actor: 'Unknown'},
    core: core(), blocked_by: section([])};
}
export function edge(from, to, resolution = status()) {
  return {from: from.identity, to: {identity: to, resolution}};
}
export function snapshot() {
  const a = issue(1);
  a.title = 'A dependent';
  const b = issue(2);
  b.title = 'B blocker';
  b.state = 'closed';
  b.progress = ['delivered'];
  const c = issue(1, other);
  a.blocked_by = section([
    edge(a, b.identity), edge(a, c.identity), edge(a, identity(8, other)),
    edge(a, identity(9, {...other, id: 0}),
      status('denied', false, 'permission_denied'))
  ]);
  const now = Date.now();
  return {schema_version: 1, started_at: new Date(now - 1000).toISOString(),
    finished_at: new Date(now).toISOString(),
    expires_at: new Date(now + 120000).toISOString(),
    repository: section([repository]), issues: section([a, b, c]), core: core(),
    pull_requests: section([{identity: identity(5, repository, 'pull'), state: 'open',
      merged_at: null, head: 'a'.repeat(40), checks: section([
        {id: 30, name: 'verify', status: 'completed', conclusion: 'success',
          head: 'a'.repeat(40), url: `${repository.url}/runs/30`},
        {id: 31, name: 'pending', status: 'queued', conclusion: null, url: ''},
        {id: 32, name: 'PR check', status: 'completed', conclusion: 'success',
          url: `${repository.url}/pull/5/checks?check_run_id=32`},
        {id: 33, name: 'Actions job', status: 'completed', conclusion: 'success',
          url: `${repository.url}/actions/runs/10/job/33`}
      ], status('unknown', false, 'collection_limit'))}]),
    runs: section([{repository, id: 10, attempt: 1, head: 'a'.repeat(40),
      status: 'in_progress', conclusion: null, url: `${repository.url}/actions/runs/10`,
      pull_requests: [identity(5, repository, 'pull')]}])};
}
