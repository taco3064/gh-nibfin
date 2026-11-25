import { describe, expect, it } from 'vitest'

import { buildGraph, compareKeys, emptyFilter, filterGraph, layoutGraph, nodeKey, type GraphEdge } from './graph'
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
      issues: [
        issue(1, [blocker(2), blocker(7, { repo: 'Acme/Infra' }), blocker(9, { state: 'CLOSED' })]),
        issue(2, [blocker(7, { repo: 'acme/infra' })]),
      ],
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
