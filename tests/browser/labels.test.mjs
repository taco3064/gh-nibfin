import test from 'node:test';
import assert from 'node:assert/strict';
import {open} from './host.mjs';
import {snapshot, issue, repository, section, status} from './fixtures.mjs';

const button = (page, name) => page.getByRole('button', {name, exact: true});
const dialog = page => page.getByRole('dialog', {name: 'Review label request'});
const output = page => page.locator('#label-results');
async function contains(locator, text) {
  await locator.page().waitForFunction(({selector, text}) =>
    document.querySelector(selector)?.textContent.includes(text),
  {selector: '#label-results', text});
  assert.ok((await locator.textContent()).includes(text));
}
function value(count = 1, paused = false) {
  const result = snapshot();
  result.issues = section(Array.from({length: count}, (_, i) => issue(i + 1)));
  for (const item of result.issues.items) item.pause_label.present = paused;
  return result;
}
async function selectFirst(page) {
  await page.locator('#graph').getByRole('button',
    {name: 'example/project#1 — Issue 1', exact: true}).click();
}

test('single actions: cancellation, submission and Unknown verdict', async t => {
  const h = await open(t, value(), 'labels');
  const {page} = h;
  await h.load();
  await selectFirst(page);
  await button(page, 'Pause').click();
  await dialog(page).waitFor();
  assert.ok((await dialog(page).textContent()).includes('#1 — Issue 1'));
  assert.ok((await dialog(page).textContent()).includes('original pause actor: Unknown'));
  await button(page, 'Cancel').click();
  const applied = h.requests.filter(r => r.postData()?.includes('"phase":"apply"'));
  assert.equal(applied.length, 0);
  await button(page, 'Pause').click();
  await button(page, 'Confirm request').click();
  await contains(output(page),
    '#1: Requested; observed label present; final Core verdict Unknown');
  assert.equal(await button(page, 'Pause').isDisabled(), true);
  await h.load();
  await button(page, 'Resume').click();
  await dialog(page).waitFor();
  await button(page, 'Confirm request').click();
  await contains(output(page),
    '#1: Requested; observed label absent; final Core verdict Unknown');
  assert.ok((await output(page).textContent()).includes(
    'does not prove an interrupted delivery restarted'));
  assert.deepEqual(h.errors, []);
});

test('batches: exact set, mixed outcomes and disabled duplicates', async t => {
  const data = value(5);
  data.issues.status = status('unknown', false, 'collection_limit');
  data.issues.items[0].title = '<img src=x onerror=alert(1)> hostile';
  const h = await open(t, data, 'labels');
  const {page} = h;
  await h.load();
  assert.ok((await page.locator('#label-controls').textContent()).includes(
    'Partial collection: this is not the entire repository'));
  await button(page, 'Pause All').click();
  await dialog(page).waitFor();
  assert.equal(await dialog(page).locator('li').count(), 5);
  assert.equal(await dialog(page).locator('img').count(), 0);
  await button(page, 'Confirm request').click();
  assert.equal(await button(page, 'Confirm request').isDisabled(), true);
  assert.equal(await button(page, 'Pause All').isDisabled(), true);
  await contains(output(page), '#5: Request failed');
  await contains(output(page), '#1: Requested; observed label present');
  await contains(output(page), '#2: Request failed; observed label absent');
  await contains(output(page), '#3: Requested; observed label unreadable');
  await contains(output(page), '#4: Unknown; observed label present');
  await contains(output(page), 'Submissions: Requested 2; Request failed 2; Unknown 1');
  await contains(output(page), 'Labels: present 2; absent 2; unreadable 1');
  await contains(output(page), 'Final Core verdicts: Unknown 5');
  const applies = h.requests.filter(r => r.postData()?.includes('"phase":"apply"'));
  assert.equal(applies.length, 1);
  assert.deepEqual(h.errors, []);
});

test('Resume All selects paused scoped candidates', async t => {
  const data = value(3, true);
  data.issues.items[1].pause_label.present = false;
  data.issues.items[2].progress = [];
  const foreign = issue(99, {id: 9, name: 'other/repo',
    url: 'https://github.com/other/repo'});
  foreign.pause_label.present = true;
  data.issues.items.push(foreign);
  const h = await open(t, data, 'labels');
  await h.load();
  await button(h.page, 'Resume All').click();
  await dialog(h.page).waitFor();
  assert.equal(await dialog(h.page).locator('li').count(), 1);
  assert.ok((await dialog(h.page).textContent()).includes('#1 — Issue 1'));
  await button(h.page, 'Confirm request').click();
  await contains(output(h.page), '#1: Requested; observed label absent');
  assert.deepEqual(h.errors, []);
});

test('keyboard/mobile confirmation and stale observations fail closed', async t => {
  const data = value();
  const h = await open(t, data, 'labels');
  const {page} = h;
  await page.setViewportSize({width: 375, height: 700});
  await h.load();
  await selectFirst(page);
  await button(page, 'Pause').focus();
  await page.keyboard.press('Enter');
  await dialog(page).waitFor();
  const focused = await button(page, 'Cancel').evaluate(
    el => el === document.activeElement);
  assert.equal(focused, true);
  await page.keyboard.press('Escape');
  assert.equal(await dialog(page).isVisible(), false);
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <=
    window.innerWidth), true);
  await page.route('**/api/snapshot', route => route.fulfill({status: 503,
    contentType: 'application/json', body: '{"error":"unavailable"}'}));
  await button(page, 'Read snapshot').click();
  await page.waitForFunction(() => document.querySelector('#snapshot').textContent
    .includes('Snapshot unavailable'));
  assert.equal(await button(page, 'Pause').isDisabled(), true);
  assert.equal(await button(page, 'Pause All').isDisabled(), true);
  assert.deepEqual(h.errors, []);
});
