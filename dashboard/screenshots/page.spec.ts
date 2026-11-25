import { expect, test, type Page } from '@playwright/test'

import { expectClean, expectNonce, prepare, shot, storageKey, unauthorized, widths } from './support'

// Screenshots of the Dashboard page and its theme control, served by the
// real Go server with its CSP. The API is mocked with fictional bodies, so no
// gh login, real proof or session is involved.

// settle waits for the connection state, or for the person view once the
// session is open.
async function settle(page: Page, state: string) {
  const ready = state === 'connected' ? '[data-view="person"]' : `[role=status][data-state="${state}"]`
  await expect(page.locator(ready)).toBeVisible()
  await page.evaluate(() => document.fonts.ready.then(() => undefined))
  const fonts = await page.evaluate(() => [
    document.fonts.check('16px "DM Sans Variable"'),
    document.fonts.check('16px "Space Grotesk Variable"'),
  ])
  expect(fonts).toEqual([true, true])
  // No horizontal page scroll at any width.
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
}

async function expectTheme(page: Page, theme: string, stored: string | null) {
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
  expect(await page.evaluate((k) => localStorage.getItem(k), storageKey)).toBe(stored)
}

for (const width of widths) {
  test(`connected page and theme control at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const w = await prepare(page)
    await page.goto('/#proof=SCREENSHOT')
    await settle(page, 'connected')
    expect(new URL(page.url()).hash).toBe('')
    await expectTheme(page, 'dark', null)
    await shot(page, `dark-${width}`)

    const group = page.getByRole('radiogroup', { name: 'Theme' })
    await group.getByRole('radio', { name: 'Light' }).click()
    await expectTheme(page, 'light', 'light')
    await expect(group.getByRole('radio', { name: 'Light' })).toHaveAttribute('aria-checked', 'true')
    await shot(page, `light-${width}`)

    // Keyboard: Tab into the group lands on the selected item; the arrow
    // key moves focus to System without selecting it.
    // Clicking the heading moves the Tab starting point before the group.
    await page.getByRole('heading', { level: 1 }).click()
    await page.keyboard.press('Tab')
    await expect(group.getByRole('radio', { name: 'Light' })).toBeFocused()
    await page.keyboard.press('ArrowRight')
    await expect(group.getByRole('radio', { name: 'System' })).toBeFocused()
    await shot(page, `light-${width}-control-focus`)

    // System follows the OS preference live.
    await page.keyboard.press('Space')
    await expectTheme(page, 'light', 'system')
    await page.emulateMedia({ colorScheme: 'dark' })
    await expectTheme(page, 'dark', 'system')
    await page.emulateMedia({ colorScheme: 'light' })
    await expectTheme(page, 'light', 'system')

    await group.getByRole('radio', { name: 'Dark' }).click()
    await expectTheme(page, 'dark', 'dark')
    await expectClean(w)
  })
}

test('the theme persists and a reload finds the proof used', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 })
  const w = await prepare(page)
  await page.goto('/#proof=SCREENSHOT')
  await settle(page, 'connected')
  await page.getByRole('radio', { name: 'Light' }).click()
  await expectTheme(page, 'light', 'light')
  await expectClean(w)

  // The fragment is gone, so a reload has no proof but keeps the theme.
  await page.reload()
  await settle(page, 'no-proof')
  await expectTheme(page, 'light', 'light')
  // Opening the session URL again finds the proof used.
  await page.goto('about:blank')
  await page.goto('/#proof=SCREENSHOT')
  await settle(page, 'failed')
  await expectTheme(page, 'light', 'light')
  await expect(page.getByRole('status')).toContainText('already used or is invalid')
  await shot(page, 'proof-invalid-1280')
  await expectClean(w, [unauthorized])
})

test('the page without a fragment asks for the session URL', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 })
  const w = await prepare(page)
  const res = await page.goto('/')
  await settle(page, 'no-proof')
  // Every load gets a fresh nonce, the same in the header and the meta.
  const first = await expectNonce(page, res)
  expect(await expectNonce(page, await page.reload())).not.toBe(first)
  await settle(page, 'no-proof')
  await expect(page.getByRole('status')).toContainText('Open the session URL printed by gh nibfin dashboard.')
  await shot(page, 'no-proof-1280')
  await expectClean(w)
})
