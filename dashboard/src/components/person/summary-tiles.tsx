import { useId, type ReactNode } from 'react'
import {
  CircleAlert,
  CircleCheck,
  CircleDashed,
  CircleDot,
  CircleHelp,
  CircleX,
  Hand,
  History,
  ListChecks,
  LoaderCircle,
  PencilRuler,
  Rocket,
  Server,
  type LucideIcon,
} from 'lucide-react'

import { SectionLoading } from '@/components/shell/section-loading'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Toggle } from '@/components/ui/toggle'
import { runBucketLabels } from '@/lib/issue'
import { useLoading } from '@/lib/loading-context'
import { needsYouCount, repairingCount, runCounts, runnerCounts, stageCounts } from '@/lib/person'
import { gib } from '@/lib/runner'
import { hasNoRunner } from '@/lib/shell'
import type { ApiError, PersonView } from '@/lib/status'

import { StackedBar } from './stacked-bar'

export interface SummaryTilesProps {
  // data is the unfiltered read; the counts never follow the filter.
  data: PersonView
  needsYouOnly: boolean
  onNeedsYouOnly: (on: boolean) => void
  // onShowRunners opens the Runner view; without it the Runners tile only
  // says that this host has no Runner.
  onShowRunners?: () => void
}

// SummaryTiles heads the person view: the listed Issues by stage, those that
// need the person, their runs' conclusions and this host's Runners. Each
// tile shows its own loading or error state.
export function SummaryTiles({ data, needsYouOnly, onNeedsYouOnly, onShowRunners }: SummaryTilesProps) {
  return (
    <div role="group" aria-label="Summary" className="grid grid-cols-2 gap-3 lg:grid-cols-4" data-slot="summary">
      <IssuesTile data={data} />
      <NeedsYouTile data={data} on={needsYouOnly} onChange={onNeedsYouOnly} />
      <RunsTile data={data} />
      <RunnersTile data={data} onShowRunners={onShowRunners} />
    </div>
  )
}

function Tile({ title, icon: Icon, tile, children }: { title: string; icon: LucideIcon; tile: string; children: ReactNode }) {
  const id = useId()
  return (
    <Card aria-labelledby={id} role="region" className="min-w-0 gap-3 border-border-subtle py-4" data-tile={tile}>
      <CardHeader className="px-3 sm:px-4">
        <h3 id={id} className="flex min-w-0 items-center gap-1.5 text-sm font-medium text-muted-foreground">
          <Icon aria-hidden="true" className="size-4 shrink-0" />
          <span className="min-w-0 break-words">{title}</span>
        </h3>
      </CardHeader>
      <CardContent className="flex min-w-0 flex-col gap-2 px-3 sm:px-4">{children}</CardContent>
    </Card>
  )
}

function Count({ value, children }: { value: number | string; children?: ReactNode }) {
  return (
    <p className="flex min-w-0 flex-wrap items-baseline gap-x-1.5 break-words" data-slot="tile-count">
      <span className="font-heading text-2xl font-semibold tabular-nums">{value}</span>
      {children && <span className="text-sm text-muted-foreground">{children}</span>}
    </p>
  )
}

const errorText = (e: ApiError) => `${e.code}: ${e.message}`

// TileError is a tile's failed read: a compact alert line with its code.
function TileError({ what, error }: { what: string; error: ApiError }) {
  return (
    <p role="alert" className="flex min-w-0 items-start gap-1.5 text-xs text-danger" data-note="tile-error">
      <CircleAlert aria-hidden="true" className="mt-px size-3.5 shrink-0" />
      <span className="min-w-0 break-words">
        {what} could not be read: {errorText(error)}
      </span>
    </p>
  )
}

function IssuesError({ error }: { error: ApiError }) {
  return <TileError what="Issues" error={error} />
}

function IssuesTile({ data }: { data: PersonView }) {
  const loading = useLoading()
  const c = stageCounts(data)
  return (
    <Tile title="Open Issues" icon={ListChecks} tile="issues">
      {loading.has('repos') ? (
        <SectionLoading label="Reading Issues…" data-loading="repos" />
      ) : data.issuesError ? (
        <IssuesError error={data.issuesError} />
      ) : (
        <>
          <Count value={c.total}>{c.total === 1 ? 'Issue' : 'Issues'}</Count>
          <StackedBar
            label="Open Issues by stage"
            segments={[
              { key: 'shape', label: 'Shape', count: c.shape, className: 'bg-info', icon: PencilRuler },
              { key: 'deliver', label: 'Deliver', count: c.deliver, className: 'bg-primary', icon: Rocket },
              ...(c.other > 0 ? [{ key: 'other', label: 'Other', count: c.other, className: 'bg-muted-foreground', icon: CircleDashed }] : []),
            ]}
          />
          {data.truncated && <p className="text-xs text-warning" data-note="tile-truncated">The list may be incomplete.</p>}
        </>
      )}
    </Tile>
  )
}

function NeedsYouTile({ data, on, onChange }: { data: PersonView; on: boolean; onChange: (on: boolean) => void }) {
  const loading = useLoading()
  const n = needsYouCount(data)
  const fixing = repairingCount(data)
  const idle = n === 0 && !on
  return (
    <Tile title="Needs you" icon={Hand} tile="needs-you">
      {loading.has('repos') ? (
        <SectionLoading label="Reading Issues…" data-loading="repos" />
      ) : data.issuesError ? (
        <IssuesError error={data.issuesError} />
      ) : (
        <>
          <Count value={n}>pending or paused</Count>
          {fixing > 0 && (
            <p className="text-xs text-muted-foreground" data-note="repairing">
              {`${fixing} repairing not counted: Nibfin is repairing ${fixing === 1 ? 'it' : 'them'} itself.`}
            </p>
          )}
          <Toggle
            variant="outline"
            size="sm"
            pressed={on}
            onPressedChange={onChange}
            disabled={idle}
            className="h-auto min-h-8 w-full min-w-0 justify-center py-1 whitespace-normal"
            data-action="needs-you"
          >
            {on ? 'Showing only these' : 'Show only these'}
          </Toggle>
          {idle && (
            <p className="text-xs text-muted-foreground" data-slot="disabled-reason">
              No Issue waits on you.
            </p>
          )}
        </>
      )}
    </Tile>
  )
}

function RunsTile({ data }: { data: PersonView }) {
  const loading = useLoading()
  const c = runCounts(data)
  return (
    <Tile title="Runs" icon={History} tile="runs">
      {loading.has('repos') || loading.has('runs') ? (
        <SectionLoading label="Reading runs…" data-loading="runs" />
      ) : data.issuesError ? (
        <IssuesError error={data.issuesError} />
      ) : (
        <>
          <Count value={c.total}>{c.total === 1 ? 'run' : 'runs'}</Count>
          {c.total === 0 && !c.incomplete ? (
            <p className="text-sm text-muted-foreground" data-state="no-runs">
              No runs
            </p>
          ) : (
            <StackedBar
              label="Runs by conclusion"
              segments={[
                { key: 'succeeded', label: 'Succeeded', count: c.succeeded, className: 'bg-success', icon: CircleCheck },
                { key: 'failed', label: 'Failed', count: c.failed, className: 'bg-danger', icon: CircleX },
                { key: 'running', label: 'Running', count: c.running, className: 'bg-info', icon: LoaderCircle },
                ...(c.skipped > 0 ? [{ key: 'skipped', label: runBucketLabels.skipped, count: c.skipped, className: 'bg-muted-foreground', icon: CircleHelp }] : []),
              ]}
            />
          )}
          {c.incomplete && (
            <p className="text-xs text-warning" data-note="runs-incomplete">
              Some runs were not read or were cut short; the counts are a lower bound.
            </p>
          )}
        </>
      )}
    </Tile>
  )
}

const absentCodes = ['podman_missing', 'machine_missing']

function RunnersTile({ data, onShowRunners }: { data: PersonView; onShowRunners?: () => void }) {
  const loading = useLoading()
  if (loading.has('host')) {
    return (
      <Tile title="Runners" icon={Server} tile="runners">
        <SectionLoading label="Reading the host…" data-loading="host" />
      </Tile>
    )
  }
  const host = data.host
  if (hasNoRunner(host)) {
    return (
      <Tile title="Runners" icon={Server} tile="runners">
        <p className="text-sm" data-state="no-runner">
          No Runner on this host
        </p>
        {onShowRunners && (
          <Button variant="link" size="sm" className="h-auto justify-start p-0 whitespace-normal" onClick={onShowRunners} data-action="show-runners">
            Set up in Runners
          </Button>
        )}
      </Tile>
    )
  }
  const c = runnerCounts(host)
  const r = host.runners
  const m = host.memory
  const used = c.usedKB !== null && c.totalKB ? Math.round((c.usedKB / c.totalKB) * 100) : null
  return (
    <Tile title="Runners" icon={Server} tile="runners">
      {r.error && !absentCodes.includes(r.error.code) ? (
        <TileError what="Runners" error={r.error} />
      ) : r.githubError ? (
        <p className="text-sm break-words text-warning" data-state="github-not-read">
          Online and busy not read: {errorText(r.githubError)}
        </p>
      ) : c.github === 'needs-admin' ? (
        <p className="text-sm break-words text-muted-foreground" data-state="github-needs-admin">
          Online and busy not read: GitHub&apos;s Runner list needs admin of {r.repo ?? 'the repository'}.
        </p>
      ) : (
        <p className="flex flex-wrap items-baseline gap-x-1.5 text-sm text-muted-foreground" data-slot="runner-counts">
          <CircleDot aria-hidden="true" className="size-3.5 self-center text-success" />
          <span className="font-heading text-2xl font-semibold text-foreground tabular-nums">{c.online}</span> online ·{' '}
          <span className="font-semibold text-foreground tabular-nums">{c.busy}</span> busy
        </p>
      )}
      {m.error ? (
        <TileError what="Memory" error={m.error} />
      ) : c.usedKB !== null && c.totalKB !== null && used !== null ? (
        <div className="flex flex-col gap-1.5" data-slot="memory-used">
          <p className="text-xs text-muted-foreground">
            Memory <span className="font-semibold text-foreground tabular-nums">{gib(c.usedKB)}</span> of {gib(c.totalKB)} in use
          </p>
          <div
            role="meter"
            aria-label="Memory in use"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={used}
            aria-valuetext={`${used}% in use`}
            className="h-2 w-full overflow-hidden rounded-full border border-border bg-muted"
          >
            <div className="h-full rounded-full bg-primary" style={{ width: `${used}%` }} />
          </div>
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">Memory not read</p>
      )}
    </Tile>
  )
}
