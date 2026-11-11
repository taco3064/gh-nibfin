import {parse, items, fresh, text, statusText} from './model.js';
import {repositoryPanel} from './repository.js';
import {graphCards, details, diagram} from './graph.js';

export function dashboard() {
  const byId = id => document.getElementById(id);
  const search = byId('search');
  const state = byId('state');
  const progress = byId('progress');
  let snapshot;
  let selection;
  let previous = false;
  let lastFresh;
  function select(value) {
    selection = value;
    render(true);
  }
  function render(focus = false) {
    if (!snapshot) return;
    const all = items(snapshot.issues);
    const query = search.value.toLowerCase();
    const visible = all.filter(issue =>
      text(`${issue.identity.number} ${issue.title}`).toLowerCase().includes(query) &&
      (!state.value || state.value === issue.state) &&
      (!progress.value || issue.progress.includes(progress.value)));
    lastFresh = fresh(snapshot);
    byId('freshness').textContent = (previous ? 'Previous data. ' : '') +
      (lastFresh ? 'Within observation lifetime; see independent statuses.' :
        'Stale or invalid observation time. Retained facts are incomplete.');
    repositoryPanel(byId('repository'), snapshot);
    byId('issue-status').textContent = 'Issues: ' + statusText(snapshot,
      snapshot.issues.status, snapshot.issues.items);
    byId('count').textContent = `${visible.length} of ${all.length} ` +
      'retained Issues shown. ' +
      'Arrows point from dependent to blocker. Outside-view dependencies remain facts.';
    graphCards(byId('nodes'), snapshot, visible, selection, select);
    diagram(byId('diagram'), visible);
    const heading = details(byId('details'), snapshot, selection, all, visible, select);
    if (focus) heading.focus();
  }
  for (const control of [search, state, progress]) {
    control.addEventListener('input', () => render());
  }
  byId('reset').addEventListener('click', () => {
    search.value = state.value = progress.value = '';
    render();
    search.focus();
  });
  // Re-evaluate nested status observations too, not just top-level expiry.
  setInterval(() => {
    if (snapshot && fresh(snapshot) !== lastFresh) render();
  }, 250);
  return {
    load(value) {
      const validated = parse(value);
      snapshot = validated;
      selection = null;
      previous = false;
      byId('views').hidden = false;
      render();
    },
    failed() {
      if (snapshot) {
        previous = true;
        render();
      }
    }
  };
}
