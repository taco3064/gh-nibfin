import { describe, expect, it } from 'vitest'

import {
  buildGraph,
  closedGroups,
  compareKeys,
  edgePath,
  emptyFilter,
  filterGraph,
  graphSpacing,
  groupLabel,
  heldFlags,
  laneX,
  layoutGraph,
  nodeKey,
  nodeName,
  portPoint,
  positionGraph,
  unlinkedGroups,
  type GraphEdge,
  type Size,
} from './graph'
import type { Blocker, StatusIssue } from './status'

// Fictional repository data.
const repo = 'github.com/acme/app'
const url = (r: string, n: number) => `https://github.com/${r}/issues/${n}`

function blocker(n: number, over: Partial<Blocker> = {}): Blocker {
  const r = over.repo ?? 'acme/app'
  return { repo: r, number: n, title: `Blocker ${n}`, state: 'OPEN', url: url(r, n), ...over }
}

// blockedBy 'unread' leaves the field out.
function issue(n: number, blockedBy: Blocker[] | 'unread' = [], over: Partial<StatusIssue> = {}): StatusIssue {
  return {
    number: n,
    title: `Issue ${n}`,
    url: url('acme/app', n),
    labels: ['scope'],
    pause: false,
    pending: false,
    createdAt: '2026-11-01T00:00:00Z',
    ...(blockedBy !== 'unread' ? { blockedBy } : {}),
    ...over,
  }
}

const k = (n: number, r = 'acme/app') => nodeKey(r, n)

describe('buildGraph', () => {
  it('adds a node per listed Issue and per distinct outside blocker', () => {
    const g = buildGraph({
      repo,
      issues: [issue(1, [blocker(2), blocker(7, { repo: 'Acme/Infra' }), blocker(9, { state: 'CLOSED' })]), issue(2, [blocker(7, { repo: 'acme/infra' })])],
    })
    expect(g.repo).toBe('acme/app')
    expect([...g.nodes.keys()]).toEqual([k(1), k(2), k(7, 'acme/infra'), k(9)])
    expect(g.nodes.get(k(2))!.kind).toBe('issue')
    expect(g.nodes.get(k(7, 'acme/infra'))!.kind).toBe('blocker')
    expect(g.nodes.get(k(9))!.kind).toBe('blocker')
    expect(g.edges).toEqual([
      { from: k(1), to: k(2), closed: false },
      { from: k(1), to: k(7, 'acme/infra'), closed: false },
      { from: k(1), to: k(9), closed: true },
      { from: k(2), to: k(7, 'acme/infra'), closed: false },
    ])
  })

  it('collects Issues whose blockers failed or were not read', () => {
    const e = { code: 'github_error', message: 'HTTP 502' }
    const g = buildGraph({ repo, issues: [issue(1, [], { blockedByError: e }), issue(2, 'unread'), issue(3, null as unknown as Blocker[]), issue(4)] })
    expect(g.unread.map((i) => i.number)).toEqual([1, 2, 3])
  })

  it('reads no Issues as an empty graph', () => {
    const g = buildGraph({ repo, issues: null })
    expect(g.nodes.size).toBe(0)
    expect(g.edges).toEqual([])
  })
})

describe('filterGraph', () => {
  const g = buildGraph({
    repo,
    issues: [
      issue(1, [blocker(2)], { title: 'Export reports' }),
      issue(2, [blocker(3)], { title: 'Reports API', labels: ['scope', 'shaped'] }),
      issue(3, [blocker(50, { repo: 'acme/infra' })], { title: 'Storage', pause: true }),
      issue(4, [blocker(60, { repo: 'acme/infra' })], { title: 'Mobile login', pending: true }),
      issue(5, [blocker(70, { state: 'CLOSED' })], { title: 'Docs', labels: [] }),
    ],
  })

  it('matches every listed Issue without a filter', () => {
    const f = filterGraph(g, emptyFilter)
    expect(f.matched).toEqual([k(1), k(2), k(3), k(4), k(5)])
    expect(f.context).toEqual([k(50, 'acme/infra'), k(60, 'acme/infra'), k(70)])
    expect(f.edges).toHaveLength(g.edges.length)
  })

  it('keeps the blockers of a match visible as context, through a chain', () => {
    const f = filterGraph(g, { ...emptyFilter, query: 'export' })
    expect(f.matched).toEqual([k(1)])
    expect(f.context).toEqual([k(2), k(3), k(50, 'acme/infra')])
    expect(f.edges.map((e) => [e.from, e.to])).toEqual([
      [k(1), k(2)],
      [k(2), k(3)],
      [k(3), k(50, 'acme/infra')],
    ])
  })

  it('keeps outside blockers only for shown dependents', () => {
    const f = filterGraph(g, { ...emptyFilter, query: 'mobile' })
    expect(f.matched).toEqual([k(4)])
    expect(f.context).toEqual([k(60, 'acme/infra')])
  })

  it('matches a number with or without #', () => {
    expect(filterGraph(g, { ...emptyFilter, query: '#3' }).matched).toEqual([k(3)])
    expect(filterGraph(g, { ...emptyFilter, query: ' 4 ' }).matched).toEqual([k(4)])
  })

  it('filters by stage and by the toggles, all together', () => {
    expect(filterGraph(g, { ...emptyFilter, stages: ['deliver'] }).matched).toEqual([k(2)])
    expect(filterGraph(g, { ...emptyFilter, stages: ['none', 'deliver'] }).matched).toEqual([k(2), k(5)])
    expect(filterGraph(g, { ...emptyFilter, paused: true }).matched).toEqual([k(3)])
    expect(filterGraph(g, { ...emptyFilter, pending: true }).matched).toEqual([k(4)])
    // Issue 5's only blocker is closed.
    expect(filterGraph(g, { ...emptyFilter, blocked: true }).matched).toEqual([k(1), k(2), k(3), k(4)])
    expect(filterGraph(g, { ...emptyFilter, blocked: true, query: 'reports', stages: ['shape'] }).matched).toEqual([k(1)])
  })

  it('filters on the Scope stage: request without scope', () => {
    const s = buildGraph({
      repo,
      issues: [issue(1, [], { labels: ['request'] }), issue(2, [], { labels: ['request', 'scope'] }), issue(3, [], { labels: ['bug'] })],
    })
    expect(filterGraph(s, { ...emptyFilter, stages: ['scope'] }).matched).toEqual([k(1)])
    expect(filterGraph(s, { ...emptyFilter, stages: ['none'] }).matched).toEqual([k(3)])
  })
})

describe('layoutGraph', () => {
  const e = (from: number, to: number): GraphEdge => ({ from: k(from), to: k(to), closed: false })

  it('ranks a chain with blockers left of their dependents', () => {
    const l = layoutGraph([k(1), k(2), k(3), k(4)], [e(1, 2), e(2, 3), e(1, 3)])
    expect(l.columns).toEqual([[k(3)], [k(2)], [k(1)]])
    expect(l.unlinked).toEqual([k(4)])
    expect(l.cycles).toEqual([])
    expect(l.edges.every((x) => !x.cycle)).toBe(true)
  })

  it('ends with a cycle edge on a two- and a three-node cycle', () => {
    const two = layoutGraph([k(1), k(2)], [e(1, 2), e(2, 1)])
    expect(two.edges.filter((x) => x.cycle)).toHaveLength(1)
    expect(two.cycles).toEqual([[k(1), k(2)]])
    expect(two.columns.flat().sort()).toEqual([k(1), k(2)])

    const three = layoutGraph([k(1), k(2), k(3)], [e(1, 2), e(2, 3), e(3, 1)])
    expect(three.edges.map((x) => x.cycle)).toEqual([false, false, true])
    expect(three.cycles).toEqual([[k(1), k(2), k(3)]])
    expect(three.columns).toEqual([[k(3)], [k(2)], [k(1)]])
  })

  it('orders a column by the mean row of its blockers, then by key', () => {
    const l = layoutGraph([k(10), k(9), k(2), k(1), k(3)], [e(10, 2), e(9, 1), e(3, 1)])
    expect(l.columns).toEqual([
      [k(1), k(2)],
      [k(3), k(9), k(10)],
    ])
    // The same input in another order gives the same layout.
    expect(layoutGraph([k(3), k(1), k(2), k(9), k(10)], [e(3, 1), e(9, 1), e(10, 2)]).columns).toEqual(l.columns)
  })

  it('compares keys by repository, then by number', () => {
    expect([k(10), k(9), k(2, 'acme/a')].sort(compareKeys)).toEqual([k(2, 'acme/a'), k(9), k(10)])
  })

  it('lays out a few hundred nodes quickly', () => {
    const n = 400
    const keys = Array.from({ length: n }, (_, i) => k(i + 1))
    const edges: GraphEdge[] = []
    for (let i = 2; i <= n; i++) {
      edges.push(e(i, Math.max(1, i - 1 - (i % 7))))
      if (i % 3 === 0) edges.push(e(i, Math.floor(i / 2)))
    }
    edges.push(e(1, n))
    const start = performance.now()
    const l = layoutGraph(keys, edges)
    expect(performance.now() - start).toBeLessThan(1000)
    expect(l.columns.flat()).toHaveLength(n)
    expect(l.cycles.length).toBeGreaterThan(0)
  })
})

describe('closedGroups', () => {
  const g = buildGraph({
    repo,
    issues: [
      issue(1, [blocker(9, { state: 'CLOSED' }), blocker(2), blocker(8, { state: 'CLOSED' })]),
      issue(2, [blocker(9, { state: 'CLOSED' })]),
      issue(3, [blocker(2)]),
    ],
  })

  it('groups the closed blockers per dependent in key order and shares a common one', () => {
    const groups = closedGroups(g, g.edges)
    expect([...groups.keys()]).toEqual([k(1), k(2)])
    expect(groups.get(k(1))!.map((n) => n.key)).toEqual([k(8), k(9)])
    expect(groups.get(k(2))!.map((n) => n.key)).toEqual([k(9)])
  })

  it('makes no group without closed edges', () => {
    expect(
      closedGroups(
        g,
        g.edges.filter((e) => !e.closed),
      ).size,
    ).toBe(0)
    expect(closedGroups(g, []).size).toBe(0)
  })
})

describe('groupLabel', () => {
  const g = { repo: 'acme/app' }
  const n = (number: number, r = 'acme/app') => ({ repo: r, number })

  it('names up to max blockers and marks the rest', () => {
    expect(groupLabel(g, [n(353)])).toBe('1 closed · #353')
    expect(groupLabel(g, [n(353), n(354), n(355)])).toBe('3 closed · #353 #354 #355')
    expect(groupLabel(g, [n(353), n(354), n(355), n(356), n(357)])).toBe('5 closed · #353 #354 #355 …')
    expect(groupLabel(g, [n(1), n(2)], 1)).toBe('2 closed · #1 …')
  })

  it('names blockers of another repository in full', () => {
    expect(groupLabel(g, [n(7, 'acme/infra'), n(8)])).toBe('2 closed · acme/infra#7 #8')
  })
})

describe('positionGraph', () => {
  const e = (from: number, to: number): GraphEdge => ({ from: k(from), to: k(to), closed: false })
  const sizes = (keys: string[], s: Size = { width: 200, height: 100 }) => new Map(keys.map((key) => [key, s]))
  // 3 and 4 are blocked by 1 and 2; 5 by 3 and 4.
  const keys = [k(1), k(2), k(3), k(4), k(5)]
  const edges = [e(3, 1), e(3, 2), e(4, 1), e(4, 2), e(5, 3), e(5, 4)]

  it('puts every blocker left of its dependents in LR', () => {
    const p = positionGraph(keys, edges, sizes(keys), 'LR')
    expect(p.order).toEqual([k(1), k(2), k(3), k(4), k(5)])
    for (const ed of edges) {
      const dep = p.boxes.get(ed.from)!
      const blk = p.boxes.get(ed.to)!
      expect(blk.x + blk.width).toBeLessThanOrEqual(dep.x - graphSpacing.rank + 1)
    }
    const xs = [...p.boxes.values()].map((b) => b.x)
    const ys = [...p.boxes.values()].map((b) => b.y)
    expect(Math.min(...xs)).toBe(0)
    expect(Math.min(...ys)).toBe(0)
    expect(p.width).toBe(Math.max(...[...p.boxes.values()].map((b) => b.x + b.width)))
    // Two nodes of one rank never overlap.
    const one = p.boxes.get(k(1))!
    const two = p.boxes.get(k(2))!
    expect(one.x).toBe(two.x)
    expect(Math.abs(one.y - two.y)).toBeGreaterThanOrEqual(100 + graphSpacing.node)
  })

  it('stacks one node per row in TB, blockers above, right of the gutter', () => {
    const p = positionGraph(keys, edges, sizes(keys), 'TB')
    let y = 0
    for (const key of p.order) {
      const b = p.boxes.get(key)!
      expect(b.x).toBe(graphSpacing.gutter)
      expect(b.y).toBe(y)
      y += 100 + graphSpacing.row
    }
    for (const ed of edges) expect(p.boxes.get(ed.to)!.y).toBeLessThan(p.boxes.get(ed.from)!.y)
    expect(p.width).toBe(graphSpacing.gutter + 200)
    expect(p.height).toBe(5 * 100 + 4 * graphSpacing.row)
  })

  it('gives each edge its own port on a node with several edges', () => {
    for (const dir of ['LR', 'TB'] as const) {
      const p = positionGraph(keys, edges, sizes(keys), dir)
      const points = new Map<string, string[]>()
      for (const ed of p.edges) {
        const lr = dir === 'LR'
        const a = portPoint(p.boxes.get(ed.from)!, ed.fromPort, false)
        const b = portPoint(p.boxes.get(ed.to)!, ed.toPort, lr)
        for (const [key, pt] of [
          [ed.from, a],
          [ed.to, b],
        ] as const)
          points.set(key, [...(points.get(key) ?? []), `${pt.x},${pt.y}`])
        const r = (n: number) => Math.round(n * 10) / 10
        const end = dir === 'LR' ? `${r(b.x)} ${r(b.y)}` : `${r(b.y)} H ${r(b.x)}`
        expect(edgePath(p, ed).startsWith(`M ${r(a.x)} ${r(a.y)} `)).toBe(true)
        expect(edgePath(p, ed).endsWith(` ${end}`)).toBe(true)
      }
      for (const [key, list] of points) {
        // In LR a node's in- and out-ports sit on opposite sides.
        expect(new Set(list).size, `${dir} ${key}`).toBe(list.length)
      }
      // 3 meets 1 and 2 on its left side at different points.
      const three = p.edges.filter((ed) => ed.from === k(3)).map((ed) => ed.fromPort)
      expect(three.map((pt) => pt.index).sort()).toEqual([0, 1])
    }
  })

  it('gives overlapping TB edges their own lanes inside the gutter, shorter ones nearer the nodes', () => {
    const p = positionGraph(keys, edges, sizes(keys), 'TB')
    const ys = (id: string) => {
      const ed = p.edges.find((x) => x.id === id)!
      const a = portPoint(p.boxes.get(ed.from)!, ed.fromPort, false).y
      const b = portPoint(p.boxes.get(ed.to)!, ed.toPort, false).y
      return [Math.min(a, b), Math.max(a, b)]
    }
    for (const x of p.edges) {
      expect(x.lanes).toBe(Math.max(...p.edges.map((y) => y.lane)) + 1)
      const lx = laneX(x.lane, x.lanes)
      expect(lx).toBeGreaterThan(0)
      expect(lx).toBeLessThan(graphSpacing.gutter)
      // The edge's vertical run is at its lane.
      expect(edgePath(p, x)).toContain(` ${Math.round(lx * 10) / 10} `)
      for (const y of p.edges) {
        if (x === y) continue
        const [a, b] = ys(x.id)
        const [c, d] = ys(y.id)
        if (a <= d && c <= b) expect(x.lane, `${x.id} vs ${y.id}`).not.toBe(y.lane)
        if (c < a && b < d) expect(x.lane).toBeLessThan(y.lane)
      }
    }
    // LR edges have no lanes.
    for (const x of positionGraph(keys, edges, sizes(keys), 'LR').edges) expect([x.lane, x.lanes]).toEqual([0, 1])
  })

  it('ends on cycles and places every node', () => {
    const ks = [k(1), k(2), k(3), k(4)]
    for (const dir of ['LR', 'TB'] as const) {
      const p = positionGraph(ks, [e(1, 2), e(2, 3), e(3, 1), e(4, 1)], sizes(ks), dir)
      expect(p.boxes.size).toBe(4)
      expect(p.edges.filter((ed) => ed.cycle)).toHaveLength(1)
      for (const ed of p.edges) expect(edgePath(p, ed)).not.toBe('')
    }
  })

  it('places nodes without a shown edge and an empty graph', () => {
    const p = positionGraph([k(1), k(2)], [], sizes([k(1), k(2)]), 'LR')
    expect(p.order).toEqual([k(1), k(2)])
    expect(p.boxes.size).toBe(2)
    expect(positionGraph([], [], new Map(), 'TB')).toMatchObject({ width: 0, height: 0, order: [], edges: [] })
  })
})

describe('nodeName', () => {
  const g = buildGraph({
    repo,
    issues: [
      issue(1, [blocker(7, { repo: 'acme/infra', title: 'Infra prerequisite' }), blocker(9, { state: 'CLOSED' })], {
        title: 'Add export',
        labels: ['scope', 'shaped'],
      }),
    ],
  })

  it('names an Issue by label, title and stage, then context and closed blockers', () => {
    expect(nodeName(g, g.nodes.get(k(1))!, false, 0)).toBe('#1 Add export, stage Deliver')
    expect(nodeName(g, g.nodes.get(k(1))!, true, 1)).toBe('#1 Add export, stage Deliver, context: blocks a shown Issue, 1 closed blocker')
    expect(nodeName(g, g.nodes.get(k(1))!, false, 3)).toBe('#1 Add export, stage Deliver, 3 closed blockers')
  })

  it('names a blocker outside the list by its state', () => {
    expect(nodeName(g, g.nodes.get(k(7, 'acme/infra'))!, true, 0)).toBe(
      'acme/infra#7 Infra prerequisite, open blocker not listed here, context: blocks a shown Issue',
    )
  })
})

describe('unlinkedGroups', () => {
  it('groups by stage in the track order, then Not in Nibfin, then Paused or Pending', () => {
    const g = buildGraph({
      repo,
      issues: [
        issue(1, [], { labels: [] }),
        issue(2, [], { labels: ['scope', 'shaped', 'delivered'] }),
        issue(3, [], { labels: ['scope', 'shaped'] }),
        issue(4, [], { labels: ['scope'] }),
        issue(5, [], { labels: ['request'] }),
        issue(6, [], { labels: ['scope'], pause: true }),
      ],
    })
    const groups = unlinkedGroups(g, [k(1), k(2), k(3), k(4), k(5), k(6)])
    expect(groups.map((x) => [x.id, x.label, x.keys])).toEqual([
      ['scope', 'Scope', [k(5)]],
      ['shape', 'Shape', [k(4)]],
      ['deliver', 'Deliver', [k(3)]],
      ['done', 'Done', [k(2)]],
      ['none', 'Not in Nibfin', [k(1)]],
      ['held', 'Paused or Pending', [k(6)]],
    ])
  })

  it('lists a paused or pending Issue only under Paused or Pending', () => {
    const g = buildGraph({
      repo,
      issues: [issue(1, [], { labels: ['scope', 'shaped'], pending: true }), issue(2, [], { labels: [], pause: true, pending: true })],
    })
    expect(unlinkedGroups(g, [k(1), k(2)])).toEqual([{ id: 'held', label: 'Paused or Pending', keys: [k(1), k(2)] }])
  })

  it('keeps a pending Issue held whatever its repair state, and flags it by that state', () => {
    const r = (state: 'repairing' | 'choose' | 'act') => ({ state, reason: 'r' })
    const issues = [
      issue(1, [], { pending: true, repair: r('repairing') }),
      issue(2, [], { pending: true, repair: r('choose') }),
      issue(3, [], { pending: true, pause: true, repair: r('act') }),
      issue(4, [], { pending: true }),
    ]
    const g = buildGraph({ repo, issues })
    expect(unlinkedGroups(g, [k(1), k(2), k(3), k(4)])).toEqual([{ id: 'held', label: 'Paused or Pending', keys: [k(1), k(2), k(3), k(4)] }])
    expect(issues.map(heldFlags)).toEqual([
      [{ key: 'pending', label: 'Repairing', tone: 'info' }],
      [{ key: 'pending', label: 'Waiting for your choice', tone: 'warning' }],
      [
        { key: 'paused', label: 'Paused', tone: 'warning' },
        { key: 'pending', label: 'Needs your action', tone: 'warning' },
      ],
      [{ key: 'pending', label: 'Pending', tone: 'warning' }],
    ])
    // The toolbar's Pending filter still matches every pending Issue.
    expect(filterGraph(g, { ...emptyFilter, pending: true }).matched).toEqual([k(1), k(2), k(3), k(4)])
  })

  it('drops empty groups and keeps the given order of keys', () => {
    const g = buildGraph({ repo, issues: [issue(1), issue(2, [], { labels: [] }), issue(3)] })
    expect(unlinkedGroups(g, [k(3), k(2), k(1)])).toEqual([
      { id: 'shape', label: 'Shape', keys: [k(3), k(1)] },
      { id: 'none', label: 'Not in Nibfin', keys: [k(2)] },
    ])
    expect(unlinkedGroups(g, [])).toEqual([])
  })
})
