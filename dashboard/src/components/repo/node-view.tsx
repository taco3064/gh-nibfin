import { CircleCheck } from 'lucide-react'

import { BlockerItem, IssueItem } from '@/components/issue'
import { ExternalMark } from '@/components/issue/external-mark'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { groupLabel, nodeLabel, type Graph, type GraphNode } from '@/lib/graph'
import { stageOf, type Change } from '@/lib/issue'
import type { RepoPermission } from '@/lib/status'
import { cn } from '@/lib/utils'

// Shared is what every node of the repository view shares.
export interface Shared {
  graph: Graph
  access: RepoPermission
  onChanged: (changes: Change[]) => void
}

export type NodeViewProps = Shared & {
  node: GraphNode
  context: boolean
  blocks: string[]
  closed: GraphNode[]
  // graphNode draws the node on the canvas: stacked, with its closed
  // blockers' marker; its stage track names the stage. Otherwise it is a
  // list entry.
  graphNode: boolean
  width?: number
}

// NodeView is one node of the repository view: a graph node on the canvas
// or a list entry below it, around the shared Issue element.
export function NodeView({ node, graph, context, blocks, closed, access, graphNode, width, onChanged }: NodeViewProps) {
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
          <IssueItem issue={node.issue} repo={graph.repo} access={access} onChanged={onChanged} stacked />
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
