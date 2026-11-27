import { expect, test, type Page } from '@playwright/test'

import { expectDiagramFits, type Shown } from './diagram-fit'
import { expectClean, expectGutter, expectNonce, prepare, shot, storageKey, widths } from './support'

// Screenshots and checks of the shared site diagram (#456) in its harness
// (diagram.html), served by the real Go server with its CSP. The examples are
// fictional and appear on no site page.

const themes = ['dark', 'light'] as const
const idea = { items: 7, first: 'Idea: Someone writes down a wish.', legend: false }
// shown is the presentation each example shows at each width in the
// harness's column (at most 976 px): together they show every one.
const examples: { items: number; first: string; legend: boolean; shown: Record<number, Shown> }[] = [
  { ...idea, shown: { 375: 'stacked', 768: 'TB', 1280: 'LR', 1920: 'LR' } },
  { ...idea, shown: { 375: 'stacked', 768: 'TB', 1280: 'LR', 1920: 'LR' } },
  { items: 6, first: 'Cut: Saw the parts to size.', legend: true, shown: { 375: 'TB', 768: 'LR', 1280: 'LR', 1920: 'LR' } },
]

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
        for (const svg of await figure.locator('svg').all()) await expect(svg).toHaveAttribute('aria-hidden', 'true')
        // The text alternative lists every node.
        await expect(figure.locator('ol > li')).toHaveCount(example.items)
        await expect(figure.locator('ol > li').first()).toContainText(example.first)
        await expect(figure.locator('[data-slot=diagram-legend]')).toBeVisible({ visible: example.legend })
        // Exactly one presentation shows; it fits without sideways scroll.
        expect(await expectDiagramFits(figure, `example ${i} ${width}px ${theme}`)).toBe(example.shown[width])
      }
      await expectGutter(page)
      await expectClean(w)
      await shot(page, `diagram-${theme}-${width}`)
    })
  }
}
