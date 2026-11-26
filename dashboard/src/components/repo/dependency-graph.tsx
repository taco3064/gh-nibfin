import { createContext, useCallback, useContext, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type FocusEvent } from 'react'
import { Check, CircleCheck, Search, X } from 'lucide-react'
import {
  Controls,
  MiniMap,
  ReactFlow,
  ReactFlowProvider,
  ViewportPortal,
  useReactFlow,
  type Node,
  type NodeChangeset,
  type NodeProps,
} from '@xyflow/react'
import '@xyflow/react/dist/base.css'

import { BlockerItem, IssueItem, RunLegend } from '@/components/issue'
import { ExternalMark } from '@/components/issue/issue-item'
import { Note } from '@/components/shell/note'
import { SectionLoading } from '@/components/shell/section-loading'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Toggle } from '@/components/ui/toggle'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import {
  buildGraph,
  closedGroups,
  edgePath,
  emptyFilter,
  filterGraph,
  graphSpacing,
  groupLabel,
  isEmptyFilter,
  layoutGraph,
  nodeKey,
  nodeLabel,
  nodeName,
  positionGraph,
  type Direction,
  type Graph,
  type GraphEdge,
  type GraphFilter,
  type GraphNode,
  type Size,
} from '@/lib/graph'
import { accessOf, stageLabels, stageOf, type Change, type Stage } from '@/lib/issue'
import { useLoading } from '@/lib/loading-context'
import type { RepoViewData } from '@/lib/repo-source'
import type { RepoPermission } from '@/lib/status'
import { cn } from '@/lib/utils'

import './dependency-graph.css'

export interface DependencyGraphProps {
  data: RepoViewData
  onChanged: (changes: Change[]) => void
}

const stages = Object.keys(stageLabels) as Stage[]

// stageAccent colours the bar on top of an Issue node by its stage, from
// labels only. The stage track inside the node names the stage in text.
const stageAccent: Record<Stage, string> = {
  none: 'bg-border',
  scope: 'bg-warning',
  shape: 'bg-info',
  deliver: 'bg-primary',
  done: 'bg-success',
}

// joinLabels names keys as "#1, #2 and #3".
function joinLabels(labels: readonly string[]): string {
  if (labels.length <= 1) return labels.join('')
  return `${labels.slice(0, -1).join(', ')} and ${labels[labels.length - 1]}`
}

// DependencyGraph shows the repository's open Issues as a layered node
// graph (#409): blockers stand left of their dependents (above them on
// narrow screens) and arrows point from an Issue to what blocks it. Closed
// blockers collapse into one marker per dependent. Search and filters hide
// Issues but keep every blocker of a shown one visible, labelled as context.
export function DependencyGraph({ data, onChanged }: DependencyGraphProps) {
  const loading = useLoading()
  const blockersLoading = loading.has('blockedBy')
  const graph = useMemo(() => buildGraph(data), [data])
  const [filter, setFilter] = useState<GraphFilter>(emptyFilter)
  const filtered = useMemo(() => filterGraph(graph, filter), [graph, filter])
  const context = useMemo(() => new Set(filtered.context), [filtered])
  // The full layout decides what stands in the graph region: an Issue whose
  // only blockers are closed has an edge, so it stays in the graph with its
  // marker instead of being listed as having no dependencies.
  const layout = useMemo(() => {
    const visible = new Set([...filtered.matched, ...filtered.context])
    return layoutGraph([...graph.nodes.keys()].filter((k) => visible.has(k)), filtered.edges)
  }, [graph, filtered])
  const closed = useMemo(() => new Set(layout.edges.filter((e) => e.closed).map((e) => e.to)), [layout])
  const groups = useMemo(() => closedGroups(graph, layout.edges), [graph, layout])
  // The full nodes and the open edges among them are what the canvas draws.
  const drawn = useMemo(() => {
    const keys = layout.columns.flat().filter((k) => !closed.has(k))
    const edges: GraphEdge[] = filtered.edges.filter((e) => !e.closed)
    return { keys, edges }
  }, [layout, closed, filtered])
  // blocks lists, per blocker, the shown Issues it blocks.
  const blocks = useMemo(() => {
    const m = new Map<string, string[]>()
    for (const e of layout.edges) m.set(e.to, [...(m.get(e.to) ?? []), nodeLabel(graph, graph.nodes.get(e.from)!)])
    return m
  }, [graph, layout])
  const access: RepoPermission = accessOf({ permission: data.permission ?? null, permissionError: data.permissionError }, loading)
  // Issues without a shown edge are split: those whose blockers could not
  // be read are never listed as having no dependencies.
  const [unreadAlone, unlinked] = useMemo(() => {
    const unread = new Set(graph.unread.map((i) => nodeKey(graph.repo, i.number)))
    return [layout.unlinked.filter((k) => unread.has(k)), layout.unlinked.filter((k) => !unread.has(k))]
  }, [graph, layout])
  const total = data.issues?.length ?? 0
  const label = (k: string) => nodeLabel(graph, graph.nodes.get(k)!)
  const shared = useMemo<Shared>(() => ({ graph, access, onChanged }), [graph, access, onChanged])
  const contextShown = filtered.context.filter((k) => !closed.has(k)).length

  const listNode = (k: string) => (
    <li key={k}>
      <NodeView node={graph.nodes.get(k)!} context={context.has(k)} blocks={blocks.get(k) ?? []} closed={[]} graphNode={false} {...shared} />
    </li>
  )

  return (
    <div className="flex flex-col gap-4" data-slot="dependency-graph">
      <div className="flex flex-col gap-1">
        <h2 className="text-lg font-semibold tracking-tight">
          Dependency graph
        </h2>
        <p className="text-sm text-muted-foreground">Open Issues and what blocks them. Stages come from labels only.</p>
      </div>
      {loading.has('issues') && <SectionLoading label="Reading open Issues…" data-loading="issues" />}
      {data.issuesError && (
        <Note tone="danger" data-note="issues-error">
          Issues could not be read: {data.issuesError.code}: {data.issuesError.message}
        </Note>
      )}
      {!loading.has('issues') && total === 0 && !data.issuesError && (
        <p className="rounded-md border border-dashed border-border-subtle px-4 py-8 text-center text-sm text-muted-foreground" data-empty="issues">
          This repository has no open Issues.
        </p>
      )}
      {total > 0 && (
        <>
          <Toolbar
            filter={filter}
            onFilter={setFilter}
            count={
              `Showing ${filtered.matched.length} of ${total} open Issues · ${contextShown} shown as context` +
              (closed.size > 0 ? ` · ${closed.size} closed ${closed.size === 1 ? 'blocker' : 'blockers'} collapsed` : '')
            }
          />
          <RunLegend />
          {blockersLoading && <SectionLoading label="Reading blockers… edges appear once they are read." data-loading="blockedBy" />}
          {!blockersLoading && graph.unread.length > 0 && (
            <Note tone="warning" data-note="unread">
              Blockers of {joinLabels(graph.unread.map((i) => `#${i.number}`))} could not be read; edges may be missing.
            </Note>
          )}
          {layout.cycles.map((c) => (
            <Note tone="warning" data-note="cycle" key={c.join(' ')}>
              {joinLabels(c.map(label))} block each other in a cycle.
            </Note>
          ))}
          {filtered.matched.length === 0 && (
            <p className="rounded-md border border-dashed border-border-subtle px-4 py-8 text-center text-sm text-muted-foreground" data-empty="matches">
              No open Issue matches the search and filters.
            </p>
          )}
          {drawn.keys.length > 0 && (
            <SharedContext.Provider value={shared}>
              <ReactFlowProvider>
                <GraphRegion keys={drawn.keys} edges={drawn.edges} context={context} blocks={blocks} groups={groups} />
              </ReactFlowProvider>
            </SharedContext.Provider>
          )}
          {unreadAlone.length > 0 && (
            <div className="flex flex-col gap-3" data-slot="unread">
              <h3 className="text-base font-semibold">{blockersLoading ? 'Issues whose blockers are being read' : 'Issues whose blockers could not be read'}</h3>
              <ul className="flex flex-col gap-3">{unreadAlone.map(listNode)}</ul>
            </div>
          )}
          {unlinked.length > 0 && (
            <div className="flex flex-col gap-3" data-slot="unlinked">
              <h3 className="text-base font-semibold">Issues without dependencies</h3>
              <ul className="flex flex-col gap-3">{unlinked.map(listNode)}</ul>
            </div>
          )}
        </>
      )}
    </div>
  )
}

interface Shared {
  graph: Graph
  access: RepoPermission
  onChanged: (changes: Change[]) => void
}

// SharedContext hands the graph nodes what every node shares, so a node's
// own data stays small.
const SharedContext = createContext<Shared | null>(null)

function Toolbar({ filter, onFilter, count }: { filter: GraphFilter; onFilter: (f: GraphFilter) => void; count: string }) {
  const id = useId()
  const flag = (key: 'paused' | 'pending' | 'blocked', text: string) => (
    <Toggle
      variant="outline"
      size="sm"
      pressed={filter[key]}
      onPressedChange={(on) => onFilter({ ...filter, [key]: on })}
      className="border-border px-2.5"
      data-filter={key}
    >
      {filter[key] && <Check aria-hidden="true" />}
      {text}
    </Toggle>
  )
  return (
    <div className="flex flex-col gap-2" data-slot="graph-toolbar">
      <div className="flex flex-wrap items-center gap-2">
        <Label htmlFor={`${id}-q`} className="sr-only">
          Search Issues
        </Label>
        <div className="relative min-w-0 flex-1 basis-56">
          <Search aria-hidden="true" className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            id={`${id}-q`}
            type="search"
            placeholder="Search by title or #number"
            value={filter.query}
            onChange={(e) => onFilter({ ...filter, query: e.target.value })}
            className="h-8 pl-9"
          />
        </div>
        <ToggleGroup
          type="multiple"
          variant="outline"
          size="sm"
          spacing={1}
          aria-label="Stage"
          value={filter.stages}
          onValueChange={(v) => onFilter({ ...filter, stages: v as Stage[] })}
          className="flex-wrap"
        >
          {stages.map((s) => (
            <ToggleGroupItem key={s} value={s} className="border-border px-2.5" data-filter={`stage-${s}`}>
              {filter.stages.includes(s) && <Check aria-hidden="true" />}
              {stageLabels[s]}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        <div className="flex flex-wrap items-center gap-1" role="group" aria-label="Flags">
          {flag('paused', 'Paused')}
          {flag('pending', 'Pending')}
          {flag('blocked', 'Has open blockers')}
        </div>
        {!isEmptyFilter(filter) && (
          <Button variant="ghost" size="sm" onClick={() => onFilter(emptyFilter)} data-action="reset-filter">
            <X aria-hidden="true" />
            Reset
          </Button>
        )}
      </div>
      <p className="text-sm text-muted-foreground" aria-live="polite" data-slot="graph-count">
        {count}
      </p>
    </div>
  )
}

function NodeView({
  node,
  graph,
  context,
  blocks,
  closed,
  access,
  graphNode,
  width,
  onChanged,
}: Shared & {
  node: GraphNode
  context: boolean
  blocks: string[]
  closed: GraphNode[]
  // graphNode draws the node on the canvas: stacked, with its stage accent
  // and its closed blockers' marker. Otherwise it is a list entry.
  graphNode: boolean
  width?: number
}) {
  const stage = node.kind === 'issue' ? stageOf(node.issue.labels) : undefined
  return (
    <div
      data-node={node.key}
      data-kind={node.kind}
      data-context={context ? 'true' : undefined}
      data-stage={graphNode ? stage : undefined}
      style={width !== undefined ? { width } : undefined}
      className={cn('relative flex flex-col gap-1', context && 'rounded-xl border border-dashed border-muted-foreground p-1.5', graphNode && !context && 'rounded-xl')}
    >
      {context && <p className="px-1 text-xs font-medium text-muted-foreground">Context: blocks a shown Issue</p>}
      {node.kind === 'issue' ? (
        graphNode ? (
          <div className="flex flex-col">
            <span aria-hidden="true" data-slot="stage-accent" className={cn('h-1.5 rounded-t-lg', stageAccent[stage!])} />
            <IssueItem issue={node.issue} repo={graph.repo} access={access} onChanged={onChanged} stacked className="rounded-t-none border-t-0" />
          </div>
        ) : (
          <IssueItem issue={node.issue} repo={graph.repo} access={access} onChanged={onChanged} />
        )
      ) : (
        <BlockerItem blocker={node.blocker} blocks={blocks} />
      )}
      {node.kind === 'issue' && blocks.length > 0 && (
        <p className="px-1 text-sm break-words" data-slot="blocks">
          <span className="text-muted-foreground">Blocks</span> {blocks.join(', ')}
        </p>
      )}
      {closed.length > 0 && <ClosedMarker graph={graph} owner={node} group={closed} />}
      <span className="sr-only">{nodeLabel(graph, node)}</span>
    </div>
  )
}

// ClosedMarker is the small grey marker of an Issue's closed blockers; it
// opens the list of them with their GitHub links.
function ClosedMarker({ graph, owner, group }: { graph: Graph; owner: GraphNode; group: GraphNode[] }) {
  const name = nodeLabel(graph, owner)
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          data-slot="closed-blockers"
          className="nodrag nopan h-auto min-h-8 max-w-full self-start border-dashed bg-muted py-1 text-left whitespace-normal text-muted-foreground"
        >
          <CircleCheck aria-hidden="true" />
          <span className="min-w-0 break-words">{groupLabel(graph, group)}</span>
          <span className="sr-only">(closed blockers of {name})</span>
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" collisionPadding={16} className="w-80 max-w-[calc(100vw-2rem)]" data-slot="closed-list">
        <h4 className="text-sm font-semibold">Closed blockers of {name}</h4>
        <ul className="mt-2 flex flex-col gap-2 text-sm">
          {group.map(
            (b) =>
              b.kind === 'blocker' && (
                <li key={b.key}>
                  <a href={b.blocker.url} target="_blank" rel="noreferrer" className="rounded-sm text-link underline-offset-4 hover:underline">
                    <span className="break-all tabular-nums">{nodeLabel(graph, b)}</span> {b.blocker.title} <ExternalMark label="(opens on GitHub in a new tab)" />
                  </a>
                </li>
              ),
          )}
        </ul>
      </PopoverContent>
    </Popover>
  )
}

interface FlowData extends Record<string, unknown> {
  context: boolean
  blocks: string[]
  closed: GraphNode[]
  width: number
}

type FlowNode = Node<FlowData, 'graph'>

function FlowNodeView({ id, data }: NodeProps<FlowNode>) {
  const shared = useContext(SharedContext)!
  return <NodeView node={shared.graph.nodes.get(id)!} context={data.context} blocks={data.blocks} closed={data.closed} graphNode width={data.width} {...shared} />
}

const nodeTypes = { graph: FlowNodeView }

// nodeWidth is a node's width in LR; in TB the nodes take the region's width.
const nodeWidth = 288
// pad is the space between the region's border and the graph.
const pad = 12
// band is the strip below the graph that holds the zoom controls in LR.
const band = 56
const fitOptions = { padding: { top: pad, left: pad, right: pad, bottom: band }, maxZoom: 1 }
// readableZoom is the smallest zoom a graph opens at.
const readableZoom = 0.75
// lrMinWidth is the narrowest region where two levels fit side by side.
const lrMinWidth = 2 * nodeWidth + graphSpacing.rank + 2 * pad
const ariaLabelConfig = {
  'node.a11yDescription.default': 'Tab moves to the links and buttons of this Issue.',
  'node.a11yDescription.keyboardDisabled': 'Tab moves to the links and buttons of this Issue.',
  'controls.ariaLabel': 'Graph zoom controls',
  'controls.zoomIn.ariaLabel': 'Zoom in',
  'controls.zoomOut.ariaLabel': 'Zoom out',
  'controls.fitView.ariaLabel': 'Fit the graph',
  'minimap.ariaLabel': 'Overview of the graph',
}

// GraphRegion draws the full nodes and their edges on a React Flow canvas.
// It renders the nodes, lets React Flow measure them, places them with the
// measured sizes and fits the view. Wide regions run left to right and can
// be zoomed and panned; narrow ones run top to bottom at full size and grow
// with the page, so a phone scrolls the page instead of the graph.
function GraphRegion({
  keys,
  edges,
  context,
  blocks,
  groups,
}: {
  keys: string[]
  edges: GraphEdge[]
  context: Set<string>
  blocks: Map<string, string[]>
  groups: Map<string, GraphNode[]>
}) {
  const rf = useReactFlow<FlowNode>()
  const { graph } = useContext(SharedContext)!
  const marker = `${useId().replace(/[^a-zA-Z0-9_-]/g, '')}-arrow`
  // The region is state, not a ref: its width is observed in a layout
  // effect, which runs before a parent's ref is attached.
  const [region, setRegion] = useState<HTMLDivElement | null>(null)
  const [regionWidth, setRegionWidth] = useState(0)
  useLayoutEffect(() => {
    if (!region) return
    const measure = () => setRegionWidth(region.clientWidth)
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(region)
    return () => ro.disconnect()
  }, [region])
  const direction: Direction = regionWidth === 0 || regionWidth >= lrMinWidth ? 'LR' : 'TB'
  const width = direction === 'LR' ? nodeWidth : Math.max(160, regionWidth - graphSpacing.gutter - 2 * pad)

  // sizes holds React Flow's measurements; it batches them to one frame.
  const [sizes, setSizes] = useState<ReadonlyMap<string, Size>>(() => new Map())
  const onNodesChange = useCallback((changes: NodeChangeset<FlowNode>) => {
    setSizes((prev) => {
      let next: Map<string, Size> | null = null
      for (const c of changes.getByType('dimensions')) {
        if (!c.dimensions) continue
        const old = (next ?? prev).get(c.id)
        if (old && old.width === c.dimensions.width && old.height === c.dimensions.height) continue
        next ??= new Map(prev)
        next.set(c.id, { width: c.dimensions.width, height: c.dimensions.height })
      }
      return next ?? prev
    })
  }, [])
  const measured = keys.every((k) => sizes.has(k))
  const placement = useMemo(() => (measured ? positionGraph(keys, edges, sizes, direction) : null), [measured, keys, edges, sizes, direction])

  const nodes = useMemo<FlowNode[]>(
    () =>
      (placement?.order ?? keys).map((k) => {
        const box = placement?.boxes.get(k)
        const size = sizes.get(k)
        return {
          id: k,
          type: 'graph',
          position: box ? { x: box.x, y: box.y } : { x: 0, y: 0 },
          data: { context: context.has(k), blocks: blocks.get(k) ?? [], closed: groups.get(k) ?? [], width },
          ...(size ? { measured: size } : {}),
          ariaLabel: nodeName(graph, graph.nodes.get(k)!, context.has(k), groups.get(k)?.length ?? 0),
          draggable: false,
          selectable: false,
          deletable: false,
          connectable: false,
        }
      }),
    [placement, keys, sizes, context, blocks, groups, width, graph],
  )

  // Fit once per shown set and direction: later size changes, such as runs
  // read in, move nodes without taking the operator's zoom and pan away.
  const fitted = useRef('')
  useEffect(() => {
    if (!placement) return
    if (direction === 'TB') {
      void rf.setViewport({ x: pad, y: pad, zoom: 1 })
      fitted.current = ''
      return
    }
    const key = `${direction}|${keys.join(' ')}`
    if (fitted.current === key || !region) return
    fitted.current = key
    // A graph that fits only below a readable zoom opens at that zoom on
    // its first blocker, top left; Fit and the minimap show all of it.
    const fit = Math.min(1, (region.clientWidth - 2 * pad) / placement.width, (region.clientHeight - pad - band) / placement.height)
    const first = placement.boxes.get(placement.order[0])
    if (fit >= readableZoom || !first) void rf.fitView({ ...fitOptions, minZoom: readableZoom })
    else void rf.setViewport({ x: pad - first.x * readableZoom, y: pad - first.y * readableZoom, zoom: readableZoom })
  }, [placement, direction, keys, rf, region])

  const lr = direction === 'LR'
  // Keyboard focus on a node, or on a link or button inside one, pans the
  // view so the focused element is in sight. React Flow's own pan on node
  // focus covers only the node itself, and only with its keyboard handling,
  // which would also take Enter and Space from the controls inside.
  const onFocus = useCallback(
    (e: FocusEvent<HTMLDivElement>) => {
      const target = e.target as HTMLElement
      if (!lr || !region || !target.closest('.react-flow__node')) return
      requestAnimationFrame(() => {
        const r = region.getBoundingClientRect()
        const t = target.getBoundingClientRect()
        const view = { left: r.left + pad, top: r.top + pad, right: r.right - pad, bottom: r.bottom - band }
        if (t.left >= view.left && t.top >= view.top && t.right <= view.right && t.bottom <= view.bottom) return
        // Bring the element's top left into view, centred when it fits.
        const dx = t.width < view.right - view.left ? (view.left + view.right) / 2 - (t.left + t.right) / 2 : view.left - t.left
        const dy = t.height < view.bottom - view.top ? (view.top + view.bottom) / 2 - (t.top + t.bottom) / 2 : view.top - t.top
        const { x, y, zoom } = rf.getViewport()
        void rf.setViewport({ x: x + dx, y: y + dy, zoom }).then(() => target.scrollIntoView({ block: 'nearest', inline: 'nearest' }))
      })
    },
    [lr, region, rf],
  )
  const content = placement ? placement.height + (lr ? pad + band : 2 * pad) : 320
  // The minimap helps only when the whole graph does not fit at a readable
  // zoom, so the view opens on part of it.
  const large =
    placement !== null &&
    Math.min((regionWidth - 2 * pad) / placement.width, (0.75 * window.innerHeight - pad - band) / placement.height) < readableZoom
  const cycle = placement?.edges.some((e) => e.cycle) ?? false
  return (
    <div className="flex flex-col gap-2">
      <p className="text-sm text-muted-foreground" data-slot="legend" data-direction={direction}>
        {lr ? 'Blockers stand left of the Issues they block' : 'Blockers stand above the Issues they block'}; arrows point from an Issue to what blocks it
        {cycle && '; dashed arrows close a cycle'}. A grey “closed” marker on an Issue lists its closed blockers.
        {lr && ' Drag to pan; zoom with the buttons or Ctrl or ⌘ and the scroll wheel.'}
      </p>
      <div
        ref={setRegion}
        onFocus={onFocus}
        role="region"
        aria-label="Dependency graph"
        tabIndex={0}
        data-slot="graph-region"
        data-direction={direction}
        style={{ height: lr ? `min(75vh, ${Math.max(content, 240)}px)` : `${content}px` }}
        className={cn(
          'nf-graph relative w-full overflow-hidden rounded-lg border border-border-subtle bg-background outline-none focus-visible:outline-3 focus-visible:outline-offset-2 focus-visible:outline-ring',
          !lr && 'nf-graph-static',
        )}
      >
        <ReactFlow<FlowNode>
          nodes={nodes}
          nodeTypes={nodeTypes}
          onNodesChange={onNodesChange}
          nodesDraggable={false}
          nodesConnectable={false}
          elementsSelectable={false}
          nodesFocusable
          edgesFocusable={false}
          disableKeyboardA11y
          autoPanOnNodeFocus={false}
          panOnDrag={lr}
          zoomOnScroll={false}
          panOnScroll={false}
          zoomOnPinch={lr}
          zoomOnDoubleClick={false}
          zoomActivationKeyCode={lr ? undefined : null}
          preventScrolling={false}
          minZoom={0.05}
          maxZoom={1.5}
          proOptions={{ hideAttribution: true }}
          ariaLabelConfig={ariaLabelConfig}
        >
          <ViewportPortal>
            {placement && (
              <svg
                aria-hidden="true"
                width={Math.max(placement.width, 1)}
                height={Math.max(placement.height, 1)}
                className="pointer-events-none absolute top-0 left-0 overflow-visible"
                data-slot="edges"
              >
                <defs>
                  <marker id={marker} viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
                    <path d="M 0 0 L 10 5 L 0 10 z" className="fill-muted-foreground" />
                  </marker>
                </defs>
                {placement.edges.map((e) => (
                  <path
                    key={e.id}
                    d={edgePath(placement, e)}
                    data-edge={e.id}
                    data-cycle={e.cycle ? 'true' : undefined}
                    className={cn('fill-none', e.cycle ? 'stroke-warning' : 'stroke-muted-foreground')}
                    strokeWidth={1.5}
                    strokeDasharray={e.cycle ? '6 4' : undefined}
                    markerEnd={`url(#${marker})`}
                  />
                ))}
              </svg>
            )}
          </ViewportPortal>
          {lr && <Controls showInteractive={false} orientation="horizontal" fitViewOptions={fitOptions} />}
          {lr && large && <MiniMap pannable ariaLabel="Overview of the graph" nodeBorderRadius={8} />}
        </ReactFlow>
      </div>
    </div>
  )
}
