import {node, line, link} from './dom.js';

let overview;
let request;
let host;
let identity;
let selectedAttempt = '';
let selectionKey = '';
let outcome;
let pending = false;
const submissions = new Map();
let activeSubmission;

const key = value => value ? JSON.stringify(value) : '';
const attemptKey = value => `${value.stage}:${value.id}`;
const reportsFor = row => (row?.evidence ?? [])
  .flatMap(work => work.diagnosis?.reports ?? []);
const historyFor = row => (row?.evidence ?? [])
  .flatMap(work => work.diagnosis?.diagnoses?.results ?? []);
const currentRow = () => (overview?.issues?.items ?? []).find(row =>
  key(row.identity) === key(identity));
const fresh = row => row && !row.retained &&
  Date.now() < Date.parse(overview.expires_at) &&
  Date.now() >= Date.parse(overview.observed_at);

export function observeDiagnosis(value, send) {
  overview = value;
  request = send;
  render();
}
export function explanation(parent, issueIdentity) {
  host = node('section', undefined, 'issue-explanation');
  parent.append(host);
  identity = issueIdentity;
  render();
}
function list(parent, title, values) {
  if (!values?.length) return;
  const section = node('details');
  section.append(node('summary', title));
  const items = node('ul');
  for (const value of values) items.append(node('li', value));
  section.append(items);
  parent.append(section);
}
function reportView(parent, report) {
  line(parent, `${report.confidence === 'attributed' ? 'Attributed explanation' :
    'Observed native facts'} — ${report.producer}. Historical report; ` +
    'current cause is not established.');
  line(parent, report.rootCause || report.summary);
  const details = node('details');
  details.append(node('summary', 'Facts, missing evidence and sources'));
  line(details, `Stage: ${report.binding.stage}; ` +
    `admission attempt: ${report.binding.admissionId}`);
  line(details, `Evidence observed: ${report.observedAt}`);
  if (report.code) line(details, `Core code: ${report.code}`);
  list(details, 'Observed facts', report.confirmed);
  if (report.termination) {
    const value = report.termination;
    line(details, `Termination owner: ${value.owner}; observed: ` +
      `${value.observedAt || 'Unknown'}; exit: ${value.exitCode ?? 'Unknown'}; ` +
      `container OOM: ${value.oomKilled ?? 'Unknown'}. Host cause is unknown.`);
  }
  if (report.workflow) {
    line(details, `Native workflow conclusion: ${report.workflow.conclusion}`);
    link(details, 'Native workflow evidence', report.workflow.url);
  }
  list(details, 'Attributed findings', report.findings);
  list(details, 'Missing evidence', report.missing);
  for (const url of report.references ?? []) link(details, 'Native source', url);
  parent.append(details);
}
function diagnosisView(parent, value) {
  if (value.status === 'unresolved-effect') {
    line(parent, 'Unresolved diagnosis effect. An in-flight record does not ' +
      'confirm running progress. No automatic retry.');
    return;
  }
  line(parent, `Attributed diagnosis: ${value.explanation}`);
  line(parent, `Collected: ${value.observedAt}. Historical explanation; ` +
    'source evidence may be stale. This cannot resume delivery.');
  list(parent, 'Possible causes (hypotheses)', value.hypotheses);
  list(parent, 'Missing evidence', value.missing);
  list(parent, 'Observed facts in source reports', value.confirmed);
  list(parent, 'Source observation times', (value.sourceTimes ?? [])
    .map(time => time || 'Unknown source time'));
}
function render() {
  if (!host?.isConnected) return;
  host.hidden = !overview;
  if (!overview) return;
  const row = currentRow();
  const nextKey = key([identity, row?.node_id, overview?.subject]);
  if (nextKey !== selectionKey) {
    selectionKey = nextKey;
    selectedAttempt = '';
    outcome = undefined;
    pending = false;
  }
  const reports = reportsFor(row);
  const attempts = [...new Map(reports.map(report => {
    const value = {id: report.binding.admissionId, stage: report.binding.stage};
    return [attemptKey(value), value];
  })).values()];
  if (!attempts.some(value => attemptKey(value) === selectedAttempt)) {
    selectedAttempt = attempts.length ? attemptKey(attempts[0]) : '';
    outcome = undefined;
  }
  const attempt = attempts.find(value => attemptKey(value) === selectedAttempt);
  activeSubmission = key([nextKey, selectedAttempt]);
  const submission = submissions.get(activeSubmission);
  outcome = submission?.outcome;
  pending = submission?.pending ?? false;
  const retained = historyFor(row).filter(value =>
    attemptKey(value.attempt) === selectedAttempt);
  if (outcome?.status === 'unresolved-effect' && retained.some(value =>
    value.status === 'reported' && !submission.prior.includes(value.invocationId))) {
    submissions.delete(activeSubmission);
    outcome = undefined;
  }
  // Preserve keyboard focus and open disclosures during periodic reconciliation.
  const signature = key([row, selectedAttempt, outcome, pending, fresh(row)]);
  if (host.signature === signature) return;
  host.signature = signature;
  const focus = host.contains(document.activeElement) ?
    document.activeElement.dataset.control : undefined;
  const open = [...host.querySelectorAll('details')].map(value => value.open);
  host.replaceChildren(node('h4', 'Blocker explanation and diagnosis'));
  if (!fresh(row)) line(host, 'Selection unavailable or stale. Refresh before acting.');
  if (!reports.length) {
    line(host, 'Blocker report unavailable. No execution attempt can be guessed.');
    return;
  }
  const label = node('label', 'Retained stage and attempt ');
  const select = node('select');
  select.dataset.control = 'attempt';
  select.setAttribute('aria-label', 'Retained stage and attempt');
  for (const value of attempts) {
    const option = node('option', `${value.stage} / ${value.id}`);
    option.value = attemptKey(value);
    select.append(option);
  }
  select.value = selectedAttempt;
  select.disabled = pending;
  select.onchange = () => {
    selectedAttempt = select.value;
    outcome = undefined;
    render();
  };
  label.append(select);
  host.append(label);
  for (const report of reports.filter(value =>
    attemptKey({id: value.binding.admissionId, stage: value.binding.stage}) ===
      selectedAttempt)) reportView(host, report);
  for (const value of retained) diagnosisView(host, value);
  if (outcome?.report) diagnosisView(host, outcome.report);
  const status = node('p', pending ? 'Diagnosis submission pending; ' +
    'running progress is unconfirmed.' : outcome ?
    `Diagnosis ${outcome.status}: ${outcome.code || 'retained result'}. ` +
      'No automatic retry.' : 'Diagnosis runs only on explicit request.');
  status.setAttribute('role', 'status');
  host.append(status);
  const button = node('button', 'Request read-only diagnosis');
  button.type = 'button';
  button.dataset.control = 'request';
  button.disabled = !request || !fresh(row) || pending ||
    outcome?.status === 'unresolved-effect' ||
    retained.some(value => value.status === 'unresolved-effect');
  button.onclick = () => submit(row, attempt);
  host.append(button);
  [...host.querySelectorAll('details')].forEach((value, index) => {
    value.open = open[index] ?? false;
  });
  if (focus) host.querySelector(`[data-control="${focus}"]`)?.focus();
}
async function submit(row, attempt) {
  if (!fresh(row) || pending) return;
  pending = true;
  const submissionKey = activeSubmission;
  const submission = {pending: true, prior: historyFor(row)
    .map(value => value.invocationId)};
  submissions.set(submissionKey, submission);
  const selection = {identity: row.identity, node_id: row.node_id,
    subject: overview.subject};
  render();
  let result;
  try {
    const response = await request('/api/diagnosis', {
      method: 'POST', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({selection, attempt, diagnose: true})
    });
    result = response.ok ? await response.json() :
      [400, 401, 403, 409].includes(response.status) ?
        {status: 'denied', code: 'Selection or service unavailable'} :
        {status: 'unresolved-effect', code: 'Response unavailable; outcome unknown'};
  } catch {
    result = {status: 'unresolved-effect', code: 'Response lost; outcome unknown'};
  }
  submission.pending = false;
  submission.outcome = result;
  render();
}
