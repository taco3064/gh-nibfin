import { mkdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

import { expect, test, type Page, type WebSocketRoute } from '@playwright/test'

import { event, liveData, liveUrl, runnerNames } from './live-site-fixtures'

// Screenshots and checks of the public site (#362) and its Live page and
// Home summary (#365), served from dist-site/ under its base path by vite
// preview. The Worker stream is mocked: no test reaches the network.
// Console errors fail the spec.

const outDir = fileURLToPath(new URL('../.screenshots/', import.meta.url))
mkdirSync(outDir, { recursive: true })

const widths = [375, 768, 1280, 1920]
const themes = ['dark', 'light'] as const
const pages = [
  { name: 'home', path: './' },
  { name: 'quick-start', path: 'quick-start/' },
  { name: 'how-it-works', path: 'how-it-works/' },
  { name: 'setup', path: 'setup/' },
  { name: 'cli', path: 'cli/' },
  { name: 'live', path: 'live/' },
]

type Theme = (typeof themes)[number]

// Worker is the mocked live stream: each connection gets greeting, and
// send reaches every open one.
class Worker {
  sockets: WebSocketRoute[] = []
  greeting: string[] = [event({ event: 'snapshot', at: '2026-11-25T08:00:00Z', result: liveData() }), event({ event: 'checked', at: new Date().toISOString() })]
  send(message: string) {
    for (const ws of this.sockets) ws.send(message)
  }
  close() {
    const open = this.sockets
    this.sockets = []
    for (const ws of open) void ws.close({ code: 1001, reason: 'going away' })
  }
}

async function mockWorker(page: Page): Promise<Worker> {
  const worker = new Worker()
  await page.routeWebSocket(liveUrl, (ws) => {
    worker.sockets.push(ws)
    for (const m of worker.greeting) ws.send(m)
  })
  return worker
}

// open loads a page with Starlight's stored theme, the mocked Worker and
// records console errors.
async function open(page: Page, path: string, theme: Theme, width: number, worker?: Worker): Promise<string[]> {
  if (!worker) await mockWorker(page)
  const errors: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') errors.push(message.text())
  })
  page.on('pageerror', (error) => errors.push(error.message))
  await page.addInitScript((value) => localStorage.setItem('starlight-theme', value), theme)
  await page.setViewportSize({ width, height: 900 })
  await page.goto(path)
  await page.evaluate(() => document.fonts.ready)
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
  return errors
}

async function expectNoPageScroll(page: Page) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
  expect(overflow).toBeLessThanOrEqual(0)
}

for (const { name, path } of pages) {
  for (const theme of themes) {
    test(`${name} ${theme}`, async ({ page }) => {
      for (const width of widths) {
        const errors = await open(page, path, theme, width)
        await expect(page.getByRole('img', { name: 'Nibfin', exact: true }).first()).toBeVisible()
        await expectNoPageScroll(page)
        await page.screenshot({ path: `${outDir}site-${name}-${theme}-${width}.png`, fullPage: true })
        expect(errors).toEqual([])
      }
    })
  }
}

test('keyboard reaches the skip link first', async ({ page }) => {
  const errors = await open(page, './', 'dark', 1280)
  await page.keyboard.press('Tab')
  await expect(page.locator(':focus')).toHaveText('Skip to content')
  expect(errors).toEqual([])
})

test('mobile menu', async ({ page }) => {
  for (const theme of themes) {
    const errors = await open(page, 'how-it-works/', theme, 375)
    await page.getByRole('button', { name: 'Menu' }).click()
    const nav = page.getByRole('navigation', { name: 'Main' })
    for (const label of ['Home', 'Quick start', 'How it works', 'Setup and management', 'CLI commands', 'Live']) {
      await expect(nav.getByRole('link', { name: label, exact: true })).toBeVisible()
    }
    await page.screenshot({ path: `${outDir}site-menu-${theme}-375.png` })
    expect(errors).toEqual([])
  }
})

test('search finds page text', async ({ page }) => {
  for (const theme of themes) {
    for (const width of [375, 1280]) {
      const errors = await open(page, './', theme, width)
      await page.getByRole('button', { name: 'Search' }).click()
      const dialog = page.getByRole('dialog')
      await dialog.getByRole('textbox', { name: 'Search' }).fill('pending')
      await expect(dialog.locator('.pagefind-ui__result').first()).toBeVisible()
      await expect(dialog.getByRole('link', { name: 'How it works' }).first()).toBeVisible()
      await page.screenshot({ path: `${outDir}site-search-${theme}-${width}.png` })
      expect(errors).toEqual([])
    }
  }
})

const repoView = (page: Page) => page.locator('[data-view=repo]')

async function expectNoOperation(page: Page) {
  const view = repoView(page)
  for (const a of ['pause', 'resume', 'init', 'upgrade', 'open-repo']) await expect(view.locator(`[data-action=${a}]`)).toHaveCount(0)
  await expect(view.locator('[data-slot=manual-entry]')).toHaveCount(0)
  await expect(view.getByRole('checkbox')).toHaveCount(0)
}

async function expectNoRunnerName(page: Page) {
  const text = await page.locator('body').innerText()
  for (const name of runnerNames) expect(text).not.toContain(name)
}

for (const theme of themes) {
  test(`live page states ${theme}`, async ({ page }) => {
    for (const width of widths) {
      const errors = await open(page, 'live/', theme, width)
      await expect(page.locator('[data-slot=live-connection]')).toHaveText('Live')
      await expect(page.locator('[data-slot=freshness]')).toContainText('Current')
      await expect(page.locator('[data-slot=summary-created] [data-slot=rolling-number]')).toHaveAttribute('data-value', '128')
      await expect(page.locator('[data-slot=runner-card]')).toHaveCount(5)
      await expect(page.locator('[data-slot=capacity-counts]')).toHaveText('5 runners · 3 busy · 1 idle · 1 offline')
      await expect(page.locator('[data-note=runners-error]')).toContainText('Runners of taco3064/gh-nibfin could not be read')
      await expect(page.locator('[data-slot=runner-issue]').first()).toHaveAttribute('href', 'https://github.com/taco3064/nibfin-core/issues/14')
      await expect(page.getByText('Busy; its job could not be matched.')).toBeVisible()
      await expect(repoView(page).locator('[data-slot=repo-link]')).toHaveAttribute('href', 'https://github.com/taco3064/nibfin-core')
      await expect(repoView(page).locator('[data-slot=dependency-graph]')).toContainText('Showing 54 of 54 open Issues')
      // The same node graph, read-only: closed blockers are markers and the
      // layout follows the width.
      const graph = repoView(page).getByRole('region', { name: 'Dependency graph', exact: true })
      await expect(graph).toHaveAttribute('data-direction', width < 700 ? 'TB' : 'LR')
      await expect(graph.locator('[data-slot=closed-blockers]').first()).toBeVisible()
      expect(await graph.locator('[data-slot=edges] path[data-edge]').count()).toBeGreaterThan(0)
      await expectNoOperation(page)
      await expectNoRunnerName(page)
      await expectNoPageScroll(page)
      await page.screenshot({ path: `${outDir}site-live-full-${theme}-${width}.png`, fullPage: true })
      expect(errors).toEqual([])
    }
  })
}

test('live page: runs dialog, links and the repository picker', async ({ page }) => {
  for (const theme of themes) {
    for (const width of [375, 1280]) {
      const errors = await open(page, 'live/', theme, width)
      const view = repoView(page)
      const item = view.locator('[data-issue="https://github.com/taco3064/nibfin-core/issues/67"]').first()
      const bar = view.locator('[data-issue="https://github.com/taco3064/nibfin-core/issues/66"] [data-slot=run-strip]').getByRole('img')
      await expect(bar).toHaveCount(1)
      await expect(bar).toHaveAccessibleName(/^Nibfin · taco3064\/nibfin-core#66 · shape, success, /)
      await expect(item.locator('[data-slot=run-strip]')).toContainText('No runs')
      await expect(view.locator('[data-slot=run-legend]')).toBeVisible()
      await item.getByRole('button', { name: /^Run history of #67/ }).click()
      const dialog = page.getByRole('dialog')
      await expect(dialog.locator('[data-note=truncated]')).toContainText('only the newest runs were read')
      await page.screenshot({ path: `${outDir}site-live-runs-${theme}-${width}.png` })
      await page.keyboard.press('Escape')
      await expect(dialog).toBeHidden()
      for (const href of await view.locator('a[href]').evaluateAll((as) => as.map((a) => a.getAttribute('href') ?? ''))) {
        expect(href).toMatch(/^https:\/\/github\.com\//)
      }

      await view.locator('[data-action=change-repo]').click()
      await expect(view.locator('[data-repo-entry]')).toHaveCount(3)
      await expect(view.locator('[data-slot=picker]')).not.toContainText('no permission reported')
      await page.screenshot({ path: `${outDir}site-live-picker-${theme}-${width}.png` })
      await view.locator('[data-repo-entry="taco3064/gh-nibfin"]').click()
      await expect(view.locator('[data-note=issues-error]')).toContainText('HTTP 502')
      await view.locator('[data-action=change-repo]').click()
      await view.locator('[data-repo-entry="taco3064/nibfin-e2e"]').click()
      await expect(view.locator('[data-empty=issues]')).toBeVisible()
      await expectNoOperation(page)
      await expectNoPageScroll(page)
      expect(errors).toEqual([])
    }
  }
})

test('live page updates without a reload, shows errors and reconnects', async ({ page }) => {
  const worker = await mockWorker(page)
  const errors = await open(page, 'live/', 'dark', 1280, worker)
  await expect(page.locator('[data-slot=live-connection]')).toHaveText('Live')
  await repoView(page).locator('[data-action=change-repo]').click()
  await repoView(page).locator('[data-repo-entry="taco3064/nibfin-e2e"]').click()
  expect(await page.evaluate(() => document.getAnimations().length)).toBeGreaterThan(0)

  worker.send(event({ event: 'snapshot', at: '2026-11-25T08:05:00Z', result: liveData(131, 1059, true) }))
  await expect(page.locator('[data-slot=summary-created] [data-slot=rolling-number]')).toHaveAttribute('data-value', '131')
  await expect(page.locator('[data-slot=summary-delivered] [data-slot=rolling-number]')).toHaveAttribute('data-value', '1059')
  await expect(page.locator('[data-slot=capacity-counts]')).toHaveText('5 runners · 4 busy · 0 idle · 1 offline')
  await expect(page.locator('[data-slot=runner-issue]').nth(1)).toHaveText(/taco3064\/nibfin-core#41/)
  // The chosen repository stays through a new snapshot.
  await expect(repoView(page).locator('[data-slot=repo-link]')).toHaveAttribute('href', 'https://github.com/taco3064/nibfin-e2e')
  await page.waitForTimeout(800)
  await page.screenshot({ path: `${outDir}site-live-updated-dark-1280.png` })

  worker.send(event({ event: 'error', source: 'github', error: { code: 'github_api_failed', message: 'GET search/issues: HTTP 502' } }))
  await expect(page.locator('[data-note=check-error]')).toContainText('GitHub check failed: github_api_failed: GET search/issues: HTTP 502')

  worker.greeting = [event({ event: 'snapshot', at: '2026-11-25T08:05:00Z', result: liveData(131, 1059, true) })]
  worker.close()
  await expect(page.locator('[data-slot=live-connection]')).toHaveText('Disconnected: reconnecting')
  await expect(page.locator('[data-note=kept]')).toBeVisible()
  await expect(page.locator('[data-slot=runner-card]')).toHaveCount(5)
  await page.screenshot({ path: `${outDir}site-live-disconnected-dark-1280.png` })
  await expect(page.locator('[data-slot=live-connection]')).toHaveText('Live', { timeout: 10_000 })
  await expect(page.locator('[data-note=kept]')).toHaveCount(0)
  await expectNoRunnerName(page)
  expect(errors).toEqual([])
})

test('live page respects reduced motion', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  const worker = await mockWorker(page)
  const errors = await open(page, 'live/', 'light', 1280, worker)
  await expect(page.locator('[data-slot=runner-card][data-runner-state=busy]')).toHaveCount(3)
  worker.send(event({ event: 'snapshot', at: '2026-11-25T08:05:00Z', result: liveData(131, 58, true) }))
  await expect(page.locator('[data-slot=summary-created] [data-slot=rolling-number]')).toHaveAttribute('data-value', '131')
  // Nothing moves: no keyframe animation and no transform transition. The
  // shared badges keep their short color transitions.
  const moving = await page.evaluate(() =>
    document
      .getAnimations()
      .filter((a) => !(a instanceof CSSTransition) || !['color', 'background-color', 'border-color', 'box-shadow'].includes(a.transitionProperty))
      .map((a) => (a instanceof CSSAnimation ? a.animationName : (a as CSSTransition).transitionProperty)),
  )
  expect(moving).toEqual([])
  expect(errors).toEqual([])
})

test('live page without data, stale and before a check', async ({ page }) => {
  const worker = await mockWorker(page)
  worker.greeting = []
  let errors = await open(page, 'live/', 'dark', 375, worker)
  await expect(page.locator('[data-slot=live-connection]')).toHaveText('Connecting…')
  await expect(page.locator('[data-slot=summary-created]')).toContainText('Not read yet')
  await expect(repoView(page)).toContainText('Reading repositories…')
  await page.screenshot({ path: `${outDir}site-live-empty-dark-375.png`, fullPage: true })
  worker.send(event({ event: 'error', source: 'github', error: { code: 'auth_unavailable', message: 'the Worker has no GITHUB_TOKEN secret' } }))
  await expect(page.locator('[data-note=check-error]')).toContainText('auth_unavailable')
  expect(errors).toEqual([])

  const stale = await mockWorker(page)
  stale.greeting = [event({ event: 'snapshot', at: '2026-11-25T08:00:00Z', result: liveData() }), event({ event: 'checked', at: '2026-11-25T08:00:00Z' })]
  errors = await open(page, 'live/', 'light', 768, stale)
  await expect(page.locator('[data-slot=freshness]')).toContainText('Stale: over 5 minutes old')
  await page.screenshot({ path: `${outDir}site-live-stale-light-768.png` })
  expect(errors).toEqual([])

  const unchecked = await mockWorker(page)
  unchecked.greeting = [event({ event: 'snapshot', at: '2026-11-25T08:00:00Z', result: liveData() })]
  errors = await open(page, 'live/', 'dark', 768, unchecked)
  await expect(page.locator('[data-slot=unconfirmed]')).toHaveText('Not confirmed by a GitHub check yet')
  expect(errors).toEqual([])
})

test('home live summary links the Live page', async ({ page }) => {
  for (const theme of themes) {
    const errors = await open(page, './', theme, 375)
    const summary = page.locator('[data-slot=live-summary]')
    await summary.scrollIntoViewIfNeeded()
    await expect(summary.locator('[data-slot=summary-delivered] [data-slot=rolling-number]')).toHaveAttribute('data-value', '57')
    await expect(summary.locator('[data-slot=live-connection]')).toHaveText('Live')
    await summary.screenshot({ path: `${outDir}site-home-live-${theme}-375.png` })
    await summary.getByRole('link', { name: 'See the live view' }).click()
    await expect(page).toHaveURL(/\/gh-nibfin\/live\/$/)
    expect(errors).toEqual([])
  }
})

test('search finds a flag on the CLI commands page', async ({ page }) => {
  const errors = await open(page, './', 'dark', 1280)
  await page.getByRole('button', { name: 'Search' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('textbox', { name: 'Search' }).fill('delete-login')
  await expect(dialog.getByRole('link', { name: 'CLI commands' }).first()).toBeVisible()
  await page.screenshot({ path: `${outDir}site-search-cli-dark-1280.png` })
  expect(errors).toEqual([])
})

test('the Setup and CLI pages link to anchors that exist', async ({ page }) => {
  const errors = await open(page, 'cli/', 'dark', 1280)
  const cliIds = await page.locator('[id]').evaluateAll((els) => els.map((e) => e.id))
  await expect(page.locator('[data-command="runner add"] [data-slot=usage]')).toContainText('gh nibfin runner add <claude|codex>')
  const cliLocal = await page.locator('.sl-markdown-content a[href^="#"]').evaluateAll((as) => as.map((a) => a.getAttribute('href')!.slice(1)))
  expect(cliLocal.length).toBeGreaterThan(0)
  for (const id of cliLocal) expect(cliIds).toContain(id)
  await open(page, 'setup/', 'dark', 1280)
  await expect(page.locator('.sl-markdown-content img')).toHaveCount(3)
  const toCli = await page.locator('.sl-markdown-content a[href*="cli/#"]').evaluateAll((as) => as.map((a) => a.getAttribute('href')!.split('#')[1]))
  expect(toCli.length).toBeGreaterThan(0)
  for (const id of toCli) expect(cliIds).toContain(id)
  expect(errors).toEqual([])
})
