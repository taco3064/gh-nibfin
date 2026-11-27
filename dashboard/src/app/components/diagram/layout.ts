// Pure layout of the site's static diagrams (#456): boxes, directed edges
// with optional labels, and named groups drawn as frames or lanes. Authors
// give no coordinates; dagre ranks and orders the nodes, and every size comes
// from the text. Everything here runs synchronously at render time, so Astro
// can output the figure as HTML at build time.

import dagre from '@dagrejs/dagre'

export interface DiagramNode {
  id: string
  title: string
  detail?: string
  // badges are short labels such as label names, drawn as monospace pills.
  badges?: string[]
  // group is the id of the node's group or lane.
  group?: string
}

export interface DiagramEdge {
  from: string
  to: string
  label?: string
}

export interface DiagramGroup {
  id: string
  title: string
}

export interface DiagramSpec {
  nodes: DiagramNode[]
  edges: DiagramEdge[]
  groups?: DiagramGroup[]
  // groupStyle draws groups as dashed frames around their members (the
  // default) or as lanes: one band per group across the whole figure.
  groupStyle?: 'frames' | 'lanes'
}

export type Direction = 'LR' | 'TB'

export interface Point {
  x: number
  y: number
}

// Box is a rectangle by its top-left corner.
export interface Box {
  x: number
  y: number
  width: number
  height: number
}

// TextLine is one line of a node's text, with its baseline relative to the
// node's top.
export interface TextLine {
  text: string
  kind: 'title' | 'detail'
  y: number
}

// PlacedBadge is a badge pill relative to the node's top-left corner.
export interface PlacedBadge extends Box {
  text: string
}

export interface PlacedNode extends Box {
  id: string
  lines: TextLine[]
  badges: PlacedBadge[]
}

export interface PlacedLabel extends Box {
  lines: string[]
}

export interface PlacedEdge {
  from: string
  to: string
  // points runs from the source box's border to the target box's border.
  points: Point[]
  label?: PlacedLabel
}

export interface PlacedGroup extends Box {
  // id is undefined for the trailing lane of nodes without a group.
  id?: string
  title: string
  // titleLines are the wrapped title with their baselines (absolute).
  titleLines: { text: string; x: number; y: number }[]
}

export interface Placement {
  direction: Direction
  groupStyle: 'frames' | 'lanes'
  width: number
  height: number
  nodes: PlacedNode[]
  edges: PlacedEdge[]
  groups: PlacedGroup[]
  // order lists the node ids in reading order: by rank, then across it.
  order: string[]
}

// Sizes in SVG user units. The character limits are conservative for the
// site fonts at these sizes, so wrapped text stays inside its box.
export const sizes = {
  nodeWidth: 148,
  nodePadX: 12,
  nodePadY: 12,
  titleSize: 14,
  titleLine: 18,
  titleChars: 15,
  detailSize: 12,
  detailLine: 16,
  detailChars: 20,
  badgeSize: 11,
  badgeCharWidth: 6.8,
  badgeHeight: 18,
  badgePadX: 6,
  badgeGap: 4,
  labelSize: 12,
  labelLine: 16,
  labelChars: 20,
  labelCharWidth: 6.6,
  labelPad: 6,
  groupTitleSize: 12,
  groupTitleLine: 16,
  groupTitleCharWidth: 7.2,
  groupPad: 14,
  margin: 8,
} as const

// wrapText breaks text at spaces into lines of at most maxChars characters;
// a word longer than that is split.
export function wrapText(text: string, maxChars: number): string[] {
  const limit = Math.max(1, Math.floor(maxChars))
  const lines: string[] = []
  let line = ''
  for (const raw of text.trim().split(/\s+/)) {
    let word = raw
    while (word.length > limit) {
      if (line) {
        lines.push(line)
        line = ''
      }
      lines.push(word.slice(0, limit))
      word = word.slice(limit)
    }
    if (!word) continue
    if (!line) line = word
    else if (line.length + 1 + word.length <= limit) line = `${line} ${word}`
    else {
      lines.push(line)
      line = word
    }
  }
  if (line) lines.push(line)
  return lines
}

// validate rejects specs that would draw a wrong figure, naming the id.
function validate(spec: DiagramSpec) {
  const groups = new Set<string>()
  for (const g of spec.groups ?? []) {
    if (groups.has(g.id)) throw new Error(`diagram: duplicate group id "${g.id}"`)
    groups.add(g.id)
  }
  const nodes = new Set<string>()
  for (const n of spec.nodes) {
    if (nodes.has(n.id)) throw new Error(`diagram: duplicate node id "${n.id}"`)
    nodes.add(n.id)
    if (n.group !== undefined && !groups.has(n.group)) {
      throw new Error(`diagram: node "${n.id}" is in unknown group "${n.group}"`)
    }
  }
  for (const e of spec.edges) {
    for (const end of [e.from, e.to]) {
      if (!nodes.has(end)) throw new Error(`diagram: edge ${e.from} -> ${e.to} names unknown node "${end}"`)
    }
    if (e.from === e.to) throw new Error(`diagram: edge from node "${e.from}" to itself`)
  }
}

// nodeContent sizes a node from its text and places its lines and badges.
function nodeContent(n: DiagramNode): Pick<PlacedNode, 'width' | 'height' | 'lines' | 'badges'> {
  const s = sizes
  const width = s.nodeWidth
  const inner = width - 2 * s.nodePadX
  const lines: TextLine[] = []
  let y = s.nodePadY
  for (const text of wrapText(n.title, s.titleChars)) {
    y += s.titleLine
    lines.push({ text, kind: 'title', y: y - 4 })
  }
  if (n.detail) {
    y += 4
    for (const text of wrapText(n.detail, s.detailChars)) {
      y += s.detailLine
      lines.push({ text, kind: 'detail', y: y - 4 })
    }
  }
  const badges: PlacedBadge[] = []
  if (n.badges?.length) {
    y += 8
    let x = s.nodePadX
    for (const text of n.badges) {
      const w = Math.min(inner, text.length * s.badgeCharWidth + 2 * s.badgePadX)
      if (x > s.nodePadX && x + w > s.nodePadX + inner) {
        x = s.nodePadX
        y += s.badgeHeight + s.badgeGap
      }
      badges.push({ text, x, y, width: w, height: s.badgeHeight })
      x += w + s.badgeGap
    }
    y += s.badgeHeight
  }
  return { width, height: y + s.nodePadY, lines, badges }
}

function labelSize(text: string): { lines: string[]; width: number; height: number } {
  const s = sizes
  const lines = wrapText(text, s.labelChars)
  const longest = Math.max(...lines.map((l) => l.length))
  return { lines, width: longest * s.labelCharWidth + 2 * s.labelPad, height: lines.length * s.labelLine + s.labelPad }
}

// titlePlate is the background plate under one line of a group title.
export function titlePlate(t: { text: string; x: number; y: number }): Box {
  const s = sizes
  return { x: t.x - 4, y: t.y - s.groupTitleLine + 3, width: t.text.length * s.groupTitleCharWidth + 8, height: s.groupTitleLine + 2 }
}

// groupTitle wraps a group title to the given width.
function groupTitle(title: string, width: number) {
  const s = sizes
  const lines = wrapText(title, (width - 2 * s.groupPad) / s.groupTitleCharWidth)
  return { lines, height: 10 + lines.length * s.groupTitleLine }
}

const nodeKey = (id: string) => `n:${id}`
const groupKey = (id: string) => `g:${id}`

// clip moves from the centre of b towards p and returns where that line
// leaves b's border.
function clip(b: Box, p: Point): Point {
  const cx = b.x + b.width / 2
  const cy = b.y + b.height / 2
  const dx = p.x - cx
  const dy = p.y - cy
  if (dx === 0 && dy === 0) return { x: cx, y: b.y + b.height }
  const sx = dx === 0 ? Infinity : b.width / 2 / Math.abs(dx)
  const sy = dy === 0 ? Infinity : b.height / 2 / Math.abs(dy)
  const t = Math.min(sx, sy)
  return { x: cx + dx * t, y: cy + dy * t }
}

const centre = (b: Box): Point => ({ x: b.x + b.width / 2, y: b.y + b.height / 2 })

// layoutDiagram places spec in direction: LR runs left to right, TB top to
// bottom. It throws an Error naming the id for a duplicate id, an edge to an
// unknown node or a node in an unknown group.
export function layoutDiagram(spec: DiagramSpec, direction: Direction): Placement {
  validate(spec)
  const groupStyle = spec.groupStyle ?? 'frames'
  const content = new Map(spec.nodes.map((n) => [n.id, nodeContent(n)]))
  const labels = spec.edges.map((e) => (e.label ? labelSize(e.label) : undefined))
  const raw =
    groupStyle === 'lanes'
      ? layoutLanes(spec, direction, content, labels)
      : layoutFrames(spec, direction, content, labels)
  return normalize(raw, spec, direction, groupStyle)
}

type Content = Map<string, ReturnType<typeof nodeContent>>
type Labels = (ReturnType<typeof labelSize> | undefined)[]
type Raw = Pick<Placement, 'nodes' | 'edges' | 'groups'>

// dagreGraph lays out the nodes and edges with dagre; with compound set,
// each group becomes a cluster that keeps its members together.
function dagreGraph(
  spec: DiagramSpec,
  direction: Direction,
  content: Content,
  labels: Labels,
  compound: boolean,
  sep = { nodesep: 24, ranksep: 40 },
) {
  const g = new dagre.graphlib.Graph({ compound, multigraph: true })
  g.setGraph({ rankdir: direction, ...sep, edgesep: 12, marginx: 0, marginy: 0 })
  for (const n of spec.nodes) {
    const c = content.get(n.id)!
    g.setNode(nodeKey(n.id), { width: c.width, height: c.height })
  }
  if (compound) {
    const used = new Set(spec.nodes.map((n) => n.group).filter((x) => x !== undefined))
    for (const gr of spec.groups ?? []) if (used.has(gr.id)) g.setNode(groupKey(gr.id), {})
    for (const n of spec.nodes) if (n.group !== undefined) g.setParent(nodeKey(n.id), groupKey(n.group))
  }
  spec.edges.forEach((e, i) => {
    const l = labels[i]
    g.setEdge(nodeKey(e.from), nodeKey(e.to), l ? { width: l.width, height: l.height, labelpos: 'c' } : {}, `e${i}`)
  })
  dagre.layout(g)
  return g
}

// titleRoom is the space a frame keeps above its members for its title:
// the title, then a gap that leaves an arrowhead entering a member's top
// clear of the title's plate.
const titleRoom = (lines: number) => 10 + lines * sizes.groupTitleLine + 10

function membersOf(spec: DiagramSpec, nodes: PlacedNode[], group: string) {
  return nodes.filter((n) => spec.nodes.find((m) => m.id === n.id)!.group === group)
}

const bounds = (boxes: Box[]) => {
  const x = Math.min(...boxes.map((b) => b.x))
  const y = Math.min(...boxes.map((b) => b.y))
  return {
    x,
    y,
    width: Math.max(...boxes.map((b) => b.x + b.width)) - x,
    height: Math.max(...boxes.map((b) => b.y + b.height)) - y,
  }
}

function layoutFrames(spec: DiagramSpec, direction: Direction, content: Content, labels: Labels): Raw {
  const s = sizes
  const compound = (spec.groups ?? []).length > 0
  // dagre keeps every other node out of a cluster's box, but how much room
  // it leaves above the members depends on the spacing. Widen the spacing
  // across the top until every cluster has room for its title.
  const sep = { nodesep: 24, ranksep: 40 }
  let g = dagreGraph(spec, direction, content, labels, compound, sep)
  for (let round = 0; compound && round < 4; round++) {
    let short = 0
    for (const gr of spec.groups ?? []) {
      const cl = g.node(groupKey(gr.id)) as { x: number; y: number; width: number; height: number } | undefined
      if (!cl?.width) continue
      const members = spec.nodes.filter((n) => n.group === gr.id).map((n) => g.node(nodeKey(n.id)))
      const top = Math.min(...members.map((m) => m.y - m.height / 2))
      const width = Math.max(...members.map((m) => m.x + m.width / 2)) - Math.min(...members.map((m) => m.x - m.width / 2))
      const lines = groupTitle(gr.title, width + 2 * s.groupPad).lines.length
      short = Math.max(short, titleRoom(lines) - (top - (cl.y - cl.height / 2)))
    }
    if (short <= 0) break
    if (direction === 'TB') sep.ranksep += Math.ceil(short) * 2
    else sep.nodesep += Math.ceil(short) * 2
    g = dagreGraph(spec, direction, content, labels, compound, sep)
  }
  const nodes: PlacedNode[] = spec.nodes.map((n) => {
    const p = g.node(nodeKey(n.id))
    const c = content.get(n.id)!
    return { id: n.id, x: p.x - c.width / 2, y: p.y - c.height / 2, ...c }
  })
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const edges: PlacedEdge[] = spec.edges.map((e, i) => {
    const d = g.edge(nodeKey(e.from), nodeKey(e.to), `e${i}`) as { points: Point[]; x?: number; y?: number }
    const points = d.points.map((p) => ({ x: p.x, y: p.y }))
    // Snap the ends onto the box borders, where dagre's intersection already
    // puts them up to rounding.
    points[0] = clip(byId.get(e.from)!, points[1] ?? centre(byId.get(e.to)!))
    points[points.length - 1] = clip(byId.get(e.to)!, points[points.length - 2] ?? centre(byId.get(e.from)!))
    const l = labels[i]
    return {
      from: e.from,
      to: e.to,
      points,
      label: l && { lines: l.lines, width: l.width, height: l.height, x: d.x! - l.width / 2, y: d.y! - l.height / 2 },
    }
  })
  const groups: PlacedGroup[] = []
  for (const gr of spec.groups ?? []) {
    const members = membersOf(spec, nodes, gr.id)
    if (members.length === 0) continue
    const m = bounds(members)
    const title = groupTitle(gr.title, m.width + 2 * s.groupPad)
    const room = titleRoom(title.lines.length)
    let box: Box = { x: m.x - s.groupPad, y: m.y - room, width: m.width + 2 * s.groupPad, height: m.height + room + s.groupPad }
    // Keep the frame inside dagre's cluster, which no other node enters.
    const cl = g.node(groupKey(gr.id)) as { x: number; y: number; width: number; height: number } | undefined
    if (cl?.width) {
      const cx = cl.x - cl.width / 2
      const cy = cl.y - cl.height / 2
      const x0 = Math.max(box.x, cx)
      const y0 = Math.max(box.y, cy)
      const x1 = Math.min(box.x + box.width, cx + cl.width)
      const y1 = Math.min(box.y + box.height, cy + cl.height)
      box = { x: x0, y: y0, width: x1 - x0, height: y1 - y0 }
    }
    groups.push({
      id: gr.id,
      title: gr.title,
      ...box,
      titleLines: title.lines.map((text, j) => ({ text, x: box.x + 10, y: box.y + 4 + (j + 1) * s.groupTitleLine })),
    })
  }
  return { nodes, edges, groups }
}

// layoutLanes takes the ranks from dagre and places each group in its own
// band, in the declared order: horizontal bands for LR, columns for TB.
// Nodes without a group go in a trailing unnamed band.
function layoutLanes(spec: DiagramSpec, direction: Direction, content: Content, labels: Labels): Raw {
  const s = sizes
  const lr = direction === 'LR'
  const g = dagreGraph(spec, direction, content, labels, false)
  // Ranks: dagre gives every node of a rank the same coordinate along the flow.
  const flowOf = (id: string) => {
    const p = g.node(nodeKey(id))
    return Math.round(lr ? p.x : p.y)
  }
  const crossOf = (id: string) => {
    const p = g.node(nodeKey(id))
    return lr ? p.y : p.x
  }
  const flows = [...new Set(spec.nodes.map((n) => flowOf(n.id)))].sort((a, b) => a - b)
  const rankOf = new Map(spec.nodes.map((n) => [n.id, flows.indexOf(flowOf(n.id))]))
  const ranks = flows.length

  const lanes: { id?: string; title: string; members: DiagramNode[] }[] = (spec.groups ?? []).map((gr) => ({
    id: gr.id,
    title: gr.title,
    members: spec.nodes.filter((n) => n.group === gr.id),
  }))
  const loose = spec.nodes.filter((n) => n.group === undefined)
  if (loose.length) lanes.push({ title: '', members: loose })

  const cellGap = 16
  const laneGap = 8
  const pad = s.groupPad
  const maxLabel = Math.max(0, ...labels.map((l) => (l ? (lr ? l.width : l.height) : 0)))
  // Forward edges bend in the gap after their source's rank, each on its
  // own line (slot), so no two edges share a segment there. The gap fits the
  // slots, then a label clear of them, centred in the gap.
  const forwardOf = (e: DiagramEdge) => rankOf.get(e.to)! > rankOf.get(e.from)!
  const backOf = (e: DiagramEdge) => rankOf.get(e.to)! < rankOf.get(e.from)!
  const indexed = spec.edges.map((e, i) => ({ e, i }))
  const slotStart = 10
  const slotStep = 8
  const perGap = Array.from({ length: ranks }, (_, r) => spec.edges.filter((e) => forwardOf(e) && rankOf.get(e.from) === r).length)
  const slots = Math.max(1, ...perGap)
  const flowGap = Math.max(40, maxLabel + 2 * (slotStart + 4 + (slots - 1) * slotStep))
  const slotZone = slotStart + 4 + (slots - 1) * slotStep

  // Edges against the flow (back edges) run only through node-free space:
  // out of the source's far side into the gap after its rank, along that gap
  // to a return channel outside the lanes (right of the last column in TB,
  // below the last band in LR), along the channel to the gap after the
  // target's rank, and along that gap into the target's far side. Each back
  // edge has its own track in the channel, its own slot in the source's gap
  // and its own band, wide enough for its label, in the target's gap. Inner
  // tracks go to the edges that span the fewest ranks, so nested back edges
  // do not cross each other.
  const back = indexed.filter(({ e }) => backOf(e))
  const span = ({ e }: { e: DiagramEdge }) => rankOf.get(e.from)! - rankOf.get(e.to)!
  const trackOf = new Map([...back].sort((p, q) => span(p) - span(q) || p.i - q.i).map(({ i }, k) => [i, k]))
  const track = (p: { i: number }) => trackOf.get(p.i)!
  const bandPad = 6
  const bandSize = (i: number) => {
    const l = labels[i]
    return (l ? (lr ? l.width : l.height) : 0) + 2 * bandPad
  }
  // In a source's gap the inner tracks take the upper slots; in a target's
  // gap the outer tracks take the upper bands.
  const outBack = (r: number) => back.filter(({ e }) => rankOf.get(e.from) === r).sort((p, q) => track(p) - track(q))
  const inBack = (r: number) => back.filter(({ e }) => rankOf.get(e.to) === r).sort((p, q) => track(q) - track(p))
  // gaps[r] is the gap after rank r, relative to its start: its size, where
  // the forward label into rank r + 1 is centred, the back slots and bands.
  const gaps = Array.from({ length: ranks }, (_, r) => {
    const outs = outBack(r)
    const used = perGap[r] + outs.length
    const zone = Math.max(r === ranks - 1 ? 0 : slotZone, used > 0 ? slotStart + 4 + (used - 1) * slotStep : 0)
    const backSlot = new Map(outs.map(({ i }, k) => [i, slotStart + (perGap[r] + k) * slotStep]))
    let cursor = zone
    const band = new Map<number, number>()
    for (const { i } of inBack(r)) {
      band.set(i, cursor + bandSize(i) / 2)
      cursor += bandSize(i)
    }
    if (r === ranks - 1) return { size: outs.length ? zone + 4 : 0, labelAt: 0, backSlot, band }
    const middle = flowGap - 2 * slotZone
    return { size: cursor + middle + slotZone, labelAt: cursor + middle / 2, backSlot, band }
  })
  const gapTotal = gaps.reduce((sum, g) => sum + g.size, 0)

  // cells[lane][rank] lists the lane's nodes of that rank in dagre's order.
  const cells = lanes.map((lane) =>
    Array.from({ length: ranks }, (_, r) =>
      lane.members.filter((n) => rankOf.get(n.id) === r).sort((a, b) => crossOf(a.id) - crossOf(b.id)),
    ),
  )
  const crossSize = (id: string) => (lr ? content.get(id)!.height : content.get(id)!.width)
  const flowSize = (id: string) => (lr ? content.get(id)!.width : content.get(id)!.height)
  const cellCross = (cell: DiagramNode[]) =>
    cell.reduce((sum, n) => sum + crossSize(n.id), 0) + Math.max(0, cell.length - 1) * cellGap
  const laneContent = cells.map((row) => Math.max(sizes.nodeWidth / 2, ...row.map(cellCross)))
  // Size of each rank along the flow.
  const rankSize = Array.from({ length: ranks }, (_, r) =>
    Math.max(0, ...spec.nodes.filter((n) => rankOf.get(n.id) === r).map((n) => flowSize(n.id))),
  )
  const starts = (first: number) => {
    const out: number[] = []
    let at = first
    for (let r = 0; r < ranks; r++) {
      out.push(at)
      at += rankSize[r] + gaps[r].size
    }
    return out
  }

  const nodes = new Map<string, PlacedNode>()
  const groups: PlacedGroup[] = []
  let rankStart: number[]
  // laneEnd is where the last lane ends across the flow.
  let laneEnd = 0
  if (lr) {
    const width = rankSize.reduce((a, b) => a + b, 0) + gapTotal + 2 * pad
    rankStart = starts(pad)
    let y = 0
    lanes.forEach((lane, li) => {
      const title = groupTitle(lane.title || ' ', width)
      const header = lane.title ? title.height + 4 : pad
      const height = header + laneContent[li] + pad
      cells[li].forEach((cell, r) => {
        let c = y + header + (laneContent[li] - cellCross(cell)) / 2
        for (const n of cell) {
          const k = content.get(n.id)!
          nodes.set(n.id, { id: n.id, x: rankStart[r] + (rankSize[r] - k.width) / 2, y: c, ...k })
          c += k.height + cellGap
        }
      })
      groups.push({
        id: lane.id,
        title: lane.title,
        x: 0,
        y,
        width,
        height,
        titleLines: lane.title ? title.lines.map((text, j) => ({ text, x: 10, y: y + 4 + (j + 1) * s.groupTitleLine })) : [],
      })
      laneEnd = y + height
      y += height + laneGap
    })
  } else {
    const laneWidth = laneContent.map((c) => c + 2 * pad)
    const titles = lanes.map((lane, li) => groupTitle(lane.title || ' ', laneWidth[li]))
    const header = Math.max(pad, ...lanes.map((lane, li) => (lane.title ? titles[li].height + 4 : 0)))
    rankStart = starts(header)
    const height = header + rankSize.reduce((a, b) => a + b, 0) + gapTotal + pad
    let x = 0
    lanes.forEach((lane, li) => {
      cells[li].forEach((cell, r) => {
        let c = x + pad + (laneContent[li] - cellCross(cell)) / 2
        for (const n of cell) {
          const k = content.get(n.id)!
          nodes.set(n.id, { id: n.id, x: c, y: rankStart[r] + (rankSize[r] - k.height) / 2, ...k })
          c += k.width + cellGap
        }
      })
      groups.push({
        id: lane.id,
        title: lane.title,
        x,
        y: 0,
        width: laneWidth[li],
        height,
        titleLines: lane.title
          ? titles[li].lines.map((text, j) => ({ text, x: x + 10, y: 4 + (j + 1) * s.groupTitleLine }))
          : [],
      })
      laneEnd = x + laneWidth[li]
      x += laneWidth[li] + laneGap
    })
  }
  const gapStart = (r: number) => rankStart[r] + rankSize[r]

  // Every edge leaves and enters its boxes at separate ports, spread along
  // the side, so no two edges share a segment next to a box. Forward edges
  // enter the near side and leave the far side in the order of the other
  // end; back edges use the far side after them, towards the channel: the
  // ones entering first (inner tracks first), then the ones leaving (outer
  // tracks first).
  const crossCentre = (id: string) => {
    const n = nodes.get(id)!
    return lr ? n.y + n.height / 2 : n.x + n.width / 2
  }
  const outPort = new Map<number, number>()
  const inPort = new Map<number, number>()
  const backOutPort = new Map<number, number>()
  const backInPort = new Map<number, number>()
  for (const n of spec.nodes) {
    const box = nodes.get(n.id)!
    const [start, size] = lr ? [box.y, box.height] : [box.x, box.width]
    const forward = (end: 'from' | 'to') => {
      const other = end === 'from' ? 'to' : 'from'
      return indexed
        .filter(({ e }) => e[end] === n.id && forwardOf(e))
        .sort((p, q) => crossCentre(p.e[other]) - crossCentre(q.e[other]) || p.i - q.i)
    }
    const spread = (side: { i: number; port: Map<number, number> }[]) =>
      side.forEach(({ i, port }, k) => port.set(i, start + (size * (k + 1)) / (side.length + 1)))
    spread(forward('to').map(({ i }) => ({ i, port: inPort })))
    spread([
      ...forward('from').map(({ i }) => ({ i, port: outPort })),
      ...back
        .filter(({ e }) => e.to === n.id)
        .sort((p, q) => track(p) - track(q))
        .map(({ i }) => ({ i, port: backInPort })),
      ...back
        .filter(({ e }) => e.from === n.id)
        .sort((p, q) => track(q) - track(p))
        .map(({ i }) => ({ i, port: backOutPort })),
    ])
  }
  const bendAt = new Map<number, number>()
  for (let r = 0; r < ranks; r++) {
    indexed
      .filter(({ e }) => forwardOf(e) && rankOf.get(e.from) === r)
      .sort((p, q) => outPort.get(p.i)! - outPort.get(q.i)! || inPort.get(p.i)! - inPort.get(q.i)! || p.i - q.i)
      .forEach(({ i }, k) => bendAt.set(i, gapStart(r) + slotStart + k * slotStep))
  }

  // A back edge's label sits on its stretch along the target's gap, past
  // every back edge entering a box of that rank and clear of the forward
  // edges crossing the gap. The channel starts past every such label.
  const backLabel = new Map<number, number>()
  let channel = laneEnd + 12
  for (const { e, i } of back) {
    const l = labels[i]
    if (!l) continue
    const r = rankOf.get(e.to)!
    const width = lr ? l.height : l.width
    const stubs = back.filter((p) => rankOf.get(p.e.to) === r).map((p) => backInPort.get(p.i)!)
    const crossing = indexed
      .filter((p) => forwardOf(p.e) && rankOf.get(p.e.from)! <= r && rankOf.get(p.e.to)! > r)
      .map((p) => inPort.get(p.i)!)
      .sort((a, b) => a - b)
    let from = Math.max(...stubs) + 8
    for (const c of crossing) if (c > from - 6 && c < from + width + 6) from = c + 6
    backLabel.set(i, from)
    channel = Math.max(channel, from + width + 12)
  }

  // at places a point given along the flow (f) and across it (c).
  const at = (f: number, c: number): Point => (lr ? { x: f, y: c } : { x: c, y: f })
  const edges: PlacedEdge[] = spec.edges.map((e, i) => {
    const a = nodes.get(e.from)!
    const b = nodes.get(e.to)!
    const l = labels[i]
    let points: Point[]
    let mid: Point
    if (forwardOf(e)) {
      // Bend on the edge's own slot after the source's rank; the label sits
      // on the edge's own last stretch, centred in the gap before the target.
      const so = outPort.get(i)!
      const ti = inPort.get(i)!
      const m = bendAt.get(i)!
      const before = rankOf.get(e.to)! - 1
      const gapMid = gapStart(before) + gaps[before].labelAt
      if (lr) {
        const sx = a.x + a.width
        points = so === ti ? [{ x: sx, y: so }, { x: b.x, y: ti }] : [{ x: sx, y: so }, { x: m, y: so }, { x: m, y: ti }, { x: b.x, y: ti }]
        mid = { x: gapMid, y: ti }
      } else {
        const sy = a.y + a.height
        points = so === ti ? [{ x: so, y: sy }, { x: ti, y: b.y }] : [{ x: so, y: sy }, { x: so, y: m }, { x: ti, y: m }, { x: ti, y: b.y }]
        mid = { x: ti, y: gapMid }
      }
    } else if (backOf(e)) {
      const rs = rankOf.get(e.from)!
      const rt = rankOf.get(e.to)!
      const po = backOutPort.get(i)!
      const pi = backInPort.get(i)!
      const slot = gapStart(rs) + gaps[rs].backSlot.get(i)!
      const band = gapStart(rt) + gaps[rt].band.get(i)!
      const t = channel + trackOf.get(i)! * slotStep
      const far = (box: Box) => (lr ? box.x + box.width : box.y + box.height)
      points = [at(far(a), po), at(slot, po), at(slot, t), at(band, t), at(band, pi), at(far(b), pi)]
      const width = l ? (lr ? l.height : l.width) : 0
      mid = at(band, (backLabel.get(i) ?? pi) + width / 2)
    } else {
      // Same rank: a straight line between the borders.
      const p = clip(a, centre(b))
      const q = clip(b, centre(a))
      points = [p, q]
      mid = { x: (p.x + q.x) / 2, y: (p.y + q.y) / 2 }
    }
    return {
      from: e.from,
      to: e.to,
      points,
      label: l && { lines: l.lines, width: l.width, height: l.height, x: mid.x - l.width / 2, y: mid.y - l.height / 2 },
    }
  })
  return { nodes: spec.nodes.map((n) => nodes.get(n.id)!), edges, groups }
}

// normalize moves everything so the figure starts at the margin, and sets
// the overall size and the reading order.
function normalize(raw: Raw, spec: DiagramSpec, direction: Direction, groupStyle: Placement['groupStyle']): Placement {
  const boxes: Box[] = [...raw.nodes, ...raw.groups, ...raw.edges.flatMap((e) => (e.label ? [e.label] : []))]
  const points = raw.edges.flatMap((e) => e.points)
  const xs = [...boxes.flatMap((b) => [b.x, b.x + b.width]), ...points.map((p) => p.x)]
  const ys = [...boxes.flatMap((b) => [b.y, b.y + b.height]), ...points.map((p) => p.y)]
  const m = sizes.margin
  const dx = m - Math.min(...xs)
  const dy = m - Math.min(...ys)
  const round = (v: number) => Math.round(v * 100) / 100
  const move = <T extends Box>(b: T): T => ({ ...b, x: round(b.x + dx), y: round(b.y + dy) })
  const nodes = raw.nodes.map(move)
  const index = new Map(spec.nodes.map((n, i) => [n.id, i]))
  const lr = direction === 'LR'
  const order = [...nodes]
    .sort((a, b) => {
      const fa = Math.round(lr ? a.x + a.width / 2 : a.y)
      const fb = Math.round(lr ? b.x + b.width / 2 : b.y)
      if (fa !== fb) return fa - fb
      const ca = lr ? a.y : a.x
      const cb = lr ? b.y : b.x
      return ca !== cb ? ca - cb : index.get(a.id)! - index.get(b.id)!
    })
    .map((n) => n.id)
  return {
    direction,
    groupStyle,
    width: Math.ceil(Math.max(...xs) + dx + m),
    height: Math.ceil(Math.max(...ys) + dy + m),
    nodes,
    edges: raw.edges.map((e) => ({
      ...e,
      points: e.points.map((p) => ({ x: round(p.x + dx), y: round(p.y + dy) })),
      label: e.label && move(e.label),
    })),
    groups: raw.groups.map((g) => ({
      ...move(g),
      titleLines: g.titleLines.map((t) => ({ ...t, x: round(t.x + dx), y: round(t.y + dy) })),
    })),
    order,
  }
}

export interface DescribedEdge {
  to: string
  label?: string
}

export interface DescribedNode {
  id: string
  title: string
  detail?: string
  badges: string[]
  // group is the title of the node's group or lane.
  group?: string
  edges: DescribedEdge[]
}

// describeDiagram is the text alternative of a placed diagram: every node
// once, in reading order, with its group and its outgoing edges as text.
export function describeDiagram(spec: DiagramSpec, placement: Pick<Placement, 'order'>): DescribedNode[] {
  const nodes = new Map(spec.nodes.map((n) => [n.id, n]))
  const groups = new Map((spec.groups ?? []).map((g) => [g.id, g.title]))
  return placement.order.map((id) => {
    const n = nodes.get(id)!
    return {
      id,
      title: n.title,
      detail: n.detail,
      badges: n.badges ?? [],
      group: n.group === undefined ? undefined : groups.get(n.group),
      edges: spec.edges
        .filter((e) => e.from === id)
        .map((e) => ({ to: nodes.get(e.to)!.title, label: e.label })),
    }
  })
}

// edgeText reads one outgoing edge: "to <target title>", plus "(<label>)".
export function edgeText(e: DescribedEdge): string {
  return e.label ? `to ${e.to} (${e.label})` : `to ${e.to}`
}
