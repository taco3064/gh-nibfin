import { ExternalLink } from 'lucide-react'

import { tones } from '@/components/issue/tones'
import { Badge } from '@/components/ui/badge'
import { elapsed, githubLabel, jobLabel, serviceLabel, tileState, type Label, type TileState } from '@/lib/runner'
import type { ApiError, CurrentJob, RunnerState } from '@/lib/status'
import { cn } from '@/lib/utils'

const stateText: Record<TileState['state'], string> = { idle: 'Idle', busy: 'Busy', offline: 'Offline' }

// StateRing pictures a tile's state: a solid ring when idle, a primary ring
// with a slowly turning arc when busy (still under reduced motion), a dashed
// muted ring when offline. The tile's text names the state.
export function StateRing({ state }: { state: TileState['state'] }) {
  return (
    <svg viewBox="0 0 40 40" aria-hidden="true" data-ring={state} className="size-10 shrink-0">
      {state === 'offline' ? (
        <circle cx="20" cy="20" r="16" fill="none" strokeWidth="3" strokeDasharray="4 4" className="stroke-border" />
      ) : state === 'idle' ? (
        <>
          <circle cx="20" cy="20" r="16" fill="none" strokeWidth="3" className="stroke-success" />
          <circle cx="20" cy="20" r="5" className="fill-success" />
        </>
      ) : (
        <>
          <circle cx="20" cy="20" r="16" fill="none" strokeWidth="3" className="stroke-primary/35" />
          <g className="origin-center motion-safe:animate-[spin_4s_linear_infinite]">
            <circle cx="20" cy="20" r="16" fill="none" strokeWidth="3" strokeLinecap="round" pathLength={100} strokeDasharray="30 70" className="stroke-primary" />
          </g>
          <circle cx="20" cy="20" r="5" className="fill-primary" />
        </>
      )}
    </svg>
  )
}

function Small({ label, slot }: { label: Label; slot: string }) {
  return (
    <Badge variant="outline" data-badge={slot} className={cn('whitespace-normal text-[0.7rem] font-normal', tones[label.tone])}>
      {label.text}
    </Badge>
  )
}

export interface TileProps {
  slot: RunnerState
  current?: CurrentJob
  githubError?: ApiError
  needsAdmin: boolean
  // activityRead is false while the activity is unknown (loading or failed).
  activityRead: boolean
  now: number
}

// RunnerTile is one Runner: its state ring and state, and when busy the
// Issue, stage and how long the Job has run; the service and GitHub labels
// below as secondary detail.
export function RunnerTile({ slot: s, current, githubError, needsAdmin, activityRead, now }: TileProps) {
  const t = tileState(s, !githubError && !needsAdmin, current)
  const busy = t.state === 'busy'
  const took = elapsed(current?.startedAt, now)
  return (
    <li data-slot-id={s.slot} data-state={t.state} className="flex min-w-0 flex-col gap-3 rounded-lg border border-border-subtle bg-background/40 p-3">
      <div className="flex min-w-0 items-center gap-3">
        <StateRing state={t.state} />
        <div className="flex min-w-0 flex-col">
          <p className="min-w-0 break-words">
            <span className="font-medium">{s.slot}</span>
            {s.name && <span className="text-sm text-muted-foreground"> · {s.name}</span>}
          </p>
          <p data-slot="state" className="text-sm">
            <span className={cn('font-semibold', busy ? 'text-primary' : t.state === 'idle' ? 'text-success' : 'text-muted-foreground')}>{stateText[t.state]}</span>
            {!t.github && <span className="text-muted-foreground"> · local state</span>}
          </p>
        </div>
      </div>
      {busy && (
        <div data-slot="job" className="flex min-w-0 flex-col gap-1 rounded-md bg-muted/60 px-2.5 py-2 text-sm">
          {current ? (
            <>
              <p className="min-w-0 break-words">
                {current.issueUrl ? (
                  <a href={current.issueUrl} target="_blank" rel="noreferrer" className="font-medium text-link underline-offset-2 hover:underline">
                    {jobLabel(current)}
                    <ExternalLink aria-hidden="true" className="ml-1 inline size-3.5 align-[-2px]" />
                  </a>
                ) : (
                  <span className="text-muted-foreground">{jobLabel(current)}</span>
                )}
              </p>
              <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-muted-foreground">
                <span data-slot="stage" className={cn(current.stage && 'font-medium text-foreground')}>
                  {current.stage ?? 'stage unknown'}
                </span>
                {took && (
                  <>
                    <span aria-hidden="true">·</span>
                    <span data-slot="elapsed">
                      for <time dateTime={current.startedAt}>{took}</time>
                    </span>
                  </>
                )}
              </p>
            </>
          ) : (
            <p className="text-muted-foreground">{activityRead ? 'The Job is not one of the shared-runner runs.' : 'Job details not read.'}</p>
          )}
        </div>
      )}
      <div className="flex flex-wrap gap-1.5">
        <Small slot="service" label={serviceLabel(s)} />
        <Small slot="github" label={githubLabel(s, githubError, needsAdmin)} />
      </div>
    </li>
  )
}
