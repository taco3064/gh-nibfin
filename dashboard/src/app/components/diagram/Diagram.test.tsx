import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { framesExample, lanesExample, pipelineExample } from '@/screenshots/diagram-fixtures'

import { architecture } from '../../lib/architecture'
import { issueFlow } from '../../lib/issue-flow'
import { labelStateDiagram } from '../../lib/label-flow'
import { labelStates } from '../../lib/labels'
import { stageLanes } from '../../lib/stage-lanes'

import { Diagram } from './Diagram'
import type { DiagramSpec } from './layout'

const siteSpecs = [issueFlow, stageLanes, labelStateDiagram(labelStates), architecture]

// The site renders the diagram without a client directive, so it must render
// on the server without a DOM, as Astro does at build time.
describe('Diagram', () => {
  it('renders statically with a caption, hidden presentations and a text list', () => {
    const html = renderToStaticMarkup(
      <Diagram spec={framesExample} title="From idea to shelf" description="A fictional example." />,
    )
    expect(html).toMatch(/^<figure[^>]*class="[^"]*not-content/)
    expect(html).toContain('<figcaption')
    expect(html).toContain('From idea to shelf')
    expect(html).toContain('A fictional example.')
    const svgs = drawings(html)
    expect(svgs).toHaveLength(2)
    for (const svg of svgs) expect(svg).toContain('aria-hidden="true"')
    expect(svgs[0]).toContain('data-direction="LR"')
    expect(svgs[1]).toContain('data-direction="TB"')
    // The figure is a size container; each presentation shows only where its
    // box fits it and no earlier one fits: left to right from 896 px (80% of
    // 1159), top to bottom from 384 px (80% of 500), the stacked layout below.
    expect(html).toContain('<div class="@container min-w-0" data-slot="diagram-box">')
    expect(presentations(html)).toEqual([
      { slot: 'diagram-drawing', classes: 'min-w-0 hidden @min-[896px]:block' },
      { slot: 'diagram-drawing', classes: 'min-w-0 hidden @min-[384px]:block @min-[896px]:hidden' },
      { slot: 'diagram-stacked', classes: 'block @min-[384px]:hidden' },
    ])
    // Nothing scrolls sideways.
    expect(html).not.toContain('overflow-x-auto')
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
    // Each kind marker carries its title as text in both drawings; the TB
    // drawing always fits, so there is no stacked layout.
    expect(html).not.toContain('diagram-stacked')
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
  it('draws only top to bottom with layout="vertical"', () => {
    const html = renderToStaticMarkup(<Diagram spec={lanesExample} title="Lanes" layout="vertical" />)
    const svgs = drawings(html)
    expect(svgs).toHaveLength(1)
    expect(svgs[0]).toContain('aria-hidden="true"')
    expect(svgs[0]).toContain('data-direction="TB"')
    // The TB drawing (724 wide) from 576 px, the stacked layout below.
    expect(presentations(html)).toEqual([
      { slot: 'diagram-drawing', classes: 'min-w-0 hidden @min-[576px]:block' },
      { slot: 'diagram-stacked', classes: 'block @min-[576px]:hidden' },
    ])
    expect(html).not.toContain('md:')
    expect(html).not.toContain('overflow-x-auto')
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

describe('Diagram presentations', () => {
  it('never renders a drawing too wide to fit at 80% of its size, nor one an earlier drawing always beats', () => {
    // stageLanes is 3483 wide left to right: past the largest step.
    const html = renderToStaticMarkup(<Diagram spec={stageLanes} title="Stages" />)
    expect(drawings(html).map((s) => s.match(/data-direction="(\w+)"/)![1])).toEqual(['TB'])
    expect(presentations(html).map((p) => p.slot)).toEqual(['diagram-drawing', 'diagram-stacked'])
  })

  it('shows the stacked layout at every width when no drawing can fit', () => {
    // A chain through ten lanes: ten ranks across left to right, ten lane
    // columns across top to bottom, both far past the largest step.
    const ids = Array.from({ length: 10 }, (_, i) => `n${i}`)
    const wide: DiagramSpec = {
      groupStyle: 'lanes',
      groups: ids.map((id) => ({ id, title: `Lane ${id}` })),
      nodes: ids.map((id) => ({ id, title: `Step ${id}`, group: id })),
      edges: ids.slice(1).map((id, i) => ({ from: ids[i], to: id })),
    }
    const html = renderToStaticMarkup(<Diagram spec={wide} title="x" />)
    expect(presentations(html)).toEqual([{ slot: 'diagram-stacked', classes: 'block' }])
    expect(drawings(html)).toHaveLength(0)
  })

  it('lists every node in the stacked layout in reading order, with its group, badges, kinds and hand-offs', () => {
    const html = renderToStaticMarkup(<Diagram spec={framesExample} title="x" />)
    const stacked = html.match(/data-slot="diagram-stacked">(.*?)<\/div><\/div><div class="flex flex-col gap-2" data-slot="diagram-legend"|data-slot="diagram-stacked">(.*)<\/div><\/div><ol/)!
    const blocks = [...(stacked[1] ?? stacked[2]).matchAll(/data-block="([^"]+)"/g)].map((m) => m[1])
    expect(blocks).toEqual(listOrder(html, framesExample))
    const idea = block(html, 'idea')
    expect(idea).toContain('Idea')
    expect(idea).toContain('Someone writes down a wish')
    expect(idea).toMatch(/<span data-slot="diagram-stacked-group"[^>]*>Plan at the desk</)
    expect(idea).toMatch(/<span data-slot="badge"[^>]*class="[^"]*whitespace-nowrap[^"]*font-mono[^"]*">draft</)
    expect(edges(idea)).toEqual(['Sketch', 'Notes (unclear)'])
    expect(edges(block(html, 'check'))).toEqual(['Show it (stands)', 'Archive the notes'])
    expect(block(html, 'archive')).not.toContain('diagram-stacked-group')
    expect(block(html, 'archive')).not.toContain('diagram-stacked-edges')

    // Kind markers carry an icon and their title; the stage lanes' paths back
    // appear as hand-offs too.
    const lanes = renderToStaticMarkup(<Diagram spec={stageLanes} title="x" layout="vertical" />)
    for (const n of stageLanes.nodes) {
      const b = block(lanes, n.id)
      const group = stageLanes.groups!.find((g) => g.id === n.group)!.title
      expect(b).toMatch(new RegExp(`<span data-slot="diagram-stacked-group"[^>]*>${group.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}<`))
      const out = stageLanes.edges.filter((e) => e.from === n.id)
      expect(edges(b)).toEqual(out.map((e) => `${stageLanes.nodes.find((m) => m.id === e.to)!.title}${e.label ? ` (${e.label})` : ''}`))
    }
  })

  it('keeps every presentation free of inline styles and the stacked layout hidden from assistive technology', () => {
    for (const spec of siteSpecs) {
      const html = renderToStaticMarkup(<Diagram spec={spec} title="x" />)
      expect(html).not.toMatch(/\sstyle="/)
      expect(html).not.toContain('overflow-x-auto')
      if (html.includes('diagram-stacked')) expect(html).toContain('<div aria-hidden="true" class="block')
    }
  })
})

// drawings lists the drawings' svg tags (icons have no data-direction).
function drawings(html: string): string[] {
  return html.match(/<svg[^>]*data-direction="[^"]*"[^>]*>/g) ?? []
}

// presentations lists the slot and classes of each presentation in the box.
function presentations(html: string) {
  return [...html.matchAll(/<div (?:aria-hidden="true" )?class="([^"]*)" data-slot="(diagram-drawing|diagram-stacked)"/g)].map((m) => ({
    slot: m[2],
    classes: m[1],
  }))
}

// listOrder is the node ids of the text list, matched by title.
function listOrder(html: string, spec: DiagramSpec): string[] {
  return listItems(html).map((item) => spec.nodes.find((n) => item.startsWith(`${n.title}:`) || item.startsWith(`${n.title}.`))!.id)
}

// block is the HTML of one stacked block, up to the next one.
function block(html: string, id: string): string {
  const start = html.indexOf(`data-block="${id}"`)
  const next = html.indexOf('data-block="', start + 1)
  return html.slice(start, next === -1 ? html.indexOf('<ol class="sr-only"') : next).replaceAll('<!-- -->', '')
}

// edges reads a block's hand-offs as text.
function edges(b: string): string[] {
  const list = b.match(/data-slot="diagram-stacked-edges">(.*?)<\/ul>/)
  if (!list) return []
  return [...list[1].matchAll(/<li[^>]*>(.*?)<\/li>/g)].map((m) => unescape(m[1].replace(/<[^>]+>/g, '')))
}

// unescape reads the HTML entities React writes in text.
const unescape = (t: string) => t.replaceAll('&lt;', '<').replaceAll('&gt;', '>').replaceAll('&quot;', '"').replaceAll('&#x27;', "'").replaceAll('&amp;', '&')
