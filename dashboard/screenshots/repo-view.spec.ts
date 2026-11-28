import { expect, test, type Locator, type Page } from '@playwright/test'

import { unevenRoundedBorders } from './border-scan'
import { edgeCount, outsideCount, platform, repoStatus } from './repo-fixtures'
import { expectClean, expectGutter, expectNonce, fulfillPlanStream, outDir, prepare, shot, storageKey, widths, type Watch } from './support'

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
  await page.route('**/api/commands/*/plan/stream', async (route) => {
    const api = new URL(route.request().url()).pathname.split('/')[3]
    const body = route.request().postDataJSON() as Record<string, unknown>
    ops.plans.push({ api, body })
    await fulfillPlanStream(route, {
      id: `PLAN${ops.plans.length}`,
      plan: {
        operation: api,
        repo: body.repo,
        steps: [{ id: 'workflows', action: 'write workflows', target: `${String(body.repo)} .github/workflows`, current: 'absent', next: 'present', noChange: false, afterStart: false }],
        confirm: false,
        readOnly: false,
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

// expectNoPageScroll checks that the page does not scroll sideways and that
// its controls and cards keep the side gutter.
async function expectNoPageScroll(page: Page) {
  await expectGutter(page)
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

// The Issues without dependencies of the platform fixture, grouped as the
// list groups them (#496): a paused or pending Issue only under Paused or
// Pending, any other under its stage from labels, in the stage track's
// order. Empty groups are left out.
const blockerKeys = new Set(platform.flatMap((i) => (i.blockedBy ?? []).map((b) => `${b.repo}#${b.number}`)))
const unlinkedIssues = platform.filter((i) => i.blockedBy !== undefined && i.blockedBy.length === 0 && !blockerKeys.has(`acme/platform#${i.number}`))
function groupOf(i: (typeof platform)[number]): string {
  if (i.pause || i.pending) return 'Paused or Pending'
  const l = i.labels
  if (!l.includes('scope')) return l.includes('request') ? 'Scope' : 'Not in Nibfin'
  return l.includes('delivered') ? 'Done' : l.includes('shaped') ? 'Deliver' : 'Shape'
}
const unlinkedGroups = ['Scope', 'Shape', 'Deliver', 'Done', 'Not in Nibfin', 'Paused or Pending']
  .map((label) => ({ label, issues: unlinkedIssues.filter((i) => groupOf(i) === label) }))
  .filter((g) => g.issues.length > 0)
const groupName = (g: { label: string; issues: unknown[] }) => `${g.label} · ${g.issues.length} ${g.issues.length === 1 ? 'Issue' : 'Issues'}`
const groupButtons = (page: Page) => page.locator('[data-slot=unlinked] [data-unlinked-group] > [data-slot=collapsible-trigger]')

// openGroups opens every collapsed group of the Issues without dependencies.
async function openGroups(page: Page) {
  const buttons = groupButtons(page)
  for (let i = 0; i < (await buttons.count()); i++) {
    const b = buttons.nth(i)
    if ((await b.getAttribute('aria-expanded')) !== 'true') await b.click()
    await expect(b).toHaveAttribute('aria-expanded', 'true')
  }
}

// fullCount is the graph count line of the whole platform graph: closed
// blockers collapse into markers and are counted apart from the context.
const fullCount = `Showing ${platform.length} of ${platform.length} open Issues · ${outsideCount(platform, 'acme/platform')} shown as context · ${outsideCount(platform, 'acme/platform', 'CLOSED')} closed blockers collapsed`

// edgesDrawn waits for the drawn edges to settle at n.
async function edgesDrawn(page: Page, n: number) {
  await expect(page.locator('[data-slot=edges] path[data-edge]')).toHaveCount(n)
}

for (const theme of themes) {
  for (const width of [...widths, 390]) {
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
      await expect(page.locator('[data-slot=graph-count]')).toHaveText(fullCount)
      await edgesDrawn(page, edgeCount(platform, 'acme/platform'))
      // Every listed Issue appears once: in the graph, in the unread list or
      // counted in a collapsed group of Issues without dependencies (#496).
      const unlinkedTotal = unlinkedGroups.reduce((n, g) => n + g.issues.length, 0)
      expect(unlinkedTotal).toBe(32)
      await expect(page.locator('[data-view=repo] article[data-issue]')).toHaveCount(platform.length - unlinkedTotal)
      expect(await nodes.count()).toBeGreaterThan(100)
      await expect(page.locator('[data-note=cycle]')).toHaveText([
        '#120 and #121 block each other in a cycle.',
        '#122, #123 and #124 block each other in a cycle.',
      ])
      await expect(page.locator('[data-note=unread]')).toHaveText('Blockers of #125 could not be read; edges may be missing.')
      // The Issues without dependencies are grouped by stage, collapsed, with
      // each count in its button's name; Scope has none and is not shown.
      await expect(groupButtons(page)).toHaveCount(unlinkedGroups.length)
      expect(unlinkedGroups.map((g) => g.label)).toEqual(['Shape', 'Deliver', 'Done', 'Not in Nibfin', 'Paused or Pending'])
      for (const [i, g] of unlinkedGroups.entries()) {
        await expect(groupButtons(page).nth(i)).toHaveAccessibleName(groupName(g))
        await expect(groupButtons(page).nth(i)).toHaveAttribute('aria-expanded', 'false')
      }
      await expect(page.locator('[data-unlinked-group=scope]')).toHaveCount(0)
      await expect(page.locator('[data-unlinked-row]')).toHaveCount(0)
      // Collapsed, the list keeps the page short.
      if (width === 1280) expect(await page.evaluate(() => document.documentElement.scrollHeight)).toBeLessThanOrEqual(2000)
      if (width === 375) expect((await page.locator('[data-slot=unlinked]').boundingBox())!.height).toBeLessThanOrEqual(600)
      // An Issue whose blockers could not be read is never listed as having
      // no dependencies.
      await expect(page.locator('[data-slot=unread] > h3')).toHaveText('Issues whose blockers could not be read')
      await expect(page.locator('[data-slot=unread] article[data-issue]')).toHaveCount(1)
      await expect(page.locator('[data-slot=unread] article[data-issue$="/issues/125"]')).toHaveCount(1)
      // Closed blockers are markers, not nodes.
      await expect(page.locator('[data-slot=graph-region] [data-node="acme/platform#400"]')).toHaveCount(0)
      await expectNoPageScroll(page)
      const region = graphRegion(page)
      await expectDirection(page, width)
      // Every graph node keeps one even border with its radius (#488).
      expect(await unevenRoundedBorders(page, page.locator('[data-slot=graph-region]')), 'graph: rounded boxes with uneven borders').toEqual([])
      if (width >= 700) await expectOpensOnFirstNode(page)
      await region.focus()
      await expect(region).toBeFocused()
      await shot(page, `repo-graph-top-${theme}-${width}`, false)
      await shot(page, `repo-graph-${theme}-${width}`)

      // Opened, every group shows its compact rows, not full items.
      await openGroups(page)
      for (const g of unlinkedGroups) {
        const id = { Shape: 'shape', Deliver: 'deliver', Done: 'done', 'Not in Nibfin': 'none', 'Paused or Pending': 'held' }[g.label]
        await expect(page.locator(`[data-unlinked-group=${id}] [data-unlinked-row]`)).toHaveCount(g.issues.length)
      }
      await expect(page.locator('[data-slot=unlinked] [data-unlinked-row]')).toHaveCount(unlinkedTotal)
      await expect(page.locator('[data-slot=unlinked] article[data-issue]')).toHaveCount(0)
      await expectNoPageScroll(page)
      await page.locator('[data-slot=unlinked]').screenshot({ path: `${outDir}repo-unlinked-${theme}-${width}.png`, animations: 'disabled' })
      await expectClean(w)
    })
  }
}

for (const theme of themes) {
  for (const width of widths) {
    test(`Issues without dependencies open rows and preview on focus at ${width}px, ${theme}`, async ({ page }) => {
      const { w } = await open(page, width, theme)
      await pick(page, 'acme/platform')
      await expect(page.locator('[data-slot=graph-count]')).toHaveText(fullCount)
      const list = page.locator('[data-slot=unlinked]')
      const shape = groupButtons(page).first()
      await shape.click()
      await expect(shape).toHaveAttribute('aria-expanded', 'true')
      const rows = list.locator('[data-unlinked-group=shape] [data-unlinked-row]')
      await expect(rows).toHaveCount(unlinkedGroups[0].issues.length)
      const [first, second, third] = unlinkedGroups[0].issues
      const rowButton = (n: number) => list.locator(`[data-unlinked-row="acme/platform#${n}"] [data-slot=popover-anchor] > [data-slot=collapsible-trigger]`)
      await expect(rowButton(first.number)).toHaveAccessibleName(`#${first.number} ${first.title}, stage Shape, No runs`)
      // Opening a row shows its full Issue item inline.
      await rowButton(first.number).click()
      await expect(rowButton(first.number)).toHaveAttribute('aria-expanded', 'true')
      await expect(list.locator(`article[data-issue$="/issues/${first.number}"]`)).toBeVisible()
      await expect(page.locator('[data-unlinked-preview]')).toHaveCount(0)
      // A paused or pending Issue names its flags in its row.
      await openGroups(page)
      const held = unlinkedGroups.at(-1)!.issues[0]
      const repairLabel: Record<string, string> = { repairing: 'Repairing', choose: 'Waiting for your choice', act: 'Needs your action' }
      const pendingFlag = (i: object) => {
        const state = 'repair' in i ? (i.repair as { state?: string }).state : undefined
        return state ? repairLabel[state] : 'Pending'
      }
      await expect(rowButton(held.number)).toContainText(held.pause ? 'Paused' : pendingFlag(held))
      // Every held row names its repair state, or Pending when it was not read.
      for (const i of unlinkedGroups.at(-1)!.issues.filter((x) => x.pending)) {
        await expect(rowButton(i.number).locator('[data-flag=pending]')).toHaveText(pendingFlag(i))
      }
      expect(await unevenRoundedBorders(page, list), 'unlinked: rounded boxes with uneven borders').toEqual([])
      await list.screenshot({ path: `${outDir}repo-unlinked-open-${theme}-${width}.png`, animations: 'disabled' })

      // Keyboard focus shows the same item as a floating preview; the list
      // does not move and focus stays on the row.
      await rowButton(first.number).click()
      await expect(list.locator(`article[data-issue$="/issues/${first.number}"]`)).toHaveCount(0)
      const before = await rowButton(third.number).boundingBox()
      await rowButton(first.number).focus()
      await page.keyboard.press('Tab')
      await expect(list.locator(`[data-unlinked-row="acme/platform#${first.number}"] a`).first()).toBeFocused()
      await page.keyboard.press('Tab')
      await expect(rowButton(second.number)).toBeFocused()
      const preview = page.locator(`[data-unlinked-preview="acme/platform#${second.number}"]`)
      await expect(preview.locator(`article[data-issue$="/issues/${second.number}"]`)).toBeVisible()
      expect(await rowButton(third.number).boundingBox()).toEqual(before)
      expect(await unevenRoundedBorders(page, preview), 'preview: rounded boxes with uneven borders').toEqual([])
      await expectNoPageScroll(page)
      await shot(page, `repo-unlinked-preview-${theme}-${width}`, false)
      // The focus that opened the preview does not dismiss it.
      await expect(preview).toBeVisible()
      await expect(rowButton(second.number)).toBeFocused()
      // Tab moves on and the preview follows; Escape closes it.
      await page.keyboard.press('Tab')
      await expect(preview).toHaveCount(0)
      await page.keyboard.press('Tab')
      await expect(rowButton(third.number)).toBeFocused()
      const next = page.locator(`[data-unlinked-preview="acme/platform#${third.number}"]`)
      await expect(next).toBeVisible()
      await page.keyboard.press('Escape')
      await expect(next).toHaveCount(0)
      await expect(rowButton(third.number)).toBeFocused()
      await expectClean(w)
    })
  }
}

// expectInside waits until the element lies wholly inside the graph region:
// fitted after the shown set changed, or panned to on focus.
async function expectInside(page: Page, el: Locator) {
  await expect
    .poll(async () => {
      const r = await graphRegion(page).boundingBox()
      const t = await el.boundingBox()
      return !!r && !!t && t.x >= r.x - 0.5 && t.y >= r.y - 0.5 && t.x + t.width <= r.x + r.width + 0.5 && t.y + t.height <= r.y + r.height + 0.5
    })
    .toBe(true)
}

// expectOpensOnFirstNode waits until a graph too large to fit opens on its
// first matched node in reading order (leftmost, then topmost, then lowest
// number): that node lies wholly inside the region with its top left at the
// region's padding, another node is in view, and the region's top-left
// quarter shows a node rather than empty canvas.
async function expectOpensOnFirstNode(page: Page) {
  await expect
    .poll(() =>
      graphRegion(page).evaluate((el, pad) => {
        const r = el.getBoundingClientRect()
        const nodes = [...el.querySelectorAll<HTMLElement>('[data-node]')].map((n) => ({
          key: n.dataset.node!,
          context: n.hasAttribute('data-context'),
          b: n.getBoundingClientRect(),
        }))
        const num = (k: string) => Number(k.slice(k.lastIndexOf('#') + 1))
        const first = nodes
          .filter((n) => !n.context)
          .sort((p, q) => Math.round(p.b.left) - Math.round(q.b.left) || Math.round(p.b.top) - Math.round(q.b.top) || num(p.key) - num(q.key))[0]
        if (!first) return 'no matched node'
        const inside = first.b.left >= r.left - 0.5 && first.b.top >= r.top - 0.5 && first.b.right <= r.right + 0.5 && first.b.bottom <= r.bottom + 0.5
        if (!inside) return `${first.key} not wholly inside`
        const x0 = r.left + el.clientLeft + pad
        const y0 = r.top + el.clientTop + pad
        if (Math.abs(first.b.left - x0) > 2 || Math.abs(first.b.top - y0) > 2) return `${first.key} at ${first.b.left - r.left},${first.b.top - r.top}`
        const meets = (b: DOMRect, left: number, top: number, right: number, bottom: number) => b.right > left && b.left < right && b.bottom > top && b.top < bottom
        const others = nodes.filter((n) => n !== first && meets(n.b, r.left, r.top, r.right, r.bottom))
        if (others.length === 0) return 'no other node in view'
        const quarter = nodes.some((n) => meets(n.b, r.left, r.top, r.left + r.width / 2, r.top + r.height / 2))
        return quarter ? 'ok' : 'top-left quarter empty'
      }, 12),
    )
    .toBe('ok')
}

// expectDirection checks the layout of the graph region at a width: top to
// bottom on a phone, with every node inside the region and no sideways
// scroll; left to right from a tablet up, fitted, with zoom controls and a
// minimap.
async function expectDirection(page: Page, width: number) {
  const region = graphRegion(page)
  const legend = page.locator('[data-slot=legend]')
  if (width < 700) {
    await expect(region).toHaveAttribute('data-direction', 'TB')
    await expect(legend).toContainText('Blockers stand above the Issues they block')
    await expect(region.locator('.react-flow__controls')).toHaveCount(0)
    const out = await region.evaluate((el) => {
      const box = el.getBoundingClientRect()
      return [...el.querySelectorAll('[data-node]')].filter((n) => {
        const b = n.getBoundingClientRect()
        return b.left < box.left || b.right > box.right + 0.5 || b.width === 0
      }).length
    })
    expect(out).toBe(0)
    expect(await region.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true)
    // Each node stands on its own row.
    const rows = await region.locator('[data-node]').evaluateAll((ns) => ns.map((n) => Math.round(n.getBoundingClientRect().top)))
    expect(new Set(rows).size).toBe(rows.length)
  } else {
    await expect(region).toHaveAttribute('data-direction', 'LR')
    await expect(legend).toContainText('Blockers stand left of the Issues they block')
    await expect(region.getByRole('button', { name: 'Zoom in' })).toBeVisible()
    await expect(region.getByRole('button', { name: 'Zoom out' })).toBeVisible()
    await expect(region.getByRole('button', { name: 'Fit the graph' })).toBeVisible()
    await expect(region.locator('.react-flow__minimap')).toBeVisible()
    // Fitted on load: the region never scrolls by itself.
    expect(await region.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true)
  }
}

for (const theme of themes) {
  for (const width of [375, 390, 1280]) {
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
      // The seven-node chain is fitted on load, the match included.
      await expectInside(page, page.locator('[data-slot=graph-region] [data-node="acme/platform#21"]'))
      await expectInside(page, page.locator('[data-slot=graph-region] [data-node="acme/infra#201"]'))
      await expectNoPageScroll(page)
      await shot(page, `repo-graph-search-${theme}-${width}`)

      // A search match among the Issues without dependencies opens its group
      // (#496): one match is a short list with its full item.
      await search.fill('Standalone improvement 129')
      await expect(groupButtons(page)).toHaveCount(1)
      await expect(page.locator('[data-unlinked-group=deliver] > [data-slot=collapsible-trigger]')).toHaveAttribute('aria-expanded', 'true')
      await expect(page.locator('[data-slot=unlinked] article[data-issue$="/issues/129"]')).toBeVisible()
      // Many matches stay compact, every group that holds one open; a
      // closed group reopens once the search text changes.
      await search.fill('Standalone improvement')
      const matches = unlinkedIssues.filter((i) => i.title.startsWith('Standalone improvement'))
      await expect(page.locator('[data-slot=unlinked] [data-unlinked-row]')).toHaveCount(matches.length)
      for (const b of await groupButtons(page).all()) await expect(b).toHaveAttribute('aria-expanded', 'true')
      await groupButtons(page).first().click()
      await expect(groupButtons(page).first()).toHaveAttribute('aria-expanded', 'false')
      await search.fill('Standalone improvement 1')
      await expect(groupButtons(page).first()).toHaveAttribute('aria-expanded', 'true')
      await expectNoPageScroll(page)

      await search.fill('')
      // Without search text the long list is collapsed again.
      for (const b of await groupButtons(page).all()) await expect(b).toHaveAttribute('aria-expanded', 'false')
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
      await expect(page.locator('[data-slot=graph-count]')).toHaveText(fullCount)
      await expectClean(w)
    })
  }
}

for (const [width, theme] of [
  [375, 'dark'],
  [1280, 'light'],
] as const) {
  test(`closed blockers collapse into a marker that lists them at ${width}px, ${theme}`, async ({ page }) => {
    const { w } = await open(page, width, theme)
    await pick(page, 'acme/platform')
    const region = graphRegion(page)
    await expect(page.locator('[data-slot=graph-count]')).toHaveText(fullCount)
    // #2 is blocked only by the closed #400: it stays in the graph with its
    // marker and is not listed as having no dependencies.
    const two = region.locator('[data-node="acme/platform#2"]')
    await expect(two).toBeVisible()
    // Opened groups mount their rows, so the check looks at every listed one.
    await openGroups(page)
    await expect(page.locator('[data-slot=unlinked] [data-unlinked-row]')).toHaveCount(32)
    await expect(page.locator('[data-unlinked-row="acme/platform#2"]')).toHaveCount(0)
    await expect(page.locator('[data-slot=unlinked] article[data-issue$="/issues/2"]')).toHaveCount(0)
    // Keep the pointer off the graph, where hovering a node would highlight it.
    await page.mouse.move(0, 0)
    const marker = two.locator('[data-slot=closed-blockers]')
    await expect(marker).toHaveAccessibleName('1 closed · #400 (closed blockers of #2)')
    // Chain steps 3 of chains 1, 4 and 7 carry their own closed blocker.
    await expect(region.locator('[data-slot=closed-blockers]')).toHaveCount(4)
    await expect(region.locator('[data-node="acme/platform#12"] [data-slot=closed-blockers]')).toHaveAccessibleName('1 closed · #500 (closed blockers of #12)')
    // Edges into one node meet it at separate points.
    await edgesDrawn(page, edgeCount(platform, 'acme/platform'))
    const ends = await page.locator('[data-slot=edges] path[data-edge$=">acme/platform#1"]').evaluateAll((ps) =>
      (ps as SVGPathElement[]).map((p) => {
        const end = p.getPointAtLength(p.getTotalLength())
        return `${Math.round(end.x)} ${Math.round(end.y)}`
      }),
    )
    expect(ends.length).toBeGreaterThan(20)
    expect(new Set(ends).size).toBe(ends.length)
    const intoOne = page.locator('[data-slot=edges] path[data-edge$=">acme/platform#1"]')
    if (width < 700) {
      // Top to bottom, edges into one node overlap, so each runs in its own lane.
      const lanes = await intoOne.evaluateAll((ps) => ps.map((p) => p.getAttribute('data-lane')))
      expect(new Set(lanes).size).toBe(lanes.length)
    }
    // Focus on a node brings out its edges and fades the rest.
    await region.locator('.react-flow__node[data-id="acme/platform#1"]').focus()
    await expect(page.locator('[data-slot=edges] path[data-active]')).toHaveCount(await intoOne.count())
    await shot(page, `repo-graph-active-${theme}-${width}`, false)

    // Each node is named for assistive technology, and keyboard focus on a
    // node or a control inside one is always in sight.
    await expect(region.locator('.react-flow__node[data-id="acme/platform#2"]')).toHaveAttribute(
      'aria-label',
      '#2 Second foundation: configuration loader, stage Done, 1 closed blocker',
    )
    await region.focus()
    for (let i = 0; i < 14; i++) {
      await page.keyboard.press('Tab')
      await expect
        .poll(() =>
          page.evaluate(() => {
            const el = document.activeElement!
            const r = el.closest('[data-slot=graph-region]')!.getBoundingClientRect()
            const t = el.getBoundingClientRect()
            return t.left >= r.left - 0.5 && t.right <= r.right + 0.5 && t.top >= r.top - 0.5 && t.bottom <= r.bottom + 0.5
          }),
        )
        .toBe(true)
    }

    // Searching for #2 fits it in the view; its closed blocker stays a marker.
    await page.getByRole('searchbox', { name: 'Search Issues' }).fill('#2')
    await expect(page.locator('[data-slot=graph-count]')).toHaveText(`Showing 1 of ${platform.length} open Issues · 0 shown as context · 1 closed blocker collapsed`)
    await expect(region.locator('[data-node]')).toHaveCount(1)
    await edgesDrawn(page, 0)
    await expectInside(page, region.locator('[data-node="acme/platform#2"]'))
    await marker.click()
    const list = page.locator('[data-slot=closed-list]')
    await expect(list).toBeVisible()
    await expect(list.getByRole('heading')).toHaveText('Closed blockers of #2')
    const link = list.getByRole('link')
    await expect(link).toHaveCount(1)
    await expect(link).toHaveAttribute('href', 'https://github.com/acme/platform/issues/400')
    await expect(link).toHaveAttribute('target', '_blank')
    await expect(link).toHaveAttribute('rel', 'noreferrer')
    await expect(link).toContainText('#400 Platform task 400')
    await expectNoPageScroll(page)
    await shot(page, `repo-graph-closed-${theme}-${width}`, false)
    await page.keyboard.press('Escape')
    await expect(list).toBeHidden()
    await expect(marker).toBeFocused()

    // Tab moves from the region into the nodes and the controls inside them.
    await region.focus()
    await page.keyboard.press('Tab')
    await expect(page.locator('.react-flow__node:focus')).toHaveCount(1)
    await page.keyboard.press('Tab')
    await expect(page.locator('.react-flow__node :focus')).toHaveCount(1)
    await expectClean(w)
  })
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
      const workflows = summary.locator('[data-slot=workflows]')
      await change(page)
      await openManual(page, 'acme/new')
      await expect(summary.locator('[data-image]')).toHaveText('No Nibfin workflows')
      await expect(run).toContainText('Does not use Nibfin')
      await expect(run).toContainText('No nibfin-issue.yml on the default branch')
      await expect(workflows).toHaveAttribute('data-workflows', 'none')
      await expect(workflows).toContainText('No Nibfin workflows')
      await expect(workflows.locator('[data-slot=workflow-files]')).toHaveCount(0)
      await expect(summary.locator('[data-action=init]')).toBeEnabled()
      await expect(summary.locator('[data-action=upgrade]')).toHaveCount(0)
      await expect(page.locator('[data-empty=issues]')).toHaveText('This repository has no open Issues.')
      await expectNoPageScroll(page)
      await shot(page, `repo-summary-init-${theme}-${width}`)

      await change(page)
      await pick(page, 'acme/app')
      await expect(summary.locator('[data-image]')).toHaveText('Latest imageghcr.io/acme/nibfin:v1.4.0')
      await expect(run.locator('[data-state=succeeded]')).toContainText('A Nibfin run succeeded after the last change')
      await expect(run.locator('[data-slot=change]')).toContainText('nibfin-issue.yml last changed in PR #42(opens on GitHub in a new tab), merged')
      await expect(run.getByRole('link', { name: /Successful run/ })).toHaveAttribute('href', 'https://github.com/acme/app/actions/runs/8801')
      // The three lights, in order, each with its own text label.
      await expect(summary.locator('[data-slot=health] > section > h3')).toHaveText(['Nibfin runs', 'Image', 'Workflows'])
      await expect(summary.locator('[data-slot=health] > section[data-tone=success]')).toHaveCount(3)
      await expect(workflows).toHaveAttribute('data-workflows', 'execution')
      // The execution set confirms only nibfin-job.yml.
      await expect(workflows.locator('[data-slot=workflow-files]')).toHaveText('nibfin-job.yml found')
      await expect(summary.locator('[data-action=upgrade]')).toBeEnabled()
      await expect(summary.locator('[data-action=init]')).toHaveCount(0)
      // #101 → #99 and #99 → acme/infra#12; the closed #98 is a marker.
      await edgesDrawn(page, 2)
      // A graph that fits at a readable zoom needs no minimap.
      await expect(graphRegion(page).locator('.react-flow__minimap')).toHaveCount(0)
      await expectNoPageScroll(page)
      await shot(page, `repo-summary-app-${theme}-${width}`)

      await change(page)
      await pick(page, 'acme/docs')
      await expect(summary.locator('[data-image]')).toContainText('Not the latest image')
      await expect(run.locator('[data-state=not-succeeded]')).toContainText('No successful Nibfin run since the last change')
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
      await expect(run.locator('[data-state=error]')).toContainText('github_api_failed: list the commits of .github/workflows/nibfin-issue.yml of acme/infra: HTTP 502')
      await expect(workflows).toHaveAttribute('data-workflows', 'error')
      await expect(workflows).toContainText('github_error: reading .github/workflows of acme/infra: HTTP 502')
      await expect(workflows).toHaveAttribute('data-tone', 'danger')
      await expect(summary.locator('[data-action=init]')).toBeEnabled()
      await expect(summary.locator('[data-action=upgrade]')).toBeEnabled()
      await expect(page.locator('[data-note=issues-error]')).toHaveText('Issues could not be read: github_error: listing the Issues of acme/infra: HTTP 502')
      await expectNoPageScroll(page)
      await shot(page, `repo-summary-infra-${theme}-${width}`)

      await change(page)
      await openManual(page, 'github.com/acme/site')
      await expect(summary.locator('[data-image]')).toHaveText('Peer set: no image pin')
      await expect(run.locator('[data-slot=change]')).toContainText('nibfin-issue.yml last changed in commit c0ffee1')
      await expect(workflows).toHaveAttribute('data-workflows', 'peer')
      await expect(workflows.locator('[data-slot=workflow-files]')).toHaveText('nibfin-issue.yml found')
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
  // Before a repository is open, Refresh reads the person view by the plan
  // route; the first person read came from the live stream.
  await page.getByRole('button', { name: 'Refresh' }).click()
  await expect.poll(() => w.statusBodies.length).toBe(1)
  expect(lastBody(w)).toEqual({})

  await pick(page, 'acme/platform')
  await expect(page.locator('[data-slot=repo-summary]')).toBeVisible()
  await expect(freshness(page)).toHaveAttribute('data-state', 'current')
  // The repository read has no live updates, so the header says that it
  // updates on Refresh instead of the person stream's Live badge.
  await expect(page.locator('[data-slot=live]')).toHaveAttribute('data-state', 'manual')
  await expect(page.locator('[data-slot=live]')).toHaveText('Updates on Refresh')
  const personTime = await (async () => {
    await tab(page, 'Person').click()
    await expect(page.locator('[data-slot=live]')).toHaveText('Live')
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

// A Refresh that drops Issues from the graph, or brings them back linked
// again, redraws the graph without an error (#454).
test('a Refresh that removes and re-links Issues redraws the graph', async ({ page }) => {
  const { w } = await open(page, 1280, 'dark')
  await pick(page, 'acme/platform')
  await expect(page.locator('[data-slot=graph-count]')).toHaveText(fullCount)
  await edgesDrawn(page, edgeCount(platform, 'acme/platform'))
  const node = (n: number) => graphRegion(page).locator(`[data-node="acme/platform#${n}"]`)
  await expect(node(12)).toHaveCount(1)

  const gone = [12, 13, 16]
  const left = platform
    .filter((i) => !gone.includes(i.number))
    .map((i) => (i.blockedBy ? { ...i, blockedBy: i.blockedBy.filter((b) => b.repo !== 'acme/platform' || !gone.includes(b.number)) } : i))
  const view = repoStatus['github.com/acme/platform'].result as Record<string, unknown>
  w.repoStatus['github.com/acme/platform'] = { result: { ...view, issues: left } }
  await page.getByRole('button', { name: 'Refresh' }).click()
  await expect(page.locator('[data-slot=graph-count]')).toContainText(`Showing ${left.length} of ${left.length} open Issues`)
  await edgesDrawn(page, edgeCount(left, 'acme/platform'))
  for (const n of gone) await expect(node(n)).toHaveCount(0)
  await expect(node(14)).toHaveCount(1)
  await expect(graphRegion(page)).toBeVisible()
  await expect(page.locator('[data-slot=repo-summary]')).toBeVisible()
  await expect(page.locator('[data-note=graph-error]')).toHaveCount(0)
  await shot(page, 'repo-graph-removed-dark-1280', false)

  w.repoStatus['github.com/acme/platform'] = repoStatus['github.com/acme/platform']
  await page.getByRole('button', { name: 'Refresh' }).click()
  await expect(page.locator('[data-slot=graph-count]')).toHaveText(fullCount)
  await edgesDrawn(page, edgeCount(platform, 'acme/platform'))
  for (const n of gone) await expect(node(n)).toHaveCount(1)
  await expect(page.locator('[data-note=graph-error]')).toHaveCount(0)
  await expectClean(w)
})

test.describe('in a Traditional Chinese browser', () => {
  test.use({ locale: 'zh-TW', timezoneId: 'Asia/Taipei' })

  test('times stay in the one English format', async ({ page }) => {
    const { w } = await open(page, 1280, 'light')
    await pick(page, 'acme/platform')
    const summary = page.locator('[data-slot=repo-summary]')
    await expect(summary).toBeVisible()
    await expect(freshness(page).locator('time')).toHaveText(/^\d{2}:\d{2}:\d{2}$/)
    const times = summary.locator('time')
    expect(await times.count()).toBeGreaterThan(0)
    for (const t of await times.all()) {
      await expect(t).toHaveText(/^\d{1,2} [A-Z][a-z]{2,3} \d{4}, \d{2}:\d{2}$/)
      expect(Number.isNaN(Date.parse((await t.getAttribute('datetime')) ?? ''))).toBe(false)
    }
    await shot(page, 'repo-zh-tw-light-1280', false)
    await expectClean(w)
  })
})

test('the reading states of the list and of a repository', async ({ page }) => {
  await page.setViewportSize({ width: 768, height: 900 })
  await page.addInitScript(([k, t]) => localStorage.setItem(k, t), [storageKey, 'light'])
  const w = await prepare(page)
  w.repoStatus = { ...repoStatus }
  await mockOps(page)
  // The person read, which supplies the list, comes from the live stream.
  let release!: () => void
  w.live.hold = new Promise<void>((r) => (release = r))
  await page.goto('/#proof=SCREENSHOT')
  await tab(page, 'Repositories').click()
  await expect(picker(page).locator('[data-state=list-reading]')).toHaveText('Reading repositories…')
  await shot(page, 'repo-list-reading-light-768', false)
  release()
  await expect(picker(page).locator('[data-repo-entry]')).toHaveCount(5)

  const held = new Promise<void>((r) => (release = r))
  await page.route('**/api/commands/status/read', async (route) => {
    await held
    await route.fallback()
  })
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
  await page.route('**/api/commands/status/read', async (route) => {
    const body = route.request().postDataJSON() as { repo?: string }
    if (body.repo === 'github.com/acme/docs') await held
    await route.fallback()
  })
  await pick(page, 'acme/docs')
  await change(page)
  await pick(page, 'acme/app')
  await expect(page.locator('[data-slot=repo-link]')).toHaveAttribute('href', 'https://github.com/acme/app')
  release()
  await expect.poll(() => w.statusBodies.length).toBe(2)
  await page.waitForTimeout(200)
  await expect(page.locator('[data-slot=repo-link]')).toHaveAttribute('href', 'https://github.com/acme/app')
  await expectClean(w)
})

