import { mkdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

import { expect, test, type Page, type WebSocketRoute } from '@playwright/test'

import { expectDiagramFits } from './diagram-fit'
import { event, liveData, liveUrl, runnerNames, trendCounts, withoutDays, withoutIssues } from './live-site-fixtures'

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

// The site's diagrams (#456–#460, #474). Each figure shows exactly one
// presentation that fits the box it has: the left-to-right drawing, the
// top-to-bottom one or the stacked layout, whichever comes first among those
// that fit. Nothing scrolls sideways, and no text leaves its node, pill or
// block. At the site's widths the stage swimlane and the architecture are too
// wide for a drawing at 80% and show the stacked layout.
const siteDiagrams = [
  { page: 'how-it-works/', name: 'stages', caption: 'The stages, by role', items: 10, first: 'Split into Issues', shown: ['stacked'] },
  {
    page: 'how-it-works/',
    name: 'labels',
    caption: 'Label states, checked in order',
    items: 34,
    first: 'The Issue is closed. Lane: Checks, in order. Leads to closed (yes)',
    shown: ['TB', 'stacked'],
  },
  { page: 'how-it-works/', name: 'architecture', caption: 'Where Nibfin runs', items: 10, first: 'Login volume: The provider CLI', shown: ['stacked'] },
  { page: './', name: 'home-flow', caption: 'Who acts at each step', items: 8, first: 'Issue: scope or request label', shown: ['LR', 'TB'] },
]

for (const theme of themes) {
  test(`site diagrams fit their box ${theme}`, async ({ page }) => {
    for (const path of [...new Set(siteDiagrams.map((d) => d.page))]) {
      for (const width of widths) {
        const errors = await open(page, path, theme, width)
        const here = siteDiagrams.filter((d) => d.page === path)
        await expect(page.locator('main figure[data-slot=diagram]')).toHaveCount(here.length)
        for (const d of here) {
          const figure = page.locator('figure[data-slot=diagram]', { has: page.getByText(d.caption, { exact: true }) })
          await expect(figure).toHaveCount(1)
          await expect(figure).toBeVisible()
          await expect(figure.locator('ol.sr-only > li')).toHaveCount(d.items)
          await expect(figure.locator('ol.sr-only > li').first()).toContainText(d.first)
          const shown = await expectDiagramFits(figure, `${d.name} ${width}px ${theme}`)
          expect(d.shown, `${d.name} ${width}px shows ${shown}`).toContain(shown)
          await figure.screenshot({ path: `${outDir}site-diagram-${d.name}-${theme}-${width}.png` })
        }
        await expectNoPageScroll(page)
        expect(errors).toEqual([])
      }
    }
  })
}

// The architecture figure sits under its heading.
test('how-it-works architecture diagram sits under its heading', async ({ page }) => {
  const errors = await open(page, 'how-it-works/', 'dark', 1280)
  await expect(page.getByRole('heading', { name: 'Architecture', exact: true })).toBeVisible()
  const figure = page.locator('main [data-slot=diagram]').filter({ hasText: 'Where Nibfin runs' })
  await expect(figure).toHaveCount(1)
  const below = await figure.evaluate((f) => {
    const h = document.getElementById('architecture')
    return h !== null && Boolean(h.compareDocumentPosition(f) & Node.DOCUMENT_POSITION_FOLLOWING)
  })
  expect(below).toBe(true)
  expect(errors).toEqual([])
})

test('keyboard reaches the skip link first', async ({ page }) => {
  const errors = await open(page, './', 'dark', 1280)
  await page.keyboard.press('Tab')
  await expect(page.locator(':focus')).toHaveText('Skip to content')
  expect(errors).toEqual([])
})

const navLabels = ['Home', 'Quick start', 'How it works', 'Setup and management', 'CLI commands', 'Live']

test('mobile menu', async ({ page }) => {
  for (const theme of themes) {
    for (const { name, path } of [
      { name: 'how-it-works', path: 'how-it-works/' },
      { name: 'home', path: './' },
    ]) {
      const errors = await open(page, path, theme, 375)
      await page.getByRole('button', { name: 'Menu' }).click()
      const nav = page.getByRole('navigation', { name: 'Main' })
      for (const label of navLabels) {
        await expect(nav.getByRole('link', { name: label, exact: true })).toBeVisible()
      }
      await page.screenshot({ path: `${outDir}site-menu-${name}-${theme}-375.png` })
      expect(errors).toEqual([])
    }
  }
})

test('home shows the site navigation on a wide screen', async ({ page }) => {
  for (const theme of themes) {
    const errors = await open(page, './', theme, 1280)
    await expect(page.getByRole('button', { name: 'Menu' })).toBeHidden()
    const nav = page.getByRole('navigation', { name: 'Main' })
    for (const label of navLabels) {
      await expect(nav.getByRole('link', { name: label, exact: true })).toBeVisible()
    }
    await expect(page.locator('.hero')).toBeVisible()
    await expectNoPageScroll(page)
    await page.screenshot({ path: `${outDir}site-home-nav-${theme}-1280.png` })
    expect(errors).toEqual([])
  }
})

// commandIds are the ids of the generated command sections' headings.
async function commandIds(page: Page): Promise<string[]> {
  return page.locator('[data-command] > h2[id]').evaluateAll((hs) => hs.map((h) => h.id))
}

test('the CLI page lists every command on this page', async ({ page }) => {
  for (const theme of themes) {
    let errors = await open(page, 'cli/', theme, 1280)
    const ids = await commandIds(page)
    expect(ids.length).toBeGreaterThan(0)
    const allIds = await page.locator('[id]').evaluateAll((els) => els.map((e) => e.id))
    const desktop = page.locator('.right-sidebar starlight-toc')
    await expect(desktop).toBeVisible()
    let hrefs = await desktop.locator('a[href^="#"]').evaluateAll((as) => as.map((a) => a.getAttribute('href')!.slice(1)))
    // Overview, then each command in the CLI's order, then Troubleshooting.
    expect(hrefs[0]).toBe('_top')
    expect(hrefs.slice(1, 1 + ids.length)).toEqual(ids)
    expect(hrefs[1 + ids.length]).toBe('troubleshooting')
    for (const id of hrefs.slice(1)) expect(allIds).toContain(id)
    await desktop.screenshot({ path: `${outDir}site-cli-toc-${theme}-1280.png` })
    expect(errors).toEqual([])

    errors = await open(page, 'cli/', theme, 375)
    const mobile = page.locator('mobile-starlight-toc')
    await mobile.locator('summary').click()
    hrefs = await mobile.locator('a[href^="#"]').evaluateAll((as) => as.map((a) => a.getAttribute('href')!.slice(1)))
    expect(hrefs.slice(1, 1 + ids.length)).toEqual(ids)
    await expect(mobile.getByRole('link', { name: `gh nibfin ${(await page.locator('[data-command]').first().getAttribute('data-command'))!}`, exact: true })).toBeVisible()
    await page.screenshot({ path: `${outDir}site-cli-toc-${theme}-375.png` })
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

// The stage track of the busy card of runner n (1-based), by its place.
const track = (page: Page, n: number) => page.locator('[data-slot=runner-card]').nth(n - 1).locator('[data-slot=runner-stage]')

async function expectTrack(page: Page, n: number, states: string, current: string | null) {
  const steps = track(page, n).locator('[data-slot=run-stage-track] li')
  await expect(steps).toHaveText([/^Scope/, /^Shape/, /^Deliver/, /^Review/, /^Finish/])
  expect((await steps.evaluateAll((lis) => lis.map((li) => li.getAttribute('data-state')?.[0]))).join('')).toBe(states)
  const cur = track(page, n).locator('[aria-current=step]')
  if (current === null) await expect(cur).toHaveCount(0)
  else await expect(cur).toHaveAttribute('data-step', current)
}

for (const theme of themes) {
  test(`live page delivered trend and runner stage tracks ${theme}`, async ({ page }) => {
    const worker = await mockWorker(page)
    worker.greeting = [event({ event: 'snapshot', at: '2026-11-25T08:00:00Z', result: liveData(128, 57, true) }), event({ event: 'checked', at: new Date().toISOString() })]
    for (const width of widths) {
      const errors = await open(page, 'live/', theme, width, worker)
      const trend = page.locator('[data-slot=summary] [data-slot=delivered-trend]')
      await expect(trend).toBeVisible()
      // The trend has its own row below both stat cards, not inside either.
      await expect(page.locator('[data-slot=summary-delivered] [data-slot=delivered-trend], [data-slot=summary-created] [data-slot=delivered-trend]')).toHaveCount(0)
      const row = await trend.locator('xpath=..').boundingBox()
      const cards = await page.locator('[data-slot=summary] dl').boundingBox()
      expect(row && cards && row.y >= cards.y + cards.height).toBe(true)
      // From 768 px the row spans both columns.
      if (width >= 768) expect(Math.abs((row?.width ?? 0) - (cards?.width ?? 0))).toBeLessThanOrEqual(1)
      await expect(trend.locator('figcaption')).toHaveText(`Delivered per day, last 7 days (${trendCounts.reduce((a, b) => a + b, 0)} in total)`)
      const days = trend.locator('[data-slot=trend-day]')
      await expect(days).toHaveCount(7)
      expect(await days.evaluateAll((lis) => lis.map((li) => Number(li.getAttribute('data-value'))))).toEqual(trendCounts)
      // Each day's date and exact count are text, today last.
      await expect(days.first()).toContainText('Sat 21 Nov: 4 delivered')
      await expect(days.last()).toContainText('Fri 27 Nov: 3 delivered (today)')
      // The day without a delivery has only the baseline.
      await expect(days.nth(2).locator('[data-slot=trend-bar]')).toHaveCount(0)
      await expect(days.nth(4).locator('[data-slot=trend-bar]')).toHaveAttribute('style', 'height: 100%;')

      await expectTrack(page, 1, 'cccuu', 'deliver')
      await expect(track(page, 2).locator('[data-slot=runner-waking]')).toHaveText(/^Waking/)
      await expectTrack(page, 2, 'uuuuu', null)
      await expectTrack(page, 4, 'ccuuu', 'shape')
      // Idle and offline cards have no track.
      await expect(page.locator('[data-slot=runner-card][data-runner-state=idle], [data-slot=runner-card][data-runner-state=offline]').locator('[data-slot=run-stage-track]')).toHaveCount(0)
      await expectNoPageScroll(page)
      await expectNoRunnerName(page)
      await page.locator('[data-slot=summary]').screenshot({ path: `${outDir}site-live-trend-${theme}-${width}.png` })
      await page.locator('[data-slot=capacity]').screenshot({ path: `${outDir}site-live-capacity-${theme}-${width}.png` })
      expect(errors).toEqual([])
    }
  })
}

test('live page without a trend shows no chart and an unknown stage as text', async ({ page }) => {
  const worker = await mockWorker(page)
  const data = withoutDays(liveData())
  const [first] = data.runners[0].runners
  first.job = first.job && { ...first.job, title: 'Nibfin · taco3064/nibfin-core#14 · accept', stage: 'accept' }
  worker.greeting = [event({ event: 'snapshot', at: '2026-11-25T08:00:00Z', result: data }), event({ event: 'checked', at: new Date().toISOString() })]
  for (const theme of themes) {
    for (const width of widths) {
      const errors = await open(page, 'live/', theme, width, worker)
      await expect(page.locator('[data-slot=summary-delivered] [data-slot=rolling-number]')).toHaveAttribute('data-value', '57')
      await expect(page.locator('[data-slot=delivered-trend]')).toHaveCount(0)
      await expect(track(page, 1)).toHaveText('accept')
      await expect(track(page, 1).locator('[data-slot=run-stage-track]')).toHaveCount(0)
      await expectNoPageScroll(page)
      await page.locator('[data-slot=summary]').screenshot({ path: `${outDir}site-live-no-trend-${theme}-${width}.png` })
      expect(errors).toEqual([])
    }
  }
})

test('live page Issue lists have no list markers', async ({ page }) => {
  for (const theme of themes) {
    for (const width of [375, 1280]) {
      const errors = await open(page, 'live/', theme, width)
      for (const slot of ['unlinked', 'unread']) {
        const list = repoView(page).locator(`[data-slot=${slot}] > ul`)
        await expect(list).toBeVisible()
        expect(await list.evaluate((ul) => getComputedStyle(ul).paddingLeft)).toBe('0px')
        const markers = await list.locator('li').evaluateAll((lis) => lis.map((li) => getComputedStyle(li).listStyleType))
        expect(markers.length).toBeGreaterThan(0)
        for (const m of markers) expect(m).toBe('none')
      }
      await page.locator('[data-slot=unlinked]').scrollIntoViewIfNeeded()
      await page.screenshot({ path: `${outDir}site-live-lists-${theme}-${width}.png` })
      expect(errors).toEqual([])
    }
  }
  // The site's Markdown lists keep their bullets.
  await open(page, 'cli/', 'dark', 1280)
  expect(await page.locator('.sl-markdown-content > ul > li').first().evaluate((li) => getComputedStyle(li).listStyleType)).not.toBe('none')
})

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


// A snapshot without Issues that stand in the graph used to blank the whole
// page (#454): the graph redraws, and the rest of the page stays.
for (const width of [375, 1280]) {
  test(`live page redraws the graph when Issues leave it and come back ${width}`, async ({ page }) => {
    const worker = await mockWorker(page)
    const errors = await open(page, 'live/', 'dark', width, worker)
    const view = repoView(page)
    const graph = view.getByRole('region', { name: 'Dependency graph', exact: true })
    const node = (n: number) => graph.locator(`[data-issue="https://github.com/taco3064/nibfin-core/issues/${n}"]`)
    await expect(view.locator('[data-slot=dependency-graph]')).toContainText('Showing 54 of 54 open Issues')
    await expect(node(15)).toHaveCount(1)
    const edges = await graph.locator('[data-slot=edges] path[data-edge]').count()

    worker.send(event({ event: 'snapshot', at: '2026-11-25T08:05:00Z', result: withoutIssues(liveData(131, 58), [15, 16, 25]) }))
    await expect(view.locator('[data-slot=dependency-graph]')).toContainText('Showing 51 of 51 open Issues')
    await expect(graph).toBeVisible()
    for (const n of [15, 16, 25]) await expect(node(n)).toHaveCount(0)
    await expect(node(17)).toHaveCount(1)
    await expect.poll(() => graph.locator('[data-slot=edges] path[data-edge]').count()).toBeLessThan(edges)
    await expect(page.locator('[data-slot=summary-created] [data-slot=rolling-number]')).toHaveAttribute('data-value', '131')
    await expect(page.locator('[data-slot=runner-card]')).toHaveCount(5)
    await expect(view.locator('[data-action=change-repo]')).toBeVisible()
    await expect(page.locator('[data-note=graph-error]')).toHaveCount(0)
    await page.screenshot({ path: `${outDir}site-live-removed-dark-${width}.png`, fullPage: true })

    // They come back, linked as before.
    worker.send(event({ event: 'snapshot', at: '2026-11-25T08:10:00Z', result: liveData(131, 58) }))
    await expect(view.locator('[data-slot=dependency-graph]')).toContainText('Showing 54 of 54 open Issues')
    for (const n of [15, 16, 25]) await expect(node(n)).toHaveCount(1)
    await expect.poll(() => graph.locator('[data-slot=edges] path[data-edge]').count()).toBe(edges)
    await expect(page.locator('[data-note=graph-error]')).toHaveCount(0)
    expect(errors).toEqual([])
  })
}

// The Home flow (#457): the pipeline diagram with its legend; which drawing
// shows is checked with the other site diagrams above.
test('home shows the Issue flow as a pipeline', async ({ page }) => {
  for (const theme of themes) {
    for (const width of widths) {
      const errors = await open(page, './', theme, width)
      const figure = page.locator('figure[data-slot=diagram]')
      await expect(figure).toHaveCount(1)
      await expect(figure.locator('[data-slot=diagram-legend]')).toBeVisible()
      await expect(figure.locator('[data-slot=diagram-legend] li')).toHaveCount(4)
      await expect(figure.locator('ol.sr-only > li')).toHaveCount(8)
      await expect(figure.locator('ol.sr-only > li').last()).toContainText('Who acts: Nibfin, no agent.')
      await expectNoPageScroll(page)
      expect(errors).toEqual([])
    }
  }
})

test('home flow respects reduced motion', async ({ page }) => {
  const flowing = () =>
    page.evaluate(() =>
      document
        .getAnimations()
        .filter((a) => a instanceof CSSAnimation && a.animationName === 'nf-diagram-flow')
        .filter((a) => (a.effect as KeyframeEffect | null)?.target?.closest('figure[data-slot=diagram]')).length,
    )
  // A straight overlay has an empty box, so read its display instead.
  const overlayDisplay = () =>
    page.locator('svg[data-direction=LR] [data-slot=diagram-flow]').evaluateAll((ps) => [...new Set(ps.map((p) => getComputedStyle(p).display))])
  let errors = await open(page, './', 'dark', 1280)
  // Without reduced motion a dash flows along every shown edge.
  expect(await flowing()).toBeGreaterThan(0)
  expect(await overlayDisplay()).toEqual(['inline'])
  expect(errors).toEqual([])

  await page.emulateMedia({ reducedMotion: 'reduce' })
  errors = await open(page, './', 'dark', 1280)
  expect(await flowing()).toBe(0)
  // Nothing in the figure moves, and the overlay is not shown.
  const moving = await page.evaluate(
    () => document.getAnimations().filter((a) => (a.effect as KeyframeEffect | null)?.target?.closest('figure[data-slot=diagram]')).length,
  )
  expect(moving).toBe(0)
  expect(await overlayDisplay()).toEqual(['none'])
  await page.locator('figure[data-slot=diagram]').screenshot({ path: `${outDir}site-home-flow-reduced-dark-1280.png` })
  expect(errors).toEqual([])
})

test('live page respects reduced motion', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  const worker = await mockWorker(page)
  const errors = await open(page, 'live/', 'light', 1280, worker)
  await expect(page.locator('[data-slot=runner-card][data-runner-state=busy]')).toHaveCount(3)
  worker.send(event({ event: 'snapshot', at: '2026-11-25T08:05:00Z', result: liveData(131, 58, true) }))
  await expect(page.locator('[data-slot=summary-created] [data-slot=rolling-number]')).toHaveAttribute('data-value', '131')
  // The trend bars and the stage tracks, with their pulse marks, are there.
  await expect(page.locator('[data-slot=trend-bar]')).toHaveCount(6)
  await expect(page.locator('[data-slot=run-stage-pulse]')).toHaveCount(2)
  await expect(page.locator('[data-slot=runner-waking]')).toHaveCount(1)
  await page.locator('[data-slot=summary]').screenshot({ path: `${outDir}site-live-trend-reduced-light-1280.png` })
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

test('search finds the connector settings on the Setup page', async ({ page }) => {
  const errors = await open(page, './', 'dark', 1280)
  await page.getByRole('button', { name: 'Search' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('textbox', { name: 'Search' }).fill('deniedMcpServers')
  await expect(dialog.getByRole('link', { name: 'Setup and management' }).first()).toBeVisible()
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
