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
  assert.deepEqual(await page.locator('.issue-counts dd').allTextContents(),
    ['3', '2', '1']);
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
  // The repository link and disclosures precede the graph controls.
  for (let i = 0; i < 6; i++) {
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
  const selectedPanel = await details(page).boundingBox();
  const issueList = await graph(page).boundingBox();
  assert.ok(selectedPanel.y < issueList.y);
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

// The full suite remains hosted; this case is also a focused development scenario.
test('overview live updates, uncertainty and responsive navigation', async t => {
  const {overviewFixture} = await import('./overview-fixtures.mjs');
  const fixture = overviewFixture();
  for (const value of fixture.overviews) {
    value.issues.items[0].title += '\u202e<img src=x onerror=alert(1)>';
  }
  const h = await open(t, fixture, 'overview');
  const {page} = h;
  await page.locator('.runner-card').first().waitFor();
  assert.equal(await page.locator('.runner-card').count(), 4);
  assert.equal(await page.locator('.busy-indicator').count(), 1);
  await contains(page.locator('.runner-card').first(), 'Issue: Unknown');
  await contains(page.locator('.runner-card').first(), 'Stage: Unknown');
  await contains(page.locator('#assigned-issues'), 'Execution unknown');
  const title = await page.locator('#assigned-issues button').textContent();
  assert.ok(title.includes('�') && !title.includes('\u202e'));
  assert.equal(await page.locator('#assigned-issues img').count(), 0);
  const api = h.requests.filter(request => request.url().includes('/api/'));
  assert.ok(api.some(request => request.url().endsWith('/api/updates')));
  const proof = new URL(h.url).hash.slice(1).split(':')[0];
  assert.ok(api.every(request => request.headers()['x-nibfin-session'] === proof));
  assert.ok(h.requests.every(request => !request.url().includes(proof)));
  assert.ok(!await page.locator('body').innerHTML().then(html => html.includes(proof)));
  await page.emulateMedia({reducedMotion: 'reduce'});
  assert.equal(await page.locator('.busy-indicator').evaluate(element =>
    getComputedStyle(element).animationName), 'none');
  for (const viewport of [{width: 1280, height: 900}, {width: 390, height: 844}]) {
    await page.setViewportSize(viewport);
    assert.ok(await page.evaluate(() =>
      document.documentElement.scrollWidth <= window.innerWidth));
    if (process.env.NIBFIN_OVERVIEW_SHOTS) {
      await page.screenshot({path: `${process.env.NIBFIN_OVERVIEW_SHOTS}/` +
        `candidate-fixture-${viewport.width}.png`, fullPage: true});
    }
  }
  await page.evaluate(() => {
    window.fixtureNow = Date.now;
    Date.now = () => window.fixtureNow() + 31000;
  });
  await page.waitForFunction(() =>
    document.querySelector('.runner-card').textContent.includes('stale observation'));
  assert.equal(await page.locator('.busy-indicator').count(), 0);
  await page.evaluate(() => { Date.now = window.fixtureNow; delete window.fixtureNow; });
  await page.waitForFunction(() => document.querySelector('.busy-indicator'));
  await page.locator('#overview-search').fill('Assigned');
  await page.getByRole('button', {name: /example\/project#1 — Assigned/}).click();
  await page.locator('#dashboard').waitFor({state: 'visible'});
  await contains(page.locator('#details'), 'A dependent');
  await page.getByRole('button', {name: 'Back to overview', exact: true}).click();
  assert.equal(await page.locator('#overview-search').inputValue(), 'Assigned');
  h.advance();
  await page.waitForFunction(() =>
    document.querySelectorAll('.busy-indicator').length === 0,
    null, {timeout: 15000});
  await contains(page.locator('.runner-card').first(), 'idle');
  assert.equal(await page.locator('#assigned-issues .selected').count(), 1);
  await page.evaluate(() => {
    window.fixtureOfflineEvents = 0;
    window.addEventListener('offline', () => window.fixtureOfflineEvents++, {once: true});
  });
  await h.context.setOffline(true);
  if (!await page.evaluate(() => window.fixtureOfflineEvents)) {
    // Protocol transport emulation can leave navigator/window state unchanged.
    console.log('Fixture: supplying an offline event omitted by transport emulation.');
    await page.evaluate(() => window.dispatchEvent(new Event('offline')));
  }
  await page.waitForFunction(() => document.getElementById('overview-connection')
    .textContent.includes('Disconnected'));
  await h.context.setOffline(false);
  await page.waitForFunction(() => document.getElementById('overview-connection')
    .textContent === 'Connected', null, {timeout: 20000});
  const streams = h.requests.filter(request => request.url().endsWith('/api/updates'));
  assert.ok(streams.length >= 2);
  assert.ok(streams.at(-1).headers()['last-event-id']);
  assert.equal(await page.locator('#assigned-issues article').count(), 1);
  assert.equal(await page.locator('#assigned-issues .selected').count(), 1);
  assert.equal(await page.locator('#overview-search').inputValue(), 'Assigned');
  assert.deepEqual(h.errors, []);
});

test('overview stream parses split resets and resumes with a header cursor', async () => {
  const {connectOverview} = await import(
    '../../internal/dashboard/assets/overview-stream.js');
  const calls = [];
  const received = [];
  const statuses = [];
  const encoder = new TextEncoder();
  const request = async (path, options) => {
    calls.push({path, headers: options.headers});
    if (calls.length > 1) return new Response('', {status: 401});
    const event = 'id: 7\nevent: reset\ndata: ' + JSON.stringify({
      id: 7, kind: 'reset', overview: {schema_version: 1}
    }) + '\n\n';
    return new Response(new ReadableStream({start(controller) {
      controller.enqueue(encoder.encode(event.slice(0, 15)));
      controller.enqueue(encoder.encode(event.slice(15)));
      controller.close();
    }}), {headers: {'Content-Type': 'text/event-stream'}});
  };
  await connectOverview(request, value => received.push(value),
    value => statuses.push(value), new AbortController().signal);
  assert.deepEqual(received, [{schema_version: 1}]);
  assert.equal(calls.length, 2);
  assert.equal(calls[1].headers['Last-Event-ID'], '7');
  assert.ok(calls.every(call => call.path === '/api/updates'));
  assert.match(statuses.at(-1), /expired or denied/);
});

test('overview known empty and denied coverage remain explicit', async t => {
  const {overviewFixture} = await import('./overview-fixtures.mjs');
  const fixture = overviewFixture();
  const empty = structuredClone(fixture.overviews[0]);
  empty.runners.items = [];
  empty.issues.items = [];
  const denied = structuredClone(empty);
  denied.local_status = 'denied';
  for (const section of [denied.runners, denied.issues, denied.repositories]) {
    section.status = status('denied', false, 'permission_denied');
  }
  fixture.overviews = [empty, denied];
  const h = await open(t, fixture, 'overview');
  await h.page.getByText('No local Runner registrations.', {exact: true}).waitFor();
  await contains(h.page.locator('#assigned-empty'), 'No assigned open Issues');
  h.advance();
  await h.page.waitForFunction(() => document.getElementById('runner-coverage')
    .textContent.includes('Access denied'), null, {timeout: 15000});
  await contains(h.page.locator('#runners'), 'unavailable or partial');
  await contains(h.page.locator('#assigned-empty'), 'inventory is incomplete');
  assert.equal(await h.page.locator('.busy-indicator').count(), 0);
  assert.deepEqual(h.errors, []);
});

test('selected Issue diagnosis preserves attempt and unknown outcomes', async t => {
  const {diagnosisFixture} = await import('./overview-fixtures.mjs');
  const h = await open(t, diagnosisFixture(), 'overview');
  const {page} = h;
  await page.getByRole('button', {name: /example\/project#1 — Assigned/}).click();
  const panel = page.locator('.issue-explanation');
  await contains(panel, 'A required input was unavailable.');
  await contains(panel, 'Historical report');
  await contains(panel, 'Attributed explanation');
  assert.equal(h.requests.filter(r => r.url().endsWith('/api/diagnosis')).length, 0);
  await panel.getByRole('combobox').selectOption(
    'accept:22222222-2222-2222-2222-222222222222');
  await panel.getByRole('button', {name: 'Request read-only diagnosis'}).click();
  await panel.getByRole('status').filter({hasText: 'no-capacity'}).waitFor();
  const submitted = h.requests.find(r => r.url().endsWith('/api/diagnosis'));
  const body = submitted.postDataJSON();
  assert.deepEqual(body.attempt,
    {id: '22222222-2222-2222-2222-222222222222', stage: 'accept'});
  assert.equal(body.selection.identity.repository.id, 1);
  assert.equal(body.selection.identity.id, 1001);
  assert.equal(body.selection.node_id, 'I_one');
  assert.equal(body.diagnose, true);
  assert.ok(!JSON.stringify(body).includes('token'));
  await contains(panel, 'Possible causes (hypotheses)');
  await contains(panel, 'The retained evidence is incomplete.');
  // Simulated denial checks presentation; native refusals are adapter-tested.
  await page.route('**/api/diagnosis', route => route.fulfill({status: 200,
    contentType: 'application/json', body: JSON.stringify({status: 'denied',
      code: 'DIAGNOSIS_ACCESS_DENIED'})}));
  await panel.getByRole('button', {name: 'Request read-only diagnosis'}).click();
  await panel.getByRole('status').filter({hasText: 'Diagnosis denied'}).waitFor();
  await page.unroute('**/api/diagnosis');
  await page.route('**/api/diagnosis', route => route.abort());
  await panel.getByRole('button', {name: 'Request read-only diagnosis'}).click();
  await panel.getByRole('status').filter({hasText: 'outcome unknown'}).waitFor();
  assert.equal(await panel.getByRole('button').isDisabled(), true);
  // Observation updates do not resubmit uncertain work.
  const count = h.requests.filter(r => r.url().endsWith('/api/diagnosis')).length;
  await page.getByRole('button', {name: 'Back to overview', exact: true}).click();
  h.advance();
  await page.waitForFunction(() =>
    document.querySelector('.runner-card').textContent.includes('idle'),
    null, {timeout: 15000});
  assert.equal(h.requests.filter(r => r.url().endsWith('/api/diagnosis')).length, count);
  await page.getByRole('button', {name: /example\/project#1 — Assigned/}).click();
  await panel.getByRole('combobox').selectOption(
    'deliver:11111111-1111-1111-1111-111111111111');
  await panel.getByRole('combobox').selectOption(
    'accept:22222222-2222-2222-2222-222222222222');
  await contains(panel.getByRole('status'), 'outcome unknown');
  assert.equal(await panel.getByRole('button').isDisabled(), true);
  for (const viewport of [{width: 1280, height: 900}, {width: 390, height: 844}]) {
    await page.setViewportSize(viewport);
    assert.ok(await page.evaluate(() =>
      document.documentElement.scrollWidth <= window.innerWidth));
  }
  await panel.getByRole('combobox').selectOption(
    'deliver:11111111-1111-1111-1111-111111111111');
  await page.evaluate(() => {
    const nativeNow = Date.now;
    Date.now = () => nativeNow() + 31000;
  });
  await panel.getByText('Selection unavailable or stale. Refresh before acting.')
    .waitFor();
  assert.equal(await panel.getByRole('button').isDisabled(), true);
  assert.deepEqual(h.errors, []);
});

test('selected Issue diagnosis keeps Pause reviews bound across selection changes',
  async t => {
    const {diagnosisFixture} = await import('./overview-fixtures.mjs');
    const h = await open(t, diagnosisFixture(), 'overview');
    const {page} = h;
    await page.getByRole('button', {name: /example\/project#1 — Assigned/}).click();
    await details(page).getByRole('button', {name: 'Pause', exact: true}).click();
    const dialog = page.getByRole('dialog');
    await dialog.waitFor({state: 'visible'});
    const review = h.requests.find(r => r.url().endsWith('/api/labels'));
    assert.deepEqual(review.postDataJSON().targets,
      [{repository_id: 1, issue_id: 1001, number: 1}]);
    await page.evaluate(async identity => {
      const view = await import('/view.js');
      view.selectIssue(identity);
    }, identity(1, {id: 2, name: 'other/project',
      url: 'https://github.com/other/project'}));
    await dialog.waitFor({state: 'hidden'});
    await contains(details(page).locator('.issue-explanation'),
      'Blocker report unavailable.');
    assert.equal(await details(page).getByRole('button',
      {name: 'Pause', exact: true}).isDisabled(), true);
    assert.equal(h.requests.filter(r => r.url().endsWith('/api/labels')).length, 1);
    assert.deepEqual(h.errors, []);
  });

test('project picker opens distinct empty projects and restores safe view state',
  async t => {
  const {overviewFixture} = await import('./overview-fixtures.mjs');
  const h = await open(t, overviewFixture(), 'overview');
  const {page} = h;
  await page.locator('#project-picker option').first().waitFor({state: 'attached'});
  const picker = page.getByLabel('Adopted project', {exact: true});
  assert.equal(await picker.locator('option').count(), 2);
  await page.locator('#overview-search').fill('Assigned');
  await picker.selectOption({label: 'example/project'});
  await page.getByRole('button', {name: 'Open project', exact: true}).click();
  await page.locator('#dashboard').waitFor({state: 'visible'});
  await contains(page.locator('#repository'), 'example/project');
  await selectA(page);
  await page.locator('#search').fill('dependent');
  await page.getByRole('button', {name: 'Back to overview', exact: true}).click();
  assert.equal(await page.locator('#overview-search').inputValue(), 'Assigned');
  await picker.selectOption({label: 'example/empty'});
  await page.setViewportSize({width: 390, height: 844});
  await page.getByRole('button', {name: 'Open project', exact: true}).click();
  await page.locator('#dashboard').waitFor({state: 'visible'});
  await contains(page.locator('#repository'), 'example/empty');
  assert.equal(await page.locator('#repository-heading').evaluate(element =>
    element === document.activeElement), true);
  await contains(page.locator('#issue-status'), 'Known empty Issue collection');
  assert.equal(await page.locator('#search').inputValue(), '');
  assert.equal(await page.locator('#details .label-action').count(), 0);
  h.advance();
  await page.waitForFunction(() => !document.querySelector('.busy-indicator'),
    null, {timeout: 15000});
  assert.ok(await page.evaluate(() =>
    document.documentElement.scrollWidth <= window.innerWidth));
  await page.getByRole('button', {name: 'Back to overview', exact: true}).click();
  await picker.selectOption({label: 'example/project'});
  await page.getByRole('button', {name: 'Open project', exact: true}).click();
  await page.locator('#dashboard').waitFor({state: 'visible'});
  await contains(page.locator('#repository'), 'example/project');
  assert.equal(await page.locator('#search').inputValue(), 'dependent');
  await contains(page.locator('#details'), 'A dependent');
  assert.deepEqual(h.errors, []);
});

test('superseded project reads and mismatched responses cannot replace navigation',
  async t => {
    const {overviewFixture} = await import('./overview-fixtures.mjs');
    const h = await open(t, overviewFixture(), 'overview');
    const {page} = h;
    let release;
    let observed;
    let delivered;
    const finished = new Promise(resolve => { delivered = resolve; });
    const gate = new Promise(resolve => { release = resolve; });
    const started = new Promise(resolve => { observed = resolve; });
    let first = true;
    await page.route('**/api/snapshot', async route => {
      if (!first) return route.continue();
      first = false;
      const response = await route.fetch();
      observed();
      await gate;
      await route.fulfill({response});
      delivered();
    });
    t.after(() => release());
    await page.getByRole('button', {name: 'Open project', exact: true}).click();
    await started;
    await page.getByRole('button', {name: 'Back to overview', exact: true}).click();
    await page.getByLabel('Adopted project', {exact: true})
      .selectOption({label: 'example/empty'});
    await page.getByRole('button', {name: 'Open project', exact: true}).click();
    await page.locator('#dashboard').waitFor({state: 'visible'});
    await contains(page.locator('#repository'), 'example/empty');
    release();
    await finished;
    await page.evaluate(() => new Promise(resolve =>
      requestAnimationFrame(() => requestAnimationFrame(resolve))));
    await contains(page.locator('#repository'), 'example/empty');
    await page.unroute('**/api/snapshot');
    await page.route('**/api/snapshot', async route => {
      const response = await route.fetch();
      const value = await response.json();
      value.repository.items[0].id = 999;
      await route.fulfill({json: value});
    });
    await page.getByRole('button', {name: 'Read snapshot', exact: true}).click();
    await page.locator('#user-overview').waitFor({state: 'visible'});
    assert.equal(await page.locator('#dashboard').isVisible(), false);
    assert.equal(await page.locator('#open-project').isEnabled(), true);
    assert.deepEqual(h.errors, []);
  });


test('project inventory distinguishes empty, partial, stale and retained observations',
  async t => {
    const {overviewFixture} = await import('./overview-fixtures.mjs');
    const fixture = overviewFixture();
    const empty = structuredClone(fixture.overviews[0]);
    empty.repositories.items = [];
    empty.issues.items = [];
    const partial = structuredClone(empty);
    partial.repositories = section([fixture.overviews[0].repositories.items[1]]);
    partial.repositories.status.complete = false;
    const unavailable = structuredClone(partial);
    unavailable.repositories.items = [];
    unavailable.repositories.status = status('unknown', false, 'unavailable');
    fixture.overviews = [empty, partial, unavailable];
    const h = await open(t, fixture, 'overview');
    const {page} = h;
    await page.waitForFunction(() => document.getElementById('project-coverage')
      .textContent.includes('No adopted projects'));
    assert.equal(await page.locator('#open-project').isEnabled(), false);
    h.advance();
    await page.waitForFunction(() => document.getElementById('project-coverage')
      .textContent.includes('Partial coverage'), null, {timeout: 15000});
    assert.equal(await page.locator('#open-project').isEnabled(), true);
    await page.evaluate(() => {
      window.fixtureNow = Date.now;
      Date.now = () => window.fixtureNow() + 31000;
    });
    await page.waitForFunction(() => document.getElementById('project-coverage')
      .textContent.includes('Stale project inventory'));
    assert.equal(await page.locator('#open-project').isEnabled(), false);
    await page.evaluate(() => { Date.now = window.fixtureNow; });
    h.advance();
    await page.waitForFunction(() => document.getElementById('project-coverage')
      .textContent.includes('Retained projects'), null, {timeout: 15000});
    assert.equal(await page.locator('#open-project').isEnabled(), false);
    assert.match(await page.locator('#project-picker option').textContent(), /retained/);
    assert.deepEqual(h.errors, []);
  });
