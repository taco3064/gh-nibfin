import { describe, expect, it } from 'vitest'

import { framesExample, lanesExample } from '@/screenshots/diagram-fixtures'

import { describeDiagram, edgeText, layoutDiagram, wrapText, type Box, type DiagramSpec, type Placement, type Point } from './layout'

const directions = ['LR', 'TB'] as const
const eps = 0.5

const intersects = (a: Box, b: Box) =>
  a.x < b.x + b.width - eps && b.x < a.x + a.width - eps && a.y < b.y + b.height - eps && b.y < a.y + a.height - eps

const encloses = (outer: Box, inner: Box) =>
  inner.x >= outer.x - eps &&
  inner.y >= outer.y - eps &&
  inner.x + inner.width <= outer.x + outer.width + eps &&
  inner.y + inner.height <= outer.y + outer.height + eps

// onBorder: p lies on b's outline.
const onBorder = (b: Box, p: Point) => {
  const inX = p.x >= b.x - eps && p.x <= b.x + b.width + eps
  const inY = p.y >= b.y - eps && p.y <= b.y + b.height + eps
  const atX = Math.abs(p.x - b.x) <= eps || Math.abs(p.x - b.x - b.width) <= eps
  const atY = Math.abs(p.y - b.y) <= eps || Math.abs(p.y - b.y - b.height) <= eps
  return (inX && atY) || (inY && atX)
}

const node = (p: Placement, id: string) => p.nodes.find((n) => n.id === id)!
const groupOf = (spec: DiagramSpec, id: string) => spec.nodes.find((n) => n.id === id)!.group

function expectCommon(spec: DiagramSpec, p: Placement) {
  // Every node is placed once, and no two boxes overlap.
  expect(p.nodes.map((n) => n.id).sort()).toEqual(spec.nodes.map((n) => n.id).sort())
  for (const [i, a] of p.nodes.entries()) {
    for (const b of p.nodes.slice(i + 1)) expect(intersects(a, b), `${a.id} overlaps ${b.id}`).toBe(false)
  }
  // Everything lies inside the figure.
  const figure = { x: 0, y: 0, width: p.width, height: p.height }
  for (const b of [...p.nodes, ...p.groups]) expect(encloses(figure, b)).toBe(true)
  // Each edge runs from its source's border to its target's border, and a
  // labelled edge has a label box.
  expect(p.edges).toHaveLength(spec.edges.length)
  spec.edges.forEach((e, i) => {
    const pe = p.edges[i]
    expect([pe.from, pe.to]).toEqual([e.from, e.to])
    expect(pe.points.length).toBeGreaterThanOrEqual(2)
    expect(onBorder(node(p, e.from), pe.points[0]), `${e.from} -> ${e.to} start`).toBe(true)
    expect(onBorder(node(p, e.to), pe.points.at(-1)!), `${e.from} -> ${e.to} end`).toBe(true)
    if (e.label) {
      expect(pe.label?.lines.join(' ')).toBe(e.label)
      expect(pe.label!.width).toBeGreaterThan(0)
      expect(encloses(figure, pe.label!)).toBe(true)
    } else expect(pe.label).toBeUndefined()
  })
}

describe('layoutDiagram', () => {
  for (const direction of directions) {
    it(`places an ungrouped diagram ${direction} without overlaps`, () => {
      const spec: DiagramSpec = {
        nodes: [
          { id: 'a', title: 'Start here with a fairly long title' },
          { id: 'b', title: 'Branch', badges: ['one', 'two'] },
          { id: 'c', title: 'Other branch', detail: 'With some detail' },
          { id: 'd', title: 'Join' },
        ],
        edges: [
          { from: 'a', to: 'b', label: 'yes' },
          { from: 'a', to: 'c', label: 'no' },
          { from: 'b', to: 'd' },
          { from: 'c', to: 'd' },
        ],
      }
      const p = layoutDiagram(spec, direction)
      expectCommon(spec, p)
      expect(p.groups).toEqual([])
      // The flow runs in the direction: the source comes before the join.
      const a = node(p, 'a')
      const d = node(p, 'd')
      if (direction === 'LR') expect(a.x + a.width).toBeLessThan(d.x)
      else expect(a.y + a.height).toBeLessThan(d.y)
      expect(p.order[0]).toBe('a')
      expect(p.order.at(-1)).toBe('d')
    })

    it(`draws frames around their members only (${direction})`, () => {
      const spec = framesExample
      const p = layoutDiagram(spec, direction)
      expectCommon(spec, p)
      expect(p.groupStyle).toBe('frames')
      expect(p.groups.map((g) => g.id)).toEqual(['plan', 'make'])
      for (const g of p.groups) {
        expect(g.titleLines.map((t) => t.text).join(' ')).toBe(g.title)
        for (const n of p.nodes) {
          if (groupOf(spec, n.id) === g.id) expect(encloses(g, n), `${g.id} encloses ${n.id}`).toBe(true)
          else expect(intersects(g, n), `${g.id} meets ${n.id}`).toBe(false)
        }
        // The title sits inside the frame, above every member.
        for (const t of g.titleLines) {
          expect(t.y).toBeGreaterThan(g.y)
          for (const n of p.nodes.filter((m) => groupOf(spec, m.id) === g.id)) expect(t.y).toBeLessThanOrEqual(n.y)
        }
      }
      expect(intersects(p.groups[0], p.groups[1])).toBe(false)
    })

    it(`puts each lane in its own band, in the declared order (${direction})`, () => {
      const spec = lanesExample
      const p = layoutDiagram(spec, direction)
      expectCommon(spec, p)
      expect(p.groupStyle).toBe('lanes')
      // The declared groups, then the unnamed band of the ungrouped node.
      expect(p.groups.map((g) => g.id)).toEqual(['plan', 'make', undefined])
      expect(p.groups.at(-1)!.titleLines).toEqual([])
      const start = (b: Box) => (direction === 'LR' ? b.y : b.x)
      const end = (b: Box) => (direction === 'LR' ? b.y + b.height : b.x + b.width)
      for (const [i, g] of p.groups.entries()) {
        if (i > 0) expect(start(g)).toBeGreaterThanOrEqual(end(p.groups[i - 1]))
        // Bands span the whole figure across the flow.
        if (direction === 'LR') expect(g.width).toBe(p.groups[0].width)
        else expect(g.height).toBe(p.groups[0].height)
      }
      for (const n of p.nodes) {
        const lane = p.groups.find((g) => g.id === groupOf(spec, n.id))!
        expect(encloses(lane, n), `${n.id} in lane ${lane.id}`).toBe(true)
        for (const t of lane.titleLines) expect(t.y).toBeLessThanOrEqual(n.y)
      }
    })
  }

  it('is deterministic', () => {
    for (const spec of [framesExample, lanesExample]) {
      for (const d of directions) expect(layoutDiagram(spec, d)).toEqual(layoutDiagram(structuredClone(spec), d))
    }
  })

  it('sizes nodes from their text', () => {
    const short = layoutDiagram({ nodes: [{ id: 'a', title: 'A' }], edges: [] }, 'TB').nodes[0]
    const long = layoutDiagram(
      { nodes: [{ id: 'a', title: 'A much longer title', detail: 'and a detail line', badges: ['x'] }], edges: [] },
      'TB',
    ).nodes[0]
    expect(long.width).toBe(short.width)
    expect(long.height).toBeGreaterThan(short.height)
    expect(long.lines.map((l) => l.kind)).toEqual(['title', 'title', 'detail'])
    expect(long.badges.map((b) => b.text)).toEqual(['x'])
    for (const b of long.badges) expect(b.y + b.height).toBeLessThan(long.height)
  })

  it('names the offending id of an invalid spec', () => {
    const n = (id: string, group?: string) => ({ id, title: id, group })
    expect(() => layoutDiagram({ nodes: [n('a'), n('a')], edges: [] }, 'LR')).toThrow('duplicate node id "a"')
    expect(() =>
      layoutDiagram({ nodes: [n('a')], edges: [], groups: [{ id: 'g', title: 'G' }, { id: 'g', title: 'H' }] }, 'LR'),
    ).toThrow('duplicate group id "g"')
    expect(() => layoutDiagram({ nodes: [n('a')], edges: [{ from: 'a', to: 'zz' }] }, 'LR')).toThrow('unknown node "zz"')
    expect(() => layoutDiagram({ nodes: [n('a')], edges: [{ from: 'qq', to: 'a' }] }, 'LR')).toThrow('unknown node "qq"')
    expect(() => layoutDiagram({ nodes: [n('a', 'nope')], edges: [] }, 'LR')).toThrow('node "a" is in unknown group "nope"')
    expect(() => layoutDiagram({ nodes: [n('a')], edges: [{ from: 'a', to: 'a' }] }, 'LR')).toThrow('node "a" to itself')
  })
})

describe('wrapText', () => {
  it('keeps every line within the limit', () => {
    const text = 'The quick brown fox jumps over the extraordinarily lazy dog'
    for (const limit of [4, 8, 12, 20]) {
      const lines = wrapText(text, limit)
      for (const l of lines) expect(l.length).toBeLessThanOrEqual(limit)
      expect(lines.join('').replaceAll(' ', '')).toBe(text.replaceAll(' ', ''))
    }
    expect(wrapText('one two three', 7)).toEqual(['one two', 'three'])
    expect(wrapText('abcdefghij', 4)).toEqual(['abcd', 'efgh', 'ij'])
  })
})

describe('describeDiagram', () => {
  it('lists every node once, in reading order, with its group and edges', () => {
    const p = layoutDiagram(framesExample, 'LR')
    const d = describeDiagram(framesExample, p)
    expect(d.map((e) => e.id)).toEqual(p.order)
    expect(new Set(d.map((e) => e.id)).size).toBe(framesExample.nodes.length)
    expect(d[0]).toEqual({
      id: 'idea',
      title: 'Idea',
      detail: 'Someone writes down a wish',
      badges: ['draft'],
      group: 'Plan at the desk',
      edges: [{ to: 'Sketch' }, { to: 'Notes', label: 'unclear' }],
    })
    expect(d[0].edges.map(edgeText)).toEqual(['to Sketch', 'to Notes (unclear)'])
    const archive = d.find((e) => e.id === 'archive')!
    expect(archive.group).toBeUndefined()
    expect(archive.edges).toEqual([])
    expect(d.at(-1)!.id).not.toBe('idea')
  })
})
