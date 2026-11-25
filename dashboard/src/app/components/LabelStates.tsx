import { tones, type Tone } from '@/components/issue/tones'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

import { labelStates, outcomeText, type Outcome } from '../lib/labels'

const outcomeTone: Record<Outcome, Tone> = { waits: 'neutral', runs: 'info', done: 'success' }

/** The label state diagram: Core's checks in order, each with its outcome in text. */
export function LabelStates() {
  return (
    <ol className="not-content m-0 flex list-none flex-col gap-2 p-0" aria-label="Label states, checked in order">
      {labelStates.map((state, index) => (
        <li
          key={state.id}
          className="grid grid-cols-[2rem_1fr] gap-x-3 gap-y-1 rounded-lg border border-border-subtle bg-card p-3 text-card-foreground"
        >
          <span
            aria-hidden="true"
            className="row-span-2 flex size-8 items-center justify-center rounded-full bg-muted font-heading text-sm font-semibold"
          >
            {index + 1}
          </span>
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <span className="font-heading font-semibold">{state.when}</span>{' '}
            <Badge variant="outline" className={cn(tones[outcomeTone[state.outcome]])}>
              {outcomeText[state.outcome]}
            </Badge>{' '}
            {state.labels.map((label) => (
              <Badge key={label} variant="outline" className="font-mono">
                {label}
              </Badge>
            ))}{' '}
          </div>
          <p className="m-0 text-sm text-muted-foreground">
            <code className="text-xs">{state.id}</code>: {state.meaning}
          </p>
        </li>
      ))}
    </ol>
  )
}
