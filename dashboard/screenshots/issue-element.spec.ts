import { expect, test, type Locator, type Page } from '@playwright/test'

import { expectClean, expectNonce, prepare, shot, storageKey, widths } from './support'

// Screenshots and checks of the shared Issue element (#350) in the
// screenshot harness (screenshots.html), served by the real Go server with
// its CSP. The plan and apply routes are mocked with fictional bodies.

const gh = 'https://github.com'
const url = (repo: string, n: number) => `${gh}/${repo}/issues/${n}`
const themes = ['dark', 'light'] as const

type ApplyMode = 'result' | 'error' | 'cut'

interface Api {
  plans: { action: string; issues: string[] }[]
  applies: { id: string; body: unknown }[]
  deletes: string[]
  mode: ApplyMode
  // hold, when set, delays every apply answer until it resolves.
  hold?: Promise<void>
}

async function mockOps(page: Page): Promise<Api> {
  const api: Api = { plans: [], applies: [], deletes: [], mode: 'result' }
  await page.route('**/api/commands/*/plan', async (route) => {
    const action = new URL(route.request().url()).pathname.split('/')[3]
    expect(route.request().headers()['x-nibfin-session']).toBe('SCREENSHOTSESSION')
    const body = route.request().postDataJSON() as { issues: string[] }
    api.plans.push({ action, issues: body.issues })
    await route.fulfill({ json: { id: `PLAN${api.plans.length}`, plan: { operation: action, issues: body.issues } } })
  })
  await page.route('**/api/plans/**', async (route) => {
    const req = route.request()
    const id = new URL(req.url()).pathname.split('/')[3]
    if (req.method() === 'DELETE') {
      api.deletes.push(id)
      return route.fulfill({ status: 204 })
    }
    api.applies.push({ id, body: req.postDataJSON() })
    await api.hold
    const plan = api.plans[Number(id.replace('PLAN', '')) - 1]
    const done = plan.action === 'pause' ? 'paused' : 'resumed'
    const lines =
      api.mode === 'result'
        ? [JSON.stringify({ event: 'result', result: Object.fromEntries(plan.issues.map((u) => [u, done])) })]
        : api.mode === 'error'
          ? [JSON.stringify({ event: 'error', error: { code: 'label_missing', message: 'acme/app has no pause label' } })]
          : ['{"event":"res']
    await route.fulfill({ status: 200, contentType: 'application/x-ndjson', body: lines.join('\n') + (api.mode === 'cut' ? '' : '\n') })
  })
  return api
}

async function open(page: Page, width: number, theme: (typeof themes)[number] = 'dark') {
  await page.setViewportSize({ width, height: 900 })
  await page.addInitScript(([k, t]) => localStorage.setItem(k, t), [storageKey, theme])
  const w = await prepare(page)
  const api = await mockOps(page)
  const res = await page.goto('/screenshots.html#proof=SCREENSHOT')
  await expect(page.locator('main[data-session="open"]')).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
  await expectNonce(page, res)
  await page.evaluate(() => document.fonts.ready.then(() => undefined))
  await expectNoPageScroll(page)
  return { w, api }
}

async function expectNoPageScroll(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
}

function item(page: Page, repo: string, n: number): Locator {
  return page.locator(`article[data-issue="${url(repo, n)}"]`)
}

async function openRuns(page: Page, it: Locator) {
  const button = it.getByRole('button', { name: /^Runs/ })
  await button.click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  return { button, dialog }
}

for (const width of widths) {
  for (const theme of themes) {
    test(`Issue element list and runs dialogs at ${width}px, ${theme}`, async ({ page }) => {
      const { w } = await open(page, width, theme)
      await shot(page, `issue-list-${theme}-${width}`)

      // Stage from labels only, with pause and pending.
      await expect(item(page, 'acme/app', 101).locator('[data-badge=stage]')).toHaveText('Stage: Deliver')
      await expect(item(page, 'acme/app', 104).locator('[data-badge=stage]')).toHaveText('Stage: Done')
      await expect(item(page, 'acme/app', 105).locator('[data-badge=stage]')).toHaveText('Stage: Not in Nibfin')
      await expect(item(page, 'acme/legacy', 3).locator('[data-badge=stage]')).toHaveText('Stage: Shape')
      await expect(item(page, 'acme/app', 102).locator('[data-badge=paused]')).toHaveText('Paused')
      await expect(item(page, 'acme/app', 103).locator('[data-badge=pending] a')).toHaveAttribute(
        'href',
        `${url('acme/app', 103)}#issuecomment-5001`,
      )
      await expect(item(page, 'acme/app', 104)).toContainText('Reason comment could not be read: github_error: comments of acme/app#104: HTTP 500')
      await expect(item(page, 'acme/docs', 7)).toContainText('Reason comment not found')

      // Open blockers only; failed and unread are labelled, never "no blockers".
      const blockers = item(page, 'acme/app', 101).locator('[data-slot=blockers]')
      await expect(blockers.getByRole('link')).toHaveCount(2)
      await expect(blockers).not.toContainText('#98')
      await expect(item(page, 'acme/app', 103).locator('[data-slot=blockers]')).toContainText('could not be read: github_error')
      await expect(item(page, 'acme/app', 105).locator('[data-slot=blockers]')).toContainText('not read')
      await expect(item(page, 'acme/infra', 12).locator('[data-slot=blockers]')).toHaveCount(0)
      await expect(page.getByText(/no blockers/i)).toHaveCount(0)

      // The GitHub link opens the Issue in a new tab.
      const title = item(page, 'acme/app', 101).getByRole('heading').getByRole('link')
      await expect(title).toHaveAttribute('href', url('acme/app', 101))
      await expect(title).toHaveAttribute('target', '_blank')

      // Runs from two repositories, newest first.
      let { button, dialog } = await openRuns(page, item(page, 'acme/app', 101))
      await expect(dialog.getByRole('heading')).toHaveText('Nibfin runs of #101')
      const rows = dialog.getByRole('list', { name: 'Runs, newest first' }).getByRole('listitem')
      await expect(rows).toHaveCount(4)
      await expect(rows.first()).toContainText('running')
      await expect(rows.first()).toContainText('in progress')
      await expect(rows.nth(1)).toContainText('acme/nibfin-exec')
      await expect(rows.nth(1)).toContainText('failure')
      await expect(rows.first().locator('time')).toHaveAttribute('datetime', '2026-11-24T15:20:00Z')
      await expect(dialog).not.toContainText('No Nibfin runs')
      // The scroll lock's <style> element carries the page nonce.
      const styles = await page.evaluate(() => {
        const meta = document.querySelector<HTMLMetaElement>('meta[property=csp-nonce]')?.nonce
        return [...document.querySelectorAll('style')].map((s) => s.nonce === meta && !!meta)
      })
      expect(styles.length).toBeGreaterThan(0)
      expect(styles.every(Boolean)).toBe(true)
      await expectNoPageScroll(page)
      await shot(page, `issue-runs-${theme}-${width}`, false)
      await dialog.hover()
      await page.mouse.wheel(0, 200)
      await page.keyboard.press('Escape')
      await expect(dialog).toBeHidden()
      await expect(button).toBeFocused()

      // A failed read shows what was read, labelled; never "No Nibfin runs".
      ;({ button, dialog } = await openRuns(page, item(page, 'acme/app', 102)))
      await expect(dialog.locator('[data-note=failed]')).toContainText('github_error: listing runs of acme/nibfin-exec: HTTP 502')
      await expect(dialog.getByRole('listitem')).toHaveCount(1)
      await expect(dialog).not.toContainText('No Nibfin runs')
      await shot(page, `issue-runs-failed-${theme}-${width}`, false)
      await dialog.getByRole('button', { name: 'Close' }).click()
      await expect(dialog).toBeHidden()
      await expect(button).toBeFocused()

      ;({ dialog } = await openRuns(page, item(page, 'acme/app', 103)))
      await expect(dialog.locator('[data-note=truncated]')).toContainText('only the newest runs were read')
      await expect(dialog).not.toContainText('No Nibfin runs')
      await shot(page, `issue-runs-truncated-${theme}-${width}`, false)
      await page.keyboard.press('Escape')

      ;({ dialog } = await openRuns(page, item(page, 'acme/app', 105)))
      await expect(dialog.locator('[data-note=notRead]')).toContainText('not read')
      await expect(dialog).not.toContainText('No Nibfin runs')
      await page.keyboard.press('Escape')

      ;({ dialog } = await openRuns(page, item(page, 'acme/app', 104)))
      await expect(dialog.locator('[data-note=empty]')).toHaveText('No Nibfin runs')
      await shot(page, `issue-runs-empty-${theme}-${width}`, false)
      await page.keyboard.press('Escape')
      await expect(dialog).toBeHidden()

      await expectClean(w)
    })
  }
}

test('pause sends one plan and an unconfirmed apply; the badge follows the read-back', async ({ page }) => {
  const { w, api } = await open(page, 1280)
  let release!: () => void
  api.hold = new Promise((r) => (release = r))
  const it = item(page, 'acme/app', 101)
  await it.getByRole('button', { name: 'Pause' }).click()

  await expect(it.getByRole('button', { name: 'Pausing…' })).toBeDisabled()
  await expect.poll(() => api.applies.length).toBe(1)
  await expect(it.locator('[data-badge=paused]')).toHaveCount(0)
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await shot(page, 'issue-pausing-1280')
  release()

  await expect(it.locator('[data-badge=paused]')).toHaveText('Paused')
  await expect(it.getByRole('button', { name: 'Resume' })).toBeEnabled()
  expect(api.plans).toEqual([{ action: 'pause', issues: [url('acme/app', 101)] }])
  expect(api.applies).toEqual([{ id: 'PLAN1', body: { confirm: false } }])
  expect(api.deletes).toEqual([])
  await shot(page, 'issue-paused-1280')
  await expectClean(w)
})

for (const width of [375, 1280]) {
  test(`two selected Issues resume in one operation at ${width}px`, async ({ page }) => {
    const { w, api } = await open(page, width)
    const bulk = page.getByRole('region', { name: 'Selected Issues' })
    await expect(bulk.getByRole('button', { name: 'Resume selected' })).toBeDisabled()
    await page.getByRole('checkbox', { name: 'Select #102' }).check()
    await page.getByRole('checkbox', { name: 'Select #103' }).check()
    await expect(bulk).toContainText('2 selected')
    await shot(page, `issue-selected-${width}`)
    await bulk.getByRole('button', { name: 'Resume selected' }).click()

    await expect(bulk.getByRole('status')).toHaveText('Confirmed: resumed 2.')
    expect(api.plans).toEqual([{ action: 'resume', issues: [url('acme/app', 102), url('acme/app', 103)] }])
    expect(api.applies).toEqual([{ id: 'PLAN1', body: { confirm: false } }])
    await expect(item(page, 'acme/app', 102).locator('[data-badge=paused]')).toHaveCount(0)
    await expect(item(page, 'acme/app', 103).locator('[data-badge=pending]')).toHaveCount(0)
    await expect(item(page, 'acme/app', 102).getByRole('button', { name: 'Pause' })).toBeEnabled()
    await expectNoPageScroll(page)
    await shot(page, `issue-bulk-resumed-${width}`)
    await expectClean(w)
  })
}

test('permission below triage, unknown or unread disables the action with the reason', async ({ page }) => {
  const { w, api } = await open(page, 768)
  const cases: [string, number, string, string][] = [
    ['acme/docs', 7, 'Resume', 'Needs triage permission or higher; this account has READ on acme/docs.'],
    ['acme/infra', 12, 'Pause', 'GitHub reported no permission for this account on acme/infra.'],
    ['acme/legacy', 3, 'Pause', 'Permission on acme/legacy could not be read: github_error: repository acme/legacy: timeout after 10s.'],
  ]
  for (const [repo, n, name, reason] of cases) {
    const it = item(page, repo, n)
    const button = it.getByRole('button', { name, exact: true })
    await expect(button).toBeDisabled()
    await expect(it.locator('[data-slot=disabled-reason]')).toHaveText(reason)
    await expect(button).toHaveAccessibleDescription(reason)
  }
  await expect(item(page, 'acme/app', 101).getByRole('button', { name: 'Pause' })).toBeEnabled()
  await expect(item(page, 'acme/app', 101).locator('[data-slot=disabled-reason]')).toHaveCount(0)

  // A mixed selection names the repository that blocks it.
  await page.getByRole('checkbox', { name: 'Select #101' }).check()
  await page.getByRole('checkbox', { name: 'Select #7' }).check()
  const bulk = page.getByRole('region', { name: 'Selected Issues' })
  await expect(bulk.getByRole('button', { name: 'Pause selected' })).toBeDisabled()
  await expect(bulk.locator('[data-slot=disabled-reason]')).toHaveText(
    'Needs triage permission or higher; this account has READ on acme/docs.',
  )
  await shot(page, 'issue-permission-768')
  expect(api.plans).toEqual([])
  await expectClean(w)
})

test('an error line shows its code and message and changes no badge', async ({ page }) => {
  const { w, api } = await open(page, 375)
  api.mode = 'error'
  const it = item(page, 'acme/app', 103)
  await it.getByRole('button', { name: 'Resume' }).click()
  await expect(it.getByRole('alert')).toHaveText('label_missing: acme/app has no pause label')
  await expect(it.locator('[data-badge=pending]')).toBeVisible()
  await expect(it.getByRole('button', { name: 'Resume' })).toBeEnabled()
  await expectNoPageScroll(page)
  await shot(page, 'issue-error-375')
  await expectClean(w)
})

test('a cut-off stream says the result is unknown', async ({ page }) => {
  const { w, api } = await open(page, 1280, 'light')
  api.mode = 'cut'
  const it = item(page, 'acme/app', 101)
  await it.getByRole('button', { name: 'Pause' }).click()
  await expect(it.getByRole('alert')).toContainText('The result is unknown; reload the status to see the labels.')
  await expect(it.locator('[data-badge=paused]')).toHaveCount(0)
  // The view was asked to read the status again.
  await expect(page.locator('[data-rereads="1"]')).toHaveCount(1)
  await shot(page, 'issue-unknown-light-1280')
  await expectClean(w)
})

test('the runs dialog does not animate when reduced motion is requested', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  const { w } = await open(page, 1280, 'dark')
  const { dialog } = await openRuns(page, item(page, 'acme/app', 101))
  const animations = async () =>
    page.evaluate(() =>
      ['[data-slot=dialog-content]', '[data-slot=dialog-overlay]'].map(
        (s) => getComputedStyle(document.querySelector(s)!).animationName,
      ),
    )
  expect(await animations()).toEqual(['none', 'none'])
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()

  // With motion allowed, the same dialog keeps its enter animation.
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await openRuns(page, item(page, 'acme/app', 101))
  expect((await animations()).every((n) => n !== 'none')).toBe(true)
  await expectClean(w)
})
