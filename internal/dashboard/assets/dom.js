import {text, claim} from './model.js';

export function node(tag, value, className) {
  const element = document.createElement(tag);
  if (value !== undefined) element.textContent = text(String(value));
  if (className) element.className = className;
  return element;
}
export function line(parent, value) {
  parent.append(node('p', value));
}
export function link(parent, label, url) {
  if (!url) {
    parent.append(node('span', `${label} (link unavailable)`));
    return;
  }
  const anchor = node('a', label);
  anchor.href = url;
  anchor.target = '_blank';
  anchor.rel = 'noopener noreferrer';
  anchor.referrerPolicy = 'no-referrer';
  parent.append(anchor);
}
export function claims(parent, core) {
  const list = node('dl');
  for (const name of ['completion', 'pause', 'execution']) {
    list.append(node('dt', `${name[0].toUpperCase()}${name.slice(1)} evidence`),
      node('dd', claim(core?.[name])));
  }
  parent.append(list);
}
