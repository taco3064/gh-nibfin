import { useId, useLayoutEffect, useMemo, useState, type ReactElement } from 'react'
import { Check, Search, X } from 'lucide-react'

import { BlockerItem, IssueItem } from '@/components/issue'
import { Note } from '@/components/shell/note'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Toggle } from '@/components/ui/toggle'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import {
  buildGraph,
  emptyFilter,
  filterGraph,
  isEmptyFilter,
  layoutGraph,
  nodeLabel,
  type Graph,
  type GraphFilter,
  type GraphNode,
  type LayoutEdge,
} from '@/lib/graph'
import { stageLabels, type Change, type Stage } from '@/lib/issue'
import type { RepoPermission, RepoView } from '@/lib/status'
import { cn } from '@/lib/utils'

export interface DependencyGraphProps {
  data: RepoView
  onChanged: (changes: Change[]) => void
}

const stages = Object.keys(stageLabels) as Stage[]

// joinLabels names keys as "#1, #2 and #3".
function joinLabels(labels: readonly string[]): string {
  if (labels.length <= 1) return labels.join('')
  return `${labels.slice(0, -1).join(', ')} and ${labels[labels.length - 1]}`
}

// DependencyGraph shows the repository's open Issues as a layered graph:
// blockers stand left of their dependents and arrows point from an Issue to
// what blocks it. Search and filters hide Issues but keep every blocker of
// a shown one visible, labelled as context.
export function DependencyGraph({ data, onChanged }: DependencyGraphProps) {
  const graph = useMemo(() => buildGraph(data), [data])
  const [filter, setFilter] = useState<GraphFilter>(emptyFilter)
  const filtered = useMemo(() => filterGraph(graph, filter), [graph, filter])
  const context = useMemo(() => new Set(filtered.context), [filtered])
  const layout = useMemo(() => {
    const visible = new Set([...filtered.matched, ...filtered.context])
    return layoutGraph([...graph.nodes.keys()].filter((k) => visible.has(k)), filtered.edges)
  }, [graph, filtered])
  // blocks lists, per blocker, the shown Issues it blocks.
  const blocks = useMemo(() => {
    const m = new Map<string, string[]>()
    for (const e of layout.edges) m.set(e.to, [...(m.get(e.to) ?? []), nodeLabel(graph, graph.nodes.get(e.from)!)])
    return m
  }, [graph, layout])
  const access: RepoPermission = { permission: data.permission, ...(data.permissionError ? { permissionError: data.permissionError } : {}) }
  const total = data.issues?.length ?? 0
  const label = (k: string) => nodeLabel(graph, graph.nodes.get(k)!)

  const renderNode = (k: string, stacked: boolean) => (
    <NodeView
      key={k}
      node={graph.nodes.get(k)!}
      graph={graph}
      context={context.has(k)}
      blocks={blocks.get(k) ?? []}
      repo={graph.repo}
      access={access}
      stacked={stacked}
      onChanged={onChanged}
    />
  )

  return (
    <div className="flex flex-col gap-4" data-slot="dependency-graph">
      <div className="flex flex-col gap-1">
        <h2 className="text-lg font-semibold tracking-tight">
          Dependency graph
        </h2>
        <p className="text-sm text-muted-foreground">Open Issues and what blocks them. Stages come from labels only.</p>
      </div>
      {data.issuesError && (
        <Note tone="danger" data-note="issues-error">
          Issues could not be read: {data.issuesError.code}: {data.issuesError.message}
        </Note>
      )}
      {total === 0 && !data.issuesError && (
        <p className="rounded-md border border-dashed border-border-subtle px-4 py-8 text-center text-sm text-muted-foreground" data-empty="issues">
          This repository has no open Issues.
        </p>
      )}
      {total > 0 && (
        <>
          <Toolbar
            filter={filter}
            onFilter={setFilter}
            count={`Showing ${filtered.matched.length} of ${total} open Issues · ${filtered.context.length} shown as context`}
          />
          {graph.unread.length > 0 && (
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
          {layout.columns.length > 0 && <GraphRegion layout={layout} renderNode={renderNode} />}
          {layout.unlinked.length > 0 && (
            <div className="flex flex-col gap-3" data-slot="unlinked">
              <h3 className="text-base font-semibold">Issues without dependencies</h3>
              <ul className="flex flex-col gap-3">{layout.unlinked.map((k) => renderNode(k, false))}</ul>
            </div>
          )}
        </>
      )}
    </div>
  )
}

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
  repo,
  access,
  stacked,
  onChanged,
}: {
  node: GraphNode
  graph: Graph
  context: boolean
  blocks: string[]
  repo: string
  access: RepoPermission
  stacked: boolean
  onChanged: (changes: Change[]) => void
}) {
  return (
    <li
      data-node={node.key}
      data-kind={node.kind}
      data-context={context ? 'true' : undefined}
      className={cn('relative flex flex-col gap-1', context && 'rounded-xl border border-dashed border-muted-foreground p-1.5')}
    >
      {context && <p className="px-1 text-xs font-medium text-muted-foreground">Context: blocks a shown Issue</p>}
      {node.kind === 'issue' ? (
        <IssueItem issue={node.issue} repo={repo} access={access} onChanged={onChanged} stacked={stacked} />
      ) : (
        <BlockerItem blocker={node.blocker} blocks={blocks} />
      )}
      {node.kind === 'issue' && blocks.length > 0 && (
        <p className="px-1 text-sm break-words" data-slot="blocks">
          <span className="text-muted-foreground">Blocks</span> {blocks.join(', ')}
        </p>
      )}
      <span className="sr-only">{nodeLabel(graph, node)}</span>
    </li>
  )
}

function GraphRegion({ layout, renderNode }: { layout: ReturnType<typeof layoutGraph>; renderNode: (k: string, stacked: boolean) => ReactElement }) {
  // The wrapper is state, not a ref: Edges measures it in a layout effect,
  // which runs before a parent's ref is attached.
  const [wrapper, setWrapper] = useState<HTMLDivElement | null>(null)
  return (
    <div className="flex flex-col gap-2">
      <p className="text-sm text-muted-foreground" data-slot="legend">
        Arrows point from an Issue to what blocks it; dashed: closed blocker.
      </p>
      <div
        role="region"
        aria-label="Dependency graph"
        tabIndex={0}
        className="overflow-x-auto rounded-lg border border-border-subtle bg-background p-3 outline-none focus-visible:outline-3 focus-visible:outline-offset-2 focus-visible:outline-ring sm:p-4"
        data-slot="graph-region"
      >
        <div ref={setWrapper} className="relative w-max">
          <Edges wrapper={wrapper} edges={layout.edges} />
          <div className="relative flex items-start gap-16">
            {layout.columns.map((col, r) => (
              <div key={r} className="flex w-[min(20rem,calc(100vw-5rem))] flex-col gap-3" data-column={r}>
                <h3 className="text-sm font-semibold">
                  Level {r + 1}
                  <span className="block text-xs font-normal text-muted-foreground">
                    {r === 0 ? 'Not blocked by a shown Issue' : 'Blocked by Issues on the left'}
                  </span>
                </h3>
                <ul className="flex flex-col gap-3">{col.map((k) => renderNode(k, true))}</ul>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}

interface Path {
  d: string
  edge: LayoutEdge
}

// Edges draws the edges over the measured node boxes. It measures after the
// shown graph changes and again, batched to one frame, when a box resizes.
function Edges({ wrapper, edges }: { wrapper: HTMLDivElement | null; edges: LayoutEdge[] }) {
  const marker = `${useId().replace(/[^a-zA-Z0-9_-]/g, '')}-arrow`
  const [geo, setGeo] = useState<{ w: number; h: number; paths: Path[] }>({ w: 0, h: 0, paths: [] })
  useLayoutEffect(() => {
    const el = wrapper
    if (!el) return
    let frame = 0
    const measure = () => {
      frame = 0
      const base = el.getBoundingClientRect()
      const boxes = new Map<string, { l: number; r: number; y: number }>()
      for (const n of el.querySelectorAll<HTMLElement>('[data-node]')) {
        const b = n.getBoundingClientRect()
        boxes.set(n.dataset.node!, { l: b.left - base.left, r: b.right - base.left, y: b.top - base.top + Math.min(b.height / 2, 32) })
      }
      const paths: Path[] = []
      for (const e of edges) {
        const a = boxes.get(e.from)
        const b = boxes.get(e.to)
        if (!a || !b) continue
        const x1 = a.l
        const x2 = b.r + 6
        const dx = Math.max(24, Math.abs(x1 - x2) / 2)
        paths.push({ edge: e, d: `M ${x1} ${a.y} C ${x1 - dx} ${a.y}, ${x2 + dx} ${b.y}, ${x2} ${b.y}` })
      }
      setGeo({ w: el.scrollWidth, h: el.scrollHeight, paths })
    }
    measure()
    const ro = new ResizeObserver(() => {
      if (frame === 0) frame = requestAnimationFrame(measure)
    })
    ro.observe(el)
    for (const n of el.querySelectorAll('[data-node]')) ro.observe(n)
    return () => {
      ro.disconnect()
      if (frame !== 0) cancelAnimationFrame(frame)
    }
  }, [wrapper, edges])
  return (
    <svg aria-hidden="true" width={geo.w} height={geo.h} className="pointer-events-none absolute top-0 left-0 overflow-visible" data-slot="edges">
      <defs>
        <marker id={marker} viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
          <path d="M 0 0 L 10 5 L 0 10 z" className="fill-muted-foreground" />
        </marker>
      </defs>
      {geo.paths.map((p) => (
        <path
          key={`${p.edge.from}>${p.edge.to}`}
          d={p.d}
          data-edge={`${p.edge.from}>${p.edge.to}`}
          data-closed={p.edge.closed ? 'true' : undefined}
          data-cycle={p.edge.cycle ? 'true' : undefined}
          className="fill-none stroke-muted-foreground"
          strokeWidth={1.5}
          strokeDasharray={p.edge.closed ? '6 4' : undefined}
          markerEnd={`url(#${marker})`}
        />
      ))}
    </svg>
  )
}
