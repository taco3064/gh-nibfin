import { expect, test, type Page } from '@playwright/test'

import { endStream, push } from './live-mock'
import { issueURL, personView, repos } from './person-fixtures'
import { expectClean, expectNonce, prepare, shot, storageKey, widths, type Watch } from './support'

// Screenshots and checks of the live updates (#355): the Live badge, an
// Issue re-read, a check moving the time, the notes, the stream ending and
// Refresh reconnecting, and the fallback read. The stream is a fictional
// mock in the page.

const themes = ['dark', 'light'] as const
type Theme = (typeof themes)[number]

async function open(page: Page, width: number, theme: Theme, setup?: (w: Watch) => void): Promise<Watch> {
  await page.setViewportSize({ width, height: 900 })
  await page.addInitScript(([k, t]) => localStorage.setItem(k, t), [storageKey, theme])
  const w = await prepare(page)
  setup?.(w)
  const res = await page.goto('/#proof=SCREENSHOT')
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
  await expectNonce(page, res)
  await expect(page.locator('[data-content] > *').first()).toBeVisible()
  await page.evaluate(() => document.fonts.ready.then(() => undefined))
  await expectNoPageScroll(page)
  return w
}

async function expectNoPageScroll(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
}

const live = (page: Page) => page.locator('[data-slot=live]')
const time = (page: Page) => page.locator('[data-slot=freshness] time')
const app101 = (page: Page) => page.locator(`article[data-issue="${issueURL('acme/app', 101)}"]`)

for (const theme of themes) {
  for (const width of widths) {
    test(`live updates at ${width}px, ${theme}`, async ({ page }) => {
      const w = await open(page, width, theme)
      await expect(live(page)).toHaveAttribute('data-state', 'on')
      await expect(live(page)).toHaveText('Live')
      // The session went in the header; the URL carries none.
      expect(w.live.sessions).toEqual(['SCREENSHOTSESSION'])
      expect(w.live.urls).toEqual(['/api/live'])
      expect(w.statusReads).toBe(1)

      // An issue event updates that Issue's badges only.
      const orig = repos[0].issues[0]
      await expect(app101(page).locator('[data-badge=paused]')).toHaveCount(0)
      await push(page, {
        event: 'issue',
        at: new Date().toISOString(),
        repo: 'acme/app',
        issue: { ...orig, labels: [...orig.labels, 'pause'], pause: true },
      })
      await expect(app101(page).locator('[data-badge=paused]')).toHaveText('Paused')

      // A check moves the time forward.
      const before = await time(page).getAttribute('datetime')
      const later = new Date(Date.now() + 90_000).toISOString().replace(/\.\d+Z$/, '.000Z')
      await push(page, { event: 'checked', at: later })
      await expect(time(page)).toHaveAttribute('datetime', later)
      expect(later).not.toBe(before)

      // A failed check and an unwatched podman show their notes.
      await push(
        page,
        {
          event: 'error',
          source: 'github',
          error: { code: 'github_api_failed', message: 'check the open scope Issues assigned to you: HTTP 403: API rate limit exceeded' },
        },
        {
          event: 'podman',
          watching: false,
          error: { code: 'machine_stopped', message: 'podman machine podman-machine-default is stopped; the Dashboard does not start it' },
        },
      )
      await expect(page.locator('[data-note=check-error]')).toHaveText(
        'GitHub check failed: github_api_failed: check the open scope Issues assigned to you: HTTP 403: API rate limit exceeded',
      )
      await expect(page.locator('[data-note=podman]')).toHaveText(
        'Local Job events are not watched: machine_stopped: podman machine podman-machine-default is stopped; the Dashboard does not start it; GitHub is still checked.',
      )
      await expectNoPageScroll(page)
      await shot(page, `live-notes-${theme}-${width}`, false)
      // The next check clears the check error.
      await push(page, { event: 'checked', at: new Date(Date.now() + 120_000).toISOString() })
      await expect(page.locator('[data-note=check-error]')).toHaveCount(0)
      await shot(page, `live-on-${theme}-${width}`, false)

      // The stream ending turns live updates off; Refresh reconnects.
      await endStream(page)
      await expect(live(page)).toHaveAttribute('data-state', 'off')
      await expect(live(page)).toHaveText('Live updates off')
      await expect(page.locator('[data-note=live-off]')).toHaveText('Live updates stopped: stream_closed: the live stream ended. Use Refresh to reconnect.')
      await expect(page.locator('[data-note=podman]')).toHaveCount(0)
      await expectNoPageScroll(page)
      await shot(page, `live-off-${theme}-${width}`, false)
      // The data stays.
      await expect(app101(page).locator('[data-badge=paused]')).toHaveText('Paused')
      await page.getByRole('button', { name: 'Refresh' }).click()
      await expect(live(page)).toHaveAttribute('data-state', 'on')
      await expect(page.locator('[data-note=live-off]')).toHaveCount(0)
      // The new snapshot is the refresh.
      await expect(app101(page).locator('[data-badge=paused]')).toHaveCount(0)
      expect(w.live.opens).toBe(2)
      expect(w.statusReads).toBe(2)
      await expectClean(w)
    })
  }

  test(`a stream that cannot open falls back to one read, ${theme}`, async ({ page }) => {
    const w = await open(page, 375, theme, (w) => {
      w.live.answer = () => ({ httpStatus: 404, error: { code: 'not_found', message: 'live updates are not available' } })
    })
    await expect(page.locator('[data-repo]')).toHaveCount(7)
    await expect(live(page)).toHaveAttribute('data-state', 'off')
    await expect(page.locator('[data-note=live-off]')).toHaveText('Live updates stopped: not_found: live updates are not available. Use Refresh to reconnect.')
    expect(w.statusReads).toBe(1)
    for (const width of widths) {
      await page.setViewportSize({ width, height: 900 })
      await expectNoPageScroll(page)
      await shot(page, `live-fallback-${theme}-${width}`, false)
    }
    await expectClean(w, ['Failed to load resource'])
  })
}

test('a failed live re-read keeps the data with a note', async ({ page }) => {
  const w = await open(page, 768, 'dark')
  await push(page, { event: 'error', source: 'status', error: { code: 'github_api_failed', message: 'read acme/app#101: HTTP 502: Bad Gateway' } })
  await expect(page.locator('[data-note=live-read-error]')).toHaveText(
    'Live re-read failed: github_api_failed: read acme/app#101: HTTP 502: Bad Gateway. The data shown is kept.',
  )
  await expect(page.locator('[data-repo]')).toHaveCount(7)
  await shot(page, 'live-read-error-dark-768', false)
  // The next snapshot replaces the data and clears the note.
  await push(page, { event: 'snapshot', at: new Date().toISOString(), result: personView({ repos: repos.slice(0, 2) }) })
  await expect(page.locator('[data-repo]')).toHaveCount(2)
  await expect(page.locator('[data-note=live-read-error]')).toHaveCount(0)
  await expectClean(w)
})

test('without Runners the podman note stays hidden', async ({ page }) => {
  const w = await open(page, 1280, 'light', (w) => {
    w.status = { result: personView({ host: { host: { running: true }, runners: { runners: [] } } }) }
  })
  await push(page, { event: 'podman', watching: false, error: { code: 'podman_missing', message: 'podman is not installed' } })
  await page.getByRole('tab', { name: 'Person' }).click()
  await expect(live(page)).toHaveText('Live')
  await expect(page.locator('[data-note=podman]')).toHaveCount(0)
  await expectClean(w)
})
