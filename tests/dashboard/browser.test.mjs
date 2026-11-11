import {test, before, after} from 'node:test';
import assert from 'node:assert/strict';
import {spawn, execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {mkdtemp, writeFile, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
const {chromium} = await import(process.env.NIBFIN_BROWSER_MODULE || 'playwright');
import {fixture} from './fixture.mjs';
let browser, service, scratch, bootstrap, origin, proof;
const now = Date.now();
const data = fixture(now);
before(async () => {
  scratch = await mkdtemp(join(tmpdir(), 'nibfin-browser-'));
  const path = join(scratch, 'snapshot.json');
  await writeFile(path, JSON.stringify(data));
  const binary = join(scratch, process.platform === 'win32' ?
    'service.test.exe' : 'service.test');
  await promisify(execFile)('go', ['test', '-c', '-o', binary,
    './internal/dashboard'], {timeout: 60000});
  service = spawn(binary, ['-test.run=^TestBrowserService$', '-test.v',
    '-test.timeout=3m'], {env: {...process.env,
    NIBFIN_BROWSER_FIXTURE: path}, stdio: ['pipe', 'pipe', 'pipe']});
  bootstrap = await new Promise((resolve, reject) => {
    let output = '';
    const timer = setTimeout(() => reject(Error('Service startup timeout')), 60000);
    service.stdout.on('data', chunk => {
      output += chunk;
      const match = output.match(/BROWSER_HANDOFF (http:\/\/[^\s]+)/);
      if (match) {clearTimeout(timer); resolve(match[1]);}
    });
    service.once('exit', code => {clearTimeout(timer);
      reject(Error(`Service exited before handoff: ${code}`));});
  });
  origin = new URL(bootstrap).origin;
  proof = new URL(bootstrap).hash.slice(1);
  browser = await chromium.launch({headless: true});
});
after(async () => {
  await browser?.close();
  if (service && service.exitCode === null) {
    service.stdin.end('\n');
    await new Promise((resolve, reject) => {
      const timer = setTimeout(() => {service.kill();
        reject(Error('Service shutdown timeout'));}, 10000);
      service.once('exit', code => {clearTimeout(timer);
        code === 0 ? resolve() : reject(Error(`Service failed: ${code}`));});
    });
  }
  if (scratch) await rm(scratch, {recursive: true, force: true});
});
async function pageFor(t, payload) {
  const context = await browser.newContext({viewport: {width: 375, height: 812}});
  const page = await context.newPage();
  page.setDefaultTimeout(5000);
  const errors = [], requests = [], logs = [];
  page.on('pageerror', e => errors.push(e.message));
  page.on('console', m => {logs.push(m.text());
    if (m.type() === 'error' && !m.text().includes('status of')) errors.push(m.text());});
  page.on('request', r => requests.push(r));
  if (payload) await page.route('**/api/snapshot', route => route.fulfill({
    contentType: 'application/json', body: JSON.stringify(payload)}));
  await page.clock.install({time: now});
  await page.goto(bootstrap);
  await page.waitForFunction(() => !document.getElementById('connect').disabled);
  assert.equal(new URL(page.url()).hash, '');
  assert.equal(requests.filter(r => r.url().endsWith('/api/snapshot')).length, 0);
  t.after(async () => {
    assert.deepEqual(errors, []);
    assert.ok(logs.every(log => !log.includes(proof)));
    const leaks = await page.evaluate(p => ({
      dom: document.documentElement.outerHTML.includes(p),
      storage: localStorage.length + sessionStorage.length, cookies: document.cookie
    }), proof);
    assert.deepEqual(leaks, {dom: false, storage: 0, cookies: ''});
    for (const request of requests) {
      assert.equal(new URL(request.url()).origin, origin);
      assert.equal(request.method(), 'GET');
      const path = new URL(request.url()).pathname;
      assert.ok(['/', '/session.js', '/app.js', '/model.js', '/view.js',
        '/style.css', '/graph.js', '/api/snapshot'].includes(path));
      assert.ok(!request.url().includes(proof));
      const header = await request.headerValue('x-nibfin-session');
      assert.ok(path === '/api/snapshot' ? header === proof : header === null);
    }
    await context.close();
  });
  return {page, requests};
}
async function load(page) {
  await page.getByRole('button', {name: 'Read snapshot', exact: true}).click();
  await page.getByRole('status').filter({hasText: 'Snapshot loaded'}).waitFor();
}
const details = page => page.locator('#details');
async function contains(locator, value) {
  assert.ok((await locator.textContent()).includes(value), value);
}
test('actual service: fidelity, safety, mobile, keyboard and 150 nodes', async t => {
  const {page, requests} = await pageFor(t);
  await load(page);
  assert.equal(await page.locator('#nodes > li').count(), 150);
  const nodes = await page.locator('#nodes > li').evaluateAll(items => items.map(e =>
    ({identity: e.querySelector('button').dataset.identity,
      edges: [...e.querySelectorAll('p')].filter(p => p.textContent.startsWith('→'))
        .map(p => p.textContent)})));
  assert.equal(new Set(nodes.map(n => n.identity)).size, 150);
  assert.equal(await page.locator('.graph-node.collected').count(), 150);
  const visualEdges = await page.locator('.dependency-edge').evaluateAll(edges =>
    edges.map(e => [JSON.parse(e.dataset.from), JSON.parse(e.dataset.to)]));
  const identityParts = i => [i.repository.name, i.repository.id, i.id, i.number];
  assert.deepEqual(visualEdges, data.issues.items.flatMap(i =>
    (i.blocked_by.items || []).map(e =>
      [identityParts(e.from), identityParts(e.to.identity)])));
  for (let n = 0; n < nodes.length; n++) {
    const issue = data.issues.items[n];
    assert.deepEqual(JSON.parse(nodes[n].identity), [
      issue.identity.repository.name, issue.identity.repository.id,
      issue.identity.id, issue.identity.number]);
    assert.deepEqual(nodes[n].edges.map(e => e.split(';')[0]),
      (issue.blocked_by.items || []).map(e =>
        '→ blocked by ' + e.to.identity.repository.name + '#' + e.to.identity.number));
  }
  await contains(page.locator('#repository'), 'Core completion: Unknown / unverified');
  await contains(page.locator('#repository'), 'success');
  await contains(page.locator('#repository'), 'Controls, requested/applied');
  const first = page.locator('#nodes button').first();
  await first.focus();
  await page.keyboard.press('Enter');
  await contains(details(page), data.issues.items[0].title.replace('\u0000', '\\u0000'));
  await contains(details(page), 'Assignees: <svg');
  await contains(details(page), 'Original Issue: link unavailable');
  assert.equal(await first.getAttribute('aria-pressed'), 'true');
  assert.equal(await page.locator('#nodes img, #nodes script, #details svg').count(), 0);
  assert.equal(await page.evaluate(() => window.pwned), undefined);
  await details(page).getByRole('button', {name: 'Inspect blocker other/project#1',
    exact: true}).click();
  await contains(details(page), 'External or unresolved reference other/project#1');
  assert.equal(await page.locator('#nodes > li').count(), 150);
  await first.click();
  await details(page).getByRole('button', {name: 'Inspect blocker other/project#3',
    exact: true}).click();
  await contains(details(page), 'denied; incomplete; permission_denied');
  await page.getByLabel('Search', {exact: true}).fill('Issue 150');
  assert.equal(await page.locator('#nodes > li').count(), 1);
  await contains(page.locator('#nodes'), 'hidden node or reference');
  await page.locator('#nodes button').focus();
  await page.keyboard.press('Enter');
  await contains(details(page), 'example/project#150');
  await contains(details(page), 'Original Issue: link unavailable');
  await details(page).getByRole('button',
    {name: 'Inspect blocker example/project#2'}).click();
  await contains(details(page), 'example/project#2 Issue 2');
  await contains(details(page), 'example/project#2 → example/project#1');
  await page.getByRole('button', {name: 'Clear filters'}).click();
  assert.equal(await page.locator('#nodes > li').count(), 150);
  await page.getByLabel('GitHub state').selectOption('closed');
  assert.equal(await page.locator('#nodes > li').count(), 1);
  await page.getByLabel('Native progress').selectOption('shaped');
  assert.equal(await page.locator('#nodes > li').count(), 1);
  await page.getByRole('button', {name: 'Clear filters'}).click();
  await page.getByLabel('Search', {exact: true}).focus();
  await page.keyboard.press('Tab');
  assert.equal(await page.evaluate(() => document.activeElement.id), 'state');
  assert.notEqual(await page.locator('#state').evaluate(e =>
    getComputedStyle(e).outlineStyle), 'none');
  await page.keyboard.press('Tab');
  assert.equal(await page.evaluate(() => document.activeElement.id), 'progress');
  await page.keyboard.press('Tab');
  assert.equal(await page.evaluate(() => document.activeElement.id), 'reset');
  assert.ok(await page.evaluate(() =>
    document.documentElement.scrollWidth <= innerWidth));
  assert.equal(requests.filter(r => r.url().endsWith('/api/snapshot')).length, 1);
  for (const a of await page.locator('a').all()) {
    assert.ok((await a.getAttribute('href')).startsWith('https://github.com/'));
    assert.equal(await a.getAttribute('rel'), 'noopener noreferrer');
  }
});
test('all statuses expire in place, including checks and denied endpoints', async t => {
  const {page, requests} = await pageFor(t);
  await load(page);
  await page.locator('#nodes button').first().click();
  await page.clock.fastForward(300000);
  await contains(page.locator('#freshness'), 'Stale');
  await contains(page.locator('#repository'), 'unknown; incomplete; stale_observation');
  await contains(details(page), 'denied; incomplete; permission_denied');
  const content = await page.locator('main').textContent();
  assert.ok(!content.includes('known; complete;'));
  assert.equal(requests.filter(r => r.url().endsWith('/api/snapshot')).length, 1);
});
test('partial, null, empty and independently unavailable dependencies', async t => {
  const payload = structuredClone(data);
  payload.issues.status.complete = false;
  payload.issues.status.reason = 'collection_limit';
  payload.pull_requests.items = [];
  payload.runs.items = null;
  payload.runs.status = {...payload.runs.status, availability: 'denied',
    complete: false, reason: 'permission_denied', source: '/private/raw diagnostic'};
  const {page} = await pageFor(t, payload);
  await load(page);
  await contains(page.locator('#repository'), 'Known empty collection');
  await contains(page.locator('#repository'), 'Unavailable collection');
  assert.ok(!(await page.locator('main').textContent()).includes('/private/'));
  await contains(page.locator('#graph-status'), 'incomplete; collection_limit');
  await page.locator('#nodes button').nth(2).click();
  await contains(details(page), 'Unavailable collection');
  await page.locator('#nodes button').nth(3).click();
  await contains(details(page), 'collection_limit');
  await page.locator('#nodes button').nth(4).click();
  await contains(details(page), 'denied; incomplete');
});
test('safe failures and previous display on failed refresh', async t => {
  const {page} = await pageFor(t);
  await load(page);
  for (const response of [{status: 503, body: 'private diagnostic'},
    {body: '{'}, {body: '{"schema_version":2}'},
    {body: '{"schema_version":1}'}, {body: 'null'}]) {
    await page.route('**/api/snapshot', route => route.fulfill({
      contentType: 'application/json', ...response}));
    await page.getByRole('button', {name: 'Read snapshot', exact: true}).click();
    await page.locator('#snapshot').filter(
      {hasText: 'unavailable or unusable'}).waitFor();
    await contains(page.locator('#freshness'), 'Previous data');
    assert.equal(await page.locator('#nodes > li').count(), 150);
    assert.ok(!(await page.locator('body').textContent()).includes('private diagnostic'));
  }
});
test('stale at load and contradictory timestamps never claim completeness', async t => {
  const payload = structuredClone(data);
  payload.finished_at = 'invalid';
  const {page} = await pageFor(t, payload);
  await load(page);
  await contains(page.locator('#freshness'), 'Stale or invalid');
  assert.ok(!(await page.locator('main').textContent()).includes('known; complete;'));
});
test('missing and malformed bootstrap cannot collect', async () => {
  for (const hash of ['', '#invalid']) {
    const page = await browser.newPage();
    page.setDefaultTimeout(5000);
    try {
      let count = 0;
      page.on('request', r => {if (r.url().endsWith('/api/snapshot')) count++;});
      await page.goto(origin + '/' + hash);
      await page.waitForFunction(() => location.hash === '');
      assert.equal(await page.locator('#connect').isDisabled(), true);
      assert.equal(count, 0);
    } finally {await page.close();}
  }
});
test('resource links reject hostile and mismatched destinations', async t => {
  const payload = structuredClone(data);
  const candidates = ['javascript:alert(1)', 'data:text/html,attack',
    'https://evil.example/example/project/issues/1',
    'https://github.com.evil/example/project/issues/1',
    'https://user:secret@github.com/example/project/issues/1',
    'https://github.com/example/project/issues/999',
    'https://github.com/example/other/issues/1',
    'https://github.com/example/project/pull/1',
    'https://github.com/example/project/issues/1?token=secret',
    'https://github.com/example/project/issues/1#attack'];
  payload.issues.items.slice(0, candidates.length).forEach((issue, n) => {
    issue.identity.url = candidates[n];
    issue.title = `<svg onload="window.pwned=1">'"\u001b\u0007 ${n}`;
  });
  payload.repository.items[0].url = 'https://github.com@evil.example/example/project';
  payload.pull_requests.items[0].identity.url =
    'https://github.com/example/project/issues/10';
  payload.pull_requests.items[0].checks.items[0].url =
    'https://github.com/example/project/actions/jobs/999';
  payload.runs.items[0].url = 'https://github.com/example/other/actions/runs/20';
  const {page} = await pageFor(t, payload);
  await load(page);
  assert.equal(await page.locator('#repository a').count(), 0);
  for (let n = 0; n < candidates.length; n++) {
    await page.locator('#nodes button').nth(n).click();
    assert.equal(await details(page).locator('a').count(), 0);
    await contains(details(page), '\\u001b\\u0007');
    assert.equal(await page.evaluate(() => window.pwned), undefined);
  }
  await page.locator('#nodes button').nth(10).click();
  assert.equal(await details(page).locator('a').getAttribute('href'),
    'https://github.com/example/project/issues/11');
});
test('invalid windows and stale-at-load facts downgrade every nested status', async t => {
  const {page} = await pageFor(t);
  for (const times of [
    {expires_at: new Date(now - 1).toISOString()},
    {started_at: new Date(now + 1).toISOString()},
    {finished_at: new Date(now + 10000).toISOString()},
    {started_at: '0001-01-01T00:00:00Z'},
    {started_at: '2026-02-30T00:00:00Z'},
    {expires_at: 'unknown'},
    {expires_at: new Date(now + 900000).toISOString()}
  ]) {
    const payload = {...structuredClone(data), ...times};
    await page.route('**/api/snapshot', route => route.fulfill({
      contentType: 'application/json', body: JSON.stringify(payload)}));
    await load(page);
    await page.locator('#nodes button').first().click();
    await contains(page.locator('#freshness'), 'Stale or invalid');
    assert.ok(!(await page.locator('main').textContent()).includes('known; complete;'));
    await contains(details(page), 'denied; incomplete; permission_denied');
  }
});
test('unusable payloads fail safely before any successful graph', async t => {
  const {page} = await pageFor(t, {schema_version: 2});
  for (const payload of [{schema_version: 2}, {schema_version: 1},
    {...data, issues: {status: data.issues.status, items: null}},
    {...data, issues: {...data.issues, items: [data.issues.items[0],
      data.issues.items[0]]}}]) {
    await page.route('**/api/snapshot', route => route.fulfill({
      contentType: 'application/json', body: JSON.stringify(payload)}));
    await page.getByRole('button', {name: 'Read snapshot', exact: true}).click();
    await page.locator('#snapshot').filter(
      {hasText: 'unavailable or unusable'}).waitFor();
    assert.equal(await page.locator('#nodes > li').count(), 0);
    assert.equal(await page.locator('#freshness').textContent(), '');
  }
});
test('known empty graph differs from unavailable graph', async t => {
  const payload = structuredClone(data);
  payload.issues.items = [];
  const {page} = await pageFor(t, payload);
  await load(page);
  await contains(page.locator('#nodes'), 'Known empty Issue collection');
  payload.issues.items = null;
  payload.issues.status = {...payload.issues.status, availability: 'unknown',
    complete: false, reason: 'unavailable'};
  await page.route('**/api/snapshot', route => route.fulfill({
    contentType: 'application/json', body: JSON.stringify(payload)}));
  await load(page);
  await contains(page.locator('#nodes'), 'Issue collection unavailable');
  assert.ok(!(await page.locator('#nodes').textContent()).includes('Known empty'));
});
