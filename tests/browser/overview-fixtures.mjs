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
  const emptyProject = {id: 2, name: 'example/empty',
    url: 'https://github.com/example/empty'};
  value.repositories.items.push(emptyProject);
  const emptySnapshot = snapshot();
  emptySnapshot.repository = section([emptyProject]);
  emptySnapshot.issues = section([]);
  emptySnapshot.pull_requests = section([]);
  emptySnapshot.runs = section([]);
  const idle = structuredClone(value);
  idle.runners.items[0].state = 'idle';
  idle.runners.items[0].busy.value = false;
  idle.runners.items[0].association.value = null;
  return {snapshot: snapshot(), overviews: [value, idle],
    projects: {'example/empty': emptySnapshot}};
}

export function diagnosisFixture() {
  const fixture = overviewFixture();
  const issue = {repository: 'example/project', repositoryId: 1,
    number: 1, id: 'I_one', qualified: 'example/project#1'};
  const report = {reference: 'report:' + 'a'.repeat(64),
    binding: {issue, stage: 'deliver', subjectId: 7,
      admissionId: '11111111-1111-1111-1111-111111111111', native: {}},
    producer: 'agent', confidence: 'attributed',
    observedAt: '2026-11-01T00:00:00Z', summary: 'Agent reported a blocker.',
    rootCause: 'A required input was unavailable.', findings: ['Input was missing.'],
    confirmed: [], missing: ['INDEPENDENT_CAUSE_CONFIRMATION_UNAVAILABLE'],
    references: ['https://github.com/example/project/issues/1'],
    executionAuthority: false};
  const second = structuredClone(report);
  second.reference = 'report:' + 'b'.repeat(64);
  second.binding.admissionId = '22222222-2222-2222-2222-222222222222';
  second.binding.stage = 'accept';
  for (const value of fixture.overviews) {
    value.issues.items[0].evidence = [{issue, diagnosis: {status: 'reported',
      reports: [report, second], diagnoses: {status: 'observed', results: [{
        status: 'reported', producer: 'diagnosis-agent', confidence: 'attributed',
        explanation: 'The retained evidence is incomplete.',
        hypotheses: ['A prerequisite might be missing.'], missing: ['Native telemetry.'],
        invocationId: '33333333-3333-3333-3333-333333333333',
        evidenceId: 'c'.repeat(64), subjectId: 7, issue,
        attempt: {id: second.binding.admissionId, stage: 'accept'},
        observedAt: '2026-11-02T00:00:00Z', confirmed: [],
        sourceReports: [second.reference], sourceTimes: [second.observedAt],
        executionAuthority: false
      }]}}}];
  }
  return fixture;
}
