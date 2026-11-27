import type { Locator, Page } from '@playwright/test'

// Shared scan for a rounded box with uneven borders (#488): no visible
// element may combine a border radius with sides whose border width or
// colour differ, such as an accent stripe on one edge of a rounded card.

// The Expressive Code frame is the only allowed exception (#488): its header
// (with the title) and its code block are two halves of one frame in one
// colour, joined along their shared edge.
export const borderScanExceptions = [
  '.expressive-code figcaption.header',
  '.expressive-code figcaption.header .title',
  '.expressive-code .frame > pre',
] as const

export interface UnevenBorder {
  element: string
  widths: string[]
  colors: string[]
  radii: string[]
}

// unevenRoundedBorders opens every <details> on the page, so folded content
// is scanned too, and returns each visible rounded element in root (the whole
// document by default) whose four borders differ in width, or in colour among
// the sides that have a border. exceptions defaults to borderScanExceptions.
export async function unevenRoundedBorders(page: Page, root?: Locator, exceptions: readonly string[] = borderScanExceptions): Promise<UnevenBorder[]> {
  await page.evaluate(() => {
    for (const d of document.querySelectorAll('details')) d.open = true
  })
  const scope = root ?? page.locator(':root')
  return scope.evaluate((top, exceptions) => {
    const found: UnevenBorder[] = []
    const sides = ['top', 'right', 'bottom', 'left'] as const
    const corners = ['top-left', 'top-right', 'bottom-right', 'bottom-left'] as const
    const elements = [top, ...top.querySelectorAll('*')]
    for (const el of elements) {
      if (el.getClientRects().length === 0) continue
      const box = el.getBoundingClientRect()
      if (box.width === 0 || box.height === 0) continue
      const style = getComputedStyle(el)
      if (style.visibility !== 'visible') continue
      const radii = corners.map((c) => style.getPropertyValue(`border-${c}-radius`))
      if (!radii.some((r) => r.split(/\s+/).some((v) => parseFloat(v) > 0))) continue
      const widths = sides.map((s) => style.getPropertyValue(`border-${s}-width`))
      const colors = sides.map((s) => style.getPropertyValue(`border-${s}-color`))
      const px = widths.map((w) => parseFloat(w) || 0)
      const unevenWidth = px.some((w) => w !== px[0])
      const drawn = new Set(colors.filter((_, i) => px[i] > 0))
      if (!unevenWidth && drawn.size <= 1) continue
      if (exceptions.some((sel) => el.matches(sel))) continue
      const slot = el.getAttribute('data-slot')
      const classes = typeof el.className === 'string' ? el.className.trim().split(/\s+/).filter(Boolean).slice(0, 4) : []
      const name = el.tagName.toLowerCase() + (el.id ? `#${el.id}` : '') + classes.map((c) => `.${c}`).join('') + (slot ? `[data-slot=${slot}]` : '')
      found.push({ element: name, widths, colors, radii })
    }
    return found
  }, exceptions)
}
