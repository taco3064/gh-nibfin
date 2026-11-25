import { mkdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

import { expect, test, type Page } from '@playwright/test'

// Screenshots and checks of the public site (#362), served from dist-site/
// under its base path by vite preview. Console errors fail the spec.

const outDir = fileURLToPath(new URL('../.screenshots/', import.meta.url))
mkdirSync(outDir, { recursive: true })

const widths = [375, 768, 1280, 1920]
const themes = ['dark', 'light'] as const
const pages = [
  { name: 'home', path: './' },
  { name: 'quick-start', path: 'quick-start/' },
  { name: 'how-it-works', path: 'how-it-works/' },
]

type Theme = (typeof themes)[number]

// open loads a page with Starlight's stored theme and records console errors.
async function open(page: Page, path: string, theme: Theme, width: number): Promise<string[]> {
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
