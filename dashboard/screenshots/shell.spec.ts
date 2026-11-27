import { expect, test, type Locator, type Page } from '@playwright/test'

import { unevenRoundedBorders } from './border-scan'
import { commands } from './command-fixtures'
import { hosts, issueURL, longHost, longMachine, longRepo, personView, runnerActivity, runRepos } from './person-fixtures'
import { expectClean, expectGutter, expectNonce, fulfillPlanStream, prepare, shot, storageKey, widths, type StatusAnswer, type Watch } from './support'

// Screenshots and checks of the Dashboard shell (#352): the three views, the
// landing rule, the freshness indicator, Refresh and the person view. The
// real Go server serves the page with its CSP; every API answer is a
// fictional mock.

const themes = ['dark', 'light'] as const
type Theme = (typeof themes)[number]

interface Ops {
  plans: { action: string; issues: string[] }[]
  mode: 'result' | 'cut'
  // commandReads counts the schema reads.
  commandReads: number
}

// mockOps answers the pause and resume plans and applies; the status read
// falls through to prepare's mock.
async function mockOps(page: Page): Promise<Ops> {
  const ops: Ops = { plans: [], mode: 'result', commandReads: 0 }
  await page.route('**/api/commands', (route) => {
    expect(route.request().headers()['x-nibfin-session']).toBe('SCREENSHOTSESSION')
    ops.commandReads++
    return route.fulfill({ json: { commands } })
  })
  await page.route('**/api/commands/*/plan', async (route) => {
    const action = new URL(route.request().url()).pathname.split('/')[3]
    if (action === 'status') return route.fallback()
    const body = route.request().postDataJSON() as { issues: string[] }
    ops.plans.push({ action, issues: body.issues })
    await route.fulfill({ json: { id: `PLAN${ops.plans.length}`, plan: { operation: action, issues: body.issues } } })
  })
  await page.route('**/api/plans/**', async (route) => {
    const id = new URL(route.request().url()).pathname.split('/')[3]
    if (route.request().method() === 'DELETE') return route.fulfill({ status: 204 })
    const plan = ops.plans[Number(id.replace('PLAN', '')) - 1]
    const done = plan.action === 'pause' ? 'paused' : 'resumed'
    const body =
      ops.mode === 'result'
        ? JSON.stringify({ event: 'result', result: Object.fromEntries(plan.issues.map((u) => [u, done])) }) + '\n'
        : '{"event":"res'
    await route.fulfill({ status: 200, contentType: 'application/x-ndjson', body })
  })
  return ops
}

interface Opened {
  w: Watch
  ops: Ops
}

async function open(page: Page, width: number, theme: Theme, status?: StatusAnswer, ready = '[data-content] > *'): Promise<Opened> {
  await page.setViewportSize({ width, height: 900 })
  await page.addInitScript(([k, t]) => localStorage.setItem(k, t), [storageKey, theme])
  const w = await prepare(page, status)
  const ops = await mockOps(page)
  const res = await page.goto('/#proof=SCREENSHOT')
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
  await expectNonce(page, res)
  await expect(page.locator(ready).first()).toBeVisible()
  await page.evaluate(() => document.fonts.ready.then(() => undefined))
  await expectNoPageScroll(page)
  return { w, ops }
}

// expectNoPageScroll checks that the page does not scroll sideways and that
// its controls and cards keep the side gutter.
async function expectNoPageScroll(page: Page) {
  await expectGutter(page)
}

const tab = (page: Page, name: string) => page.getByRole('tablist', { name: 'Views' }).getByRole('tab', { name })
const item = (page: Page, repo: string, n: number): Locator => page.locator(`article[data-issue="${issueURL(repo, n)}"]`)
const freshness = (page: Page) => page.locator('[data-slot=freshness]')
const tile = (page: Page, name: string) => page.locator(`[data-view=person] [data-tile=${name}]`)

for (const theme of themes) {
  for (const width of [...widths, 390]) {
    test(`person landing and the three views at ${width}px, ${theme}`, async ({ page }) => {
      const { w } = await open(page, width, theme)
      await expect(tab(page, 'Person')).toHaveAttribute('aria-selected', 'true')
      await expect(page.locator('[data-view=person]')).toContainText('Open Issues with scope or request assigned to octo-dev on github.com')
      await expect(freshness(page)).toHaveAttribute('data-state', 'current')
      await expect(freshness(page)).toContainText('Current')
      // The summary tiles come before the Issue list.
      await expect(tile(page, 'issues').locator('[data-slot=tile-count]')).toHaveText('9Issues')
      await expect(tile(page, 'issues').getByRole('img')).toHaveAttribute('aria-label', 'Open Issues by stage: Shape 4, Deliver 3, Other 2')
      await expect(tile(page, 'needs-you').locator('[data-slot=tile-count]')).toHaveText('3pending or paused')
      await expect(tile(page, 'runs').getByRole('img')).toHaveAttribute('aria-label', 'Runs by conclusion: Succeeded 0, Failed 0, Running 1')
      await expect(tile(page, 'runners').locator('[data-slot=runner-counts]')).toHaveText('2 online · 0 busy')
      await expect(tile(page, 'runners').locator('[data-slot=memory-used]')).toContainText('Memory 10.3 GiB of 15.5 GiB in use')
      const top = async (l: Locator) => (await l.boundingBox())!.y
      expect(await top(page.locator('[data-slot=summary]'))).toBeLessThan(await top(page.locator('[data-repo]').first()))
      // The run legend shows once, in the first group that lists Issues,
      // above that group's first run strip (#498).
      const view = page.locator('[data-view=person]')
      const legend = view.locator('[data-slot=run-legend]')
      await expect(legend).toHaveCount(1)
      await expect(legend.locator('li')).toHaveText(['Succeeded', 'Failed', 'Skipped or cancelled', 'Running'])
      const firstGroup = view.locator('[data-repo]').filter({ has: page.locator('article[data-issue]') }).first()
      await expect(firstGroup.locator('[data-slot=run-legend]')).toHaveCount(1)
      const bottom = async (l: Locator) => {
        const b = (await l.boundingBox())!
        return b.y + b.height
      }
      expect(await bottom(legend)).toBeLessThanOrEqual(await top(firstGroup.locator('[data-slot=run-strip]').first()))
      // Nothing legend-like is left between the summary tiles and the list.
      await expect(view.locator(':scope > [data-slot=run-legend]')).toHaveCount(0)
      expect(await top(legend)).toBeGreaterThan(await top(view.locator('[data-repo]').first()))
      expect(await unevenRoundedBorders(page, view), `person view ${width}px ${theme}: rounded boxes with uneven borders`).toEqual([])
      await shot(page, `shell-person-${theme}-${width}`)

      await tab(page, 'Repositories').click()
      await expect(page.locator('[data-view=repo] [data-slot=picker]')).toContainText('Repositories using Nibfin on github.com')
      await expectNoPageScroll(page)
      await shot(page, `shell-repo-${theme}-${width}`, false)
      await tab(page, 'Runners').click()
      await expect(page.locator('[data-card=host]')).toContainText('local podman')
      await expectNoPageScroll(page)
      expect(w.statusReads).toBe(1)
      await expectClean(w)
    })

    test(`runner landing without a complete Runner at ${width}px, ${theme}`, async ({ page }) => {
      const { w } = await open(page, width, theme, { result: personView({ host: hosts.incomplete }) })
      await expect(tab(page, 'Runners')).toHaveAttribute('aria-selected', 'true')
      const guide = page.locator('[data-guide=no-runner]')
      await expect(guide).toContainText('This host has no Runner yet')
      const steps = guide.getByRole('listitem')
      await expect(steps).toHaveCount(2)
      await expect(steps.nth(0)).toContainText('gh nibfin runner add <claude|codex>')
      await expect(steps.nth(1)).toContainText('gh nibfin init')
      await expect(guide.locator('[data-slot=note]')).toHaveCount(0)
      // The host and the providers still show below the guide.
      await expect(page.locator('[data-card=host] [data-badge=machine]')).toHaveText('Running')
      await expect(page.locator('li[data-slot-id="claude-1"] [data-badge=service]')).toHaveText('Incomplete: no unit, not enabled')
      await expect(page.locator('[data-provider=codex] [data-badge=login]')).toHaveText('Not logged in')
      await expect(page.locator('[data-provider=codex]').getByRole('button', { name: 'Log in…' })).toBeEnabled()
      await expectNoPageScroll(page)
      await shot(page, `shell-runner-guide-${theme}-${width}`)
      await expectClean(w)
    })
  }
}

for (const width of widths) {
  test(`runner landing without Podman at ${width}px`, async ({ page }) => {
    const { w } = await open(page, width, 'dark', { result: personView({ host: hosts.podmanMissing }) })
    await expect(tab(page, 'Runners')).toHaveAttribute('aria-selected', 'true')
    await expect(page.locator('[data-guide=no-runner]')).toContainText('podman_missing: podman is not installed')
    await expect(page.locator('[data-provider=claude] [data-slot=login-state]')).toContainText('podman_missing: podman is not installed')
    await expect(page.locator('[data-card=host]')).toContainText('podman_missing')
    // The runners error shows once, in the guide.
    await expect(page.getByText('Runners could not be read')).toHaveCount(0)
    await expectNoPageScroll(page)
    await shot(page, `shell-runner-podman-missing-dark-${width}`)
    await expectClean(w)
  })
}

// Long host and machine names wrap inside their card on a phone instead of
// widening the page (#412).
for (const width of [375, 390]) {
  test(`long host and machine names keep the gutter at ${width}px`, async ({ page }) => {
    const { w } = await open(page, width, 'dark', { result: personView({ host: hosts.longNames, githubHost: longHost }) })
    await tab(page, 'Repositories').click()
    await expect(page.locator('[data-view=repo] [data-slot=picker-host]')).toHaveText(longHost)
    await expectNoPageScroll(page)
    await shot(page, `shell-long-names-repo-dark-${width}`, false)
    await tab(page, 'Runners').click()
    const host = page.locator('[data-card=host]')
    await expect(host.getByRole('heading', { level: 2 })).toHaveText(`podman machine ${longMachine}`)
    await expect(host).toContainText(`Machine ${longMachine} · VM type wsl`)
    await expectNoPageScroll(page)
    await shot(page, `shell-long-names-runner-dark-${width}`)
    await expectClean(w)
  })
}

test('a stopped machine lands on the person view', async ({ page }) => {
  const { w } = await open(page, 1280, 'dark', { result: personView({ host: hosts.machineStopped }) })
  await expect(tab(page, 'Person')).toHaveAttribute('aria-selected', 'true')
  await tab(page, 'Runners').click()
  const host = page.locator('[data-card=host]')
  await expect(host.locator('[data-badge=machine]')).toHaveText('Stopped')
  await expect(host).toContainText('machine_stopped: podman machine nibfin-dev is stopped')
  await expect(page.locator('[data-provider=claude] [data-slot=login-state]')).toContainText('machine_stopped')
  await expect(page.locator('[data-provider=claude]')).toContainText('No Runners of claude on this host.')
  await shot(page, 'shell-runner-stopped-dark-1280')
  await expectClean(w)
})

test('the views switch by keyboard', async ({ page }) => {
  const { w } = await open(page, 1280, 'light')
  await tab(page, 'Person').focus()
  await page.keyboard.press('ArrowRight')
  await expect(tab(page, 'Repositories')).toBeFocused()
  await expect(tab(page, 'Repositories')).toHaveAttribute('aria-selected', 'true')
  await shot(page, 'shell-tab-focus-light-1280', false)
  await page.keyboard.press('ArrowRight')
  await expect(tab(page, 'Runners')).toHaveAttribute('aria-selected', 'true')
  await page.keyboard.press('ArrowRight')
  await expect(tab(page, 'Person')).toHaveAttribute('aria-selected', 'true')
  await expect(page.locator('[data-view=person]')).toBeVisible()
  await expectClean(w)
})

test('every image state is a labelled badge', async ({ page }) => {
  const { w } = await open(page, 1280, 'dark')
  const image = (repo: string) => page.locator(`[data-repo="${repo}"] [data-image]`)
  await expect(image('acme/app')).toHaveText('Latest image')
  await expect(image('acme/docs')).toHaveText('Not the latest image')
  await expect(image('acme/infra')).toHaveText('Latest image unknowngithub_error: latest release of acme/nibfin: rate limited')
  await expect(image('acme/legacy')).toHaveText('Image could not be readgithub_error: reading .github/workflows of acme/legacy: HTTP 502')
  await expect(image('acme/mobile')).toHaveText('No valid image pin')
  await expect(image('acme/site')).toHaveText('Peer set: no image pin')
  await expect(image(longRepo)).toHaveText('No Nibfin workflows')
  await expect(page.locator('[data-repo="acme/app"] [data-slot=count]')).toHaveText('3 Issues')
  await expect(page.locator('[data-repo="acme/docs"] [data-slot=count]')).toHaveText('1 Issue')
  const link = page.getByRole('link', { name: 'acme/app (opens GitHub)', exact: true })
  await expect(link).toHaveAttribute('href', 'https://github.com/acme/app')
  await expect(link).toHaveAttribute('rel', 'noreferrer')
  await expect(link).toHaveAttribute('target', '_blank')
  await expectClean(w)
})

for (const theme of themes) {
  for (const width of [375, 1280]) {
    test(`person view errors and notes at ${width}px, ${theme}`, async ({ page }) => {
      const issuesError = { code: 'github_error', message: 'search of Issues: secondary rate limit' }
      const data = personView({
        user: undefined,
        userError: { code: 'github_error', message: 'reading the current account: HTTP 401' },
        issuesError,
        truncated: true,
        repos: personView().repos.slice(0, 2),
      })
      const { w } = await open(page, width, theme, { result: data })
      const view = page.locator('[data-view=person]')
      await expect(view).toContainText('Account could not be read: github_error: reading the current account: HTTP 401')
      await expect(view.locator('[data-note=issues-error]')).toHaveText(
        'Issue search failed: github_error: search of Issues: secondary rate limit',
      )
      await expect(view.locator('[data-note=truncated]')).toContainText('at most 1,000 Issues')
      await expect(view.locator('[data-repo]')).toHaveCount(2)
      await expect(view.locator('[data-empty]')).toHaveCount(0)
      await expect(tile(page, 'issues').locator('[data-note=tile-error]')).toHaveText(
        'Issues could not be read: github_error: search of Issues: secondary rate limit',
      )
      await expect(tile(page, 'needs-you').locator('[data-note=tile-error]')).toBeVisible()
      await shot(page, `shell-person-errors-${theme}-${width}`)

      // A failed search with nothing read is not an empty search.
      w.status = { result: personView({ repos: [], issuesError }) }
      await page.getByRole('button', { name: 'Refresh' }).click()
      await expect(view.locator('[data-repo]')).toHaveCount(0)
      await expect(view.locator('[data-empty]')).toHaveCount(0)

      w.status = { result: personView({ repos: [] }) }
      await page.getByRole('button', { name: 'Refresh' }).click()
      await expect(view.locator('[data-empty]')).toHaveText('No open Issues with scope or request are assigned to you.')
      await expect(tile(page, 'issues').locator('[data-slot=tile-count]')).toHaveText('0Issues')
      await expect(tile(page, 'needs-you').getByRole('button', { name: 'Show only these' })).toBeDisabled()
      await expect(tile(page, 'needs-you').locator('[data-slot=disabled-reason]')).toHaveText('No Issue is pending or paused.')
      await expect(tile(page, 'runs').locator('[data-state=no-runs]')).toHaveText('No runs')
      // No run strips, so no run legend.
      await expect(view.locator('[data-slot=run-legend]')).toHaveCount(0)
      await expectNoPageScroll(page)
      await shot(page, `shell-person-empty-${theme}-${width}`)
      await expectClean(w)
    })
  }
}

test('the data turns stale after five minutes without a read', async ({ page }) => {
  await page.clock.install()
  const { w } = await open(page, 1280, 'dark')
  await expect(freshness(page)).toHaveAttribute('data-state', 'current')
  await page.clock.fastForward('04:58')
  await expect(freshness(page)).toHaveAttribute('data-state', 'current')
  await page.clock.fastForward('00:03')
  await expect(freshness(page)).toHaveAttribute('data-state', 'stale')
  await expect(freshness(page)).toContainText('Stale: over 5 minutes old')
  await shot(page, 'shell-stale-dark-1280', false)
  await page.setViewportSize({ width: 375, height: 900 })
  await expectNoPageScroll(page)
  await shot(page, 'shell-stale-dark-375', false)
  expect(w.statusReads).toBe(1)

  // Refresh reads again, shows the new time and is current again.
  const before = await freshness(page).locator('time').getAttribute('datetime')
  await page.getByRole('button', { name: 'Refresh' }).click()
  await expect(freshness(page)).toHaveAttribute('data-state', 'current')
  await expect(freshness(page).locator('time')).not.toHaveAttribute('datetime', before!)
  expect(w.statusReads).toBe(2)
  await expectClean(w)
})

test('refresh shows its progress and a failure keeps the data', async ({ page }) => {
  const { w } = await open(page, 1280, 'light')
  await tab(page, 'Repositories').click()
  let release!: () => void
  const held = new Promise<void>((r) => (release = r))
  await page.route('**/api/commands/status/read', async (route) => {
    await held
    await route.fallback()
  })
  w.status = { error: { code: 'github_error', message: 'search of Issues: HTTP 502' }, httpStatus: 502 }
  const before = await freshness(page).locator('time').getAttribute('datetime')
  await page.getByRole('button', { name: 'Refresh' }).click()
  await expect(page.getByRole('button', { name: 'Refreshing…' })).toBeDisabled()
  await shot(page, 'shell-refreshing-light-1280', false)
  release()
  await expect(page.locator('[data-note=read-error]')).toHaveText('Refresh failed: github_error: search of Issues: HTTP 502')
  await expect(page.getByRole('button', { name: 'Refresh' })).toBeEnabled()
  // The view stays, and so do the data and its time.
  await expect(tab(page, 'Repositories')).toHaveAttribute('aria-selected', 'true')
  await expect(freshness(page).locator('time')).toHaveAttribute('datetime', before!)
  await tab(page, 'Person').click()
  await expect(page.locator('[data-repo]')).toHaveCount(7)
  await shot(page, 'shell-refresh-failed-light-1280', false)
  expect(w.statusReads).toBe(2)
  await expectClean(w, ['Failed to load resource: the server responded with a status of 502'])
})

for (const theme of themes) {
  test(`a failed first read offers Refresh, ${theme}`, async ({ page }) => {
    const { w } = await open(
      page,
      375,
      theme,
      { error: { code: 'github_error', message: 'reading the host: HTTP 500' }, httpStatus: 500 },
      '[data-note=read-error]',
    )
    await expect(page.locator('[data-note=read-error]')).toHaveText('Status could not be read: github_error: reading the host: HTTP 500')
    await expect(freshness(page)).toHaveText('No data yet')
    await shot(page, `shell-first-read-failed-${theme}-375`)
    w.status = { result: personView() }
    await page.getByRole('button', { name: 'Refresh' }).click()
    await expect(page.locator('[data-note=read-error]')).toHaveCount(0)
    await expect(page.locator('[data-repo]')).toHaveCount(7)
    await expect(freshness(page)).toHaveAttribute('data-state', 'current')
    await expectClean(w, ['Failed to load resource: the server responded with a status of 500'])
  })
}

test('the loading state before the first read', async ({ page }) => {
  await page.setViewportSize({ width: 768, height: 900 })
  const w = await prepare(page)
  let release!: () => void
  // The first data comes from the live stream's snapshot.
  w.live.hold = new Promise<void>((r) => (release = r))
  await page.goto('/#proof=SCREENSHOT')
  await expect(page.locator('[role=status][data-state=reading]')).toHaveText('Reading status…')
  await expect(freshness(page)).toHaveText('No data yet')
  await expect(page.getByRole('button', { name: 'Refreshing…' })).toBeDisabled()
  await shot(page, 'shell-loading-dark-768', false)
  release()
  await expect(page.locator('[data-view=person]')).toBeVisible()
  await expectClean(w)
})

test('a pause updates the badge and an unknown result reads again', async ({ page }) => {
  const { w, ops } = await open(page, 1280, 'dark')
  const it = item(page, 'acme/app', 101)
  await it.getByRole('button', { name: 'Pause' }).click()
  await expect(it.locator('[data-badge=paused]')).toHaveText('Paused')
  expect(ops.plans).toEqual([{ action: 'pause', issues: [issueURL('acme/app', 101)] }])
  expect(w.statusReads).toBe(1)

  ops.mode = 'cut'
  await it.getByRole('button', { name: 'Resume' }).click()
  await expect.poll(() => w.statusReads).toBe(2)
  // The re-read shows the labels as the data has them.
  await expect(it.locator('[data-badge=paused]')).toHaveCount(0)
  await expectClean(w)
})

test('a bulk resume across two repositories is one operation', async ({ page }) => {
  const { w, ops } = await open(page, 375, 'light')
  await expect(page.getByRole('region', { name: 'Selected Issues' })).toHaveCount(0)
  await page.getByRole('checkbox', { name: 'Select #102' }).check()
  await page.getByRole('checkbox', { name: 'Select #12' }).check()
  const bulk = page.getByRole('region', { name: 'Selected Issues' })
  await expect(bulk.locator('[data-slot=selected-count]')).toHaveText('2 selected')
  await expectNoPageScroll(page)
  await shot(page, 'shell-bulk-light-375', false)
  await bulk.getByRole('button', { name: 'Resume selected' }).click()
  await expect(bulk.getByRole('status')).toHaveText('Confirmed: resumed 2.')
  expect(ops.plans).toEqual([{ action: 'resume', issues: [issueURL('acme/app', 102), issueURL('acme/infra', 12)] }])
  await expect(item(page, 'acme/app', 102).locator('[data-badge=paused]')).toHaveCount(0)
  await expect(item(page, 'acme/infra', 12).locator('[data-badge=paused]')).toHaveCount(0)
  expect(w.statusReads).toBe(1)
  await expectClean(w)
})

// The runner view (#354): the host, each provider's login and Runner slots,
// and the operation dialogs it opens.

const provider = (page: Page, p: string) => page.locator(`[data-provider="${p}"]`)
const slotRow = (page: Page, slot: string) => page.locator(`li[data-slot-id="${slot}"]`)

for (const theme of themes) {
  for (const width of [...widths, 390]) {
    test(`runner view at ${width}px, ${theme}`, async ({ page }) => {
      const { w, ops } = await open(page, width, theme, { result: personView({ host: hosts.mixed }) })
      await tab(page, 'Runners').click()
      const host = page.locator('[data-card=host]')
      await expect(host.getByRole('heading', { level: 2 })).toHaveText('podman machine nibfin-dev')
      await expect(host).toContainText('Machine nibfin-dev · VM type applehv')
      await expect(host.locator('[data-badge=machine]')).toHaveText('Running')
      await expect(host.locator('[data-slot=memory]')).toHaveText('5.2 GiB free of 15.5 GiB')
      await expect(host.getByRole('meter', { name: 'Free memory' })).toHaveAttribute('aria-valuenow', '33')
      await expect(host.getByRole('meter', { name: 'Free memory' })).toHaveAttribute('aria-valuetext', '33% free, 5.2 GiB free of 15.5 GiB')
      await expect(host.locator('[data-slot=repo]')).toHaveText('Runners serve acme/app')
      // The route container no Runner mounts is a host note.
      await expect(host.locator('[data-slot=host-job]')).toHaveText('Job running on this host: acme/web#44 for 2m')

      // The tiles: state rings with their text, and the busy Jobs.
      const states: [string, string, string][] = [
        ['claude-1', 'busy', 'Busy'],
        ['claude-2', 'idle', 'Idle'],
        ['claude-3', 'offline', 'Offline'],
        ['codex-1', 'busy', 'Busy · local state'],
        ['codex-2', 'offline', 'Offline · local state'],
      ]
      for (const [slot, state, text] of states) {
        await expect(slotRow(page, slot)).toHaveAttribute('data-state', state)
        await expect(slotRow(page, slot).locator('[data-slot=state]')).toHaveText(text)
      }
      const busy = slotRow(page, 'claude-1').locator('[data-slot=job]')
      await expect(busy.getByRole('link', { name: 'acme/web#42' })).toHaveAttribute('href', issueURL('acme/web', 42))
      await expect(busy.locator('[data-slot=stage]')).toHaveText('deliver')
      await expect(busy.locator('[data-slot=elapsed]')).toHaveText('for 1h 05m')
      await expect(slotRow(page, 'codex-1').locator('[data-slot=stage]')).toHaveText('stage unknown')
      await expect(slotRow(page, 'codex-1').locator('[data-slot=elapsed]')).toHaveText('for 4m')

      // The last 24 hours: one lane per Runner, failed Jobs marked.
      const timeline = page.locator('[data-card=timeline]')
      await expect(timeline.locator('[data-lane]')).toHaveCount(5)
      await expect(timeline.locator('[data-lane=claude-2] [data-period=failed]')).toHaveCount(2)
      await expect(timeline.locator('[data-lane=claude-2] [data-period=busy]')).toHaveCount(2)
      await expect(timeline.locator('[data-lane=claude-1] a[data-period]').last()).toHaveAttribute('aria-label', /^acme\/web#42 · deliver · \d\d:\d\d–now$/)
      await expect(timeline.locator('[data-slot=axis] span')).toHaveCount(4)
      expect(w.activityReads).toBe(1)

      await expect(provider(page, 'claude').locator('[data-badge=login]')).toHaveText('Logged in')
      await expect(provider(page, 'codex').locator('[data-badge=login]')).toHaveText('Not logged in')
      const cases: [string, string, string][] = [
        ['claude-1', 'Service active', 'Online · Busy'],
        ['claude-2', 'Service active', 'Online · Idle'],
        ['claude-3', 'Service inactive', 'Offline'],
        ['codex-1', 'Service active', 'Not listed on GitHub'],
        ['codex-2', 'Incomplete: not configured, no unit, not enabled', 'Not listed on GitHub'],
      ]
      for (const [slot, service, github] of cases) {
        await expect(slotRow(page, slot).locator('[data-badge=service]')).toHaveText(service)
        await expect(slotRow(page, slot).locator('[data-badge=github]')).toHaveText(github)
      }
      for (const p of ['claude', 'codex']) {
        for (const name of ['Log in…', 'Add Runners…', 'Remove Runners…']) {
          await expect(provider(page, p).getByRole('button', { name })).toBeEnabled()
        }
      }
      await expectNoPageScroll(page)
      await shot(page, `shell-runner-${theme}-${width}`)

      // Each button opens its dialog with the provider chosen.
      for (const [name, path] of [
        ['Log in…', 'login'],
        ['Add Runners…', 'runner add'],
        ['Remove Runners…', 'runner remove'],
      ] as const) {
        await provider(page, 'codex').getByRole('button', { name }).click()
        const dialog = page.getByRole('dialog')
        await expect(dialog.getByRole('heading', { level: 2 })).toHaveText(path)
        await expect(dialog.getByRole('radio', { name: 'codex' })).toBeChecked()
        await expect(dialog.getByRole('radio', { name: 'claude' })).not.toBeChecked()
        await expectNoPageScroll(page)
        await shot(page, `shell-runner-dialog-${path.replace(' ', '-')}-${theme}-${width}`, false)
        await dialog.getByRole('button', { name: 'Cancel' }).click()
        await expect(dialog).toBeHidden()
      }
      expect(ops.commandReads).toBe(1)
      // Nothing was applied, so the status is not read again.
      expect(w.statusReads).toBe(1)
      await expectClean(w)
    })
  }
}

for (const width of widths) {
  test(`runner view with a GitHub error at ${width}px`, async ({ page }) => {
    const { w } = await open(page, width, 'light', { result: personView({ host: hosts.githubError }) })
    await tab(page, 'Runners').click()
    await expect(page.locator('[data-note=github-error]')).toHaveCount(1)
    await expect(page.locator('[data-note=github-error]')).toContainText('github_error: listing the runners of acme/app: HTTP 502')
    await expect(page.locator('[data-badge=github]')).toHaveText(['GitHub not read', 'GitHub not read', 'GitHub not read'])
    await expect(slotRow(page, 'claude-2').locator('[data-badge=service]')).toHaveText('Service inactive')
    await expectNoPageScroll(page)
    await shot(page, `shell-runner-github-error-light-${width}`)
    await expectClean(w)
  })
}

// Without admin, online and offline are the host's. When the jobs cannot be
// read, the lanes say why and a Job container still marks its Runner busy.
const noAdmin = {
  ...hosts.mixed,
  runners: {
    repo: 'acme/app',
    githubNeedsAdmin: true,
    runners: hosts.mixed.runners.runners.map((s) => ({ ...s, listed: false, status: undefined, busy: false })),
  },
}

for (const theme of themes) {
  for (const width of widths) {
    test(`runner view without admin, jobs not read at ${width}px, ${theme}`, async ({ page }) => {
      const { w } = await open(page, width, theme, { result: personView({ host: noAdmin }) })
      const base = runnerActivity()
      w.activity = {
        result: {
          ...base,
          jobsError: { code: 'github_api_failed', message: 'list the shared-runner runs of acme/app: HTTP 403: Resource not accessible by integration' },
          runners: base.runners.map((r) => ({ ...r, periods: [], current: r.current?.source === 'container' ? r.current : undefined })),
        },
      }
      await tab(page, 'Runners').click()
      await expect(page.locator('[data-note=local-state]')).toContainText("GitHub's Runner list needs admin of acme/app")
      await expect(page.locator('[data-badge=github]').first()).toHaveText('GitHub needs admin')
      await expect(slotRow(page, 'claude-1')).toHaveAttribute('data-state', 'idle')
      await expect(slotRow(page, 'claude-1').locator('[data-slot=state]')).toHaveText('Idle · local state')
      await expect(slotRow(page, 'claude-3')).toHaveAttribute('data-state', 'offline')
      await expect(slotRow(page, 'codex-1')).toHaveAttribute('data-state', 'busy')
      const timeline = page.locator('[data-card=timeline]')
      await expect(timeline.locator('[data-note=jobs-error]')).toContainText('github_api_failed: list the shared-runner runs of acme/app: HTTP 403')
      await expect(timeline.locator('[data-lane]')).toHaveCount(0)
      await expectNoPageScroll(page)
      await shot(page, `shell-runner-no-admin-${theme}-${width}`)
      await expectClean(w)
    })
  }
}

test('runner view when the activity route fails', async ({ page }) => {
  const { w } = await open(page, 375, 'dark', { result: personView({ host: hosts.mixed }) })
  w.activity = { error: { code: 'podman_failed', message: 'podman ps: boom' }, httpStatus: 502 }
  await tab(page, 'Runners').click()
  await expect(page.locator('[data-card=timeline] [data-note=jobs-error]')).toContainText('podman_failed: podman ps: boom')
  await expect(slotRow(page, 'claude-1').locator('[data-slot=job]')).toHaveText('Job details not read.')
  await expectNoPageScroll(page)
  await shot(page, 'shell-runner-activity-failed-dark-375')
  await expectClean(w, ['Failed to load resource: the server responded with a status of 502'])
})

test('the no-Runner guide opens runner add per provider', async ({ page }) => {
  const { w } = await open(page, 375, 'dark', { result: personView({ host: hosts.incomplete }) })
  const guide = page.locator('[data-guide=no-runner]')
  await guide.getByRole('button', { name: 'Add Codex Runners…' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { level: 2 })).toHaveText('runner add')
  await expect(dialog.getByRole('radio', { name: 'codex' })).toBeChecked()
  await shot(page, 'shell-runner-guide-dialog-dark-375', false)
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expectClean(w)
})

test('a failed schema read disables the operations', async ({ page }) => {
  const { w } = await open(page, 1280, 'dark', { result: personView({ host: hosts.mixed }) })
  await page.route('**/api/commands', (route) =>
    route.fulfill({ status: 500, json: { error: { code: 'internal_error', message: 'the schema could not be built' } } }),
  )
  await tab(page, 'Runners').click()
  await expect(page.locator('[data-note=commands-error]')).toHaveText('Commands could not be read: internal_error: the schema could not be built')
  await expect(provider(page, 'claude').getByRole('button', { name: 'Log in…' })).toBeDisabled()
  await shot(page, 'shell-runner-commands-error-dark-1280', false)
  await expectClean(w, ['Failed to load resource: the server responded with a status of 500'])
})

test('closing a dialog after an apply reads the status again', async ({ page }) => {
  const { w } = await open(page, 1280, 'dark', { result: personView({ host: hosts.mixed }) })
  await page.route('**/api/commands/runner-remove/plan/stream', (route) =>
    fulfillPlanStream(route, {
      id: 'PLANRM',
      plan: {
        operation: 'runner remove',
        steps: [{ id: 'slot:claude-3', action: 'remove Runner', target: 'claude-3', current: 'offline', next: 'absent', noChange: false, afterStart: false }],
        confirm: true,
        readOnly: false,
      },
    }),
  )
  await page.route('**/api/plans/PLANRM/apply', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/x-ndjson',
      body:
        JSON.stringify({ event: 'result', result: { removed: 1, items: [{ id: 'slot:claude-3', action: 'remove Runner', target: 'claude-3', confirmed: true, state: 'absent' }] } }) + '\n',
    }),
  )
  await tab(page, 'Runners').click()
  await provider(page, 'claude').getByRole('button', { name: 'Remove Runners…' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('radio', { name: 'claude' })).toBeChecked()
  await dialog.getByRole('button', { name: 'Preview' }).click()
  await dialog.getByRole('button', { name: 'Apply' }).click()
  await expect(dialog.locator('[data-summary]')).toHaveText('Confirmed: the item was read back.')
  expect(w.statusReads).toBe(1)
  await dialog.locator('[data-slot=dialog-footer]').getByRole('button', { name: 'Close' }).click()
  await expect.poll(() => w.statusReads).toBe(2)
  await expectClean(w)
})

for (const theme of themes) {
  for (const width of widths) {
    test(`summary tiles and the Needs you filter at ${width}px, ${theme}`, async ({ page }) => {
      const { w, ops } = await open(page, width, theme, { result: personView({ repos: runRepos, host: hosts.mixed }) })
      const view = page.locator('[data-view=person]')
      await expect(tile(page, 'runs').getByRole('img')).toHaveAttribute(
        'aria-label',
        'Runs by conclusion: Succeeded 3, Failed 2, Running 2, Skipped or cancelled 1',
      )
      await expect(tile(page, 'runs').locator('[data-note=runs-incomplete]')).toHaveCount(0)
      await expect(tile(page, 'runners').locator('[data-slot=runner-counts]')).toHaveText('2 online · 1 busy')
      await expect(tile(page, 'runners').getByRole('meter', { name: 'Memory in use' })).toHaveAttribute('aria-valuenow', '67')
      await shot(page, `summary-tiles-${theme}-${width}`, false)

      // A selection of an Issue that does not need you goes when the
      // filter hides it.
      await page.getByRole('checkbox', { name: 'Select #101' }).check()
      await page.getByRole('checkbox', { name: 'Select #102' }).check()
      const toggle = tile(page, 'needs-you').getByRole('button', { name: 'Show only these' })
      await expect(toggle).toHaveAttribute('aria-pressed', 'false')
      await toggle.click()
      await expect(tile(page, 'needs-you').getByRole('button', { name: 'Showing only these' })).toHaveAttribute('aria-pressed', 'true')
      await expect(view.locator('[data-slot=filter]')).toContainText('Showing 3 Issues that need you')
      await expect(view.locator('article[data-issue]')).toHaveCount(3)
      await expect(view.locator('[data-repo]')).toHaveCount(2)
      await expect(item(page, 'acme/app', 101)).toHaveCount(0)
      await expect(page.getByRole('checkbox', { name: 'Select #102' })).toBeChecked()
      // The tiles still count the whole read.
      await expect(tile(page, 'issues').locator('[data-slot=tile-count]')).toHaveText('9Issues')
      await expectNoPageScroll(page)
      await shot(page, `summary-needs-you-${theme}-${width}`)

      // A resume follows the new labels: #102 leaves the filtered list.
      await page.getByRole('region', { name: 'Selected Issues' }).getByRole('button', { name: 'Resume selected' }).click()
      await expect.poll(() => ops.plans.length).toBe(1)
      expect(ops.plans[0].issues).toEqual([issueURL('acme/app', 102)])
      await expect(item(page, 'acme/app', 102)).toHaveCount(0)
      await expect(view.locator('[data-slot=filter]')).toContainText('Showing 2 Issues that need you')
      await expect(tile(page, 'needs-you').locator('[data-slot=tile-count]')).toHaveText('2pending or paused')

      await view.locator('[data-slot=filter]').getByRole('button', { name: 'Clear' }).click()
      await expect(view.locator('[data-slot=filter]')).toHaveCount(0)
      await expect(view.locator('article[data-issue]')).toHaveCount(9)
      expect(w.statusReads).toBe(1)
      await expectClean(w)
    })
  }
}

for (const theme of themes) {
  for (const width of [375, 1280]) {
    test(`the Runners tile without a Runner and with read errors at ${width}px, ${theme}`, async ({ page }) => {
      // Without a complete Runner the page lands on the Runner view.
      const { w } = await open(page, width, theme, { result: personView({ host: hosts.incomplete }) })
      await tab(page, 'Person').click()
      const runners = tile(page, 'runners')
      await expect(runners.locator('[data-state=no-runner]')).toHaveText('No Runner on this host')
      await shot(page, `summary-no-runner-${theme}-${width}`, false)
      // Enter: a mouse click right after the screenshot can stall in headless
      // Chromium while the Runner view mounts its animated tiles.
      await runners.getByRole('button', { name: 'Set up in Runners' }).press('Enter')
      await expect(tab(page, 'Runners')).toHaveAttribute('aria-selected', 'true')
      await expect(page.locator('[data-guide=no-runner]')).toBeVisible()

      w.status = { result: personView({ host: hosts.githubError }) }
      await page.getByRole('button', { name: 'Refresh' }).click()
      await tab(page, 'Person').click()
      await expect(runners.locator('[data-state=github-not-read]')).toHaveText(
        'Online and busy not read: github_error: listing the runners of acme/app: HTTP 502',
      )

      // Without admin GitHub's Runner list is not read: no measured zero.
      w.status = { result: personView({ host: noAdmin }) }
      await page.getByRole('button', { name: 'Refresh' }).click()
      await expect(runners.locator('[data-state=github-needs-admin]')).toHaveText(
        "Online and busy not read: GitHub's Runner list needs admin of acme/app.",
      )
      await expect(runners.locator('[data-slot=runner-counts]')).toHaveCount(0)
      await expectNoPageScroll(page)
      await shot(page, `summary-runner-no-admin-${theme}-${width}`, false)

      w.status = { result: personView({ host: hosts.machineStopped }) }
      await page.getByRole('button', { name: 'Refresh' }).click()
      await expect(runners.locator('[data-note=tile-error]')).toHaveText([
        'Runners could not be read: machine_stopped: the podman machine nibfin-dev is stopped',
        'Memory could not be read: machine_stopped: podman machine nibfin-dev is stopped; status does not start it',
      ])
      await expectNoPageScroll(page)
      await shot(page, `summary-runner-errors-${theme}-${width}`, false)
      await expectClean(w)
    })
  }
}
