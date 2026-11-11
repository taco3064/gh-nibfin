import {test, expect} from './harness.mjs';
import {fixture} from './fixtures.mjs';

for (const fragment of [false, 'invalid', 'a'.repeat(63), 'A'.repeat(64)]) {
  test(`invalid bootstrap ${fragment}`, async ({page, app}) => {
    await app.start(fixture(), fragment);
    await expect(page.getByRole('button', {name: 'Read snapshot'})).toBeDisabled();
    expect(new URL(page.url()).hash).toBe('');
    expect(await page.evaluate(() => globalThis.bootstrapClean)).toBe(true);
    expect(app.snapshotReads()).toHaveLength(0);
  });
}

test('proof is memory-only; deliberate fixed read, packaged same-origin resources',
  async ({page, app}) => {
    await app.start();
    expect(new URL(page.url()).hash).toBe('');
    expect(app.snapshotReads()).toHaveLength(0);
    await expect(page.locator('body')).not.toContainText(app.proof());
    const storage = await page.evaluate(() => ({
      local: localStorage.length, session: sessionStorage.length, cookie: document.cookie
    }));
    expect(storage).toEqual({local: 0, session: 0, cookie: ''});
    await page.getByRole('button', {name: 'Read snapshot', exact: true}).click();
    await expect(page.locator('#snapshot')).toContainText('Snapshot loaded');
    await page.getByLabel('Find Issue').fill('Issue 3');
    await page.locator('.node > button').click();
    await page.getByRole('button', {name: 'Follow dependency → other/project#1'}).click();
    await page.getByRole('button', {name: 'Clear filters'}).click();
    expect(app.snapshotReads()).toHaveLength(1);
    for (const request of app.requests) {
      expect(request.url).not.toContain(app.proof());
      if (request.url.endsWith('/api/snapshot')) {
        expect(request.headers['x-nibfin-session']).toBe(app.proof());
      } else expect(request.headers['x-nibfin-session']).toBeUndefined();
      expect(request.method).toBe('GET');
    }
    const dom = await page.content();
    expect(dom).not.toContain(app.proof());
    const urls = await page.locator('a').evaluateAll(links => links.map(a => a.href));
    expect(urls.every(url => !url.includes(app.proof()))).toBe(true);
    expect(await page.context().cookies()).toEqual([]);
    await page.reload();
    await expect(page.getByRole('button', {name: 'Read snapshot'})).toBeDisabled();
    expect(app.snapshotReads()).toHaveLength(1);
  });

test('provider text remains readable inert text including control characters',
  async ({page, app}) => {
    const value = fixture();
    const hostile = '<img src=x onerror="globalThis.injected=1">' +
      '<script>globalThis.injected=2</script> " \' & \u0000\u001b\u202e';
    value.issues.items[0].title = hostile;
    value.issues.items[0].assignees[0].login = hostile;
    value.pull_requests.items[0].checks.items[0].name = hostile;
    await app.load(value);
    await page.locator('.node > button').first().click();
    await expect(page.locator('#details')).toContainText('<img src=x onerror=');
    await expect(page.locator('#details')).toContainText('[U+0000][U+001B][U+202E]');
    expect(await page.locator('img, iframe, [onerror], [onclick]').count()).toBe(0);
    expect(await page.locator('script').count()).toBe(1);
    expect(await page.evaluate(() => globalThis.injected)).toBeUndefined();
    const anchors = page.locator('a[target="_blank"]');
    for (const rel of await anchors.evaluateAll(all => all.map(a => a.rel))) {
      expect(rel).toContain('noopener');
      expect(rel).toContain('noreferrer');
    }
  });

for (const attack of ['javascript:alert(1)', 'data:text/html,<script>alert(1)</script>',
  'https://github.com.evil.example/example/project/issues/1',
  'https://attacker@github.com/example/project/issues/1',
  'https://github.com/other/project/issues/1',
  'https://github.com/example/project/pull/1',
  'https://github.com/example/project/issues/2',
  'https://github.com/example/project/issues/1?token=secret',
  'https://github.com/example/project/issues/1#fragment',
  'https://github.com/example/project/issues/1/../2',
  'https://github.com/example/project/issues/%31']) {
  test(`reject malicious or mismatched destination ${attack}`,
    async ({page, app}) => {
    const value = fixture();
    value.issues.items[0].identity.url = attack;
    // Keep edge.from identity consistent, URL is not a graph key.
    await app.load(value);
    await page.locator('.node > button').first().click();
    await expect(page.locator('#details')).toContainText('Issue 1');
    await expect(page.locator('#details')).toContainText('link unavailable');
    await expect(page.locator('#details a')).toHaveCount(0);
    expect(app.snapshotReads()).toHaveLength(1);
  });
}

test('exact resource links; invalid PR, run, check and repository URLs stay inert',
  async ({page, app}) => {
    const value = fixture();
    value.repository.items[0].url = 'https://evil.example';
    value.pull_requests.items[0].identity.url =
      'https://github.com/example/project/issues/7';
    value.pull_requests.items[0].checks.items[0].url =
      'https://github.com/example/project/actions/runs/99/job/999';
    value.runs.items[0].url = 'https://github.com/example/project/actions/runs/100';
    value.runs.items[0].pull_requests[0].url = 'javascript:alert(1)';
    await app.load(value);
    await expect(page.locator('#repository a')).toHaveCount(0);
    await expect(page.locator('#repository')).toContainText('link unavailable');
  });

test('safe reason and provenance fields never expose raw diagnostics',
  async ({page, app}) => {
    const value = fixture();
    value.issues.items[0].blocked_by.status.reason = 'token /private/raw Agent log';
    value.issues.items[0].blocked_by.status.source =
      'repos/example/project?token=secret_token';
    value.core.completion.reason = 'raw Agent log';
    value.core.pause.reason = 'constructor';
    await app.load(value);
    await page.locator('.node > button').first().click();
    await expect(page.locator('#details')).toContainText('unrecognized reason');
    await expect(page.locator('body')).not.toContainText('secret_token');
    await expect(page.locator('body')).not.toContainText('/private/');
    await expect(page.locator('body')).not.toContainText('raw Agent log');
    await expect(page.locator('#repository')).toContainText(
      'Core pause: Unknown / unverified; unrecognized reason');
  });

test('well-formed wrong session proof receives safe error and no data',
  async ({page, app}) => {
    await app.start(fixture(), '0'.repeat(64));
    await page.getByRole('button', {name: 'Read snapshot'}).click();
    await expect(page.locator('#snapshot')).toContainText('unavailable or unusable');
    await expect(page.locator('#views')).toBeHidden();
  });
