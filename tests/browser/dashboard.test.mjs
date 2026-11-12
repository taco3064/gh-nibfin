import test from 'node:test';
import assert from 'node:assert/strict';
import {open} from './host.mjs';
import {snapshot, issue, identity, section, status, repository} from './fixtures.mjs';

const button = (page, name) => page.getByRole('button', {name, exact: true});
const graph = page => page.locator('#graph');
const details = page => page.locator('#details');
const contains = async (locator, value) => {
  const content = await locator.textContent();
  assert.ok(content.includes(value), `Missing: ${value} in ${content}`);
};
const selectA = page => graph(page).getByRole('button',
  {name: 'example/project#1 — A dependent', exact: true}).click();

// Real default packaged service. No application routes are intercepted in this test.
test('deliberate session handoff, graph direction, identities and activity', async t => {
  const h = await open(t, snapshot());
  const {page, requests} = h;
  assert.equal(new URL(page.url()).hash, '');
  assert.equal(requests.filter(r => r.url().includes('/api/')).length, 0);
  await page.evaluate(() => {
    window.transport = [];
    const native = window.fetch;
    window.fetch = (url, options) => {
      window.transport.push({url, options, hash: location.hash});
      return native(url, options);
    };
  });
  await h.load();
  const request = requests.find(r => r.url().endsWith('/api/snapshot'));
  assert.equal(request.method(), 'GET');
  assert.equal(request.headers()['x-nibfin-session'], new URL(h.url).hash.slice(1));
  const transport = await page.evaluate(() => window.transport);
  assert.equal(transport.length, 1);
  assert.equal(transport[0].hash, '');
  assert.equal(transport[0].options.credentials, 'omit');
  assert.equal(transport[0].options.redirect, 'error');
  assert.equal(transport[0].options.cache, 'no-store');
  const proof = new URL(h.url).hash.slice(1);
  assert.ok(!(await page.locator('body').innerHTML()).includes(proof));
  assert.ok(requests.every(r => !r.url().includes(proof)));
  assert.deepEqual(await page.evaluate(() => [Object.keys(localStorage),
    Object.keys(sessionStorage), document.cookie]), [[], [], '']);
  assert.deepEqual(await h.context.cookies(), []);
  assert.equal(await page.evaluate(async () =>
    (await indexedDB.databases()).length), 0);
  await contains(page.locator('#repository'), 'Observed Issues: 3; open: 2; closed: 1');
  await contains(graph(page), 'example/project#1 → example/project#2');
  await contains(graph(page), 'example/project#1 → other/project#1');
  await contains(graph(page), 'other/project#8 — external reference (not collected)');
  await contains(graph(page), 'other/project#9 — unresolved reference; Denied');
  assert.equal(await graph(page).locator('.dependent button').count(), 3);
  await selectA(page);
  await contains(details(page), 'Assignees: reader');
  assert.equal(await details(page).getByRole('link').getAttribute('href'),
    `${repository.url}/issues/1`);
  await page.getByText('Textual blocker view', {exact: true}).click();
  await contains(page.locator('#relations'), 'example/project#1 → example/project#2');
  await page.getByText('Repository activity (not Issue associations)',
    {exact: true}).click();
  await contains(page.locator('#activity'), 'conclusion: Unknown');
  await contains(page.locator('#activity'), 'conclusion: success');
  await contains(page.locator('#activity'), 'Unknown, incomplete (collection limit)');
  await contains(page.locator('#activity'), 'Native PR reference: example/project#5');
  assert.equal(await page.getByRole('link', {name: 'PR check', exact: true})
    .getAttribute('href'), `${repository.url}/pull/5/checks?check_run_id=32`);
  assert.equal(await page.getByRole('link', {name: 'Actions job', exact: true})
    .getAttribute('href'), `${repository.url}/actions/runs/10/job/33`);
  assert.deepEqual(h.errors, []);
});

test('filters preserve hidden blocker context and selection', async t => {
  const h = await open(t, snapshot());
  const {page} = h;
  await h.load();
  await page.getByLabel('Find identity or title').fill('A dependent');
  await contains(page.locator('#issue-status'), '1 of 3');
  await contains(graph(page), 'example/project#2 — collected Issue hidden by filters');
  await graph(page).getByRole('button', {name: 'example/project#2 — B blocker',
    exact: true}).click();
  await contains(details(page), 'GitHub state: closed');
  await contains(details(page), 'Native progress: delivered');
  await contains(details(page), 'Completion evidenceUnknown, unverified');
  await contains(details(page), 'Pause evidenceUnknown, unverified');
  await contains(details(page), 'Execution evidenceUnknown, unverified');
  await button(page, 'Clear filters').click();
  await contains(page.locator('#issue-status'), '3 of 3');
  await page.getByLabel('State', {exact: true}).selectOption('closed');
  await contains(page.locator('#issue-status'), '1 of 3');
  await page.getByLabel('Native progress').selectOption('scope');
  await contains(page.locator('#issue-status'), 'No Issues match these filters.');
  await button(page, 'Clear filters').click();
  await page.getByLabel('Find identity or title').fill('other/project#1');
  await graph(page).getByRole('button').click();
  await contains(details(page), 'other/project#1');
  assert.equal(await details(page).getByRole('link').getAttribute('href'),
    'https://github.com/other/project/issues/1');
});

for (const kind of ['empty', 'partial', 'denied', 'offline', 'independent']) {
  test(`honest ${kind} section observations`, async t => {
    const value = snapshot();
    if (kind === 'empty') value.issues = section([]);
    if (kind === 'partial') {
      value.issues.status = status('unknown', false, 'collection_limit');
      value.issues.items[0].blocked_by = section(null,
        status('denied', false, 'permission_denied'));
    }
    if (kind === 'denied' || kind === 'offline') {
      const observation = kind === 'denied'
        ? status('denied', false, 'permission_denied')
        : status('unknown', false, 'unavailable');
      for (const name of ['repository', 'issues', 'pull_requests', 'runs']) {
        value[name] = section(null, observation);
      }
    }
    if (kind === 'independent') {
      value.issues.items[0].blocked_by.status =
        status('unknown', false, 'collection_limit');
      value.pull_requests = section(null, status('denied', false, 'permission_denied'));
      value.runs = section(null, status('unknown', false, 'unavailable'));
    }
    const h = await open(t, value);
    await h.load();
    const {page} = h;
    if (kind === 'empty') {
      await contains(page.locator('#issue-status'), 'Known empty Issue collection.');
    } else if (kind === 'denied' || kind === 'offline') {
      await contains(page.locator('#issue-status'), 'unavailable; not known empty');
      await contains(page.locator('#repository'), 'identity unavailable');
      await contains(page.locator('#issue-status'),
        kind === 'denied' ? 'Denied, incomplete' : 'Unknown, incomplete');
    } else {
      await contains(page.locator('#issue-status'), '3 of 3');
      await selectA(page);
      await contains(details(page), kind === 'partial'
        ? 'Blockers unavailable; not known empty.' : 'Unknown, incomplete');
    }
    if (kind === 'independent') {
      await page.getByText('Repository activity (not Issue associations)',
        {exact: true}).click();
      await contains(page.locator('#activity'), 'Denied, incomplete');
      await contains(page.locator('#activity'), 'Unknown, incomplete (unavailable)');
    }
    assert.deepEqual(h.errors, []);
  });
}

test('retained observations visibly expire without another fetch', async t => {
  const value = snapshot();
  const h = await open(t, value);
  const {page, requests} = h;
  await page.clock.install();
  await h.load();
  await contains(page.locator('#freshness'), 'Current observation window');
  await page.clock.setSystemTime(new Date(Date.parse(value.expires_at) + 1000));
  await page.clock.runFor(1000);
  await contains(page.locator('#freshness'), 'Stale observations');
  await contains(graph(page), 'A dependent');
  assert.equal(requests.filter(r => r.url().endsWith('/api/snapshot')).length, 1);
});

test('already expired observations stay visibly stale', async t => {
  const value = snapshot();
  value.expires_at = new Date(Date.now() - 1000).toISOString();
  const h = await open(t, value);
  await h.load();
  await contains(h.page.locator('#freshness'), 'Stale observations');
});

test('unsupported schema is safely rejected', async t => {
  const value = snapshot();
  value.schema_version = 2;
  const h = await open(t, value);
  await button(h.page, 'Read snapshot').click();
  await h.page.getByText(/Snapshot unavailable or unsupported/).waitFor();
  assert.equal(await h.page.locator('#dashboard').isVisible(), false);
});

test('additive response fields are ignored by the packaged client', async t => {
  const h = await open(t, snapshot());
  // Extend the real response to model a future additive server field.
  await h.page.route('**/api/snapshot', async route => {
    const response = await route.fetch();
    const value = await response.json();
    value.additive_unknown = {note: 'ignored additive content'};
    value.issues.items[0].additive_unknown = '<script>window.pwned=1</script>';
    await route.fulfill({response, json: value});
  });
  await h.load();
  await contains(graph(h.page), 'A dependent');
  assert.ok(!(await h.page.locator('body').textContent())
    .includes('ignored additive content'));
  assert.equal(await h.page.evaluate(() => window.pwned), undefined);
});

for (const mode of ['missing', 'expired']) {
  test(`safe actionable state for ${mode} proof`, async t => {
    const h = await open(t, snapshot(), mode);
    const read = button(h.page, 'Read snapshot');
    if (mode === 'missing') {
      assert.equal(await read.isDisabled(), true);
      await contains(h.page.locator('#snapshot'), 'current local session link');
    } else {
      await read.click();
      await h.page.getByText(/Snapshot unavailable or unsupported/).waitFor();
      assert.equal(await h.page.locator('#dashboard').isVisible(), false);
    }
  });
}

test('HTTP failure is safe and invalidates a retained observation', async t => {
  const h = await open(t, snapshot());
  await h.load();
  // Deliberately intercepted HTTP failure tests client error handling only.
  await h.page.route('**/api/snapshot', route => route.fulfill({status: 503,
    body: 'secret_token /private/host raw agent log'}));
  await button(h.page, 'Read snapshot').click();
  await h.page.getByText(/Snapshot unavailable or unsupported/).waitFor();
  await contains(h.page.locator('#freshness'), 'retained observations are not current');
  assert.ok(!(await h.page.locator('body').textContent()).includes('secret_token'));
});

test('more than 100 Issues are reachable with keyboard at mobile width', async t => {
  const value = snapshot();
  value.issues = section(Array.from({length: 130}, (_, i) => issue(i + 1)));
  const h = await open(t, value);
  const {page} = h;
  await page.setViewportSize({width: 360, height: 740});
  await page.keyboard.press('Tab');
  assert.equal(await button(page, 'Read snapshot').evaluate(el =>
    el === document.activeElement), true);
  assert.notEqual(await button(page, 'Read snapshot').evaluate(el =>
    getComputedStyle(el).outlineStyle), 'none');
  await page.keyboard.press('Enter');
  await page.locator('#dashboard').waitFor({state: 'visible'});
  await contains(page.locator('#issue-status'), '130 of 130');
  await page.keyboard.press('Tab');
  assert.equal(await page.getByLabel('Find identity or title').evaluate(el =>
    el === document.activeElement), false);
  // The repository link and activity disclosure precede the graph controls.
  for (let i = 0; i < 5; i++) {
    const focused = await page.locator('#search').evaluate(el =>
      el === document.activeElement);
    if (focused) break;
    await page.keyboard.press('Tab');
  }
  assert.equal(await page.locator('#search').evaluate(el =>
    el === document.activeElement), true);
  await page.keyboard.type('example/project#130');
  await contains(page.locator('#issue-status'), '1 of 130');
  await page.keyboard.press('Tab');
  await page.keyboard.press('Tab');
  await page.keyboard.press('Tab');
  await page.keyboard.press('Tab');
  await page.keyboard.press('Enter');
  await contains(details(page), 'example/project#130');
  assert.equal(await details(page).evaluate(el => el === document.activeElement), true);
  await page.keyboard.press('Tab');
  assert.equal(await details(page).getByRole('link').evaluate(el =>
    el === document.activeElement), true);
  assert.equal(await page.evaluate(() =>
    document.documentElement.scrollWidth <= innerWidth), true);
  await button(page, 'Clear filters').click();
  assert.equal(await graph(page).locator('.dependent button').count(), 130);
});

test('hostile text is inert and resource links are validated exactly', async t => {
  const value = snapshot();
  const payload = '<img src="https://evil.example/x" onerror="window.pwned=1">';
  value.issues.items[0].title = `${payload} <script>window.pwned=2</script> "\u0001`;
  const bad = ['javascript:alert(1)', 'data:text/html,unsafe',
    'https://github.com.evil.example/example/project/issues/6',
    'https://user:pass@github.com/example/project/issues/7',
    'https://github.com/example/project/issues/99',
    'https://github.com/example/project/pull/9',
    'https://github.com/example/project/issues/10?token=secret',
    'https://github.com/example/project/issues/11#unsafe'];
  value.issues.items = bad.map((url, i) => {
    const entry = issue(i + 4);
    entry.identity.url = url;
    return entry;
  });
  value.issues.items.unshift(issue(1));
  value.issues.items[0].title = `${payload} <script>window.pwned=2</script> "\u0001`;
  value.pull_requests.items[0].checks.items[0].url = 'javascript:alert(1)';
  value.runs.items[0].url = 'https://github.com/other/project/actions/runs/10';
  const h = await open(t, value);
  await h.load();
  const {page} = h;
  await contains(graph(page), payload);
  await contains(graph(page), '�');
  assert.equal(await page.locator('img, iframe, object').count(), 0);
  assert.equal(await page.evaluate(() => window.pwned), undefined);
  for (let i = 0; i < bad.length; i++) {
    await graph(page).getByRole('button',
      {name: `example/project#${i + 4} — Issue ${i + 4}`, exact: true}).click();
    assert.equal(await details(page).getByRole('link').count(), 0);
    await contains(details(page), 'link unavailable');
  }
  const anchors = await page.locator('a').evaluateAll(elements => elements.map(el =>
    ({href: el.href, rel: el.rel, policy: el.referrerPolicy, target: el.target})));
  for (const anchor of anchors) {
    assert.ok(anchor.href.startsWith('https://github.com/example/project'));
    assert.equal(anchor.rel, 'noopener noreferrer');
    assert.equal(anchor.policy, 'no-referrer');
    assert.equal(anchor.target, '_blank');
  }
  assert.ok(h.requests.every(r => new URL(r.url()).origin === h.origin));
  assert.deepEqual(h.errors, []);
});
