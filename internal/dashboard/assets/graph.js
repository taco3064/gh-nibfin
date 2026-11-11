import {key, label, text} from './model.js';
const namespace = 'http://www.w3.org/2000/svg';
function svgElement(tag, attributes, parent) {
  const node = document.createElementNS(namespace, tag);
  for (const [name, value] of Object.entries(attributes)) {
    node.setAttribute(name, String(value));
  }
  parent.append(node);
  return node;
}
// A deterministic layout uses no traversal, so cycles cannot stall rendering.
// The adjacent text graph supplies all full titles, statuses and keyboard actions.
export function drawGraph(visible, all, select) {
  const root = document.getElementById('visual-graph');
  root.replaceChildren();
  const svg = svgElement('svg', {width: 800, 'aria-hidden': 'true'}, root);
  const definitions = svgElement('defs', {}, svg);
  const marker = svgElement('marker', {id: 'blocker-arrow', viewBox: '0 0 10 10',
    refX: 10, refY: 5, markerWidth: 7, markerHeight: 7, orient: 'auto'}, definitions);
  svgElement('path', {d: 'M 0 0 L 10 5 L 0 10 z', class: 'arrow'}, marker);
  const positions = new Map();
  visible.forEach((issue, n) => positions.set(key(issue.identity), {
    x: 20, y: 20 + n * 70, identity: issue.identity, issue, kind: 'collected'}));
  let references = 0;
  for (const issue of visible) {
    for (const edge of issue.blocked_by.items || []) {
      const identity = edge.to.identity, k = key(identity);
      if (!positions.has(k)) {
        const hidden = all.some(i => key(i.identity) === k);
        positions.set(k, {x: 470, y: 20 + references++ * 70, identity,
          kind: hidden ? 'hidden' : 'reference'});
      }
    }
  }
  const rows = Math.max(visible.length, references);
  svg.setAttribute('height', Math.max(100, rows * 70 + 30));
  for (const issue of visible) {
    const fromKey = key(issue.identity), from = positions.get(fromKey);
    for (const edge of issue.blocked_by.items || []) {
      const toKey = key(edge.to.identity), to = positions.get(toKey);
      const x = from.x + 270, y = from.y + 20;
      const endX = to.kind === 'collected' ? to.x + 270 : to.x;
      const endY = to.y + 20;
      const bend = to.kind === 'collected' ? 400 : 420;
      const d = `M ${x} ${y} C ${bend} ${y - 30}, ` +
        `${bend} ${endY + 30}, ${endX} ${endY}`;
      svgElement('path', {d, class: 'dependency-edge', 'data-from': fromKey,
        'data-to': toKey, 'marker-end': 'url(#blocker-arrow)'}, svg);
    }
  }
  for (const [k, p] of positions) {
    const group = svgElement('g', {'data-key': k, class: `graph-node ${p.kind}`}, svg);
    svgElement('rect', {x: p.x, y: p.y, width: 270, height: 42, rx: 5}, group);
    const title = svgElement('title', {}, group);
    title.textContent = text(p.issue ? `${label(p.identity)} ${p.issue.title}` :
      `${p.kind}: ${label(p.identity)}`);
    const caption = svgElement('text', {x: p.x + 8, y: p.y + 27}, group);
    caption.textContent = text(`${p.kind}: ${label(p.identity)}`).slice(0, 38);
    if (p.issue) group.onclick = () => select(p.identity);
  }
}
