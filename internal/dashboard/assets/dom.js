import {text} from './model.js';
export function element(tag, value, className) {
  const node = document.createElement(tag);
  if (value !== undefined) node.textContent = text(value);
  if (className) node.className = className;
  return node;
}
export function append(parent, tag, value, className) {
  const child = element(tag, value, className);
  parent.append(child);
  return child;
}
export function action(parent, label, callback) {
  const button = append(parent, 'button', label);
  button.type = 'button';
  button.addEventListener('click', callback);
  return button;
}
export function original(parent, url, label = 'Open original on GitHub') {
  if (!url) {
    append(parent, 'span', `${label}: link unavailable`);
    return;
  }
  const anchor = append(parent, 'a', label);
  anchor.href = url;
  anchor.target = '_blank';
  anchor.rel = 'noopener noreferrer';
}
