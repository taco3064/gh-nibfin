import {snapshot, status, section, identity, repository} from './fixtures.mjs';

export function overviewFixture() {
  const now = new Date().toISOString();
  const field = (value, source = 'github-job') => ({value, source,
    observedAt: now, sourceTime: now, freshness: 'fresh', code: 'NATIVE_JOB'});
  const job = {repository: 'example/project', repositoryId: 1, runId: 10,
    attempt: 1, jobId: 20, startedAt: now,
    url: 'https://github.com/example/project/actions/runs/10/job/20'};
  const row = {key: '1:I_one', identity: identity(1), node_id: 'I_one',
    title: 'Assigned without scope', state: 'open', assignees: [{id: 7, login: 'reader'}],
    observed_at: now, evidence: []};
  const runners = [1, 2, 3, 4].map((id, index) => ({key: `1:${id}`,
    hostId: 'local-controller-1000', registration: {repository: 'example/project',
      repositoryId: 1, runnerId: id, name: `Runner ${id}`},
    routes: [{repository: 'example/project', repositoryId: 1}],
    busy: field(index === 0, 'github-runner'), online: field(true, 'github-runner'),
    state: ['busy', 'idle', 'offline', 'unknown'][index],
    association: field(index === 0 ? job : null)}));
  const value = {schema_version: 1, subject: {id: 7, login: 'reader'},
    observed_at: now, expires_at: new Date(Date.now() + 30000).toISOString(),
    local_status: 'observed', discovery: status(), repositories: section([repository]),
    runners: section(runners), issues: section([row]), work: []};
  const idle = structuredClone(value);
  idle.runners.items[0].state = 'idle';
  idle.runners.items[0].busy.value = false;
  idle.runners.items[0].association.value = null;
  return {snapshot: snapshot(), overviews: [value, idle]};
}
