import { AlarmClock, Check } from 'lucide-react'

import { cn } from '@/lib/utils'

import { runStageSteps, type RunStepState } from '@/app/lib/runners'

const dotClass: Record<RunStepState, string> = {
  complete: 'border-success bg-success text-card',
  current: 'border-info bg-info-bg text-info ring-2 ring-info ring-offset-2 ring-offset-card',
  upcoming: 'border-muted-foreground bg-transparent',
}

const stateWords: Record<RunStepState, string> = {
  complete: ' (completed)',
  current: ' (current)',
  upcoming: '',
}

// RunStageTrack places a busy runner's job on the stages of an Issue's
// path, Scope → Shape → Deliver → Review → Finish: steps before its stage
// filled, its stage highlighted. A route job is waking: a "Waking" mark
// beside an all-upcoming track. Another stage keeps its text without a
// track. The current step and the waking mark pulse under motion-safe
// only; with reduced motion they are highlighted without a pulse. It is
// not the label-based Issue stage track.
export function RunStageTrack({ stage }: { stage: string | null }) {
  const track = runStageSteps(stage)
  if (track.kind === 'unknown') return <>{stage ?? 'unknown'}</>
  return (
    <div className="flex min-w-0 flex-col gap-2">
      {track.kind === 'waking' && (
        <span
          className="inline-flex items-center gap-1.5 self-start rounded-full border border-info px-2 py-0.5 text-xs font-medium text-info"
          data-slot="runner-waking"
        >
          <AlarmClock aria-hidden="true" className="size-3.5 motion-safe:animate-pulse" />
          Waking
          <span className="sr-only">: routing the Issue to its next stage</span>
        </span>
      )}
      <ol
        aria-label="Job stage"
        className="m-0 grid list-none grid-cols-5 p-0"
        data-slot="run-stage-track"
        data-stage={track.kind === 'stage' ? track.stage : 'route'}
      >
        {track.steps.map((s, i) => (
          <li
            key={s.stage}
            data-step={s.stage}
            data-state={s.state}
            aria-current={s.state === 'current' ? 'step' : undefined}
            className="relative flex min-w-0 flex-col items-center gap-1"
          >
            {i > 0 && (
              <span
                aria-hidden="true"
                className={cn(
                  'absolute top-[7px] right-1/2 h-0.5 w-[calc(100%-1.25rem)] -translate-x-2.5 rounded-full',
                  s.state === 'upcoming' ? 'bg-border' : 'bg-success',
                )}
              />
            )}
            <span
              aria-hidden="true"
              className={cn('relative inline-flex size-4 shrink-0 items-center justify-center rounded-full border-2', dotClass[s.state])}
            >
              {s.state === 'complete' && <Check className="size-2.5" strokeWidth={3.5} />}
              {s.state === 'current' && <span className="size-1.5 rounded-full bg-info motion-safe:animate-pulse" data-slot="run-stage-pulse" />}
            </span>
            <span
              className={cn(
                'max-w-full truncate text-[0.6875rem] leading-tight',
                s.state === 'upcoming' && 'text-muted-foreground',
                s.state === 'current' && 'font-semibold text-foreground',
              )}
            >
              {s.label}
              <span className="sr-only">{stateWords[s.state]}</span>
            </span>
          </li>
        ))}
      </ol>
    </div>
  )
}
