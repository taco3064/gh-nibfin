import { Check, CircleDashed } from 'lucide-react'
import { useLayoutEffect, useRef, useState } from 'react'

import { stageLabels, stageSteps, type Stage, type StageStep as Step, type StepState } from '@/lib/issue'
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

const labelClass: Record<StepState, string> = {
  complete: '',
  current: 'font-semibold text-foreground',
  upcoming: 'text-muted-foreground',
}

function Connector({ step }: { step: Step }) {
  return <span aria-hidden="true" className={cn('mx-1.5 h-0.5 w-3 shrink-0 rounded-full sm:w-4', step.state === 'upcoming' ? 'bg-border' : 'bg-success')} />
}

function Dot({ step }: { step: Step }) {
  const filled = step.state === 'complete' || (step.state === 'current' && step.stage === 'done')
  return (
    <span
      aria-hidden="true"
      className={cn(
        'inline-flex size-4 shrink-0 items-center justify-center rounded-full border-2',
        step.state === 'current' && step.stage === 'done' ? doneCurrent : dotClass[step.state],
      )}
    >
      {filled && <Check className="size-2.5" strokeWidth={3.5} />}
    </span>
  )
}

// Form is how the track is drawn: full shows every label with connectors,
// compact one dot per stage and the current stage's name only.
type Form = 'full' | 'compact'

// fit is the full form's natural width and whether it is wider than the
// space the track has on its line.
interface Fit {
  width: number
  compact: boolean
}

// StageTrack shows the Nibfin stage from labels as Scope → Shape → Deliver → Done:
// completed steps filled, the current step highlighted. When the full track
// does not fit on its line it turns compact: one dot per stage followed by the
// current stage's name. Both forms keep the same list for screen readers. An
// Issue without scope or request says it is not in Nibfin instead.
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
  return <Track stage={stage} />
}

function Track({ stage }: { stage: Exclude<Stage, 'none'> }) {
  const steps = stageSteps(stage)
  const wrap = useRef<HTMLDivElement>(null)
  const ghost = useRef<HTMLSpanElement>(null)
  const [fit, setFit] = useState<Fit | null>(null)

  // The wrapper is given the full form's natural width (capped at its line),
  // so its width never depends on the form shown: compact exactly when the
  // line leaves it less than that width. Both widths are layout widths in CSS
  // pixels (offsetWidth, clientWidth), which a transform such as the
  // dependency graph's zoom does not scale. offsetWidth rounds to whole
  // pixels, so the wrapper gets one pixel more to always hold the full form.
  useLayoutEffect(() => {
    const w = wrap.current
    const g = ghost.current
    if (!w || !g) return
    const measure = () => {
      const natural = g.offsetWidth
      // Not laid out (for example a hidden panel): keep the last decision.
      if (natural === 0) return
      const width = natural + 1
      const compact = natural > w.clientWidth
      setFit((prev) => (prev && prev.width === width && prev.compact === compact ? prev : { width, compact }))
    }
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(w)
    ro.observe(g)
    return () => ro.disconnect()
  }, [])

  const form: Form = fit?.compact ? 'compact' : 'full'
  const current = steps.find((s) => s.state === 'current')
  return (
    <div ref={wrap} className="relative flex min-w-0 max-w-full items-center gap-2 whitespace-nowrap text-sm" style={fit ? { width: fit.width } : undefined}>
      <ol
        data-slot="stage-track"
        data-stage={stage}
        data-form={form}
        aria-label="Stage"
        className={cn('m-0 flex list-none flex-nowrap items-center p-0', form === 'compact' && 'gap-2')}
      >
        {steps.map((s, i) => (
          <li key={s.stage} data-step={s.stage} data-state={s.state} aria-current={s.state === 'current' ? 'step' : undefined} className="flex items-center">
            {form === 'full' && i > 0 && <Connector step={s} />}
            <span className="inline-flex items-center gap-1.5">
              <Dot step={s} />
              <span className={cn(form === 'compact' ? 'sr-only' : labelClass[s.state])}>
                {s.label}
                <span className="sr-only">{stateWords[s.state]}</span>
              </span>
            </span>
          </li>
        ))}
      </ol>
      {form === 'compact' && current && (
        <span aria-hidden="true" data-slot="stage-current" className={labelClass.current}>
          {current.label}
        </span>
      )}
      {/* The full form's visible parts, never shown: their width is the
          track's natural width. The zero-size box keeps them out of the
          page's scroll width. */}
      <span aria-hidden="true" className="pointer-events-none invisible absolute top-0 left-0 size-0 overflow-hidden">
        <span ref={ghost} className="flex w-max items-center">
          {steps.map((s, i) => (
            <span key={s.stage} className="flex items-center">
              {i > 0 && <Connector step={s} />}
              <span className="inline-flex items-center gap-1.5">
                <Dot step={s} />
                <span className={labelClass[s.state]}>{s.label}</span>
              </span>
            </span>
          ))}
        </span>
      </span>
    </div>
  )
}
