import {parse, key, current} from './model.js';
import {repositoryView, graphView, detailsView} from './view.js';
let snapshot, reference, previous = false;
const state = {query: '', state: '', progress: '', selected: null,
  select(identity, ref) {
    state.selected = key(identity);
    reference = ref;
    render();
    document.getElementById('details').focus();
  }};
export function display(value) {
  snapshot = parse(value);
  reference = undefined;
  wasCurrent = current(snapshot);
  previous = false;
  render();
}
export function failed() {
  previous = true;
  render();
}
function render() {
  if (!snapshot) return;
  document.getElementById('freshness').textContent =
    `${previous ? 'Previous data. ' : ''}${current(snapshot) ?
      'Within observation window.' : 'Stale or invalid observation window.'}`;
  repositoryView(snapshot);
  graphView(snapshot, state);
  detailsView(snapshot, state, reference);
}
for (const name of ['query', 'state', 'progress']) {
  document.getElementById(name).addEventListener('input', e => {
    state[name] = e.target.value;
    render();
  });
}
document.getElementById('reset').onclick = () => {
  for (const name of ['query', 'state', 'progress']) {
    state[name] = '';
    document.getElementById(name).value = '';
  }
  render();
};
let wasCurrent;
setInterval(() => {
  if (!snapshot) return;
  const fresh = current(snapshot);
  if (fresh !== wasCurrent) {
    wasCurrent = fresh;
    render();
  }
}, 250);
