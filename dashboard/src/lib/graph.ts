// Pure logic of the repository view's dependency graph (#353): the nodes and
// edges from the status data, the search and filters that keep the blockers
// of what is shown as context, and a layered layout. Edges point from a
// dependent Issue to its blocker. Stages come from labels only (stageOf);
// nothing here derives a stage from the graph.

import dagre from '@dagrejs/dagre'

import { blockersView, repairLook, stageLabels, stageOf, type Stage } from '@/lib/issue'
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

// closedGroups lists, per dependent key, its closed blockers among the given
// edges, in key order (#409). A closed blocker shared by several dependents
// is in each of their groups. The graph draws each group as one marker on
// its dependent instead of a node per blocker.
export function closedGroups(graph: Pick<Graph, 'nodes'>, edges: readonly GraphEdge[]): Map<string, GraphNode[]> {
  const keys = new Map<string, string[]>()
  for (const e of edges) {
    if (!e.closed || !graph.nodes.has(e.to)) continue
    const list = keys.get(e.from) ?? []
    if (!list.includes(e.to)) list.push(e.to)
    keys.set(e.from, list)
  }
  const out = new Map<string, GraphNode[]>()
  for (const [from, list] of keys)
    out.set(
      from,
      list.sort(compareKeys).map((k) => graph.nodes.get(k)!),
    )
  return out
}

// groupLabel is the short label of a closed group, such as
// "5 closed · #353 #354 #355 …": the count, then the first max labels.
export function groupLabel(graph: Pick<Graph, 'repo'>, group: readonly Pick<GraphNode, 'repo' | 'number'>[], max = 3): string {
  const names = group.slice(0, max).map((n) => nodeLabel(graph, n))
  if (group.length > max) names.push('…')
  return `${group.length} closed · ${names.join(' ')}`
}

// Direction is the flow of the drawn graph: LR puts blockers left of their
// dependents, TB puts them above, one node per row, for narrow regions.
export type Direction = 'LR' | 'TB'

export interface Size {
  width: number
  height: number
}

export interface Box extends Size {
  // x and y are the top left corner.
  x: number
  y: number
}

// Port is where one edge meets a node's side: the index-th of count points
// spread evenly along that side.
export interface Port {
  index: number
  count: number
}

export interface PlacedEdge extends LayoutEdge {
  // id is "from>to".
  id: string
  // fromPort sits on the dependent's left side, toPort on the blocker's
  // right side in LR and its left side in TB.
  fromPort: Port
  toPort: Port
  // lane is the edge's own vertical lane in the TB gutter, 0 nearest the
  // nodes, of lanes in all; edges whose spans overlap never share one. LR
  // edges have lane 0 of 1.
  lane: number
  lanes: number
}

export interface Placement {
  direction: Direction
  boxes: Map<string, Box>
  // order lists the keys in reading order: by rank, then by row.
  order: string[]
  edges: PlacedEdge[]
  width: number
  height: number
}

// graphSpacing is the gap between ranks and between nodes of one rank, and
// in TB the gutter left of the nodes that the edges bow into.
export const graphSpacing = { rank: 96, node: 24, row: 20, gutter: 56 } as const

// positionGraph places the full nodes of the graph. keys are the nodes to
// place, edges the edges among them (open edges: closed blockers are drawn
// as markers instead). LR runs dagre on blocker → dependent so blockers
// stand left; TB stacks one node per row in layoutGraph's rank and row
// order, blockers above. Each edge gets its own port on both nodes, ordered
// by the position of its other end, so edges into one node stay apart.
export function positionGraph(keys: readonly string[], edges: readonly GraphEdge[], sizes: ReadonlyMap<string, Size>, direction: Direction): Placement {
  const layout = layoutGraph(keys, edges)
  const order = [...layout.columns.flat(), ...layout.unlinked]
  const size = (k: string): Size => sizes.get(k) ?? { width: 0, height: 0 }
  const boxes = new Map<string, Box>()
  if (direction === 'TB') {
    let y = 0
    for (const k of order) {
      boxes.set(k, { x: graphSpacing.gutter, y, ...size(k) })
      y += size(k).height + graphSpacing.row
    }
  } else {
    const g = new dagre.graphlib.Graph()
    g.setGraph({ rankdir: 'LR', ranksep: graphSpacing.rank, nodesep: graphSpacing.node, marginx: 0, marginy: 0 })
    g.setDefaultEdgeLabel(() => ({}))
    for (const k of order) g.setNode(k, { ...size(k) })
    for (const e of layout.edges) if (!e.cycle) g.setEdge(e.to, e.from)
    dagre.layout(g)
    for (const k of order) {
      const n = g.node(k)
      boxes.set(k, { x: n.x - n.width / 2, y: n.y - n.height / 2, ...size(k) })
    }
  }
  let minX = Infinity
  let minY = Infinity
  for (const b of boxes.values()) {
    minX = Math.min(minX, b.x)
    minY = Math.min(minY, b.y)
  }
  if (direction === 'TB') minX = 0
  let width = 0
  let height = 0
  for (const b of boxes.values()) {
    b.x -= minX
    b.y -= minY
    width = Math.max(width, b.x + b.width)
    height = Math.max(height, b.y + b.height)
  }

  // Ends on each side of each node, sorted by the other end's centre.
  const ends = new Map<string, { edge: number; other: string; at: 'from' | 'to' }[]>()
  const side = (k: string, at: 'from' | 'to') => (direction === 'TB' || at === 'from' ? `${k}|left` : `${k}|right`)
  layout.edges.forEach((e, i) => {
    for (const [k, other, at] of [
      [e.from, e.to, 'from'],
      [e.to, e.from, 'to'],
    ] as const) {
      const s = side(k, at)
      ends.set(s, [...(ends.get(s) ?? []), { edge: i, other, at }])
    }
  })
  const centre = (k: string) => {
    const b = boxes.get(k)!
    return { x: b.x + b.width / 2, y: b.y + b.height / 2 }
  }
  const ports = layout.edges.map(() => ({ from: { index: 0, count: 1 }, to: { index: 0, count: 1 } }))
  for (const list of ends.values()) {
    list.sort((a, b) => centre(a.other).y - centre(b.other).y || centre(a.other).x - centre(b.other).x || a.edge - b.edge)
    list.forEach((end, index) => (ports[end.edge][end.at] = { index, count: list.length }))
  }
  // TB lanes: by span, shortest first, each edge takes the lowest lane no
  // overlapping edge holds, so shorter edges nest inside longer ones.
  const lane = layout.edges.map(() => 0)
  let lanes = 1
  if (direction === 'TB') {
    const span = layout.edges.map((e, i) => {
      const a = portPoint(boxes.get(e.from)!, ports[i].from, false).y
      const b = portPoint(boxes.get(e.to)!, ports[i].to, false).y
      return { i, top: Math.min(a, b), bottom: Math.max(a, b) }
    })
    span.sort((x, y) => x.bottom - x.top - (y.bottom - y.top) || x.top - y.top || x.i - y.i)
    const taken: { top: number; bottom: number }[][] = []
    for (const sp of span) {
      let l = 0
      while (taken[l]?.some((t) => t.top <= sp.bottom + 2 && sp.top <= t.bottom + 2)) l++
      ;(taken[l] ??= []).push(sp)
      lane[sp.i] = l
    }
    lanes = Math.max(1, taken.length)
  }
  return {
    direction,
    boxes,
    order,
    edges: layout.edges.map((e, i) => ({ ...e, id: `${e.from}>${e.to}`, fromPort: ports[i].from, toPort: ports[i].to, lane: lane[i], lanes })),
    width: order.length > 0 ? width : 0,
    height: order.length > 0 ? height : 0,
  }
}

// portPoint is where a port meets its node: on the left or right side,
// spread evenly from top to bottom.
export function portPoint(box: Box, port: Port, onRight: boolean): { x: number; y: number } {
  return { x: onRight ? box.x + box.width : box.x, y: box.y + (box.height * (port.index + 1)) / (port.count + 1) }
}

// laneX is the x of a TB lane: lanes spread across the gutter, lane 0
// nearest the nodes.
export function laneX(lane: number, lanes: number): number {
  const width = graphSpacing.gutter - 10
  return graphSpacing.gutter - 6 - (width * (lane + 0.5)) / lanes
}

// edgePath is the SVG path of one placed edge, from the dependent to the
// blocker, where the arrow head goes. In LR it leaves the dependent's left
// side and reaches the blocker's right side; in TB both ends are on the
// left side and the edge runs along its own lane in the gutter, past the
// nodes between.
export function edgePath(placement: Pick<Placement, 'direction' | 'boxes'>, edge: PlacedEdge): string {
  const a = placement.boxes.get(edge.from)
  const b = placement.boxes.get(edge.to)
  if (!a || !b) return ''
  const lr = placement.direction === 'LR'
  const p = portPoint(a, edge.fromPort, false)
  const q = portPoint(b, edge.toPort, lr)
  const r = (n: number) => Math.round(n * 10) / 10
  if (lr) {
    const dx = Math.max(24, Math.abs(p.x - q.x) / 2)
    return `M ${r(p.x)} ${r(p.y)} C ${r(p.x - dx)} ${r(p.y)}, ${r(q.x + dx)} ${r(q.y)}, ${r(q.x)} ${r(q.y)}`
  }
  const x = laneX(edge.lane, edge.lanes)
  const s = q.y < p.y ? -1 : 1
  const c = Math.min(6, Math.abs(q.y - p.y) / 2, p.x - x)
  return [
    `M ${r(p.x)} ${r(p.y)} H ${r(x + c)} Q ${r(x)} ${r(p.y)} ${r(x)} ${r(p.y + s * c)}`,
    `V ${r(q.y - s * c)} Q ${r(x)} ${r(q.y)} ${r(x + c)} ${r(q.y)} H ${r(q.x)}`,
  ].join(' ')
}

// nodeName is the accessible name of a node on the canvas: its label and
// title, then its stage (from labels) or blocker state, whether it stands
// as context and how many closed blockers its marker holds.
export function nodeName(graph: Pick<Graph, 'repo'>, node: GraphNode, context: boolean, closed: number): string {
  const parts = [
    node.kind === 'issue'
      ? `${nodeLabel(graph, node)} ${node.issue.title}, stage ${stageLabels[stageOf(node.issue.labels)]}`
      : `${nodeLabel(graph, node)} ${node.blocker.title}, ${node.blocker.state.toUpperCase() === 'CLOSED' ? 'closed' : 'open'} blocker not listed here`,
  ]
  if (context) parts.push('context: blocks a shown Issue')
  if (closed > 0) parts.push(`${closed} closed ${closed === 1 ? 'blocker' : 'blockers'}`)
  return parts.join(', ')
}

// UnlinkedGroupId names a group of the Issues without dependencies: a
// stage from labels, or held for a paused or pending Issue.
export type UnlinkedGroupId = 'scope' | 'shape' | 'deliver' | 'done' | 'none' | 'held'

export interface UnlinkedGroup {
  id: UnlinkedGroupId
  label: string
  keys: string[]
}

// The groups follow the stage track's order, then Not in Nibfin, then
// Paused or Pending.
const unlinkedOrder: readonly UnlinkedGroupId[] = ['scope', 'shape', 'deliver', 'done', 'none', 'held']

export const heldLabel = 'Paused or Pending'

export interface HeldFlag {
  key: 'paused' | 'pending'
  label: string
  tone: 'info' | 'warning'
}

// heldFlags are the flags of a held row: Paused, and the repair state of a
// pending Issue (Proposal R9.52), or Pending when it was not read.
export function heldFlags(issue: StatusIssue): HeldFlag[] {
  const out: HeldFlag[] = []
  if (issue.pause) out.push({ key: 'paused', label: 'Paused', tone: 'warning' })
  if (issue.pending) {
    const look = issue.repair ? repairLook(issue.repair) : { label: 'Pending', tone: 'warning' as const }
    out.push({ key: 'pending', label: look.label, tone: look.tone })
  }
  return out
}

// unlinkedGroups groups the Issues without dependencies (#496): a paused or
// pending Issue only under Paused or Pending, any other under its stage from
// labels. Groups keep the given order of keys and empty ones are dropped.
export function unlinkedGroups(graph: Pick<Graph, 'nodes'>, keys: readonly string[]): UnlinkedGroup[] {
  const by = new Map<UnlinkedGroupId, string[]>()
  for (const k of keys) {
    const node = graph.nodes.get(k)
    if (!node) continue
    const id: UnlinkedGroupId = node.kind !== 'issue' ? 'none' : node.issue.pause || node.issue.pending ? 'held' : stageOf(node.issue.labels)
    by.set(id, [...(by.get(id) ?? []), k])
  }
  return unlinkedOrder.filter((id) => by.has(id)).map((id) => ({ id, label: id === 'held' ? heldLabel : stageLabels[id], keys: by.get(id)! }))
}
