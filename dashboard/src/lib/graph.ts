// Pure logic of the repository view's dependency graph (#353): the nodes and
// edges from the status data, the search and filters that keep the blockers
// of what is shown as context, and a layered layout. Edges point from a
// dependent Issue to its blocker. Stages come from labels only (stageOf);
// nothing here derives a stage from the graph.

import { blockersView, stageOf, type Stage } from '@/lib/issue'
import { ownerRepo, type Blocker, type RepoView, type StatusIssue } from '@/lib/status'

export type GraphNode =
  | { kind: 'issue'; key: string; repo: string; number: number; issue: StatusIssue }
  // blocker is an Issue outside the listed ones: closed, in another
  // repository or otherwise not listed. Only its reference was read.
  | { kind: 'blocker'; key: string; repo: string; number: number; blocker: Blocker }

export interface GraphEdge {
  // from is the dependent's key, to the blocker's.
  from: string
  to: string
  closed: boolean
}

export interface Graph {
  // repo is the OWNER/REPO of the listed Issues.
  repo: string
  // nodes keeps the listed Issues first, in the data's order, then the
  // blockers in the order they were first seen.
  nodes: Map<string, GraphNode>
  edges: GraphEdge[]
  // unread are the listed Issues whose blockers failed or were not read;
  // the graph may miss their edges.
  unread: StatusIssue[]
}

// nodeKey is the case-insensitive key of OWNER/REPO#number.
export function nodeKey(repo: string, number: number): string {
  return `${repo}#${number}`.toLowerCase()
}

// nodeLabel names a node: #n in the graph's repository, OWNER/REPO#n
// otherwise.
export function nodeLabel(graph: Pick<Graph, 'repo'>, node: Pick<GraphNode, 'repo' | 'number'>): string {
  return node.repo.toLowerCase() === graph.repo.toLowerCase() ? `#${node.number}` : `${node.repo}#${node.number}`
}

export function buildGraph(view: Pick<RepoView, 'repo' | 'issues'>): Graph {
  const repo = ownerRepo(view.repo)
  const nodes = new Map<string, GraphNode>()
  const issues = view.issues ?? []
  for (const issue of issues) {
    const key = nodeKey(repo, issue.number)
    if (!nodes.has(key)) nodes.set(key, { kind: 'issue', key, repo, number: issue.number, issue })
  }
  const edges: GraphEdge[] = []
  const seen = new Set<string>()
  const unread: StatusIssue[] = []
  for (const issue of issues) {
    const from = nodeKey(repo, issue.number)
    if (issue.blockedByError || issue.blockedBy === undefined || issue.blockedBy === null) unread.push(issue)
    for (const b of issue.blockedBy ?? []) {
      const to = nodeKey(b.repo, b.number)
      if (!nodes.has(to)) nodes.set(to, { kind: 'blocker', key: to, repo: b.repo, number: b.number, blocker: b })
      const id = `${from}>${to}`
      if (seen.has(id)) continue
      seen.add(id)
      edges.push({ from, to, closed: b.state.toUpperCase() === 'CLOSED' })
    }
  }
  return { repo, nodes, edges, unread }
}

export interface GraphFilter {
  query: string
  // stages empty means every stage.
  stages: Stage[]
  paused: boolean
  pending: boolean
  // blocked keeps Issues with an open blocker.
  blocked: boolean
}

export const emptyFilter: GraphFilter = { query: '', stages: [], paused: false, pending: false, blocked: false }

export function isEmptyFilter(f: GraphFilter): boolean {
  return f.query.trim() === '' && f.stages.length === 0 && !f.paused && !f.pending && !f.blocked
}

// matches tests one listed Issue against the filter; every part must hold.
export function matches(issue: StatusIssue, f: GraphFilter): boolean {
  const q = f.query.trim().toLowerCase()
  if (q !== '') {
    const n = /^#?(\d+)$/.exec(q)
    const byNumber = n !== null && Number(n[1]) === issue.number
    if (!byNumber && !issue.title.toLowerCase().includes(q)) return false
  }
  if (f.stages.length > 0 && !f.stages.includes(stageOf(issue.labels))) return false
  if (f.paused && !issue.pause) return false
  if (f.pending && !issue.pending) return false
  if (f.blocked && blockersView(issue).open.length === 0) return false
  return true
}

export interface FilteredGraph {
  // matched are the keys of the listed Issues that pass, in node order.
  matched: string[]
  // context are the keys of the nodes a matched one depends on, directly or
  // through a chain, that are not matched themselves.
  context: string[]
  // edges are the edges among matched and context nodes.
  edges: GraphEdge[]
}

export function filterGraph(graph: Graph, f: GraphFilter): FilteredGraph {
  const matched: string[] = []
  for (const n of graph.nodes.values()) if (n.kind === 'issue' && matches(n.issue, f)) matched.push(n.key)
  const out = new Map<string, string[]>()
  for (const e of graph.edges) out.set(e.from, [...(out.get(e.from) ?? []), e.to])
  const visible = new Set(matched)
  const queue = [...matched]
  while (queue.length > 0) {
    for (const to of out.get(queue.shift()!) ?? []) {
      if (visible.has(to)) continue
      visible.add(to)
      queue.push(to)
    }
  }
  const isMatched = new Set(matched)
  const context = [...graph.nodes.keys()].filter((k) => visible.has(k) && !isMatched.has(k))
  const edges = graph.edges.filter((e) => visible.has(e.from) && visible.has(e.to))
  return { matched, context, edges }
}

export interface LayoutEdge extends GraphEdge {
  // cycle marks an edge that closes a cycle; it is drawn but not ranked.
  cycle: boolean
}

export interface Layout {
  // columns[r] holds the keys of rank r, top to bottom. Rank 0 has no shown
  // blocker; a higher rank stands right of every blocker it has.
  columns: string[][]
  // unlinked are the nodes with no shown edge, in the given order.
  unlinked: string[]
  edges: LayoutEdge[]
  // cycles are the keys of each found cycle, in edge order.
  cycles: string[][]
}

// compareKeys orders OWNER/REPO#n keys by repository, then by number.
export function compareKeys(a: string, b: string): number {
  const i = a.lastIndexOf('#')
  const j = b.lastIndexOf('#')
  const ra = a.slice(0, i)
  const rb = b.slice(0, j)
  if (ra !== rb) return ra < rb ? -1 : 1
  return Number(a.slice(i + 1)) - Number(b.slice(j + 1))
}

// layoutGraph ranks nodes by the longest path to a blocker without one.
// An edge that closes a cycle is found by a depth-first search and left out
// of the ranking, so the layout always ends.
export function layoutGraph(nodes: readonly string[], edges: readonly GraphEdge[]): Layout {
  const linked = new Set<string>()
  for (const e of edges) {
    linked.add(e.from)
    linked.add(e.to)
  }
  const unlinked = nodes.filter((k) => !linked.has(k))
  const order = nodes.filter((k) => linked.has(k)).sort(compareKeys)
  const out = new Map<string, number[]>()
  edges.forEach((e, i) => out.set(e.from, [...(out.get(e.from) ?? []), i]))

  // Iterative DFS: state 1 is on the stack, 2 is finished.
  const state = new Map<string, number>()
  const cycleEdge = new Set<number>()
  const cycles: string[][] = []
  for (const root of order) {
    if (state.has(root)) continue
    const stack: { key: string; next: number }[] = [{ key: root, next: 0 }]
    state.set(root, 1)
    while (stack.length > 0) {
      const top = stack[stack.length - 1]
      const list = out.get(top.key) ?? []
      if (top.next >= list.length) {
        state.set(top.key, 2)
        stack.pop()
        continue
      }
      const ei = list[top.next++]
      const to = edges[ei].to
      const s = state.get(to)
      if (s === 1) {
        cycleEdge.add(ei)
        const at = stack.findIndex((f) => f.key === to)
        cycles.push(stack.slice(at).map((f) => f.key))
      } else if (s === undefined) {
        state.set(to, 1)
        stack.push({ key: to, next: 0 })
      }
    }
  }

  // Ranks on the acyclic rest, in reverse finishing order of a DFS.
  const rank = new Map<string, number>()
  const rankOf = (start: string) => {
    const stack: { key: string; next: number; best: number }[] = [{ key: start, next: 0, best: 0 }]
    while (stack.length > 0) {
      const top = stack[stack.length - 1]
      const list = (out.get(top.key) ?? []).filter((i) => !cycleEdge.has(i))
      if (top.next >= list.length) {
        rank.set(top.key, top.best)
        stack.pop()
        if (stack.length > 0) {
          const parent = stack[stack.length - 1]
          parent.best = Math.max(parent.best, top.best + 1)
        }
        continue
      }
      const to = edges[list[top.next++]].to
      const r = rank.get(to)
      if (r !== undefined) top.best = Math.max(top.best, r + 1)
      else stack.push({ key: to, next: 0, best: 0 })
    }
  }
  for (const k of order) if (!rank.has(k)) rankOf(k)

  const columns: string[][] = []
  for (const k of order) {
    const r = rank.get(k)!
    ;(columns[r] ??= []).push(k)
  }
  // One pass by the mean row of each node's blockers, then by key.
  const row = new Map<string, number>()
  columns.forEach((col, r) => {
    if (r > 0) {
      const mean = new Map<string, number>()
      for (const k of col) {
        const rows = (out.get(k) ?? []).filter((i) => !cycleEdge.has(i)).map((i) => row.get(edges[i].to) ?? 0)
        mean.set(k, rows.length > 0 ? rows.reduce((a, b) => a + b, 0) / rows.length : 0)
      }
      col.sort((a, b) => mean.get(a)! - mean.get(b)! || compareKeys(a, b))
    }
    col.forEach((k, i) => row.set(k, i))
  })
  return {
    columns: columns.map((c) => c ?? []),
    unlinked,
    edges: edges.map((e, i) => ({ ...e, cycle: cycleEdge.has(i) })),
    cycles,
  }
}
