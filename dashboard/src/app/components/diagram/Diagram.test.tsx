import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { framesExample, lanesExample, pipelineExample } from '@/screenshots/diagram-fixtures'

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

  it('shows a legend of the kinds and names them in the text list', () => {
    const html = renderToStaticMarkup(<Diagram spec={pipelineExample} title="Pipeline" />)
    const legend = html.match(/<div[^>]*data-slot="diagram-legend"[^>]*>(.*?)<\/ul><\/div>/)![1]
    expect(legend).toContain('Who works')
    expect([...legend.matchAll(/data-kind="([^"]+)"/g)].map((m) => m[1])).toEqual(['hands', 'machine', 'wait'])
    expect(legend).toContain('The saw, the sander or the oven.')
    const list = html.match(/<ol class="sr-only">(.*)<\/ol>/)![1]
    const items = [...list.matchAll(/<li>(.*?)<\/li>/g)].map((m) => m[1].replaceAll('<!-- -->', ''))
    expect(items[0]).toBe('Cut: Saw the parts to size. Who works: A machine; Handwork. Leads to Sand.')
    expect(items).toContain('Paint: Two thin coats. Who works: Handwork; Waiting for it to dry. Leads to On the shelf.')
    // Each kind marker carries its title as text in both drawings.
    expect(html.match(/data-marker="wait"/g)).toHaveLength(4)
    // Icons are drawn, still without inline styles.
    expect(html).toContain('lucide-scissors')
    expect(html).not.toMatch(/\sstyle="/)
    expect(html).not.toContain('data-slot="diagram-flow"')
  })

  it('animates the flow overlay only without reduced motion', () => {
    const html = renderToStaticMarkup(<Diagram flow spec={pipelineExample} title="Pipeline" />)
    const overlays = [...html.matchAll(/<path[^>]*data-slot="diagram-flow"[^>]*>/g)].map((m) => m[0])
    expect(overlays).toHaveLength(2 * pipelineExample.edges.length)
    for (const o of overlays) {
      const cls = o.match(/class="([^"]*)"/)![1].split(' ')
      // Hidden by default; shown and animated only under motion-safe.
      expect(cls).toContain('hidden')
      expect(cls).toContain('motion-safe:inline')
      expect(cls.filter((c) => c.includes('animate-'))).toEqual(['motion-safe:animate-diagram-flow'])
    }
    expect(html).not.toMatch(/\sstyle="/)
  })

  it('draws no legend without kinds', () => {
    expect(renderToStaticMarkup(<Diagram spec={framesExample} title="x" />)).not.toContain('diagram-legend')
  })

  it('fails loudly on an invalid spec', () => {
    expect(() =>
      renderToStaticMarkup(<Diagram spec={{ nodes: [{ id: 'a', title: 'A' }], edges: [{ from: 'a', to: 'b' }] }} title="x" />),
    ).toThrow('unknown node "b"')
  })
})

// listItems reads the visually hidden list's items as text.
function listItems(html: string): string[] {
  const list = html.match(/<ol class="sr-only">(.*)<\/ol>/)![1]
  return [...list.matchAll(/<li>(.*?)<\/li>/g)].map((m) => m[1].replaceAll('<!-- -->', ''))
}

// declaredLast is declared before the node it follows, so its declaration
// order differs from its placement order.
const declaredLast = {
  nodes: [
    { id: 'b', title: 'Second' },
    { id: 'a', title: 'First' },
  ],
  edges: [{ from: 'a', to: 'b' }],
}

describe('Diagram options', () => {
  it('draws only top to bottom at every width with layout="vertical"', () => {
    const html = renderToStaticMarkup(<Diagram spec={lanesExample} title="Lanes" layout="vertical" />)
    const svgs = html.match(/<svg[^>]*>/g) ?? []
    expect(svgs).toHaveLength(1)
    expect(svgs[0]).toContain('aria-hidden="true"')
    expect(svgs[0]).toContain('data-direction="TB"')
    const wrapper = html.match(/<div class="([^"]*)" data-slot="diagram-vertical">/)
    expect(wrapper).not.toBeNull()
    expect(wrapper![1].split(' ')).not.toContain('hidden')
    expect(wrapper![1].split(' ')).not.toContain('md:hidden')
    expect(wrapper![1].split(' ')).toContain('overflow-x-auto')
    expect(html).not.toMatch(/\sstyle="/)
    expect(listItems(html)).toHaveLength(lanesExample.nodes.length)
  })

  it('lists the nodes in the drawing order by default', () => {
    expect(listItems(renderToStaticMarkup(<Diagram spec={declaredLast} title="x" />))).toEqual([
      'First. Leads to Second.',
      'Second.',
    ])
    expect(listItems(renderToStaticMarkup(<Diagram spec={declaredLast} title="x" layout="vertical" />))).toEqual([
      'First. Leads to Second.',
      'Second.',
    ])
  })

  it('lists the nodes in declaration order with readingOrder="spec"', () => {
    expect(listItems(renderToStaticMarkup(<Diagram spec={declaredLast} title="x" readingOrder="spec" />))).toEqual([
      'Second.',
      'First. Leads to Second.',
    ])
  })
})

describe('Diagram text list', () => {
  it('adds no second full stop after text that ends with one', () => {
    const html = renderToStaticMarkup(
      <Diagram spec={{ nodes: [{ id: 'a', title: 'A', detail: 'Ends here.' }, { id: 'b', title: 'B?' }], edges: [] }} title="x" />,
    )
    expect(listItems(html)).toEqual(['A: Ends here.', 'B?'])
  })
})

describe('Diagram layout', () => {
  it('draws both by default', () => {
    const html = renderToStaticMarkup(<Diagram spec={lanesExample} title="Lanes" />)
    const svgs = html.match(/<svg[^>]*>/g) ?? []
    expect(svgs.map((s) => s.match(/data-direction="(\w+)"/)![1])).toEqual(['LR', 'TB'])
    expect(html).toContain('md:hidden')
  })
})
