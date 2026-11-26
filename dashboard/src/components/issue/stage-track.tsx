import { Check, CircleDashed } from 'lucide-react'

import { stageLabels, stageSteps, type Stage, type StepState } from '@/lib/issue'
import { cn } from '@/lib/utils'

const dotClass: Record<StepState, string> = {
  complete: 'border-success bg-success text-card',
  current: 'border-info bg-info-bg text-info ring-2 ring-info ring-offset-2 ring-offset-card',
  upcoming: 'border-muted-foreground bg-transparent',
}

const doneCurrent = 'border-success bg-success text-card ring-2 ring-success ring-offset-2 ring-offset-card'

const stateWords: Record<StepState, string> = {
  complete: ' (completed)',
  current: ' (current)',
  upcoming: '',
}

// StageTrack shows the Nibfin stage from labels as Shape → Deliver → Done:
// completed steps filled, the current step highlighted. An Issue without
// scope says it is not in Nibfin instead.
export function StageTrack({ stage }: { stage: Stage }) {
  if (stage === 'none') {
    return (
      <p data-slot="stage-track" data-stage={stage} className="m-0 inline-flex items-center gap-1.5 text-sm text-muted-foreground">
        <CircleDashed aria-hidden="true" className="size-4 shrink-0" />
        <span className="sr-only">Stage: </span>
        {stageLabels.none}
      </p>
    )
  }
  const steps = stageSteps(stage)
  return (
    <ol data-slot="stage-track" data-stage={stage} aria-label="Stage" className="m-0 flex list-none flex-wrap items-center gap-y-1 p-0 text-sm">
      {steps.map((s, i) => {
        const filled = s.state === 'complete' || (s.state === 'current' && s.stage === 'done')
        return (
          <li
            key={s.stage}
            data-step={s.stage}
            data-state={s.state}
            aria-current={s.state === 'current' ? 'step' : undefined}
            className="flex items-center"
          >
            {i > 0 && (
              <span
                aria-hidden="true"
                className={cn('mx-1.5 h-0.5 w-3 rounded-full sm:w-4', s.state === 'upcoming' ? 'bg-border' : 'bg-success')}
              />
            )}
            <span className="inline-flex items-center gap-1.5">
              <span
                aria-hidden="true"
                className={cn(
                  'inline-flex size-4 shrink-0 items-center justify-center rounded-full border-2',
                  s.state === 'current' && s.stage === 'done' ? doneCurrent : dotClass[s.state],
                )}
              >
                {filled && <Check className="size-2.5" strokeWidth={3.5} />}
              </span>
              <span
                className={cn(
                  s.state === 'upcoming' && 'text-muted-foreground',
                  s.state === 'current' && 'font-semibold text-foreground',
                )}
              >
                {s.label}
                <span className="sr-only">{stateWords[s.state]}</span>
              </span>
            </span>
          </li>
        )
      })}
    </ol>
  )
}
