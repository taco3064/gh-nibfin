import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import {once} from 'node:events';
import {fixture} from './fixtures.mjs';

export async function verify(chromium, binary) {
  const browser = await chromium.launch({headless: true});
  let passed = 0;
  async function scenario(name, value, test) {
    const host = spawn(binary, [], {stdio: ['pipe', 'pipe', 'pipe']});
    let diagnostics = '';
    host.stderr.on('data', chunk => { diagnostics += chunk; });
    const exited = once(host, 'exit');
    const urlPromise = new Promise((resolve, reject) => {
      let line = '';
      const timeout = setTimeout(() => reject(new Error('Host startup timeout')), 10000);
      host.stdout.on('data', chunk => {
        line += chunk;
        if (line.includes('\n')) {
          clearTimeout(timeout);
          resolve(line.trim());
        }
      });
      host.on('error', reject);
    });
    host.stdin.write(JSON.stringify(value) + '\n');
    const context = await browser.newContext();
    const page = await context.newPage();
    page.setDefaultTimeout(5000);
    const errors = [];
    const logs = [];
    const requests = [];
    page.on('pageerror', error => errors.push(error.message));
    page.on('console', message => logs.push(message.text()));
    page.on('request', request => requests.push(request));
    let proof;
    try {
      const url = await urlPromise;
      proof = url.split('#')[1];
      const origin = new URL(url).origin;
      await page.clock.install({time: new Date(value.finished_at)});
      await page.goto(url);
      await page.waitForFunction(() => !document.getElementById('connect').disabled);
      assert.equal(await page.evaluate(() => location.hash), '');
      assert.equal(requests.filter(r => r.url().includes('/api/')).length, 0);
      await test({page, context, origin, requests, url, proof});
      assert.deepEqual(errors, [], name);
      for (const request of requests) {
        const parsed = new URL(request.url());
        assert.equal(parsed.origin, origin, 'No third-party requests');
        assert.equal(request.method(), 'GET');
        assert.equal(parsed.search, '');
        assert.ok(['/', '/session.js', '/model.js', '/view.js', '/graph.js',
          '/style.css', '/api/snapshot'].includes(parsed.pathname));
        const header = await request.headerValue('X-Nibfin-Session');
        assert.equal(header, parsed.pathname === '/api/snapshot' ? proof : null);
        assert.ok(!request.url().includes(proof));
      }
      assert.ok(!(await page.content()).includes(proof));
      assert.ok(!logs.join('\n').includes(proof));
      const cspFailure = logs.some(line =>
        /Content Security Policy|Refused to execute/.test(line));
      assert.ok(!cspFailure,
        'Packaged assets must work under the production CSP');
      assert.ok(!diagnostics.includes(proof));
      assert.deepEqual(await page.evaluate(() => [localStorage.length,
        sessionStorage.length, document.cookie]), [0, 0, '']);
      passed++;
      console.log(`PASS ${name}`);
    } finally {
      await context.close();
      host.stdin.end();
      const timeout = setTimeout(() => host.kill('SIGKILL'), 10000);
      const [code] = await exited;
      clearTimeout(timeout);
      assert.equal(code, 0, 'Fixture service must shut down cleanly');
    }
  }
  async function load(page) {
    await page.getByRole('button', {name: 'Read snapshot', exact: true}).click();
    await page.getByRole('status').filter({hasText: 'Snapshot loaded.'})
      .waitFor({state: 'visible'});
  }
  const contains = async (locator, expected) => {
    assert.ok((await locator.innerText()).includes(expected), `Missing ${expected}`);
  };
  try {
    await scenario('native identities, cycles, filters and security', fixture(),
      async ({page, requests}) => {
        await load(page);
        assert.equal(await page.locator('.node').count(), 4);
        const panel = page.locator('#repository');
        await contains(panel, 'example/project');
        await contains(panel, 'success');
        await contains(panel, 'completion: Unknown / unverified');
        await contains(panel, 'pause: Unknown / unverified');
        await contains(panel, 'Control outcomes (requested/applied)');
        const edges = await page.locator('svg line').evaluateAll(nodes =>
          nodes.map(n => [JSON.parse(n.dataset.from)[3], JSON.parse(n.dataset.to)[3]]));
        assert.deepEqual(edges, [[1, 2], [2, 1], [3, 2]]);
        await page.locator('.node > button').first().click();
        await contains(page.locator('#details'), 'maintainer');
        assert.equal(await page.locator('.node > button').first()
          .getAttribute('aria-pressed'), 'true');
        await contains(page.locator('#details'), 'GitHub: closed');
        await contains(page.locator('#details'), 'delivered');
        assert.equal(await page.locator('#details li').count(), 3);
        await page.locator('#details li button').nth(1).click();
        await contains(page.locator('#details'), 'other/project#1');
        await contains(page.locator('#details'), 'Reference only');
        assert.equal(await page.locator('.node').count(), 4);
        await page.locator('.node > button').first().click();
        await page.locator('#query').fill('Issue 1');
        assert.equal(await page.locator('.node').count(), 1);
        await contains(page.locator('#details'), 'hidden by filters');
        await page.locator('#details li button').first().click();
        await contains(page.locator('#details'), 'example/project#2');
        await contains(page.locator('#details'), 'Selected Issue is hidden');
        await page.getByRole('button', {name: 'Clear filters'}).click();
        await page.locator('#state').selectOption('closed');
        assert.equal(await page.locator('.node').count(), 1);
        await page.locator('#state').selectOption('');
        await page.locator('#progress').selectOption('delivered');
        assert.equal(await page.locator('.node').count(), 1);
        await page.getByRole('button', {name: 'Clear filters'}).click();
        assert.equal(await page.locator('.node').count(), 4);
        assert.equal(requests.filter(r => r.url().includes('/api/')).length, 1);
      });
    const partial = fixture();
    partial.issues.status = {...partial.issues.status, availability: 'unknown',
      complete: false, reason: 'collection_limit'};
    partial.issues.items[1].blocked_by = {
      status: {...partial.issues.status, reason: 'unavailable'}, items: null
    };
    partial.pull_requests.items[0].checks.status = {...partial.issues.status,
      availability: 'denied', reason: 'permission_denied'};
    await scenario('partial, denied, null, known-empty and safe provenance', partial,
      async ({page}) => {
        await load(page);
        await contains(page.locator('#issue-status'), 'collection_limit');
        await contains(page.locator('#issue-status'), 'incomplete');
        await page.locator('.node > button').nth(1).click();
        await contains(page.locator('#details'), 'Unavailable list');
        await page.locator('.node > button').nth(3).click();
        await contains(page.locator('#details'), 'Known empty');
        await page.locator('.node > button').first().click();
        await page.locator('#details li button').nth(2).click();
        await contains(page.locator('#details'), 'denied; incomplete');
        await contains(page.locator('#details'), 'permission_denied');
        await contains(page.locator('#details'), 'observed');
        await contains(page.locator('#details'), 'expires');
      });
    const empty = fixture(0);
    empty.runs.items = null;
    empty.runs.status = {...empty.runs.status, availability: 'unknown', complete: false};
    await scenario('empty differs from unavailable', empty, async ({page}) => {
      await load(page);
      await contains(page.locator('#issue-status'), 'Known empty');
      await contains(page.locator('#repository'), 'Unavailable list');
      assert.equal(await page.locator('.node').count(), 0);
    });
    await scenario('expiry downgrades every nested status without collecting', fixture(),
      async ({page, requests}) => {
        await load(page);
        await page.locator('.node > button').first().click();
        await contains(page.locator('#issue-status'), 'known; complete');
        await page.clock.fastForward(300001);
        await contains(page.locator('#issue-status'), 'unknown; incomplete');
        await contains(page.locator('#repository'), 'Head checks: 1 retained facts. ' +
          'unknown; incomplete');
        const detail = await page.locator('#details').innerText();
        assert.ok(!detail.includes('known; complete'));
        assert.ok(detail.includes('denied; incomplete; permission_denied'));
        await contains(page.locator('#nodes'), 'unknown; incomplete');
        assert.equal(requests.filter(r => r.url().includes('/api/')).length, 1);
      });
    await scenario('stale at load through the actual service',
      fixture(4, Date.now() - 301000), async ({page}) => {
        await page.clock.setSystemTime(new Date());
        await load(page);
        await contains(page.locator('#issue-status'), 'unknown; incomplete');
        await page.locator('.node > button').first().click();
        const detail = await page.locator('#details').innerText();
        assert.ok(!detail.includes('known; complete'));
        await contains(page.locator('#details'), 'denied; incomplete');
      });
    for (const kind of ['duplicate', 'invalid-identity', 'wrong-source']) {
      await scenario(`unusable identity ${kind}`, fixture(), async ({page}) => {
        const value = fixture();
        if (kind === 'duplicate') value.issues.items.push(value.issues.items[0]);
        if (kind === 'invalid-identity') value.issues.items[0].identity.id = -1;
        if (kind === 'wrong-source') {
          value.issues.items[0].blocked_by.items[0].from = value.issues.items[1].identity;
        }
        await page.route('**/api/snapshot', route => route.fulfill({json: value}));
        await page.getByRole('button', {name: 'Read snapshot', exact: true}).click();
        await page.waitForFunction(() =>
          document.getElementById('message').textContent.includes('unavailable'));
        assert.equal(await page.locator('#display').isVisible(), false);
      });
    }
    for (const kind of ['stale', 'invalid', 'contradictory', 'future', 'bad-observed',
      'long-window', 'missing-expiry', 'invalid-expiry', 'unknown-start']) {
      const value = fixture();
      const response = structuredClone(value);
      if (kind === 'long-window') response.expires_at = '2099-01-01T00:00:00Z';
      if (kind === 'missing-expiry') delete response.expires_at;
      if (kind === 'invalid-expiry') response.expires_at = 'not a time';
      if (kind === 'unknown-start') response.started_at = null;
      if (kind === 'stale') response.expires_at = response.finished_at;
      if (kind === 'invalid') response.started_at = '2026-02-31T00:00:00Z';
      if (kind === 'contradictory') response.finished_at = '2000-01-01T00:00:00Z';
      if (kind === 'future') response.finished_at = '2099-01-01T00:00:00Z';
      if (kind === 'bad-observed') {
        response.issues.status.observed_at = 'unknown';
      }
      await scenario(`freshness ${kind}`, value, async ({page}) => {
        await page.route('**/api/snapshot', route => route.fulfill({json: response}));
        await load(page);
        await contains(page.locator('#issue-status'), 'unknown; incomplete');
      });
    }
    for (const kind of ['http', 'json', 'version', 'payload']) {
      await scenario(`safe failed refresh ${kind}`, fixture(), async ({page}) => {
        await load(page);
        await page.route('**/api/snapshot', route => {
          if (kind === 'http') return route.fulfill({status: 503, body: 'PRIVATE ERROR'});
          if (kind === 'json') return route.fulfill({body: 'PRIVATE ERROR'});
          return route.fulfill({json: kind === 'version' ? {schema_version: 2} :
            {schema_version: 1}});
        });
        await page.getByRole('button', {name: 'Read snapshot', exact: true}).click();
        await page.waitForFunction(() =>
          document.getElementById('message').textContent.includes('previous data'));
        assert.equal(await page.locator('.node').count(), 4);
        assert.ok(!(await page.content()).includes('PRIVATE ERROR'));
        await page.clock.fastForward(300001);
        await contains(page.locator('#issue-status'), 'unknown; incomplete');
      });
    }
    await scenario('unusable initial payload is an error, never empty success', fixture(),
      async ({page}) => {
        await page.route('**/api/snapshot', route => route.fulfill({json: {}}));
        await page.getByRole('button', {name: 'Read snapshot', exact: true}).click();
        await page.waitForFunction(() =>
          document.getElementById('message').textContent.includes('unavailable'));
        assert.equal(await page.locator('#display').isVisible(), false);
      });
    await scenario('additive fields and hostile text', fixture(), async ({page}) => {
      const value = fixture();
      value.future = {anything: true};
      value.issues.items[0].title = '<img src=x onerror="window.pwned=1">\u0000' +
        '<script>window.pwned=2</script> "quoted"';
      value.issues.items[0].assignees[0].login = '<svg onload="window.pwned=3">\u202e';
      value.issues.items[0].identity.url = 'javascript:window.pwned=4';
      value.issues.items[0].blocked_by.items[0].from = value.issues.items[0].identity;
      value.issues.status.reason = 'RAW PRIVATE DIAGNOSTIC';
      value.issues.status.source = 'https://attacker.invalid/secret';
      await page.route('**/api/snapshot', route => route.fulfill({json: value}));
      await load(page);
      await page.locator('.node > button').first().click();
      await contains(page.locator('#details'), '<img src=x onerror=');
      await contains(page.locator('#details'), '[U+0000]');
      await contains(page.locator('#details'), '[U+202e]');
      assert.equal(await page.locator('#details').locator('img, svg, script').count(),
        0);
      assert.equal(await page.evaluate(() => window.pwned), undefined);
      assert.equal(await page.locator('#details > div > a').count(), 0);
      assert.ok(!(await page.content()).includes('RAW PRIVATE DIAGNOSTIC'));
      assert.ok(!(await page.content()).includes('attacker.invalid'));
    });
    for (const bad of ['data:text/html,hi', 'https://github.com.evil/issues/1',
      'https://evil.invalid/x', 'https://user:pass@github.com/example/project/issues/1',
      'https://github.com/example/other/issues/1',
      'https://github.com/example/project/issues/2',
      'https://github.com/example/project/issues/1?token=x']) {
      await scenario(`unsafe link ${bad.split(':')[0]}`, fixture(), async ({page}) => {
        const value = fixture();
        value.issues.items[0].identity.url = bad;
        value.repository.items[0].url = bad;
        value.pull_requests.items[0].identity.url = bad;
        value.pull_requests.items[0].checks.items[0].url = bad;
        value.runs.items[0].url = bad;
        await page.route('**/api/snapshot', route => route.fulfill({json: value}));
        await load(page);
        await page.locator('.node > button').first().click();
        for (const anchor of await page.locator('a').all()) {
          assert.notEqual(await anchor.getAttribute('href'), bad);
        }
        await contains(page.locator('#details'), 'Original Issue (link unavailable)');
      });
    }
    await scenario('150 nodes, keyboard, mobile and reset', fixture(150),
      async ({page, requests}) => {
        await page.setViewportSize({width: 375, height: 700});
        const visited = new Set();
        async function tabTo(locator) {
          for (let attempt = 0; attempt < 1000; attempt++) {
            if (await locator.evaluate(n => n === document.activeElement)) return;
            await page.keyboard.press('Tab');
            visited.add(await page.evaluate(() => document.activeElement.id));
          }
          assert.fail('Keyboard navigation could not reach required control');
        }
        await tabTo(page.getByRole('button', {name: 'Read snapshot', exact: true}));
        await page.keyboard.press('Enter');
        await page.locator('.node').nth(149).waitFor();
        assert.equal(await page.locator('.node').count(), 150);
        const target = page.locator('.node > button').nth(149);
        await tabTo(target);
        await page.keyboard.press('Enter');
        await contains(page.locator('#details'), 'Issue 150');
        assert.equal(await page.locator('#details').evaluate(n => n ===
          document.activeElement), true);
        // Continue using only keyboard to reach the original link then dependency.
        await page.keyboard.press('Tab');
        assert.equal(await page.evaluate(() => document.activeElement.tagName), 'A');
        await page.keyboard.press('Tab');
        assert.equal(await page.evaluate(() => document.activeElement.tagName), 'BUTTON');
        const outline = await page.evaluate(() =>
          getComputedStyle(document.activeElement).outlineStyle);
        assert.notEqual(outline, 'none');
        await page.keyboard.press('Enter');
        await contains(page.locator('#details'), 'Issue 2');
        await tabTo(page.locator('#query'));
        await page.keyboard.type('Issue 150');
        assert.equal(await page.locator('.node').count(), 1);
        await tabTo(page.locator('#reset'));
        await page.keyboard.press('Enter');
        assert.equal(await page.locator('.node').count(), 150);
        const overflow = await page.locator('#map').evaluate(n =>
          n.scrollWidth > n.clientWidth);
        assert.ok(overflow);
        await tabTo(page.locator('#map'));
        for (const id of ['connect', 'query', 'state', 'progress', 'reset', 'map']) {
          assert.ok(visited.has(id), `Keyboard must reach ${id}`);
        }
        await page.keyboard.press('ArrowRight');
        await page.waitForFunction(() => document.getElementById('map').scrollLeft > 0);
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= 375));
        assert.equal(requests.filter(r => r.url().includes('/api/')).length, 1);
        for (const anchor of await page.locator('a').all()) {
          assert.equal(await anchor.getAttribute('rel'), 'noopener noreferrer');
        }
      });
    await scenario('missing and invalid bootstrap cannot collect', fixture(),
      async ({page, origin, requests}) => {
        for (const suffix of ['/', '/#invalid']) {
          await page.goto(origin + suffix);
          await page.reload();
          await page.waitForFunction(() => location.hash === '');
          assert.equal(await page.locator('#connect').isDisabled(), true);
          await page.keyboard.press('Tab');
          await page.keyboard.press('Enter');
          assert.equal(requests.filter(r => r.url().includes('/api/')).length, 0);
        }
      });
  } finally {
    await browser.close();
  }
  console.log(`${passed} rendered Chromium scenarios passed on ${process.platform}.`);
}
