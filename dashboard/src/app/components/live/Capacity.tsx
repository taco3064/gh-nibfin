import { CircleHelp, CirclePause, ExternalLink, Loader, WifiOff, type LucideIcon } from 'lucide-react'

import { tones, type Tone } from '@/components/issue/tones'
import { Note } from '@/components/shell/note'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

import type { RepoRunners } from '../../../worker/messages'
import { capacityCounts, jobTarget, runnerCards, runnerErrors, safeUrl, type RunnerCard, type RunnerState } from '../../lib/runners'
import { RunStageTrack } from './RunStageTrack'

const linkClass = 'rounded-sm text-link underline-offset-4 hover:underline'

const looks: Record<RunnerState, { tone: Tone; icon: LucideIcon; label: string }> = {
  busy: { tone: 'info', icon: Loader, label: 'Busy' },
  idle: { tone: 'neutral', icon: CirclePause, label: 'Idle' },
  offline: { tone: 'neutral', icon: WifiOff, label: 'Offline' },
  other: { tone: 'neutral', icon: CircleHelp, label: '' },
}

// Dot is the runner's activity mark: a pulsing ring while busy, a still
// hollow dot while idle, a grey crossed dot while offline. It is
// decorative; the badge beside it carries the state as text.
function Dot({ state }: { state: RunnerState }) {
  if (state === 'busy') {
    return (
      <span aria-hidden="true" className="relative flex size-3 shrink-0">
        <span className="absolute inline-flex size-full rounded-full bg-primary opacity-75 motion-safe:animate-ping" />
        <span className="relative inline-flex size-3 rounded-full bg-primary" />
      </span>
    )
  }
  return <span aria-hidden="true" className={cn('size-3 shrink-0 rounded-full border-2 border-muted-foreground', state === 'offline' && 'bg-muted-foreground')} />
}

function Card({ card }: { card: RunnerCard }) {
  const look = looks[card.state]
  const Icon = look.icon
  const busy = card.state === 'busy'
  const job = card.job
  const target = job ? jobTarget(job) : undefined
  const jobUrl = safeUrl(job?.url)
  return (
    <li
      data-slot="runner-card"
      data-runner-state={card.state}
      className={cn(
        'flex min-w-0 flex-col gap-3 overflow-hidden rounded-lg border p-4',
        busy ? 'border-border bg-card text-card-foreground' : 'border-border-subtle bg-background',
        card.state === 'offline' && 'border-dashed bg-muted',
      )}
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="flex items-center gap-2 font-medium">
          <Dot state={card.state} />
          {card.label}
        </span>
        <Badge variant="outline" className={tones[look.tone]} data-slot="runner-state">
          <Icon aria-hidden="true" />
          {look.label || card.status}
        </Badge>
      </div>
      {busy ? (
        <>
          <div aria-hidden="true" className="h-1 w-full overflow-hidden rounded-full bg-muted" data-slot="flow">
            <div className="h-full w-full bg-[linear-gradient(90deg,transparent,var(--nf-accent),transparent)] bg-size-[50%_100%] bg-center bg-no-repeat motion-safe:animate-flow" />
          </div>
          {job && target ? (
            <dl className="m-0 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
              <dt className="text-muted-foreground">{target.kind === 'issue' ? 'Issue' : target.kind === 'ref' ? 'Ref' : 'Run'}</dt>
              <dd className="m-0 min-w-0 break-all">
                {target.kind === 'issue' ? (
                  <a href={target.url} target="_blank" rel="noreferrer" className={linkClass} data-slot="runner-issue">
                    {target.label}
                    <span className="sr-only"> (opens on GitHub in a new tab)</span>
                  </a>
                ) : (
                  target.label
                )}
              </dd>
              {/* The stage track takes the card's full width, under its term. */}
              <dt className="col-span-2 mt-1 text-muted-foreground">Stage</dt>
              <dd className="col-span-2 m-0 mb-1 min-w-0" data-slot="runner-stage" data-stage={job.stage ?? ''}>
                <RunStageTrack stage={job.stage} />
              </dd>
              {jobUrl && (
                <>
                  <dt className="text-muted-foreground">Job</dt>
                  <dd className="m-0">
                    <a href={jobUrl} target="_blank" rel="noreferrer" className={linkClass} data-slot="runner-job">
                      Open job
                      <ExternalLink aria-hidden="true" className="ms-1 inline size-3.5 align-[-0.125em]" />
                      <span className="sr-only"> (opens on GitHub in a new tab)</span>
                    </a>
                  </dd>
                </>
              )}
            </dl>
          ) : (
            <p className="m-0 text-sm text-muted-foreground">Busy; its job could not be matched.</p>
          )}
        </>
      ) : (
        <p className="m-0 text-sm text-muted-foreground">
          {card.state === 'idle' ? 'Waiting for a job.' : card.state === 'offline' ? 'Not connected to GitHub.' : `GitHub reports “${card.status}”.`}
        </p>
      )}
    </li>
  )
}

// Capacity shows one card per runner of the Worker's repositories, with
// the counts per state. Runner names and host hardware are never shown.
export function Capacity({ runners }: { runners?: RepoRunners[] }) {
  if (!runners) return <p className="m-0 text-sm text-muted-foreground">Waiting for live data…</p>
  const cards = runnerCards(runners)
  const counts = capacityCounts(cards)
  const errors = runnerErrors(runners)
  const parts = [`${counts.total} ${counts.total === 1 ? 'runner' : 'runners'}`, `${counts.busy} busy`, `${counts.idle} idle`, `${counts.offline} offline`]
  if (counts.other > 0) parts.push(`${counts.other} other`)
  return (
    <div className="flex flex-col gap-3" data-slot="capacity">
      <div className="flex flex-col gap-1">
        <p className="m-0 text-sm font-medium" data-slot="capacity-counts">
          {parts.join(' · ')}
        </p>
        <p className="m-0 text-sm text-muted-foreground">More runners work more Issues in parallel.</p>
      </div>
      {errors.map((e) => (
        <Note tone="warning" key={e.repo} data-note="runners-error">
          Runners of {e.repo} could not be read: {e.error.code}: {e.error.message}
        </Note>
      ))}
      {cards.length === 0 ? (
        <p className="m-0 rounded-md border border-dashed border-border-subtle px-4 py-6 text-center text-sm text-muted-foreground" data-empty="runners">
          No runners are registered.
        </p>
      ) : (
        <ul className="m-0 grid list-none grid-cols-1 gap-3 p-0 sm:grid-cols-2 xl:grid-cols-3">
          {cards.map((c) => (
            <Card key={c.key} card={c} />
          ))}
        </ul>
      )}
    </div>
  )
}
