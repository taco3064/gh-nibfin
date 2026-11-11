import {test, expect} from './harness.mjs';
import {fixture, section, status, instant, identity, edge} from './fixtures.mjs';

const node = (page, number) => page.locator('.node').filter({
  has: page.getByRole('button', {name: new RegExp(`^example/project#${number} `)})
});
const select = (page, number) => node(page, number).getByRole('button').first().click();

test('qualified native graph, cycles, shared blockers, selection and filters',
  async ({page, app}) => {
    const value = fixture();
    value.issues.items[3].blocked_by.items.push(
      edge(identity(4), identity(2)));
    value.additive_future_field = {unknown: true};
    await app.load(value);
    await expect(page.getByRole('heading',
      {name: 'Repository', exact: true})).toBeVisible();
    await expect(page.locator('.node')).toHaveCount(4);
    await expect(node(page, 1)).toContainText('GitHub closed');
    await expect(node(page, 1)).toContainText('scope, shaped, delivered');
    await expect(node(page, 1)).toContainText('→ example/project#2');
    await expect(node(page, 2)).toContainText('→ example/project#1');
    await expect(node(page, 3)).toContainText('→ example/project#2');
    await expect(node(page, 4)).toContainText('→ example/project#2');
    await select(page, 3);
    const details = page.locator('#details');
    await expect(details).toContainText('reader (ID 5)');
    await expect(details).toContainText('permission denied');
    await expect(details).toContainText('Core completion: Unknown / unverified');
    await details.getByRole('button',
      {name: 'Follow dependency → other/project#1'}).click();
    await expect(details).toContainText('not a collected node');
    await expect(details).toContainText('Provider Issue ID 20001');
    await select(page, 3);
    await details.getByRole('button',
      {name: 'Follow dependency → hidden/project#1'}).click();
    await expect(details).toContainText('Denied; incomplete; permission denied');
    await expect(details).toContainText('repository ID unavailable');
    await page.getByLabel('Find Issue').fill('Issue 3');
    await expect(page.locator('.node')).toHaveCount(1);
    await expect(node(page, 3)).toContainText('→ example/project#2 (outside view)');
    await select(page, 3);
    await details.getByRole('button',
      {name: 'Follow dependency → example/project#2'}).click();
    await expect(details).toContainText('hidden by the current filters');
    await expect(details).toContainText('Issue 2');
    await page.getByRole('button', {name: 'Clear filters'}).click();
    await page.getByLabel('GitHub state', {exact: true}).selectOption('closed');
    await expect(page.locator('.node')).toHaveCount(1);
    await page.getByLabel('Native progress').selectOption('delivered');
    await expect(node(page, 1)).toBeVisible();
    await select(page, 1);
    await expect(details).toContainText('Core pause: Unknown / unverified');
    await expect(details).toContainText('Controls unavailable');
    await expect(page.locator('#repository')).toContainText('Run 99');
    await expect(page.locator('#repository')).toContainText('success');
    expect(app.snapshotReads()).toHaveLength(1);
  });

test('independent null, empty, partial, denied and nested provenance',
  async ({page, app}) => {
    const value = fixture();
    value.issues.status = status({availability: 'unknown', complete: false,
      reason: 'collection_limit'});
    value.issues.items[0].blocked_by = section([]);
    value.issues.items[1].blocked_by = section(null, {availability: 'unknown',
      complete: false, reason: 'unavailable'});
    value.issues.items[3].blocked_by = section(null, {availability: 'denied',
      complete: false, reason: 'permission_denied'});
    value.pull_requests = section(null, {availability: 'denied', complete: false,
      reason: 'permission_denied'});
    value.runs = section([]);
    await app.load(value);
    await expect(node(page, 1)).toContainText(
      'Known; complete, current; none; known empty');
    await expect(node(page, 2)).toContainText('Unknown; incomplete; unavailable');
    await expect(node(page, 2)).toContainText('unavailable collection (null)');
    await expect(node(page, 4)).toContainText('Denied; incomplete; permission denied');
    await select(page, 1);
    await expect(page.locator('#details')).toContainText(
      'collection limit; 4 retained facts');
    await expect(page.locator('#details')).toContainText('source repos/example/project');
    await expect(page.locator('#details')).toContainText('observed 2026-11-11T12:00:00Z');
    await expect(page.locator('#repository')).toContainText('Pull requests: Denied');
    await expect(page.locator('#repository')).toContainText('Runs: Known');
    await expect(page.locator('#repository')).toContainText('known empty');
  });

test('expiry downgrades all collections, references and checks without a request',
  async ({page, app}) => {
    await app.load();
    await select(page, 3);
    await expect(page.locator('#details')).toContainText('Known; complete, current');
    await page.clock.fastForward(300001);
    await expect(page.locator('#freshness')).toContainText('Stale');
    await expect(page.locator('#views')).not.toContainText('complete, current');
    await expect(page.locator('#details')).toContainText('Denied; incomplete');
    await expect(page.locator('#repository')).toContainText(
      'Checks: Unknown; incomplete');
    await expect(page.locator('#repository')).toContainText('stale observation');
    expect(app.snapshotReads()).toHaveLength(1);
  });

test('stale-at-load and unknown timestamps never claim currentness',
  async ({page, app}) => {
    const value = fixture();
    value.started_at = '2026-11-11T11:00:00Z';
    value.finished_at = value.started_at;
    value.expires_at = '2026-11-11T11:05:00Z';
    await app.load(value);
    await expect(page.locator('#freshness')).toContainText('Stale');
    await expect(page.locator('#views')).not.toContainText('complete, current');
  });

for (const badTime of ['unknown', '2026-02-30T12:00:00Z',
  '2026-11-11T11:59:00Z', '2026-11-12T12:00:00Z']) {
  test(`invalid or contradictory expiry ${badTime}`, async ({page, app}) => {
    await app.load();
    const value = fixture();
    value.expires_at = badTime;
    await page.route('**/api/snapshot', route => route.fulfill({json: value}));
    await page.getByRole('button', {name: 'Read snapshot', exact: true}).click();
    await expect(page.locator('#snapshot')).toContainText('Snapshot loaded');
    await expect(page.locator('#freshness')).toContainText('Stale');
    await expect(page.locator('#views')).not.toContainText('complete, current');
  });
}

for (const failure of ['http', 'json', 'version', 'shape', 'collision']) {
  test(`safe refresh failure: ${failure}`, async ({page, app}) => {
    await app.load();
    await page.route('**/api/snapshot', route => {
      if (failure === 'http') return route.fulfill({status: 503, body: 'private detail'});
      if (failure === 'json') return route.fulfill({body: '<private invalid>'});
      const value = fixture();
      if (failure === 'version') value.schema_version = 2;
      if (failure === 'shape') value.issues.items = [{}];
      if (failure === 'collision') value.issues.items.push(value.issues.items[0]);
      return route.fulfill({json: value});
    });
    await page.getByRole('button', {name: 'Read snapshot', exact: true}).click();
    await expect(page.getByRole('status').first()).toContainText(
      'unavailable or unusable');
    await expect(page.locator('#freshness')).toContainText('Previous data');
    await expect(page.locator('.node')).toHaveCount(4);
    await expect(page.locator('body')).not.toContainText('private');
    await page.clock.fastForward(300001);
    await expect(page.locator('#freshness')).toContainText('Previous data. Stale');
  });
}

test('initial failure is an accessible error, never an empty successful graph',
  async ({page, app}) => {
    await app.start();
    await page.route('**/api/snapshot', route => route.fulfill({status: 500}));
    await page.getByRole('button', {name: 'Read snapshot', exact: true}).click();
    await expect(page.locator('#snapshot')).toContainText('unavailable or unusable');
    await expect(page.locator('#views')).toBeHidden();
  });

test('150 nodes, keyboard selection, dependency navigation, mobile and reset',
  async ({page, app}) => {
    await page.setViewportSize({width: 375, height: 812});
    await app.load(fixture(150));
    await expect(page.locator('.node')).toHaveCount(150);
    const search = page.getByLabel('Find Issue');
    await search.focus();
    await page.keyboard.type('Issue 150');
    await expect(page.locator('.node')).toHaveCount(1);
    // Traverse the actual controls with Tab and activate the filtered node by keyboard.
    await page.keyboard.press('Tab');
    await expect(page.getByLabel('GitHub state', {exact: true})).toBeFocused();
    await page.keyboard.press('Tab');
    await expect(page.getByLabel('Native progress')).toBeFocused();
    await page.keyboard.press('Tab');
    await expect(page.getByRole('button', {name: 'Clear filters'})).toBeFocused();
    await page.keyboard.press('Tab');
    await expect(page.getByText('Visual overview (full relationships',
      {exact: false})).toBeFocused();
    await page.keyboard.press('Tab');
    await expect(node(page, 150).getByRole('button').first()).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(page.locator('#details h3')).toBeFocused();
    await expect(page.locator('#details')).toContainText('Issue 150');
    await page.keyboard.press('Tab');
    await page.keyboard.press('Tab');
    await expect(page.getByRole('button',
      {name: 'Follow dependency → example/project#149'})).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(page.locator('#details')).toContainText('Issue 149');
    await expect(page.locator('#details')).toContainText('hidden by the current filters');
    await page.getByRole('button', {name: 'Clear filters'}).click();
    await expect(search).toBeFocused();
    await expect(page.locator('.node')).toHaveCount(150);
    await select(page, 150);
    await expect(page.locator('#details')).toContainText('Issue 150');
    await page.getByText('Visual overview (full relationships', {exact: false}).click();
    const diagram = page.getByRole('region', {name: 'Scrollable dependency diagram'});
    await diagram.focus();
    await page.keyboard.press('ArrowRight');
    const overflow = await diagram.evaluate(el => el.scrollWidth > el.clientWidth);
    expect(overflow).toBe(true);
    expect(await page.evaluate(() =>
      document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    const focused = await diagram.evaluate(el => getComputedStyle(el).outlineStyle);
    expect(focused).not.toBe('none');
    expect(app.snapshotReads()).toHaveLength(1);
  });

for (const unavailable of [false, true]) {
  test(`Issue collection remains distinct: unavailable=${unavailable}`,
    async ({page, app}) => {
      const value = fixture();
      value.issues = unavailable ? section(null, {availability: 'unknown',
        complete: false, reason: 'unavailable'}) : section([]);
      await app.load(value);
      await expect(page.locator('.node')).toHaveCount(0);
      await expect(page.locator('#issue-status')).toContainText(unavailable ?
        'Unknown; incomplete; unavailable; unavailable collection (null)' :
        'Known; complete, current; none; known empty');
    });
}
