import { expect, test, type Page } from '@playwright/test'

import { frame, push } from './live-mock'
import { personView } from './person-fixtures'
import { repoStatus } from './repo-fixtures'
import { expectClean, expectNonce, holdReads, mockHeldReads, prepare, pushRead, shot, storageKey, widths, type Watch } from './support'

// Screenshots and checks of the progressive reads (#384): a status read
// shows each section as it arrives, with its own labelled loading state,
// and a partial never replaces a complete read. Every answer is a
// fictional mock.

const themes = ['dark', 'light'] as const
type Theme = (typeof themes)[number]

type Obj = Record<string, unknown>

// strip drops the Issue fields still loading from every Issue.
function strip(issues: Obj[], keys: string[]): Obj[] {
  return issues.map((i) => Object.fromEntries(Object.entries(i).filter(([k]) => !keys.includes(k))))
}

const issueFields = ['pendingComment', 'pendingCommentError', 'blockedBy', 'blockedByError', 'runs', 'runsError', 'runsTruncated']
const app = repoStatus['github.com/acme/app'].result as Obj

// repoPartials are two partial reads of acme/app: the Issues first, then
// the blockers, permission and image.
const repoPartials = [
  {
    result: { repo: app.repo, issues: strip(app.issues as Obj[], issueFields) },
    loading: ['pendingComment', 'blockedBy', 'permission', 'image', 'nibfinRun', 'nibfinRepos', 'host', 'latestImage', 'runs'],
  },
  {
    result: {
      repo: app.repo,
      issues: strip(app.issues as Obj[], ['runs', 'runsError', 'runsTruncated']),
      permission: app.permission,
      image: { ...(app.image as Obj), latest: null },
    },
    loading: ['nibfinRun', 'nibfinRepos', 'host', 'latestImage', 'runs'],
  },
]

const person = personView() as Obj
const personPartial = {
  result: {
    githubHost: person.githubHost,
    user: person.user,
    truncated: false,
    repos: (person.repos as Obj[]).map((r) => ({ ...r, image: { ref: null, latest: null }, permission: null, issues: strip(r.issues as Obj[], issueFields) })),
  },
  loading: ['host', 'latestImage', 'pendingComment', 'image', 'blockedBy', 'permission', 'nibfinRepos', 'runs'],
}

async function setup(page: Page, width: number, theme: Theme): Promise<Watch> {
  await page.setViewportSize({ width, height: 900 })
  await page.addInitScript(([k, t]) => localStorage.setItem(k, t), [storageKey, theme])
  const w = await prepare(page)
  w.repoStatus = { ...repoStatus }
  await mockHeldReads(page)
  return w
}

async function expectNoPageScroll(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
}

const tab = (page: Page, name: string) => page.getByRole('tablist', { name: 'Views' }).getByRole('tab', { name })
const loadingOf = (page: Page, key: string) => page.locator(`[data-view] [data-loading=${key}]`)

for (const theme of themes) {
  for (const width of widths) {
    test(`a repository read fills in section by section at ${width}px, ${theme}`, async ({ page }) => {
      const w = await setup(page, width, theme)
      const res = await page.goto('/#proof=SCREENSHOT')
      await expectNonce(page, res)
      await expect(page.locator('[data-view=person]')).toBeVisible()
      await tab(page, 'Repositories').click()
      await page.evaluate(() => document.fonts.ready.then(() => undefined))
      await holdReads(page, true)
      await page.locator('[data-view=repo] [data-slot=picker] [data-repo-entry="acme/app"]').click()

      // The Issues come first; every other section says it is still read.
      await pushRead(page, [{ event: 'partial', ...repoPartials[0] }])
      const summary = page.locator('[data-slot=repo-summary]')
      await expect(summary).toBeVisible()
      await expect(loadingOf(page, 'image')).toHaveText('Reading the image pin…')
      await expect(loadingOf(page, 'nibfinRun')).toHaveText('Reading the Nibfin run…')
      await expect(loadingOf(page, 'workflows')).toHaveText('Reading the workflows…')
      await expect(loadingOf(page, 'summary')).toHaveText('Still reading: permission, host, latest image')
      await expect(loadingOf(page, 'blockedBy').first()).toBeVisible()
      await expect(page.locator('[data-view=repo] article[data-issue]')).toHaveCount(3)
      await expect(page.locator('[data-view=repo] article[data-issue] :is([data-action=pause], [data-action=resume])').first()).toBeDisabled()
      await expect(page.locator('[data-view=repo] [data-slot=disabled-reason]')).toHaveCount(0)
      await expect(page.locator('[data-view=repo] article[data-issue] .sr-only', { hasText: 'Reading the permission on acme/app…' }).first()).toBeAttached()
      await expect(page.locator('[data-state=repo-reading]')).toHaveText('Reading the rest of github.com/acme/app…')
      await expect(page.locator('[data-state=repo-reading]')).toBeVisible()
      await expectNoPageScroll(page)
      await shot(page, `partial-repo-issues-${theme}-${width}`)

      // Then the blockers, the permission and the image pin, still to be
      // compared with the latest image.
      await pushRead(page, [{ event: 'partial', ...repoPartials[1] }])
      await expect(loadingOf(page, 'image')).toHaveCount(0)
      await expect(loadingOf(page, 'latestImage')).toHaveText('Comparing with the latest image…')
      await expect(loadingOf(page, 'workflows')).toHaveCount(0)
      await expect(page.locator('[data-slot=workflows]')).toHaveAttribute('data-workflows', 'execution')
      await expect(loadingOf(page, 'blockedBy')).toHaveCount(0)
      await expect(page.locator('[data-slot=edges] path[data-edge]')).toHaveCount(3)
      await expect(loadingOf(page, 'runs').first()).toHaveText('Reading runs…')
      await expect(page.locator('[data-view=repo] article[data-issue] :is([data-action=pause], [data-action=resume])').first()).toBeEnabled()
      await expectNoPageScroll(page)
      await shot(page, `partial-repo-image-${theme}-${width}`)

      // The result completes the view: no section is loading any more.
      await pushRead(page, [{ event: 'result', result: app }], true)
      await expect(page.locator('[data-slot=nibfin-run] [data-state=succeeded]')).toBeVisible()
      await expect(page.locator('[data-view=repo] [data-slot=section-loading]')).toHaveCount(0)
      await expect(page.locator('[data-state=repo-reading]')).toHaveCount(0)
      await expect(page.locator('[data-slot=freshness]')).toHaveAttribute('data-state', 'current')

      // A Refresh keeps the complete read on screen: its partials never
      // replace it, and its result does.
      await page.getByRole('button', { name: 'Refresh' }).click()
      await expect(page.getByRole('button', { name: 'Refreshing…' })).toBeDisabled()
      await pushRead(page, [{ event: 'partial', ...repoPartials[0] }])
      await page.waitForTimeout(100)
      await expect(page.locator('[data-view=repo] [data-slot=section-loading]')).toHaveCount(0)
      await expect(page.locator('[data-slot=nibfin-run] [data-state=succeeded]')).toBeVisible()
      await pushRead(page, [{ event: 'result', result: app }], true)
      await expect(page.getByRole('button', { name: 'Refresh' })).toBeEnabled()
      await expect(page.locator('[data-view=repo] [data-slot=section-loading]')).toHaveCount(0)
      expect(w.statusBodies).toEqual([])
      await expectClean(w)
    })

    test(`the person view fills in section by section at ${width}px, ${theme}`, async ({ page }) => {
      const w = await setup(page, width, theme)
      w.live.answer = () => ({ frames: [frame({ event: 'partial', at: new Date().toISOString(), ...personPartial })], open: true })
      const res = await page.goto('/#proof=SCREENSHOT')
      await expectNonce(page, res)
      await page.evaluate(() => document.fonts.ready.then(() => undefined))
      const view = page.locator('[data-view=person]')
      await expect(view).toBeVisible()
      await expect(view.locator('[data-repo]').first()).toBeVisible()
      await expect(view.locator('[data-loading=image]').first()).toHaveText('Reading the image pin…')
      await expect(view.locator('[data-loading=runs]').first()).toHaveText('Reading runs…')
      await expect(view.locator('[data-loading=blockedBy]').first()).toHaveText('Reading blockers…')
      await expect(view.locator('[data-slot=disabled-reason]')).toHaveCount(0)
      // The tiles count what is read and label what is still being read.
      await expect(view.locator('[data-tile=issues] [data-slot=tile-count]')).toHaveText('9Issues')
      await expect(view.locator('[data-tile=runs] [data-loading=runs]')).toHaveText('Reading runs…')
      await expect(view.locator('[data-tile=runners] [data-loading=host]')).toHaveText('Reading the host…')
      await expectNoPageScroll(page)
      await shot(page, `partial-person-${theme}-${width}`)

      // The Runners view waits for the host.
      await tab(page, 'Runners').click()
      await expect(page.locator('[data-content=runner] [data-state=reading]')).toHaveText('Reading the host…')
      await tab(page, 'Person').click()

      await push(page, { event: 'snapshot', at: new Date().toISOString(), result: person })
      await expect(view.locator('[data-slot=section-loading]')).toHaveCount(0)
      await expect(view.locator('[data-tile=runners] [data-slot=runner-counts]')).toHaveText('2 online · 0 busy')
      await expect(page.locator('[data-slot=live]')).toHaveAttribute('data-state', 'on')
      await expectClean(w)
    })
  }
}
