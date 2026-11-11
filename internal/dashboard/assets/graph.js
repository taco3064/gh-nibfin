import {items, key, label, text, same} from './model.js';

export function visibleNodes(snapshot) {
  const query = document.getElementById('query').value.toLowerCase();
  const state = document.getElementById('state').value;
  const progress = document.getElementById('progress').value;
  return items(snapshot.issues).filter(node =>
    text(`${label(node.identity)} ${node.title}`).toLowerCase().includes(query) &&
    (!state || node.state === state) && (!progress || node.progress.includes(progress))
  );
}
export function targetNode(snapshot, reference) {
  return items(snapshot.issues).find(node => same(node.identity, reference.identity));
}
export function svgOverview(nodes) {
  const ns = 'http://www.w3.org/2000/svg';
  function svg(tag, attributes, content) {
    const element = document.createElementNS(ns, tag);
    for (const [name, value] of Object.entries(attributes)) {
      element.setAttribute(name, value);
    }
    if (content) element.textContent = content;
    return element;
  }
  const height = Math.max(120, Math.ceil(nodes.length / 4) * 110);
  const root = svg('svg', {width: 1000, height, 'aria-hidden': 'true'});
  const defs = svg('defs', {});
  const marker = svg('marker', {
    id: 'arrow', viewBox: '0 0 10 10', refX: 10, refY: 5,
    markerWidth: 8, markerHeight: 8, orient: 'auto-start-reverse'
  });
  marker.append(svg('polygon', {points: '0,0 10,5 0,10'}));
  defs.append(marker);
  root.append(defs);
  const positions = new Map(nodes.map((node, i) => [
    key(node.identity), {x: (i % 4) * 245 + 10, y: Math.floor(i / 4) * 110 + 20}
  ]));
  for (const node of nodes) {
    const from = positions.get(key(node.identity));
    for (const edge of items(node.blocked_by)) {
      const to = positions.get(key(edge.to.identity));
      if (!to) continue;
      root.append(svg('line', {
        x1: from.x + 100, y1: from.y + 40, x2: to.x + 100, y2: to.y,
        'marker-end': 'url(#arrow)', 'data-from': key(edge.from),
        'data-to': key(edge.to.identity)
      }));
    }
  }
  for (const node of nodes) {
    const {x, y} = positions.get(key(node.identity));
    root.append(svg('rect', {x, y, width: 215, height: 40, rx: 5}));
    root.append(svg('text', {x: x + 8, y: y + 25},
      `#${node.identity.number} · ${node.state}`));
  }
  return root;
}
