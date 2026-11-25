import { expect, test, type Page } from '@playwright/test'

import { edgeCount, outsideCount, platform, repoStatus } from './repo-fixtures'
import { expectClean, expectNonce, prepare, shot, storageKey, widths, type Watch } from './support'

// Screenshots and checks of the repository view (#353): the picker, the
// summary, the dependency graph with its search and filters, and init and
// upgrade through the operation dialog. The real Go server serves the page
// with its CSP; every API answer is a fictional mock.

const themes = ['dark', 'light'] as const
type Theme = (typeof themes)[number]

const repoFlag = {
  name: 'repo',
  shorthand: 'R',
  type: 'string',
  default: '',
  usage: 'the repository as [HOST/]OWNER/REPO (default: the current directory)',
  required: false,
  param: 'repo',
}
const yes = { name: 'yes', shorthand: 'y', type: 'bool', default: 'false', usage: 'apply without asking for confirmation', required: false }
const commands = [
  { path: 'init', api: 'init', short: 'Set up a repository for Nibfin', long: '', flags: [repoFlag, yes], args: [] },
  { path: 'upgrade', api: 'upgrade', short: 'Upgrade the Nibfin workflows of a repository', long: '', flags: [repoFlag, yes], args: [] },
]

interface Ops {
  plans: { api: string; body: Record<string, unknown> }[]
  commandLoads: number
}

// mockOps answers the command schema and the init and upgrade plans and
// applies; the status read falls through to prepare's mock.
async function mockOps(page: Page): Promise<Ops> {
  const ops: Ops = { plans: [], commandLoads: 0 }
  await page.route('**/api/commands', (route) => {
    ops.commandLoads++
    return route.fulfill({ json: { commands } })
  })
  await page.route('**/api/commands/*/plan', async (route) => {
    const api = new URL(route.request().url()).pathname.split('/')[3]
    if (api === 'status') return route.fallback()
    const body = route.request().postDataJSON() as Record<string, unknown>
    ops.plans.push({ api, body })
    await route.fulfill({
      json: {
        id: `PLAN${ops.plans.length}`,
        plan: {
          operation: api,
          repo: body.repo,
          steps: [{ id: 'workflows', action: 'write workflows', target: `${String(body.repo)} .github/workflows`, current: 'absent', next: 'present', noChange: false, afterStart: false }],
          confirm: false,
          readOnly: false,
        },
      },
    })
  })
  await page.route('**/api/plans/**', async (route) => {
    if (route.request().method() === 'DELETE') return route.fulfill({ status: 204 })
    const line = JSON.stringify({
      event: 'result',
      result: { items: [{ id: 'workflows', action: 'write workflows', target: '.github/workflows', confirmed: true, state: 'present' }] },
    })
    await route.fulfill({ status: 200, contentType: 'application/x-ndjson', body: line + '\n' })
  })
  return ops
}

interface Opened {
  w: Watch
  ops: Ops
}

async function open(page: Page, width: number, theme: Theme, height = 900): Promise<Opened> {
  await page.setViewportSize({ width, height })
  await page.addInitScript(([k, t]) => localStorage.setItem(k, t), [storageKey, theme])
  const w = await prepare(page)
  w.repoStatus = { ...repoStatus }
  const ops = await mockOps(page)
  const res = await page.goto('/#proof=SCREENSHOT')
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
  await expectNonce(page, res)
  await expect(page.locator('[data-view=person]')).toBeVisible()
  await tab(page, 'Repositories').click()
  await expect(picker(page)).toBeVisible()
  await page.evaluate(() => document.fonts.ready.then(() => undefined))
  await expectNoPageScroll(page)
  return { w, ops }
}

async function expectNoPageScroll(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
}

const tab = (page: Page, name: string) => page.getByRole('tablist', { name: 'Views' }).getByRole('tab', { name })
const picker = (page: Page) => page.locator('[data-view=repo] [data-slot=picker]')
const graphRegion = (page: Page) => page.getByRole('region', { name: 'Dependency graph', exact: true })
const freshness = (page: Page) => page.locator('[data-slot=freshness]')
const lastBody = (w: Watch) => w.statusBodies.at(-1)

async function openManual(page: Page, repo: string) {
  const form = page.locator('[data-slot=manual-entry]')
  await form.getByRole('textbox').fill(repo)
  await form.getByRole('button', { name: 'Open' }).click()
}

async function change(page: Page) {
  await page.getByRole('button', { name: 'Change' }).click()
}

async function pick(page: Page, repo: string) {
  await picker(page).locator(`[data-repo-entry="${repo}"]`).click()
}

// edgesDrawn waits for the drawn edges to settle at n.
async function edgesDrawn(page: Page, n: number) {
  await expect(page.locator('[data-slot=edges] path[data-edge]')).toHaveCount(n)
}

for (const theme of themes) {
  for (const width of widths) {
    test(`picker and the large graph at ${width}px, ${theme}`, async ({ page }) => {
      const { w } = await open(page, width, theme)
      await expect(picker(page).locator('[data-slot=picker-host]')).toHaveText('github.com')
      await expect(picker(page).locator('[data-repo-entry]')).toHaveCount(5)
      await expect(picker(page).locator('[data-repo-entry="acme/app"]')).toContainText('Runs here')
      await shot(page, `repo-picker-${theme}-${width}`)

      await picker(page).getByRole('searchbox', { name: 'Search repositories' }).fill('PLAT')
      await expect(picker(page).locator('[data-repo-entry]')).toHaveCount(1)
      await shot(page, `repo-picker-search-${theme}-${width}`, false)
      await pick(page, 'acme/platform')
      expect(lastBody(w)).toEqual({ repo: 'github.com/acme/platform' })

      await expect(page.locator('[data-slot=repo-summary]')).toBeVisible()
      await expect(picker(page)).toHaveAttribute('data-state', 'collapsed')
      await expect(picker(page)).toContainText('github.com/acme/platform')
      const nodes = page.locator('[data-slot=graph-region] [data-node]')
      await expect(page.locator('[data-slot=graph-count]')).toHaveText(`Showing ${platform.length} of ${platform.length} open Issues · ${outsideCount(platform, 'acme/platform')} shown as context`)
      await edgesDrawn(page, edgeCount(platform, 'acme/platform'))
      // Every listed Issue appears once, in the graph or below it.
      await expect(page.locator('[data-view=repo] article[data-issue]')).toHaveCount(platform.length)
      expect(await nodes.count()).toBeGreaterThan(100)
      await expect(page.locator('[data-note=cycle]')).toHaveText([
        '#120 and #121 block each other in a cycle.',
        '#122, #123 and #124 block each other in a cycle.',
      ])
      await expect(page.locator('[data-note=unread]')).toHaveText('Blockers of #125 could not be read; edges may be missing.')
      await expect(page.locator('[data-slot=unlinked] article[data-issue]')).toHaveCount(7 + 25 + 1)
      // The graph region scrolls by itself; the page does not.
      await expectNoPageScroll(page)
      const region = graphRegion(page)
      expect(await region.evaluate((el) => el.scrollWidth > el.clientWidth)).toBe(true)
      await region.focus()
      await expect(region).toBeFocused()
      await shot(page, `repo-graph-top-${theme}-${width}`, false)
      await shot(page, `repo-graph-${theme}-${width}`)
      await expectClean(w)
    })
  }
}

for (const theme of themes) {
  for (const width of [375, 1280]) {
    test(`search and filters keep blockers as context at ${width}px, ${theme}`, async ({ page }) => {
      const { w } = await open(page, width, theme)
      await pick(page, 'acme/platform')
      await expect(page.locator('[data-slot=repo-summary]')).toBeVisible()
      const search = page.getByRole('searchbox', { name: 'Search Issues' })
      await search.fill('chain 2 step 6')
      // Chain 2 step 6 (#21) depends on #20 … #16, then acme/infra#201.
      await expect(page.locator('[data-slot=graph-count]')).toHaveText(`Showing 1 of ${platform.length} open Issues · 6 shown as context`)
      const ctx = page.locator('[data-slot=graph-region] [data-context]')
      await expect(ctx).toHaveCount(6)
      await expect(ctx.first()).toContainText('Context: blocks a shown Issue')
      await expect(page.locator('[data-blocker="https://github.com/acme/infra/issues/201"]')).toContainText('Blocks #16')
      await edgesDrawn(page, 6)
      await expectNoPageScroll(page)
      await shot(page, `repo-graph-search-${theme}-${width}`)

      await search.fill('')
      await page.locator('[data-filter=stage-done]').click()
      await page.locator('[data-filter=paused]').click()
      // Done (labels delivered) and paused: numbers ≡ 2 (mod 4) and ≡ 0 (mod 11).
      const expected = platform.filter((i) => i.number % 4 === 2 && i.number % 11 === 0).length
      await expect(page.locator('[data-slot=graph-count]')).toContainText(`Showing ${expected} of ${platform.length} open Issues`)
      await expect(page.locator('[data-filter=paused]')).toHaveAttribute('aria-pressed', 'true')
      await expectNoPageScroll(page)
      await shot(page, `repo-graph-filters-${theme}-${width}`)

      await page.locator('[data-filter=stage-done]').click()
      await page.locator('[data-filter=paused]').click()
      await page.locator('[data-filter=blocked]').click()
      await search.fill('nothing matches this')
      await expect(page.locator('[data-empty=matches]')).toHaveText('No open Issue matches the search and filters.')
      await page.getByRole('button', { name: 'Reset' }).click()
      await expect(page.locator('[data-slot=graph-count]')).toHaveText(`Showing ${platform.length} of ${platform.length} open Issues · ${outsideCount(platform, 'acme/platform')} shown as context`)
      await expectClean(w)
    })
  }
}

test('a listed entry opens on the host of the read that listed it', async ({ page }) => {
  const { w } = await open(page, 1280, 'dark')
  await pick(page, 'acme/app')
  expect(lastBody(w)).toEqual({ repo: 'github.com/acme/app' })
  await expect(page.locator('[data-slot=repo-link]')).toHaveAttribute('href', 'https://github.com/acme/app')

  await change(page)
  await openManual(page, 'ghe.example.test/corp/api')
  expect(lastBody(w)).toEqual({ repo: 'ghe.example.test/corp/api' })
  const link = page.locator('[data-slot=repo-link]')
  await expect(link).toHaveAttribute('href', 'https://ghe.example.test/corp/api')
  await expect(link).toHaveAttribute('rel', 'noreferrer')
  await expect(page.locator('[data-slot=repo-summary]')).toContainText('on ghe.example.test')

  await change(page)
  await expect(picker(page).locator('[data-slot=picker-host]')).toHaveText('ghe.example.test')
  await expect(picker(page).locator('[data-repo-entry]')).toHaveCount(2)
  await expect(picker(page).locator('[data-repo-entry="corp/api"]')).toHaveAttribute('aria-pressed', 'true')
  await shot(page, 'repo-picker-enterprise-dark-1280', false)
  await pick(page, 'corp/web')
  expect(lastBody(w)).toEqual({ repo: 'ghe.example.test/corp/web' })
  await expect(page.locator('[data-slot=repo-link]')).toHaveAttribute('href', 'https://ghe.example.test/corp/web')
  await expectClean(w)
})

for (const theme of themes) {
  for (const width of [375, 1280]) {
    test(`manual entry, read errors and every summary state at ${width}px, ${theme}`, async ({ page }) => {
      const { w } = await open(page, width, theme)
      const form = page.locator('[data-slot=manual-entry]')
      const reads = w.statusReads
      await form.getByRole('button', { name: 'Open' }).click()
      await expect(form).toContainText('Enter a repository.')
      await expect(form.getByRole('textbox')).toHaveAttribute('aria-invalid', 'true')
      expect(w.statusReads).toBe(reads)
      await openManual(page, 'not a repo')
      expect(lastBody(w)).toEqual({ repo: 'not a repo' })
      await expect(page.locator('[data-note=repo-read-error]')).toHaveText(
        'not a repo could not be read: invalid_repo: not a repo is not a repository; use [HOST/]OWNER/REPO',
      )
      await expectNoPageScroll(page)
      await shot(page, `repo-invalid-${theme}-${width}`)

      const summary = page.locator('[data-slot=repo-summary]')
      const run = summary.locator('[data-slot=nibfin-run]')
      await change(page)
      await openManual(page, 'acme/new')
      await expect(summary.locator('[data-image]')).toHaveText('No Nibfin workflows')
      await expect(run).toContainText('Not initialized')
      await expect(run).toContainText('No generated nibfin-issue.yml on the default branch')
      await expect(summary.locator('[data-action=init]')).toBeEnabled()
      await expect(summary.locator('[data-action=upgrade]')).toHaveCount(0)
      await expect(page.locator('[data-empty=issues]')).toHaveText('This repository has no open Issues.')
      await expectNoPageScroll(page)
      await shot(page, `repo-summary-init-${theme}-${width}`)

      await change(page)
      await pick(page, 'acme/app')
      await expect(summary.locator('[data-image]')).toHaveText('Latest imageghcr.io/acme/nibfin:v1.4.0')
      await expect(run.locator('[data-state=succeeded]')).toContainText('A Nibfin run succeeded after init')
      await expect(run.locator('[data-slot=init]')).toContainText('Init PR #42')
      await expect(run.getByRole('link', { name: /Successful run/ })).toHaveAttribute('href', 'https://github.com/acme/app/actions/runs/8801')
      await expect(summary.locator('[data-action=upgrade]')).toBeEnabled()
      await expect(summary.locator('[data-action=init]')).toHaveCount(0)
      await edgesDrawn(page, 3)
      await expectNoPageScroll(page)
      await shot(page, `repo-summary-app-${theme}-${width}`)

      await change(page)
      await pick(page, 'acme/docs')
      await expect(summary.locator('[data-image]')).toContainText('Not the latest image')
      await expect(run.locator('[data-state=not-succeeded]')).toContainText('No successful Nibfin run since init')
      // The docs read carries its own list error for the picker.
      await change(page)
      await expect(picker(page).locator('[data-note=repos-error]')).toHaveText(
        'Repository list could not be read: github_error: listing the repositories of the account: HTTP 503',
      )
      await expectNoPageScroll(page)
      await shot(page, `repo-summary-docs-${theme}-${width}`)
      await page.getByRole('button', { name: 'Cancel' }).click()

      await change(page)
      await openManual(page, 'github.com/acme/infra')
      await expect(summary.locator('[data-image]')).toContainText('Image could not be read')
      await expect(run.locator('[data-state=error]')).toContainText('github_api_failed: list the commits of nibfin-issue.yml of acme/infra: HTTP 502')
      await expect(summary.locator('[data-action=init]')).toBeEnabled()
      await expect(summary.locator('[data-action=upgrade]')).toBeEnabled()
      await expect(page.locator('[data-note=issues-error]')).toHaveText('Issues could not be read: github_error: listing the Issues of acme/infra: HTTP 502')
      await expectNoPageScroll(page)
      await shot(page, `repo-summary-infra-${theme}-${width}`)

      await change(page)
      await openManual(page, 'github.com/acme/site')
      await expect(summary.locator('[data-image]')).toHaveText('Peer set: no image pin')
      await expect(run.locator('[data-slot=init]')).toContainText('Init commit c0ffee1')
      await expectClean(w, ['Failed to load resource: the server responded with a status of 400'])
    })
  }
}

test('the pause gate follows the repository permission', async ({ page }) => {
  const { w } = await open(page, 1280, 'light')
  await pick(page, 'acme/docs')
  const it = page.locator('article[data-issue="https://github.com/acme/docs/issues/7"]')
  await expect(it.getByRole('button', { name: 'Pause' })).toBeDisabled()
  await expect(it).toContainText('Needs triage permission or higher; this account has READ on acme/docs.')
  await expectClean(w)
})

for (const theme of themes) {
  test(`init and upgrade open the dialog with the repository filled in, ${theme}`, async ({ page }) => {
    const { w, ops } = await open(page, 375, theme)
    await openManual(page, 'acme/new')
    await page.locator('[data-action=init]').click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()
    await expect(dialog.getByRole('textbox', { name: /repo/ })).toHaveValue('github.com/acme/new')
    await shot(page, `repo-init-dialog-${theme}-375`, false)
    await dialog.getByRole('button', { name: 'Preview' }).click()
    expect(ops.plans.at(-1)).toEqual({ api: 'init', body: { repo: 'github.com/acme/new' } })
    await dialog.getByRole('button', { name: 'Apply' }).click()
    await expect(dialog.locator('[data-summary]')).toBeVisible()
    const before = w.statusReads
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    // Closing after an apply reads the repository once again.
    await expect.poll(() => w.statusReads).toBe(before + 1)
    expect(lastBody(w)).toEqual({ repo: 'acme/new' })

    await change(page)
    await pick(page, 'acme/app')
    await page.locator('[data-action=upgrade]').click()
    await expect(dialog.getByRole('textbox', { name: /repo/ })).toHaveValue('github.com/acme/app')
    await dialog.getByRole('button', { name: 'Preview' }).click()
    expect(ops.plans.at(-1)).toEqual({ api: 'upgrade', body: { repo: 'github.com/acme/app' } })
    await shot(page, `repo-upgrade-preview-${theme}-375`, false)
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    expect(ops.commandLoads).toBe(1)
    await expectClean(w)
  })
}

test('the header freshness and Refresh follow the repository read', async ({ page }) => {
  const { w } = await open(page, 1280, 'dark')
  // Before a repository is open, Refresh reads the person view.
  await page.getByRole('button', { name: 'Refresh' }).click()
  await expect.poll(() => w.statusBodies.length).toBe(2)
  expect(lastBody(w)).toEqual({})

  await pick(page, 'acme/platform')
  await expect(page.locator('[data-slot=repo-summary]')).toBeVisible()
  await expect(freshness(page)).toHaveAttribute('data-state', 'current')
  const personTime = await (async () => {
    await tab(page, 'Person').click()
    return freshness(page).locator('time').getAttribute('datetime')
  })()
  await tab(page, 'Repositories').click()
  // The chosen repository survives the tab switch.
  await expect(page.locator('[data-slot=repo-summary]')).toBeVisible()
  const repoTime = await freshness(page).locator('time').getAttribute('datetime')
  expect(repoTime).not.toBe(personTime)

  const reads = w.statusReads
  await page.getByRole('button', { name: 'Refresh' }).click()
  await expect.poll(() => w.statusReads).toBe(reads + 1)
  expect(lastBody(w)).toEqual({ repo: 'github.com/acme/platform' })

  // A failed refresh keeps the graph and shows the error in the view.
  w.repoStatus['github.com/acme/platform'] = { error: { code: 'github_error', message: 'listing the Issues: HTTP 502' }, httpStatus: 502 }
  await page.getByRole('button', { name: 'Refresh' }).click()
  await expect(page.locator('[data-note=repo-read-error]')).toHaveText('Refresh failed: github_error: listing the Issues: HTTP 502')
  await expect(page.locator('[data-note=read-error]')).toHaveCount(0)
  await expect(graphRegion(page)).toBeVisible()
  await shot(page, 'repo-refresh-failed-dark-1280', false)

  await tab(page, 'Person').click()
  await page.getByRole('button', { name: 'Refresh' }).click()
  await expect.poll(() => lastBody(w)).toEqual({})
  await expectClean(w, ['Failed to load resource: the server responded with a status of 502'])
})

test('the reading states of the list and of a repository', async ({ page }) => {
  await page.setViewportSize({ width: 768, height: 900 })
  await page.addInitScript(([k, t]) => localStorage.setItem(k, t), [storageKey, 'light'])
  const w = await prepare(page)
  w.repoStatus = { ...repoStatus }
  await mockOps(page)
  let release!: () => void
  let held = new Promise<void>((r) => (release = r))
  await page.route('**/api/commands/status/plan', async (route) => {
    await held
    await route.fallback()
  })
  await page.goto('/#proof=SCREENSHOT')
  await tab(page, 'Repositories').click()
  await expect(picker(page).locator('[data-state=list-reading]')).toHaveText('Reading repositories…')
  await shot(page, 'repo-list-reading-light-768', false)
  release()
  await expect(picker(page).locator('[data-repo-entry]')).toHaveCount(5)

  held = new Promise<void>((r) => (release = r))
  await pick(page, 'acme/app')
  await expect(page.locator('[data-state=repo-reading]')).toHaveText('Reading github.com/acme/app…')
  await expect(page.getByRole('button', { name: 'Refreshing…' })).toBeDisabled()
  await shot(page, 'repo-reading-light-768', false)
  release()
  await expect(page.locator('[data-slot=repo-summary]')).toBeVisible()
  await expectClean(w)
})

// A slow answer for a repository no longer chosen is dropped.
test('a stale repository answer is dropped', async ({ page }) => {
  const { w } = await open(page, 1280, 'dark')
  let release!: () => void
  const held = new Promise<void>((r) => (release = r))
  await page.route('**/api/commands/status/plan', async (route) => {
    const body = route.request().postDataJSON() as { repo?: string }
    if (body.repo === 'github.com/acme/docs') await held
    await route.fallback()
  })
  await pick(page, 'acme/docs')
  await change(page)
  await pick(page, 'acme/app')
  await expect(page.locator('[data-slot=repo-link]')).toHaveAttribute('href', 'https://github.com/acme/app')
  release()
  await expect.poll(() => w.statusBodies.length).toBe(3)
  await page.waitForTimeout(200)
  await expect(page.locator('[data-slot=repo-link]')).toHaveAttribute('href', 'https://github.com/acme/app')
  await expectClean(w)
})

