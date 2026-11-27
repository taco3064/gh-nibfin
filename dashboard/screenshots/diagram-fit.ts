import { expect, type Locator } from '@playwright/test'

// Shared check of a site diagram figure (#474): of its presentations (a
// drawing, the stacked layout, or the rows of the label states, #476) exactly
// one shows, it fits the figure without sideways scroll, and no text leaves
// its node, pill or block.

export type Shown = 'LR' | 'TB' | 'stacked' | 'rows'

export interface Fit {
  shown: Shown[]
  // height is the shown presentation's rendered height, in CSS px.
  height?: number
  // problems lists every overflow found, empty when everything fits.
  problems: string[]
  // scale is the shown drawing's rendered width over its natural width.
  scale?: number
}

export async function measureDiagram(figure: Locator): Promise<Fit> {
  await figure.scrollIntoViewIfNeeded()
  return figure.evaluate((fig) => {
    const eps = 0.75
    const problems: string[] = []
    const inside = (outer: DOMRect, inner: DOMRect) =>
      inner.left >= outer.left - eps &&
      inner.right <= outer.right + eps &&
      inner.top >= outer.top - eps &&
      inner.bottom <= outer.bottom + eps
    const showing = (el: Element) => getComputedStyle(el).display !== 'none' && el.getBoundingClientRect().width > 0
    const box = fig.getBoundingClientRect()
    if (box.right > window.innerWidth + eps) problems.push(`figure ends at ${box.right} past the viewport ${window.innerWidth}`)
    const all = [...fig.querySelectorAll<HTMLElement>('[data-slot=diagram-drawing], [data-slot=diagram-stacked], [data-slot=diagram-rows]')]
    const visible = all.filter(showing)
    const slots: Record<string, Shown> = { 'diagram-stacked': 'stacked', 'diagram-rows': 'rows' }
    const shown = visible.map((el) => (slots[el.dataset.slot!] ?? el.dataset.direction!) as Shown)
    let scale: number | undefined
    const height = visible.length === 1 ? visible[0].getBoundingClientRect().height : undefined
    for (const el of visible) {
      if (el.scrollWidth > el.clientWidth) problems.push(`${el.dataset.slot} scrolls: ${el.scrollWidth} > ${el.clientWidth}`)
      if (!inside(box, el.getBoundingClientRect())) problems.push(`${el.dataset.slot} leaves the figure`)
      const svg = el.querySelector<SVGSVGElement>('svg[data-direction]')
      if (svg) {
        scale = svg.getBoundingClientRect().width / svg.viewBox.baseVal.width
        for (const node of svg.querySelectorAll<SVGGElement>('g[data-node]')) {
          const id = node.dataset.node
          const rect = node.querySelector(':scope > rect')!.getBoundingClientRect()
          for (const t of node.querySelectorAll('text')) {
            if (!inside(rect, t.getBoundingClientRect())) problems.push(`node ${id}: "${t.textContent}" leaves its node`)
          }
          for (const badge of node.querySelectorAll<SVGGElement>('g[data-badge]')) {
            const pill = badge.querySelector('rect')!.getBoundingClientRect()
            if (!inside(rect, pill)) problems.push(`node ${id}: badge ${badge.dataset.badge} leaves its node`)
            if (!inside(pill, badge.querySelector('text')!.getBoundingClientRect())) {
              problems.push(`node ${id}: badge ${badge.dataset.badge} text leaves its pill`)
            }
          }
        }
      } else {
        for (const block of el.querySelectorAll<HTMLElement>('[data-block]')) {
          const r = block.getBoundingClientRect()
          if (block.scrollWidth > block.clientWidth) problems.push(`block ${block.dataset.block} scrolls`)
          if (!inside(el.getBoundingClientRect(), r)) problems.push(`block ${block.dataset.block} leaves the stack`)
          for (const child of block.querySelectorAll('*')) {
            if (!inside(r, child.getBoundingClientRect())) problems.push(`block ${block.dataset.block}: "${child.textContent}" leaves it`)
          }
        }
      }
    }
    return { shown, problems, scale, height }
  })
}

// expectDiagramFits checks one figure and returns which presentation shows.
export async function expectDiagramFits(figure: Locator, what: string): Promise<Shown> {
  const fit = await measureDiagram(figure)
  expect(fit.shown, `${what}: one presentation shows`).toHaveLength(1)
  expect(fit.problems, what).toEqual([])
  // A drawing shows at no less than about 80% of its size (75% with the
  // 64 px steps of its floor).
  if (fit.scale !== undefined) expect(fit.scale, `${what}: drawing scale`).toBeGreaterThanOrEqual(0.66)
  return fit.shown[0]
}
