import { describe, expect, it } from 'vitest'

import { Bot } from 'lucide-react'

import { framesExample, lanesExample, pipelineExample } from '@/screenshots/diagram-fixtures'

import { architecture } from '../../lib/architecture'
import { issueFlow } from '../../lib/issue-flow'
import { labelStateDiagram } from '../../lib/label-flow'
import { labelStates } from '../../lib/labels'
import { stageLanes } from '../../lib/stage-lanes'

import {
  describeDiagram,
  edgeText,
  layoutDiagram,
  sizes,
  titlePlate,
  wrapText,
  type Box,
  type DiagramSpec,
  type Placement,
  type Point,
} from './layout'

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
  // No group title covers a node or an arrowhead (the last 10 units of an
  // edge).
  const plates = p.groups.flatMap((g) => g.titleLines.map((t) => ({ t: t.text, box: titlePlate(t) })))
  for (const { t, box } of plates) {
    for (const n of p.nodes) expect(intersects(box, n), `title "${t}" covers ${n.id}`).toBe(false)
    for (const e of p.edges) {
      const [a, b] = e.points.slice(-2)
      const len = Math.hypot(b.x - a.x, b.y - a.y)
      for (const back of [0, 3, 6, 9]) {
        const k = Math.min(1, back / len)
        const q = { x: b.x + (a.x - b.x) * k, y: b.y + (a.y - b.y) * k }
        expect(inside(box, q), `title "${t}" covers the arrowhead of ${e.from} -> ${e.to}`).toBe(false)
      }
    }
  }
  // No two edges share a stretch of line.
  for (const [i, e] of p.edges.entries()) {
    for (const f of p.edges.slice(i + 1)) {
      for (const s of segments(e.points)) {
        for (const t of segments(f.points)) {
          expect(overlap(s, t), `${e.from} -> ${e.to} runs along ${f.from} -> ${f.to}`).toBeLessThanOrEqual(1)
        }
      }
    }
  }
}

const inside = (b: Box, p: Point) => p.x > b.x + eps && p.x < b.x + b.width - eps && p.y > b.y + eps && p.y < b.y + b.height - eps

const segments = (points: Point[]) => points.slice(1).map((q, i) => [points[i], q] as const)

// overlap is the length two collinear segments share, 0 for any others.
function overlap([a, b]: readonly [Point, Point], [c, d]: readonly [Point, Point]): number {
  const cross = (o: Point, p: Point, q: Point) => (p.x - o.x) * (q.y - o.y) - (p.y - o.y) * (q.x - o.x)
  const len = Math.hypot(b.x - a.x, b.y - a.y)
  if (len === 0) return 0
  // Both ends of cd within half a unit of the line through ab.
  if (Math.abs(cross(a, b, c)) / len > eps || Math.abs(cross(a, b, d)) / len > eps) return 0
  const along = (p: Point) => ((p.x - a.x) * (b.x - a.x) + (p.y - a.y) * (b.y - a.y)) / len
  const [lo, hi] = [Math.min(along(c), along(d)), Math.max(along(c), along(d))]
  return Math.max(0, Math.min(len, hi) - Math.max(0, lo))
}

// backExample has edges against the flow that span several ranks and lanes,
// several of them entering or leaving the same box.
const backExample: DiagramSpec = {
  groupStyle: 'lanes',
  groups: [
    { id: 'one', title: 'First lane' },
    { id: 'two', title: 'Second lane' },
    { id: 'three', title: 'Third lane' },
  ],
  nodes: [
    { id: 'a', title: 'Start', group: 'one' },
    { id: 'b', title: 'Second step', detail: 'with detail', group: 'two' },
    { id: 'c', title: 'Third', group: 'three' },
    { id: 'd', title: 'Fourth', badges: ['x'], group: 'two' },
    { id: 'e', title: 'End', group: 'three' },
  ],
  edges: [
    { from: 'a', to: 'b', label: 'go' },
    { from: 'b', to: 'c' },
    { from: 'c', to: 'b', label: 'not yet, try again later' },
    { from: 'c', to: 'd', label: 'next' },
    { from: 'd', to: 'a', label: 'restart' },
    { from: 'd', to: 'b' },
    { from: 'd', to: 'e' },
    { from: 'e', to: 'b', label: 'failed' },
  ],
}

// crosses: segment ab passes through the inside of box b.
function crosses([p, q]: readonly [Point, Point], b: Box): boolean {
  // Sample the segment; routes are orthogonal, so a box it enters is met.
  const len = Math.hypot(q.x - p.x, q.y - p.y)
  const steps = Math.max(1, Math.ceil(len / 2))
  for (let k = 0; k <= steps; k++) {
    const t = k / steps
    if (inside(b, { x: p.x + (q.x - p.x) * t, y: p.y + (q.y - p.y) * t })) return true
  }
  return false
}

function expectBackRoutes(p: Placement) {
  const figure = { x: 0, y: 0, width: p.width, height: p.height }
  for (const e of p.edges) {
    // Every point lies in the figure, and no segment passes through a box
    // other than the edge's own ends, nor through its ends' insides.
    for (const q of e.points) expect(encloses(figure, { ...q, width: 0, height: 0 })).toBe(true)
    for (const s of segments(e.points)) {
      for (const n of p.nodes) expect(crosses(s, n), `${e.from} -> ${e.to} crosses ${n.id}`).toBe(false)
    }
  }
  // Every label is clear of every box and of every other label.
  const placed = p.edges.flatMap((e) => (e.label ? [{ e, box: e.label }] : []))
  for (const [i, { e, box }] of placed.entries()) {
    for (const n of p.nodes) expect(intersects(box, n), `label of ${e.from} -> ${e.to} covers ${n.id}`).toBe(false)
    for (const other of placed.slice(i + 1)) {
      expect(intersects(box, other.box), `labels of ${e.from} -> ${e.to} and ${other.e.from} -> ${other.e.to}`).toBe(false)
    }
  }
  // A labelled back edge's label sits on one of its own segments.
  const rank = (id: string) => p.order.indexOf(id)
  for (const e of p.edges) {
    if (!e.label || rank(e.to) >= rank(e.from)) continue
    const c = { x: e.label.x + e.label.width / 2, y: e.label.y + e.label.height / 2 }
    const on = segments(e.points).some(
      ([a, b]) =>
        (Math.abs(a.x - b.x) <= eps && Math.abs(c.x - a.x) <= eps && c.y >= Math.min(a.y, b.y) && c.y <= Math.max(a.y, b.y)) ||
        (Math.abs(a.y - b.y) <= eps && Math.abs(c.y - a.y) <= eps && c.x >= Math.min(a.x, b.x) && c.x <= Math.max(a.x, b.x)),
    )
    expect(on, `label of ${e.from} -> ${e.to} is on its edge`).toBe(true)
  }
}

// clone copies a spec deeply but keeps its icon components, which
// structuredClone cannot copy.
const clone = (spec: DiagramSpec): DiagramSpec => ({
  ...spec,
  nodes: spec.nodes.map((n) => ({ ...n, badges: n.badges && [...n.badges], kinds: n.kinds && [...n.kinds] })),
  edges: spec.edges.map((e) => ({ ...e })),
  groups: spec.groups?.map((g) => ({ ...g })),
  kinds: spec.kinds?.map((k) => ({ ...k })),
})

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

  for (const direction of directions) {
    it(`routes back edges around the boxes in lanes (${direction})`, () => {
      for (const spec of [backExample, stageLanes]) {
        const p = layoutDiagram(spec, direction)
        expectCommon(spec, p)
        expectBackRoutes(p)
      }
    })
  }

  it('routes every lane edge orthogonally, back edges included', () => {
    for (const direction of directions) {
      for (const spec of [backExample, stageLanes]) {
        const p = layoutDiagram(spec, direction)
        for (const e of p.edges) {
          for (const [a, b] of segments(e.points)) expect(a.x === b.x || a.y === b.y, `${e.from} -> ${e.to} is orthogonal`).toBe(true)
        }
      }
    }
  })

  it('is deterministic', () => {
    for (const spec of [framesExample, lanesExample, backExample, stageLanes, pipelineExample]) {
      for (const d of directions) expect(layoutDiagram(spec, d)).toEqual(layoutDiagram(clone(spec), d))
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

// chain is n nodes in a line, wrapped into rows of wrap ranks.
const chain = (n: number, wrap?: number): DiagramSpec => {
  const nodes = Array.from({ length: n }, (_, i) => ({ id: `s${i}`, title: `Step ${i}`, detail: i % 2 ? 'with a detail' : undefined }))
  return { nodes, edges: nodes.slice(1).map((m, i) => ({ from: nodes[i].id, to: m.id })), wrap }
}

// rowsOf groups the node ids into rows of vertically overlapping boxes, each
// row from left to right.
const rowsOf = (p: Placement) => {
  const rows: Box[][] = []
  for (const n of [...p.nodes].sort((a, b) => a.y - b.y)) {
    const row = rows.find((r) => r.some((m) => m.y < n.y + n.height && n.y < m.y + m.height))
    if (row) row.push(n)
    else rows.push([n])
  }
  return rows.map((r) => [...r].sort((a, b) => a.x - b.x).map((n) => (n as Placement['nodes'][number]).id))
}

describe('wrap', () => {
  it('keeps at most wrap ranks per row, each row from the left, below the last', () => {
    const spec = chain(8, 4)
    const p = layoutDiagram(spec, 'LR')
    expectCommon(spec, p)
    const rows = rowsOf(p)
    expect(rows).toEqual([
      ['s0', 's1', 's2', 's3'],
      ['s4', 's5', 's6', 's7'],
    ])
    expect(node(p, 's4').x).toBe(node(p, 's0').x)
    expect(node(p, 's0').x).toBe(sizes.margin)
    const bottom = Math.max(...rows[0].map((id) => node(p, id).y + node(p, id).height))
    for (const id of rows[1]) expect(node(p, id).y).toBeGreaterThan(bottom)
    // About four ranks wide, not eight.
    expect(p.width).toBeLessThan(4 * sizes.nodeWidth + 4 * 40 + 2 * sizes.margin)
    expect(p.order).toEqual(spec.nodes.map((n) => n.id))
  })

  it('routes an edge into the next row through the gutter, crossing no node', () => {
    const spec = { ...chain(6, 3), edges: [...chain(6).edges.map((e) => (e.from === 's2' ? { ...e, label: 'next day' } : e))] }
    const p = layoutDiagram(spec, 'LR')
    expectCommon(spec, p)
    const e = p.edges.find((x) => x.from === 's2')!
    const a = node(p, 's2')
    const b = node(p, 's3')
    // Down from the source's bottom, along the gutter, down into the target's top.
    expect(e.points[0].y).toBeCloseTo(a.y + a.height)
    expect(e.points.at(-1)!.y).toBeCloseTo(b.y)
    for (const [q, r] of segments(e.points)) expect(q.x === r.x || q.y === r.y).toBe(true)
    for (const n of p.nodes) {
      for (const [q, r] of segments(e.points)) {
        const seg = { x: Math.min(q.x, r.x), y: Math.min(q.y, r.y), width: Math.abs(q.x - r.x), height: Math.abs(q.y - r.y) }
        expect(intersects(n, { ...seg, x: seg.x - 0.1, y: seg.y - 0.1, width: seg.width + 0.2, height: seg.height + 0.2 }), `crosses ${n.id}`).toBe(
          false,
        )
      }
    }
    // The label sits on the gutter line, between the rows.
    expect(e.label!.y).toBeGreaterThan(a.y + a.height)
    expect(e.label!.y + e.label!.height).toBeLessThan(b.y)
    for (const n of p.nodes) expect(intersects(n, e.label!)).toBe(false)
  })

  it('leaves TB and unwrapped specs alone', () => {
    expect(layoutDiagram(chain(8, 4), 'TB')).toEqual(layoutDiagram(chain(8), 'TB'))
    expect(layoutDiagram(chain(3, 4), 'LR').nodes).toEqual(layoutDiagram(chain(3), 'LR').nodes)
    expectCommon(pipelineExample, layoutDiagram(pipelineExample, 'LR'))
    expectCommon(pipelineExample, layoutDiagram(pipelineExample, 'TB'))
  })

  it('names the offending id of an invalid wrap', () => {
    expect(() => layoutDiagram({ ...chain(3), wrap: 0 }, 'LR')).toThrow('wrap 0 is not a positive integer')
    expect(() => layoutDiagram({ ...chain(3), wrap: 1.5 }, 'TB')).toThrow('wrap 1.5 is not a positive integer')
    expect(() => layoutDiagram({ ...framesExample, wrap: 2 }, 'LR')).toThrow('wrap does not combine with groups (group "plan")')
    // Back to an earlier row: a cycle that dagre reverses.
    const back = { ...chain(4, 2), edges: [...chain(4).edges, { from: 's3', to: 's0' }] }
    expect(() => layoutDiagram(back, 'LR')).toThrow('edge s3 -> s0 back to an earlier row')
    const skip = { ...chain(4, 1), edges: [...chain(4).edges, { from: 's0', to: 's2' }] }
    expect(() => layoutDiagram(skip, 'LR')).toThrow('edge s0 -> s2 across more than one row break')
  })
})

describe('icons and kinds', () => {
  const kinds = [
    { id: 'a', title: 'Agent work', icon: Bot },
    { id: 'p', title: 'A person’s control point' },
  ]

  it('indents the title past the icon and adds one marker row per kind', () => {
    const plain = layoutDiagram({ nodes: [{ id: 'n', title: 'Shape', detail: 'Author' }], edges: [] }, 'TB').nodes[0]
    const marked = layoutDiagram(
      { nodes: [{ id: 'n', title: 'Shape', detail: 'Author', icon: Bot, kinds: ['a', 'p'] }], edges: [], kinds },
      'TB',
    ).nodes[0]
    expect(plain.icon).toBeUndefined()
    expect(plain.markers).toEqual([])
    expect(marked.icon).toBeDefined()
    const title = marked.lines.find((l) => l.kind === 'title')!
    expect(title.x).toBeGreaterThanOrEqual(marked.icon!.x + marked.icon!.width)
    expect(marked.lines.find((l) => l.kind === 'detail')!.x).toBe(plain.lines[1].x)
    expect(marked.markers.map((m) => m.kind)).toEqual(['a', 'p'])
    expect(marked.markers.map((m) => m.lines.map((l) => l.text).join(' '))).toEqual(['Agent work', 'A person’s control point'])
    expect(marked.height).toBeGreaterThan(plain.height)
    const last = marked.markers.at(-1)!.lines.at(-1)!
    expect(last.y).toBeLessThan(marked.height)
    for (const m of marked.markers) for (const l of m.lines) expect(l.x).toBeGreaterThanOrEqual(m.icon.x + m.icon.width)
  })

  it('names the offending id of an invalid kind', () => {
    expect(() => layoutDiagram({ nodes: [{ id: 'n', title: 'N' }], edges: [], kinds: [kinds[0], kinds[0]] }, 'LR')).toThrow(
      'duplicate kind id "a"',
    )
    expect(() => layoutDiagram({ nodes: [{ id: 'n', title: 'N', kinds: ['zz'] }], edges: [], kinds }, 'LR')).toThrow(
      'node "n" has unknown kind "zz"',
    )
  })

  it('lists the kind titles in the text alternative', () => {
    const spec: DiagramSpec = { nodes: [{ id: 'n', title: 'N', kinds: ['p', 'a'] }, { id: 'm', title: 'M' }], edges: [], kinds }
    const d = describeDiagram(spec, layoutDiagram(spec, 'LR'))
    expect(d.find((x) => x.id === 'n')!.kinds).toEqual(['A person’s control point', 'Agent work'])
    expect(d.find((x) => x.id === 'm')!.kinds).toBeUndefined()
  })
})

describe('wrapText', () => {
  it('keeps every line within the limit, except a single word longer than it', () => {
    const text = 'The quick brown fox jumps over the extraordinarily lazy dog'
    for (const limit of [4, 8, 12, 20]) {
      const lines = wrapText(text, limit)
      for (const l of lines) if (l.includes(' ')) expect(l.length).toBeLessThanOrEqual(limit)
      expect(lines.join(' ')).toBe(text)
    }
    expect(wrapText('one two three', 7)).toEqual(['one two', 'three'])
  })

  it('never splits a word, at hyphens or anywhere else', () => {
    expect(wrapText('abcdefghij', 4)).toEqual(['abcdefghij'])
    expect(wrapText('a multiple-assignees b', 15)).toEqual(['a', 'multiple-assignees', 'b'])
    expect(wrapText('NIBFIN_GITHUB_TOKEN', 5)).toEqual(['NIBFIN_GITHUB_TOKEN'])
  })
})

// siteSpecs are the diagrams the site shows.
const siteSpecs: [string, DiagramSpec][] = [
  ['Home flow', issueFlow],
  ['stage lanes', stageLanes],
  ['label states', labelStateDiagram(labelStates)],
  ['architecture', architecture],
]
const words = (t: string) => t.trim().split(/\s+/).join(' ')

describe('text stays whole', () => {
  for (const [name, spec] of siteSpecs) {
    for (const direction of directions) {
      it(`breaks only between words in the ${name} (${direction})`, () => {
        const p = layoutDiagram(spec, direction)
        const kinds = new Map((spec.kinds ?? []).map((k) => [k.id, k.title]))
        for (const n of p.nodes) {
          const src = spec.nodes.find((m) => m.id === n.id)!
          // Joining the lines with one space gives back the source: no line
          // ends or starts inside a word.
          const text = (kind: 'title' | 'detail') => n.lines.filter((l) => l.kind === kind).map((l) => l.text).join(' ')
          expect(text('title'), n.id).toBe(words(src.title))
          expect(text('detail'), n.id).toBe(src.detail ? words(src.detail) : '')
          for (const m of n.markers) expect(m.lines.map((l) => l.text).join(' ')).toBe(words(kinds.get(m.kind)!))
          expect(n.badges.map((b) => b.text)).toEqual(src.badges ?? [])
        }
        spec.edges.forEach((e, i) => {
          if (e.label) expect(p.edges[i].label!.lines.join(' ')).toBe(words(e.label))
        })
        for (const g of p.groups) if (g.title) expect(g.titleLines.map((t) => t.text).join(' ')).toBe(words(g.title))
      })

      it(`keeps every token inside its node, badge or frame in the ${name} (${direction})`, () => {
        const p = layoutDiagram(spec, direction)
        for (const n of p.nodes) {
          const inner = n.width - sizes.nodePadX
          for (const l of n.lines) {
            const w = l.text.length * (l.kind === 'title' ? sizes.titleCharWidth : sizes.detailCharWidth)
            expect(l.x + w, `${n.id} "${l.text}"`).toBeLessThanOrEqual(inner + eps)
          }
          for (const m of n.markers) {
            for (const l of m.lines) expect(l.x + l.text.length * sizes.markerCharWidth, `${n.id} "${l.text}"`).toBeLessThanOrEqual(inner + eps)
          }
          for (const b of n.badges) {
            // The pill holds its whole text, and the node holds the pill.
            expect(b.width).toBeGreaterThanOrEqual(b.text.length * sizes.badgeCharWidth + 2 * sizes.badgePadX - eps)
            expect(b.x + b.width, `${n.id} badge ${b.text}`).toBeLessThanOrEqual(inner + eps)
            expect(b.y + b.height).toBeLessThanOrEqual(n.height - sizes.nodePadY + eps)
          }
        }
        for (const g of p.groups) {
          for (const t of g.titleLines) {
            const plate = titlePlate(t)
            expect(plate.x + plate.width, `title "${t.text}"`).toBeLessThanOrEqual(g.x + g.width + eps)
          }
        }
      })
    }
  }

  it('widens a node for a long word or badge, and only then', () => {
    const plain = layoutDiagram({ nodes: [{ id: 'a', title: 'Short title' }], edges: [] }, 'TB').nodes[0]
    expect(plain.width).toBe(sizes.nodeWidth)
    const word = layoutDiagram({ nodes: [{ id: 'a', title: 'multiple-assignees' }], edges: [] }, 'TB').nodes[0]
    expect(word.width).toBeGreaterThan(sizes.nodeWidth)
    expect(word.lines.map((l) => l.text)).toEqual(['multiple-assignees'])
    const badge = layoutDiagram({ nodes: [{ id: 'a', title: 'Peer', badges: ['NIBFIN_GITHUB_TOKEN'] }], edges: [] }, 'TB').nodes[0]
    expect(badge.badges[0].width).toBeCloseTo(19 * sizes.badgeCharWidth + 2 * sizes.badgePadX)
    expect(badge.width).toBeGreaterThanOrEqual(badge.badges[0].width + 2 * sizes.nodePadX)
  })

  it('widens a top-to-bottom lane for its title’s longest word', () => {
    const spec: DiagramSpec = {
      groupStyle: 'lanes',
      groups: [{ id: 'g', title: 'An extraordinarily-long-hyphenated-lane-title' }],
      nodes: [{ id: 'a', title: 'A', group: 'g' }],
      edges: [],
    }
    const p = layoutDiagram(spec, 'TB')
    const t = p.groups[0].titleLines
    expect(t.map((l) => l.text)).toEqual(['An', 'extraordinarily-long-hyphenated-lane-title'])
    for (const l of t) expect(titlePlate(l).x + titlePlate(l).width).toBeLessThanOrEqual(p.groups[0].x + p.groups[0].width + eps)
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
