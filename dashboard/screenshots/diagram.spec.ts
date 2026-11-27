import { expect, test, type Page } from '@playwright/test'

import { expectClean, expectGutter, expectNonce, prepare, shot, storageKey, widths } from './support'

// Screenshots and checks of the shared site diagram (#456) in its harness
// (diagram.html), served by the real Go server with its CSP. The examples are
// fictional and appear on no site page.

const themes = ['dark', 'light'] as const
const idea = { items: 7, first: 'Idea: Someone writes down a wish.', legend: false }
const examples = [idea, idea, { items: 6, first: 'Cut: Saw the parts to size.', legend: true }]

async function open(page: Page, width: number, theme: (typeof themes)[number]) {
  await page.setViewportSize({ width, height: 900 })
  await page.addInitScript(([k, t]) => localStorage.setItem(k, t), [storageKey, theme])
  const w = await prepare(page)
  const res = await page.goto('/diagram.html')
  await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
  await expect(page.locator('figure[data-slot=diagram]')).toHaveCount(examples.length)
  await expectNonce(page, res)
  await page.evaluate(() => document.fonts.ready.then(() => undefined))
  return w
}

for (const theme of themes) {
  for (const width of widths) {
    test(`diagram ${theme} ${width}px`, async ({ page }) => {
      const w = await open(page, width, theme)
      const figures = page.locator('figure[data-slot=diagram]')
      for (const [i, example] of examples.entries()) {
        const figure = figures.nth(i)
        // The left-to-right drawing from 768 px up, the top-to-bottom one below.
        const wide = width >= 768
        await expect(figure.locator('svg[data-direction=LR]')).toBeVisible({ visible: wide })
        await expect(figure.locator('svg[data-direction=TB]')).toBeVisible({ visible: !wide })
        for (const svg of await figure.locator('svg').all()) await expect(svg).toHaveAttribute('aria-hidden', 'true')
        // The text alternative lists every node.
        await expect(figure.locator('ol > li')).toHaveCount(example.items)
        await expect(figure.locator('ol > li').first()).toContainText(example.first)
        await expect(figure.locator('[data-slot=diagram-legend]')).toBeVisible({ visible: example.legend })
        // The shown drawing fits its figure, or scrolls inside it only.
        const fits = await figure.evaluate((f) => {
          const box = f.getBoundingClientRect()
          const svg = [...f.querySelectorAll('svg')].find((s) => s.getBoundingClientRect().width > 0)!
          const scroller = svg.parentElement!
          return { figure: box.right <= window.innerWidth, scroller: scroller.getBoundingClientRect().right <= box.right + 0.5 }
        })
        expect(fits).toEqual({ figure: true, scroller: true })
      }
      await expectGutter(page)
      await expectClean(w)
      await shot(page, `diagram-${theme}-${width}`)
    })
  }
}
