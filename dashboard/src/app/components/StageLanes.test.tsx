import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { layoutDiagram } from './diagram'
import { stageLanes } from '../lib/stage-lanes'
import { StageLanes } from './StageLanes'

const title = (id: string) => stageLanes.nodes.find((n) => n.id === id)!.title

// How it works renders the figure without a client directive, so it must
// render on the server without a DOM, as Astro does at build time.
describe('StageLanes', () => {
  const html = renderToStaticMarkup(<StageLanes />)

  it('renders statically as one top-to-bottom drawing', () => {
    expect(html).toMatch(/^<figure/)
    expect(html).toContain('The stages, by role')
    const svgs = html.match(/<svg[^>]*>/g) ?? []
    expect(svgs).toHaveLength(1)
    expect(svgs[0]).toContain('data-direction="TB"')
    expect(svgs[0]).toContain('aria-hidden="true"')
    expect(html).not.toMatch(/\sstyle="/)
  })

  it('has one lane per role or actor, in order', () => {
    expect(stageLanes.groupStyle).toBe('lanes')
    expect(stageLanes.groups!.map((g) => g.title)).toEqual([
      'Scope',
      'Shape Author',
      'Shape Validator',
      'Writer',
      'Acceptor',
      'reviewer',
      'GitHub (CI, merge, CD)',
    ])
    const p = layoutDiagram(stageLanes, 'TB')
    expect(p.groups.map((g) => g.id)).toEqual(stageLanes.groups!.map((g) => g.id))
    // Lanes run left to right in that order.
    for (const [i, g] of p.groups.entries()) if (i > 0) expect(g.x).toBeGreaterThan(p.groups[i - 1].x)
    for (const n of stageLanes.nodes) expect(n.group).toBeDefined()
  })

  it('reads the steps in stage order', () => {
    expect(layoutDiagram(stageLanes, 'TB').order).toEqual([
      'split',
      'write',
      'validate',
      'implement',
      'accept',
      'checks',
      'review',
      'merge',
      'cd',
      'closed',
    ])
  })

  it('labels every hand-off and the five paths back', () => {
    for (const e of stageLanes.edges) expect(e.label, `${e.from} -> ${e.to}`).toBeTruthy()
    const order = layoutDiagram(stageLanes, 'TB').order
    const backs = stageLanes.edges
      .filter((e) => order.indexOf(e.to) < order.indexOf(e.from))
      .map((e) => [title(e.from), title(e.to), e.label])
    expect(backs).toEqual([
      ['Validate the Shape', 'Write the Shape', 'findings'],
      ['Implement', 'Write the Shape', 'Shape wrong, with the reason'],
      ['Accept', 'Implement', 'defects'],
      ['Review', 'Implement', 'problems'],
      ['CD runs', 'Implement', 'failure, new fix branch'],
    ])
  })

  it('names the artifacts handed over', () => {
    const text = [...stageLanes.edges.map((e) => e.label), ...stageLanes.nodes.flatMap((n) => n.badges ?? [])].join(' | ')
    for (const artifact of ['Shape comment and shaped', 'nibfin/issue-<n>', 'pull request', 'delivered']) {
      expect(text).toContain(artifact)
    }
  })

  it('names every step in the hidden list', () => {
    const list = html.match(/<ol class="sr-only">(.*)<\/ol>/)![1]
    const items = [...list.matchAll(/<li>(.*?)<\/li>/g)].map((m) => m[1].replaceAll('<!-- -->', ''))
    expect(items).toHaveLength(stageLanes.nodes.length)
    stageLanes.nodes.forEach((n, i) => expect(items[i].startsWith(`${n.title}`), n.id).toBe(true))
    expect(items[3]).toContain('Lane: Writer.')
    expect(items[3]).toContain('to Accept (branch nibfin/issue-&lt;n&gt;)')
  })
})
