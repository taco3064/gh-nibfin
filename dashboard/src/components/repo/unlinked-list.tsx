import { useRef, useState, type FocusEvent, type ReactNode } from 'react'
import { ChevronRight } from 'lucide-react'

import { ExternalMark } from '@/components/issue/external-mark'
import { tones } from '@/components/issue/tones'
import { Badge } from '@/components/ui/badge'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Popover, PopoverAnchor, PopoverContent } from '@/components/ui/popover'
import { heldFlags, unlinkedGroups, type GraphNode, type UnlinkedGroupId } from '@/lib/graph'
import { runBucket, runBucketLabels, runsView, stageLabels, stageOf, type RunBucket } from '@/lib/issue'
import type { StatusIssue } from '@/lib/status'
import { cn } from '@/lib/utils'

import { NodeView, type Shared } from './node-view'

// compactAbove is the longest list shown expanded with full Issue items;
// a longer one starts with its groups collapsed and compact rows.
const compactAbove = 10

export type UnlinkedListProps = Shared & {
  // keys are the shown Issues without dependencies.
  keys: string[]
  // query is the search text: while it is not empty every shown group
  // opens, and a change of it restores the default.
  query: string
  context: ReadonlySet<string>
  blocks: ReadonlyMap<string, string[]>
}

const focusRing = 'outline-none focus-visible:outline-3 focus-visible:outline-offset-2 focus-visible:outline-solid focus-visible:outline-ring'

function issuesCount(n: number): string {
  return `${n} ${n === 1 ? 'Issue' : 'Issues'}`
}

// UnlinkedList is the repository view's list of Issues without dependencies
// (#496), grouped by stage with Paused or Pending last. A long list starts
// collapsed with compact rows; a short one, or one narrowed by a search,
// shows its groups open.
export function UnlinkedList({ keys, query, context, blocks, ...shared }: UnlinkedListProps) {
  const groups = unlinkedGroups(shared.graph, keys)
  const compact = keys.length > compactAbove
  const searching = query.trim() !== ''
  const defaultOpen = !compact || searching
  // The operator's choices last until the search text changes.
  const [choice, setChoice] = useState<{ query: string; open: Partial<Record<UnlinkedGroupId, boolean>> }>({ query, open: {} })
  const chosen = choice.query === query ? choice.open : {}

  const item = (k: string) => (
    <NodeView node={shared.graph.nodes.get(k)!} context={context.has(k)} blocks={blocks.get(k) ?? []} closed={[]} graphNode={false} {...shared} />
  )

  return (
    <div className="flex flex-col gap-3" data-slot="unlinked">
      <h3 className="text-base font-semibold">Issues without dependencies</h3>
      <div className="flex flex-col gap-1.5">
        {groups.map((g) => (
          <Collapsible
            key={g.id}
            open={chosen[g.id] ?? defaultOpen}
            onOpenChange={(v) => setChoice({ query, open: { ...chosen, [g.id]: v } })}
            data-unlinked-group={g.id}
            className="flex flex-col gap-2"
          >
            <CollapsibleTrigger
              className={cn(
                'group flex w-full items-center gap-2 rounded-md border border-border bg-card px-3 py-2 text-left text-sm font-medium text-card-foreground hover:bg-muted',
                focusRing,
              )}
            >
              <ChevronRight
                aria-hidden="true"
                className="size-4 shrink-0 text-muted-foreground group-data-[state=open]:rotate-90 motion-safe:transition-transform"
              />
              <span className="min-w-0 break-words">
                {g.label} <span className="text-muted-foreground">· {issuesCount(g.keys.length)}</span>
              </span>
            </CollapsibleTrigger>
            <CollapsibleContent>
              <ul className={cn('flex flex-col', compact ? 'gap-2' : 'gap-3')}>
                {g.keys.map((k) =>
                  compact ? <UnlinkedRow key={k} k={k} node={shared.graph.nodes.get(k)!} held={g.id === 'held'} item={item(k)} /> : <li key={k}>{item(k)}</li>,
                )}
              </ul>
            </CollapsibleContent>
          </Collapsible>
        ))}
      </div>
    </div>
  )
}

const dotClass: Record<RunBucket, string> = {
  succeeded: 'bg-success',
  failed: 'bg-danger',
  skipped: 'bg-muted-foreground',
  running: 'bg-info',
}

// latestRun is the compact row's run state: the newest run's bucket, or why
// there is none to show.
function latestRun(issue: StatusIssue): { bucket?: RunBucket; text: string } {
  const v = runsView(issue)
  const newest = v.runs[0]
  if (newest) {
    const bucket = runBucket(newest)
    return { bucket, text: `Last run ${runBucketLabels[bucket].toLowerCase()}` }
  }
  if (v.notes.some((n) => n.kind === 'failed')) return { text: 'Runs could not be read' }
  if (v.notes.some((n) => n.kind === 'notRead')) return { text: 'Runs not read' }
  return { text: 'No runs' }
}

// UnlinkedRow is one compact row: number, title, stage and the latest run.
// Opening it shows the full Issue item under it; keyboard focus shows the
// same item as a floating, inert preview that leaves the list in place.
function UnlinkedRow({ k, node, held, item }: { k: string; node: GraphNode; held: boolean; item: ReactNode }) {
  const [open, setOpen] = useState(false)
  const [preview, setPreview] = useState(false)
  const anchor = useRef<HTMLDivElement>(null)
  if (node.kind !== 'issue') return <li data-unlinked-row={k}>{item}</li>
  const issue = node.issue
  const run = latestRun(issue)
  const stage = stageLabels[stageOf(issue.labels)]
  const flags = held ? heldFlags(issue) : []
  // The visible text in reading order, with separators the layout shows as
  // spacing.
  const name = [`#${issue.number} ${issue.title}`, `stage ${[stage, ...flags.map((f) => f.label)].join(' ')}`, run.text].join(', ')
  const onFocus = (e: FocusEvent<HTMLButtonElement>) => {
    if (e.currentTarget.matches(':focus-visible')) setPreview(true)
  }
  return (
    <li data-unlinked-row={k}>
      <Collapsible
        open={open}
        onOpenChange={(v) => {
          setOpen(v)
          if (v) setPreview(false)
        }}
        className="flex flex-col gap-2"
      >
        <Popover open={preview && !open} onOpenChange={(v) => !v && setPreview(false)}>
          <PopoverAnchor asChild>
            <div ref={anchor} className="flex items-start gap-1 rounded-md border border-border bg-card text-card-foreground has-[button:hover]:bg-muted">
              <CollapsibleTrigger
                onFocus={onFocus}
                onBlur={() => setPreview(false)}
                aria-label={name}
                className={cn('group flex min-w-0 flex-1 items-start gap-2 rounded-md px-3 py-2 text-left text-sm', focusRing)}
              >
                <ChevronRight
                  aria-hidden="true"
                  className="mt-0.5 size-4 shrink-0 text-muted-foreground group-data-[state=open]:rotate-90 motion-safe:transition-transform"
                />
                <span className="flex min-w-0 flex-1 flex-col gap-1 md:flex-row md:items-baseline md:justify-between md:gap-4">
                  <span className="min-w-0 break-words">
                    <span className="text-muted-foreground tabular-nums">#{issue.number}</span> <span className="font-medium">{issue.title}</span>
                  </span>
                  <span className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1 text-muted-foreground">
                    <span data-slot="row-stage">
                      {stage}
                      {flags.map((f) => (
                        <span key={f.key}>
                          {' '}
                          <Badge variant="outline" data-flag={f.key} className={cn(tones[f.tone], 'align-middle whitespace-normal')}>
                            {f.label}
                          </Badge>
                        </span>
                      ))}
                    </span>
                    <span data-slot="row-run" data-run={run.bucket} className="inline-flex items-center gap-1.5 whitespace-nowrap">
                      <span
                        aria-hidden="true"
                        className={cn('size-2 shrink-0 rounded-full', run.bucket ? dotClass[run.bucket] : 'border border-muted-foreground')}
                      />
                      {run.text}
                    </span>
                  </span>
                </span>
              </CollapsibleTrigger>
              <a href={issue.url} target="_blank" rel="noreferrer" className={cn('shrink-0 rounded-sm p-2.5 text-link hover:bg-muted', focusRing)}>
                <ExternalMark label={`#${issue.number} on GitHub (opens in a new tab)`} />
              </a>
            </div>
          </PopoverAnchor>
          <PopoverContent
            side="bottom"
            align="start"
            collisionPadding={16}
            onOpenAutoFocus={(e) => e.preventDefault()}
            onCloseAutoFocus={(e) => e.preventDefault()}
            // Focus on the row itself, such as the focus that opened the
            // preview, is not focus outside it.
            onFocusOutside={(e) => anchor.current?.contains(e.target as Node) && e.preventDefault()}
            aria-hidden="true"
            data-unlinked-preview={k}
            className="w-[min(28rem,calc(100vw-2rem))] rounded-lg border-0 bg-transparent p-0 shadow-lg"
          >
            <div inert>{item}</div>
          </PopoverContent>
        </Popover>
        <CollapsibleContent>{item}</CollapsibleContent>
      </Collapsible>
    </li>
  )
}
