// Pure layout of the site's static diagrams (#456): boxes, directed edges
// with optional labels, and named groups drawn as frames or lanes. Authors
// give no coordinates; dagre ranks and orders the nodes, and every size comes
// from the text. Everything here runs synchronously at render time, so Astro
// can output the figure as HTML at build time.

import dagre from '@dagrejs/dagre'
import type { LucideIcon } from 'lucide-react'

export interface DiagramNode {
  id: string
  title: string
  detail?: string
  // badges are short labels such as label names, drawn as monospace pills.
  badges?: string[]
  // group is the id of the node's group or lane.
  group?: string
  // icon is drawn before the title.
  icon?: LucideIcon
  // kinds are ids of the spec's kinds that mark the node, such as who acts
  // at a step; the first one also sets the node's border tone.
  kinds?: string[]
}

export type KindTone = 'primary' | 'info' | 'warning' | 'success' | 'muted'

// DiagramKind is one legend entry. A node names it by id and shows its icon
// and title as text, so the meaning never rests on the tone alone.
export interface DiagramKind {
  id: string
  title: string
  description?: string
  icon?: LucideIcon
  tone?: KindTone
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
  // kinds are the legend entries that nodes can name.
  kinds?: DiagramKind[]
  // kindsTitle heads the legend and names the kinds in the text list
  // ("Kinds" when not given).
  kindsTitle?: string
  // wrap, for LR only, keeps at most this many ranks in a row; later ranks
  // continue on the next row, again from the left. TB ignores it, and it
  // does not combine with groups.
  wrap?: number
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
  x: number
  y: number
}

// PlacedBadge is a badge pill relative to the node's top-left corner.
export interface PlacedBadge extends Box {
  text: string
}

// PlacedMarker is one kind marker of a node: its icon box and its title's
// lines with their baselines, relative to the node's top-left corner.
export interface PlacedMarker {
  kind: string
  icon: Box
  lines: { text: string; x: number; y: number }[]
}

export interface PlacedNode extends Box {
  id: string
  lines: TextLine[]
  badges: PlacedBadge[]
  // icon is the box of the node's icon, when it has one.
  icon?: Box
  markers: PlacedMarker[]
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
  // titleCharWidth and detailCharWidth are the character widths the limits
  // above assume at the default node width (124 inner units).
  titleCharWidth: 124 / 15,
  detailSize: 12,
  detailLine: 16,
  detailChars: 20,
  detailCharWidth: 124 / 20,
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
  iconSize: 16,
  iconGap: 6,
  markerSize: 11,
  markerLine: 15,
  markerCharWidth: 6.4,
  markerIcon: 12,
  markerGap: 4,
  rowGap: 48,
  rowSlot: 8,
  margin: 8,
} as const

// wrapText breaks text at whitespace into lines of at most maxChars
// characters. It never splits a word: a word longer than the limit gets a
// line of its own, and hyphens are not break points.
export function wrapText(text: string, maxChars: number): string[] {
  const limit = Math.max(1, Math.floor(maxChars + 1e-9))
  const lines: string[] = []
  let line = ''
  for (const word of text.trim().split(/\s+/)) {
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

// longestWord is the length of text's longest unbreakable word.
export function longestWord(text: string): number {
  return Math.max(0, ...text.trim().split(/\s+/).map((w) => w.length))
}

// badgeWidth is the width of a badge's pill, which always holds its text.
export const badgeWidth = (text: string) => text.length * sizes.badgeCharWidth + 2 * sizes.badgePadX

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
  const kinds = new Set<string>()
  for (const k of spec.kinds ?? []) {
    if (kinds.has(k.id)) throw new Error(`diagram: duplicate kind id "${k.id}"`)
    kinds.add(k.id)
  }
  for (const n of spec.nodes) {
    for (const k of n.kinds ?? []) {
      if (!kinds.has(k)) throw new Error(`diagram: node "${n.id}" has unknown kind "${k}"`)
    }
  }
  if (spec.wrap !== undefined) {
    if (!Number.isInteger(spec.wrap) || spec.wrap < 1) throw new Error(`diagram: wrap ${spec.wrap} is not a positive integer`)
    const grouped = spec.groups?.[0]?.id ?? spec.nodes.find((n) => n.group !== undefined)?.group
    if (grouped !== undefined) throw new Error(`diagram: wrap does not combine with groups (group "${grouped}")`)
  }
}

type NodeContent = Pick<PlacedNode, 'width' | 'height' | 'lines' | 'badges' | 'icon' | 'markers'>

// nodeContent sizes a node from its text and places its icon, lines, badges
// and kind markers. A node is sizes.nodeWidth wide, or wider when one of its
// unbreakable tokens (a word of its title, detail or kind titles, or a badge)
// needs more room, so no text leaves the node.
function nodeContent(n: DiagramNode, kinds: Map<string, DiagramKind>): NodeContent {
  const s = sizes
  // An icon sits before the first title line; the title is indented past it
  // and wraps to the narrower room.
  const indent = n.icon ? s.iconSize + s.iconGap : 0
  const markerIndent = s.markerIcon + s.markerGap
  const needed = Math.max(
    indent + longestWord(n.title) * s.titleCharWidth,
    longestWord(n.detail ?? '') * s.detailCharWidth,
    ...(n.kinds ?? []).map((id) => markerIndent + longestWord(kinds.get(id)?.title ?? '') * s.markerCharWidth),
    ...(n.badges ?? []).map(badgeWidth),
  )
  const width = Math.max(s.nodeWidth, Math.ceil(needed + 2 * s.nodePadX))
  const inner = width - 2 * s.nodePadX
  const lines: TextLine[] = []
  let y = s.nodePadY
  for (const text of wrapText(n.title, (inner - indent) / s.titleCharWidth)) {
    y += s.titleLine
    lines.push({ text, kind: 'title', x: s.nodePadX + indent, y: y - 4 })
  }
  if (n.detail) {
    y += 4
    for (const text of wrapText(n.detail, inner / s.detailCharWidth)) {
      y += s.detailLine
      lines.push({ text, kind: 'detail', x: s.nodePadX, y: y - 4 })
    }
  }
  const badges: PlacedBadge[] = []
  if (n.badges?.length) {
    y += 8
    let x = s.nodePadX
    for (const text of n.badges) {
      const w = badgeWidth(text)
      if (x > s.nodePadX && x + w > s.nodePadX + inner) {
        x = s.nodePadX
        y += s.badgeHeight + s.badgeGap
      }
      badges.push({ text, x, y, width: w, height: s.badgeHeight })
      x += w + s.badgeGap
    }
    y += s.badgeHeight
  }
  // One marker row per kind: a small icon, then the kind's title wrapped
  // beside it.
  const markers: PlacedMarker[] = []
  if (n.kinds?.length) {
    y += 10
    const textX = s.nodePadX + markerIndent
    const chars = (inner - markerIndent) / s.markerCharWidth
    n.kinds.forEach((id, j) => {
      if (j > 0) y += 4
      const top = y
      const text = wrapText(kinds.get(id)!.title, chars).map((t, i) => ({ text: t, x: textX, y: top + (i + 1) * s.markerLine - 4 }))
      const icon = { x: s.nodePadX, y: top + (s.markerLine - s.markerIcon) / 2, width: s.markerIcon, height: s.markerIcon }
      markers.push({ kind: id, icon, lines: text })
      y += text.length * s.markerLine
    })
  }
  const icon = n.icon && { x: s.nodePadX, y: s.nodePadY + 3, width: s.iconSize, height: s.iconSize }
  return { width, height: y + s.nodePadY, lines, badges, markers, ...(icon && { icon }) }
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
  const kinds = new Map((spec.kinds ?? []).map((k) => [k.id, k]))
  const content = new Map(spec.nodes.map((n) => [n.id, nodeContent(n, kinds)]))
  const labels = spec.edges.map((e) => (e.label ? labelSize(e.label) : undefined))
  let raw =
    groupStyle === 'lanes'
      ? layoutLanes(spec, direction, content, labels)
      : layoutFrames(spec, direction, content, labels)
  if (direction === 'LR' && spec.wrap !== undefined) raw = wrapRows(raw, spec, spec.wrap)
  return normalize(raw, spec, direction, groupStyle)
}

type Content = Map<string, NodeContent>
type Labels = (ReturnType<typeof labelSize> | undefined)[]
// rowOf, set by wrapRows, gives each node's row for the reading order.
type Raw = Pick<Placement, 'nodes' | 'edges' | 'groups'> & { rowOf?: Map<string, number> }

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
  // A TB lane is also wide enough for its title's longest word, which never
  // breaks: the title starts 10 units in, and its plate reaches 4 past it.
  const titleNeed = (title: string) => (lr ? 0 : longestWord(title) * s.groupTitleCharWidth + 14 - 2 * pad)
  const laneContent = cells.map((row, li) =>
    Math.max(sizes.nodeWidth / 2, titleNeed(lanes[li].title), ...row.map(cellCross)),
  )
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

// wrapRows breaks an ungrouped LR placement into rows of at most wrap ranks.
// Each row starts again at the left, below the previous one. An edge inside a
// row keeps dagre's route; an edge into the next row leaves its source's
// bottom, runs along its own line in the gutter between the rows and enters
// its target's top. It throws for an edge the rows would send backwards, over
// more than one row break, or through another node.
function wrapRows(raw: Raw, spec: DiagramSpec, wrap: number): Raw {
  const s = sizes
  const flowOf = (n: Box) => Math.round(n.x + n.width / 2)
  const flows = [...new Set(raw.nodes.map(flowOf))].sort((a, b) => a - b)
  const rankOf = new Map(raw.nodes.map((n) => [n.id, flows.indexOf(flowOf(n))]))
  const rowOf = new Map(raw.nodes.map((n) => [n.id, Math.floor(rankOf.get(n.id)! / wrap)]))
  const rows = Math.max(...rowOf.values()) + 1
  for (const e of spec.edges) {
    const step = rowOf.get(e.to)! - rowOf.get(e.from)!
    if (step < 0) throw new Error(`diagram: wrap ${wrap} sends edge ${e.from} -> ${e.to} back to an earlier row`)
    if (step > 1) throw new Error(`diagram: wrap ${wrap} sends edge ${e.from} -> ${e.to} across more than one row break`)
  }
  const sameRow = (e: PlacedEdge) => rowOf.get(e.from) === rowOf.get(e.to)

  // The extent of each row before moving: its nodes, and its own edges and
  // their labels.
  const extent = Array.from({ length: rows }, (_, r) => {
    const ys: number[] = []
    for (const n of raw.nodes) if (rowOf.get(n.id) === r) ys.push(n.y, n.y + n.height)
    for (const e of raw.edges) {
      if (!sameRow(e) || rowOf.get(e.from) !== r) continue
      ys.push(...e.points.map((p) => p.y))
      if (e.label) ys.push(e.label.y, e.label.y + e.label.height)
    }
    return { top: Math.min(...ys), bottom: Math.max(...ys) }
  })
  // The edges of each gutter (after row r), each with its own line.
  const crossing = raw.edges.map((e, i) => ({ e, i })).filter(({ e }) => !sameRow(e))
  const gutters = Array.from({ length: rows - 1 }, (_, r) => crossing.filter(({ e }) => rowOf.get(e.from) === r))
  const gutterSize = gutters.map(
    (g) => s.rowGap + Math.max(0, g.length - 1) * s.rowSlot + Math.max(0, ...g.map(({ e }) => e.label?.height ?? 0)),
  )
  const shift: Point[] = []
  let top = extent[0].top
  for (let r = 0; r < rows; r++) {
    shift.push({ x: flows[0] - flows[r * wrap], y: top - extent[r].top })
    top += extent[r].bottom - extent[r].top + (gutterSize[r] ?? 0)
  }

  const moveBox = <T extends Box>(b: T, d: Point): T => ({ ...b, x: b.x + d.x, y: b.y + d.y })
  const nodes = raw.nodes.map((n) => moveBox(n, shift[rowOf.get(n.id)!]))
  const byId = new Map(nodes.map((n) => [n.id, n]))

  // Ports: a node's crossing edges leave (or enter) it at separate points
  // along its bottom (or top), in the order of their other ends.
  const centreX = (id: string) => byId.get(id)!.x + byId.get(id)!.width / 2
  const portOf = (end: 'from' | 'to') => {
    const other = end === 'from' ? 'to' : 'from'
    const at = new Map<number, number>()
    for (const n of nodes) {
      const mine = crossing.filter(({ e }) => e[end] === n.id).sort((p, q) => centreX(p.e[other]) - centreX(q.e[other]) || p.i - q.i)
      mine.forEach(({ i }, k) => at.set(i, n.x + (n.width * (k + 1)) / (mine.length + 1)))
    }
    return at
  }
  const outPort = portOf('from')
  const inPort = portOf('to')
  const lineOf = new Map<number, number>()
  gutters.forEach((g, r) => {
    const bottom = extent[r].bottom + shift[r].y
    const first = bottom + (gutterSize[r] - (g.length - 1) * s.rowSlot) / 2
    ;[...g]
      .sort((p, q) => outPort.get(p.i)! - outPort.get(q.i)! || inPort.get(p.i)! - inPort.get(q.i)! || p.i - q.i)
      .forEach(({ i }, k) => lineOf.set(i, first + k * s.rowSlot))
  })

  const edges = raw.edges.map((e, i): PlacedEdge => {
    if (sameRow(e)) {
      const d = shift[rowOf.get(e.from)!]
      return { ...e, points: e.points.map((p) => ({ x: p.x + d.x, y: p.y + d.y })), label: e.label && moveBox(e.label, d) }
    }
    const a = byId.get(e.from)!
    const b = byId.get(e.to)!
    const ox = outPort.get(i)!
    const ix = inPort.get(i)!
    const y = lineOf.get(i)!
    // The drop from the source and the rise into the target must not pass
    // another node of their rows.
    for (const n of nodes) {
      if (n.id === a.id || n.id === b.id) continue
      const spans = ox > n.x && ox < n.x + n.width
      const spansIn = ix > n.x && ix < n.x + n.width
      if ((spans && n.y > a.y && rowOf.get(n.id) === rowOf.get(a.id)) || (spansIn && n.y < b.y && rowOf.get(n.id) === rowOf.get(b.id))) {
        throw new Error(`diagram: wrap ${wrap} routes edge ${e.from} -> ${e.to} through node "${n.id}"`)
      }
    }
    const points =
      Math.abs(ox - ix) < 0.5
        ? [{ x: ox, y: a.y + a.height }, { x: ox, y: b.y }]
        : [{ x: ox, y: a.y + a.height }, { x: ox, y }, { x: ix, y }, { x: ix, y: b.y }]
    const l = e.label
    return { ...e, points, label: l && { ...l, x: (ox + ix) / 2 - l.width / 2, y: y - l.height / 2 } }
  })
  return { nodes, edges, groups: raw.groups, rowOf }
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
      const ra = raw.rowOf?.get(a.id) ?? 0
      const rb = raw.rowOf?.get(b.id) ?? 0
      if (ra !== rb) return ra - rb
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
  // kinds are the titles of the node's kinds, when it has any.
  kinds?: string[]
  edges: DescribedEdge[]
}

// describeDiagram is the text alternative of a placed diagram: every node
// once, in reading order, with its group and its outgoing edges as text.
export function describeDiagram(spec: DiagramSpec, placement: Pick<Placement, 'order'>): DescribedNode[] {
  const nodes = new Map(spec.nodes.map((n) => [n.id, n]))
  const groups = new Map((spec.groups ?? []).map((g) => [g.id, g.title]))
  const kinds = new Map((spec.kinds ?? []).map((k) => [k.id, k.title]))
  return placement.order.map((id) => {
    const n = nodes.get(id)!
    return {
      id,
      title: n.title,
      detail: n.detail,
      badges: n.badges ?? [],
      group: n.group === undefined ? undefined : groups.get(n.group),
      ...(n.kinds?.length ? { kinds: n.kinds.map((k) => kinds.get(k)!) } : {}),
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
