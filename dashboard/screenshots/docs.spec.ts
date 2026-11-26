import { mkdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

import { expect, test, type Page } from '@playwright/test'

import { hosts, personView } from './person-fixtures'
import { repoStatus } from './repo-fixtures'
import { expectClean, prepare, storageKey, type Watch } from './support'

// The website's Dashboard images (#363): the person, repo and Runner views of
// the Dashboard built from this commit, served by the real Go server with
// fictional fixture data. npm run docs-screenshots writes them into the
// tracked src/app/assets/dashboard/, which the Setup and management page
// shows; the review screenshots of the other specs stay in .screenshots/.

const assetDir = fileURLToPath(new URL('../src/app/assets/dashboard/', import.meta.url))
mkdirSync(assetDir, { recursive: true })

const latestImage = { ref: 'ghcr.io/acme/nibfin:v1.4.0' }

const tab = (page: Page, name: string) => page.getByRole('tablist', { name: 'Views' }).getByRole('tab', { name })

async function open(page: Page, status: unknown): Promise<Watch> {
  await page.setViewportSize({ width: 1280, height: 800 })
  await page.addInitScript(([k, t]) => localStorage.setItem(k, t), [storageKey, 'dark'])
  const w = await prepare(page, { result: status })
  w.repoStatus = { ...repoStatus }
  await page.goto('/#proof=SCREENSHOT')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expect(page.locator('[data-content] > *').first()).toBeVisible()
  await page.evaluate(() => document.fonts.ready.then(() => undefined))
  return w
}

// capture writes one image after checking that the session proof is
// neither in the address nor anywhere on the page.
async function capture(page: Page, w: Watch, name: string) {
  // No focus ring from the clicks that opened the view.
  await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur())
  expect(page.url()).not.toContain('SCREENSHOT')
  expect(await page.locator('body').innerText()).not.toContain('SCREENSHOT')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.screenshot({ path: `${assetDir}${name}.png`, animations: 'disabled' })
  await expectClean(w)
}

test('person view', async ({ page }) => {
  const w = await open(page, personView({ latestImage }))
  await expect(tab(page, 'Person')).toHaveAttribute('aria-selected', 'true')
  await expect(page.locator('[data-view=person] article[data-issue]').first()).toBeVisible()
  await capture(page, w, 'person')
})

test('repo view', async ({ page }) => {
  const w = await open(page, personView({ latestImage }))
  await tab(page, 'Repositories').click()
  await page.locator('[data-view=repo] [data-slot=picker] [data-repo-entry="acme/app"]').click()
  await expect(page.locator('[data-slot=repo-summary]')).toBeVisible()
  await expect(page.locator('[data-slot=edges] path[data-edge]').first()).toBeAttached()
  await capture(page, w, 'repo')
})

test('Runner view', async ({ page }) => {
  const w = await open(page, personView({ latestImage, host: hosts.mixed }))
  await tab(page, 'Runners').click()
  await expect(page.locator('[data-card=host]')).toBeVisible()
  await expect(page.locator('[data-provider=codex]')).toBeVisible()
  await capture(page, w, 'runner')
})
