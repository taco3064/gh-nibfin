import {node, line} from './dom.js';
import {items, stale, labelObservation} from './model.js';

let current;
let send;
let invalid = true;
let busy = false;
let revision = 0;
let review;
let dialog;

function available() {
  return send && current && !invalid && !stale(current) && !busy && !review;
}
export function freshness(invalidate = false) {
  invalid ||= invalidate;
  for (const button of document.querySelectorAll('.label-action')) {
    button.disabled = !available() || button.dataset.empty === 'true';
  }
  if (dialog?.open) {
    dialog.querySelector('[data-confirm]').disabled = busy || invalid || stale(current);
    dialog.querySelector('[data-cancel]').disabled = busy;
  }
}
function action(parent, caption, operation, candidates, batch = false) {
  const button = node('button', caption, 'label-action');
  button.type = 'button';
  const limit = current.label_action_limit;
  button.dataset.empty = String(!Number.isInteger(limit) ||
    candidates.length === 0 || candidates.length > limit);
  button.addEventListener('click', () => prepare(operation, candidates, batch));
  parent.append(button);
  freshness();
}
function target(issue) {
  return {repository_id: issue.identity.repository.id, issue_id: issue.identity.id,
    number: issue.identity.number};
}
function observation(parent, issue) {
  line(parent, `Observed pause label: ${labelObservation(issue)}. ` +
    'Original pause actor: Unknown.');
}
export function issueControls(parent, issue) {
  observation(parent, issue);
  const repository = items(current?.repository)?.[0];
  const matching = issue.identity?.repository?.id === repository?.id &&
    issue.identity?.repository?.name === repository?.name;
  const candidates = matching ? [issue] : [];
  action(parent, 'Pause', 'pause', candidates);
  action(parent, 'Resume', 'resume', candidates);
}
export function configure(snapshot, post) {
  current = snapshot;
  send = post;
  invalid = false;
  revision++;
  if (!busy && dialog?.open) dialog.close();
  review = undefined;
  const parent = document.getElementById('label-controls');
  parent.replaceChildren();
  const repository = items(snapshot.repository)?.[0];
  const issues = (items(snapshot.issues) ?? []).filter(issue =>
    issue.identity?.repository?.id === repository?.id &&
    issue.identity?.repository?.name === repository?.name &&
    issue.progress?.includes('scope'));
  const paused = issues.filter(issue => issue.pause_label?.present === true);
  line(parent, `Observed paused scoped Issues: ${paused.length}. ` +
    'Effective Core pause state: Unknown. Original pause actor: Unknown.');
  line(parent, 'Batches review only collected candidates; later Issues are not added. ' +
    (snapshot.issues.status?.complete ? 'Collection reported complete.' :
      'Partial collection: this is not the entire repository.'));
  line(parent, Number.isInteger(snapshot.label_action_limit)
    ? `Maximum ${snapshot.label_action_limit} reviewed Issues per request. ` +
      'Native eligibility is rechecked.'
    : 'Request limits unavailable. Read snapshot to refresh.');
  action(parent, 'Pause All', 'pause',
    issues.filter(issue => issue.state === 'open'), true);
  action(parent, 'Resume All', 'resume', paused, true);
  if (!dialog) {
    dialog = node('dialog');
    dialog.setAttribute('aria-label', 'Review label request');
    document.body.append(dialog);
    dialog.addEventListener('cancel', event => {
      event.preventDefault();
      if (!busy) cancel();
    });
  }
}
function cancel() {
  review = undefined;
  dialog.close();
  freshness();
}
async function prepare(operation, candidates, batch) {
  if (!available()) return;
  busy = true;
  freshness();
  const version = revision;
  const output = document.getElementById('label-results');
  output.textContent = 'Checking current native eligibility…';
  try {
    const response = await send({phase: 'review', action: operation, batch,
      repository: items(current.repository)[0].name, targets: candidates.map(target)});
    if (version !== revision || invalid || stale(current)) throw new Error('Stale');
    review = response;
    dialog.replaceChildren(node('h2', `Review ${operation} label request`));
    line(dialog, 'Exact reviewed set follows. Ineligible or unavailable items will ' +
      'receive results without edits. Each target is rechecked before submission.');
    const affected = response.items.filter(item => item.eligible)
      .map(item => '#' + item.target.number).join(', ') || 'None';
    line(dialog, 'Eligible affected set: ' + affected + '.');
    const list = node('ul');
    for (const item of response.items) {
      const title = item.title || 'Unknown title';
      list.append(node('li', `#${item.target.number} — ${title}; ` +
        `label ${item.label}; ${item.eligible ? 'eligible' : item.reason}; ` +
        'original pause actor: Unknown'));
    }
    dialog.append(list);
    line(dialog, 'Label removal requests wake/recovery only; it does not prove an ' +
      'interrupted delivery restarted. Core verdict remains Unknown.');
    const confirm = node('button', 'Confirm request');
    confirm.type = 'button';
    confirm.dataset.confirm = '';
    confirm.addEventListener('click', () => apply(version));
    const close = node('button', 'Cancel');
    close.type = 'button';
    close.dataset.cancel = '';
    close.addEventListener('click', cancel);
    dialog.append(confirm, close);
    dialog.showModal();
    close.focus();
    output.textContent = 'Review the exact set before confirming.';
  } catch {
    output.textContent = 'Review unavailable. ' +
      'Refresh observations before another request.';
    review = undefined;
  } finally {
    busy = false;
    freshness();
  }
}
function results(output, values) {
  output.replaceChildren();
  const count = (field, value) => values.filter(item => item[field] === value).length;
  line(output, `Submissions: Requested ${count('submission', 'Requested')}; ` +
    `Request failed ${count('submission', 'Request failed')}; ` +
    `Unknown ${count('submission', 'Unknown')}; ` +
    `Not needed ${count('submission', 'Not needed')}.`);
  line(output, `Labels: present ${count('label', 'present')}; ` +
    `absent ${count('label', 'absent')}; unreadable ${count('label', 'unreadable')}. ` +
    `Final Core verdicts: Unknown ${values.length}.`);
  const list = node('ul');
  for (const item of values) {
    list.append(node('li', `#${item.target.number}: ${item.submission}; ` +
      `observed label ${item.label}; final Core verdict Unknown; ${item.reason}.`));
  }
  output.append(list);
  line(output, 'Results are independent per Issue. ' +
    'Refresh to observe subsequent changes. ' +
    'Label removal does not prove an interrupted delivery restarted.');
}
async function apply(version) {
  if (busy || !review || invalid || stale(current) || version !== revision) return;
  busy = true;
  freshness();
  const token = review.review;
  review = undefined;
  const output = document.getElementById('label-results');
  output.textContent = 'Submitting reviewed requests…';
  try {
    const response = await send({phase: 'apply', review: token});
    results(output, response.results);
  } catch {
    output.textContent = 'Submission outcome Unknown. No automatic retry. ' +
      'Refresh observations before reviewing another request.';
  } finally {
    busy = false;
    invalid = true;
    dialog.close();
    freshness();
  }
}
