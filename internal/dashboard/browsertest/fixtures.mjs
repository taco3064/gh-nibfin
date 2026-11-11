// Synthetic projection facts, independent of production presentation rules.
export const instant = new Date('2026-11-11T12:00:00Z');
export const repo = {id: 10, name: 'example/project',
  url: 'https://github.com/example/project'};
export function status(overrides = {}) {
  return {availability: 'known', complete: true, reason: 'none',
    observed_at: instant.toISOString(), source: 'repos/example/project', ...overrides};
}
export const section = (values, overrides) =>
  ({items: values, status: status(overrides)});
export const core = () => Object.fromEntries(['completion', 'pause', 'execution'].map(
  name => [name, {availability: 'unknown', verification: 'unverified',
    reason: 'no_supported_reader', source: 'none', binding: null}]));
export function identity(number, repository = repo) {
  return {id: repository.id * 1000 + number, number, repository,
    url: `${repository.url}/issues/${number}`};
}
export function edge(from, to, resolution = status()) {
  return {from, to: {identity: to, resolution}};
}
export function fixture(count = 4) {
  const nodes = Array.from({length: count}, (_, i) => ({identity: identity(i + 1),
    title: `Issue ${i + 1}`, state: i === 0 ? 'closed' : 'open',
    progress: i === 0 ? ['scope', 'shaped', 'delivered'] : ['scope'],
    assignees: [{id: 5, login: 'reader'}], core: core(), blocked_by: section([])}));
  for (let i = 1; i < count; i++) {
    nodes[i].blocked_by = section([edge(nodes[i].identity, nodes[i - 1].identity)]);
  }
  nodes[0].blocked_by = section([edge(nodes[0].identity, nodes[1].identity)]);
  const external = {id: 20, name: 'other/project',
    url: 'https://github.com/other/project'};
  nodes[2].blocked_by.items.push(edge(nodes[2].identity, identity(1, external)));
  const denied = {id: 0, name: 'hidden/project',
    url: 'https://github.com/hidden/project'};
  nodes[2].blocked_by.items.push(edge(nodes[2].identity, identity(1, denied),
    status({availability: 'denied', complete: false, reason: 'permission_denied'})));
  const pr = {...identity(7), url: `${repo.url}/pull/7`};
  return {schema_version: 1, started_at: instant.toISOString(),
    finished_at: instant.toISOString(),
    expires_at: new Date(+instant + 300000).toISOString(),
    repository: section([repo]), issues: section(nodes), core: core(),
    pull_requests: section([{identity: pr, state: 'closed',
      merged_at: instant.toISOString(), head: 'a'.repeat(40),
      checks: section([{id: 55, head: 'a'.repeat(40), name: 'verify',
        status: 'completed', conclusion: 'success',
        url: `${repo.url}/actions/runs/99/job/55`}])}]),
    runs: section([{repository: repo, id: 99, attempt: 1, head: 'a'.repeat(40),
      status: 'completed', conclusion: 'success', url: `${repo.url}/actions/runs/99`,
      pull_requests: [pr]}])};
}
