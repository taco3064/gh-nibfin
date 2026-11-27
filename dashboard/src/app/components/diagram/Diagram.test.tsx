import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { framesExample, lanesExample } from '@/screenshots/diagram-fixtures'

import { Diagram } from './Diagram'

// The site renders the diagram without a client directive, so it must render
// on the server without a DOM, as Astro does at build time.
describe('Diagram', () => {
  it('renders statically with a caption, two hidden drawings and a text list', () => {
    const html = renderToStaticMarkup(
      <Diagram spec={framesExample} title="From idea to shelf" description="A fictional example." />,
    )
    expect(html).toMatch(/^<figure[^>]*class="[^"]*not-content/)
    expect(html).toContain('<figcaption')
    expect(html).toContain('From idea to shelf')
    expect(html).toContain('A fictional example.')
    const svgs = html.match(/<svg[^>]*>/g) ?? []
    expect(svgs).toHaveLength(2)
    for (const svg of svgs) expect(svg).toContain('aria-hidden="true"')
    expect(svgs[0]).toContain('data-direction="LR"')
    expect(svgs[1]).toContain('data-direction="TB"')
    // No inline styles: the Dashboard's CSP has style-src 'self'.
    expect(html).not.toMatch(/\sstyle="/)
    // Each drawing has its own arrowhead marker.
    const markers = [...html.matchAll(/<marker id="([^"]+)"/g)].map((m) => m[1])
    expect(new Set(markers).size).toBe(2)

    const list = html.match(/<ol class="sr-only">(.*)<\/ol>/)![1]
    const items = [...list.matchAll(/<li>(.*?)<\/li>/g)].map((m) => m[1].replaceAll('<!-- -->', ''))
    expect(items).toHaveLength(framesExample.nodes.length)
    expect(items[0]).toBe(
      'Idea: Someone writes down a wish. Labels: draft. Group: Plan at the desk. Leads to Sketch, to Notes (unclear).',
    )
    expect(items).toContain('Archive the notes.')
  })

  it('names lanes as lanes in the text list', () => {
    const html = renderToStaticMarkup(<Diagram spec={lanesExample} title="Lanes" />)
    expect(html).toContain('Lane: Make in the workshop.')
    expect(html).not.toContain('Group:')
  })

  it('fails loudly on an invalid spec', () => {
    expect(() =>
      renderToStaticMarkup(<Diagram spec={{ nodes: [{ id: 'a', title: 'A' }], edges: [{ from: 'a', to: 'b' }] }} title="x" />),
    ).toThrow('unknown node "b"')
  })
})
