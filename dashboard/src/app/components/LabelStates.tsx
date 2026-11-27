import { Fragment } from 'react'

import { ArrowDown, ArrowRight, CircleCheck, CirclePause, CirclePlay, CornerDownRight, type LucideIcon } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { cn } from '@/lib/utils'

import { DiagramText, describeDiagram, toneClasses, type KindTone } from './diagram'
import { checkId, labelStateDiagram } from '../lib/label-flow'
import { labelStates, outcomeText, type Outcome } from '../lib/labels'

// Each outcome has a fixed tone and icon; its text is always shown beside
// them, so the meaning never rests on color alone.
const outcomeLook: Record<Outcome, { tone: KindTone; icon: LucideIcon }> = {
  waits: { tone: 'muted', icon: CirclePause },
  runs: { tone: 'primary', icon: CirclePlay },
  done: { tone: 'success', icon: CircleCheck },
}

// columns is the row grid from a box of 560 px up: check | yes | outcome,
// the outcome wider so most meanings take one or two lines. The "no" rows
// use the same grid, so their arrows line up under the checks.
const columns = '@min-[560px]:grid-cols-[minmax(0,3fr)_2.25rem_minmax(0,5fr)]'

/**
 * The label state flowchart of How it works (#476): Core's checks top to
 * bottom, one per row, each with its "yes" outcome on the same row and a "no"
 * arrow down to the next check. Below 560 px the outcome sits under its check.
 * The rows are hidden from assistive technology; the visually hidden list,
 * from the same spec as before, is the text alternative.
 */
export function LabelStates() {
  const spec = labelStateDiagram(labelStates)
  const items = describeDiagram(spec, { order: spec.nodes.map((n) => n.id) })
  return (
    <figure className="not-content m-0 mt-6 flex min-w-0 flex-col gap-3 p-0" data-slot="diagram">
      <figcaption className="m-0 flex flex-col gap-1">
        <span className="font-heading text-base font-semibold text-foreground">Label states, checked in order</span>
        <span className="text-sm text-muted-foreground">
          Core checks from the top and stops at the first match: yes goes to the outcome, no goes to the next check.
        </span>
      </figcaption>
      <div aria-hidden="true" className="@container min-w-0" data-slot="diagram-rows">
        <div className="flex flex-col">
          {labelStates.map((s, i) => {
            const look = outcomeLook[s.outcome]
            const tone = toneClasses(look.tone)
            const Icon = look.icon
            return (
              <Fragment key={s.id}>
                <div className={cn('grid grid-cols-1 gap-1', columns)} data-slot="label-state" data-state={s.id}>
                  <Card className="min-w-0 justify-center gap-0 rounded-lg px-3 py-1 shadow-none" data-block={checkId(s)}>
                    <span className="text-sm leading-5">
                      <span className="mr-1.5 font-heading font-semibold">{s.when}</span>
                      {s.labels.map((l) => (
                        <Badge key={l} variant="outline" className="mr-1 px-1.5 py-0 align-[1px] font-mono leading-4 font-normal">
                          {l}
                        </Badge>
                      ))}
                    </span>
                  </Card>
                  <div className="flex min-w-0 items-stretch gap-1.5 pl-3 @min-[560px]:contents">
                    <span
                      className="flex shrink-0 items-start gap-1 pt-1 text-xs text-muted-foreground @min-[560px]:flex-col @min-[560px]:items-center @min-[560px]:justify-center @min-[560px]:gap-0 @min-[560px]:pt-0"
                      data-slot="label-yes"
                    >
                      <CornerDownRight aria-hidden="true" className="size-3.5 @min-[560px]:hidden" />
                      <ArrowRight aria-hidden="true" className="hidden size-3.5 @min-[560px]:block" />
                      yes
                    </span>
                    <Card
                      className={cn('min-w-0 flex-1 justify-center gap-0 rounded-lg border-[1.5px] px-3 py-1 shadow-none', tone.border)}
                      data-block={s.id}
                      data-outcome={s.outcome}
                    >
                      <span className="text-sm leading-5 text-muted-foreground">
                        <span className={cn('mr-1.5 text-xs font-medium whitespace-nowrap', tone.text)} data-slot="label-outcome">
                          <Icon aria-hidden="true" className="mr-1 inline size-3.5 align-[-2px]" />
                          {outcomeText[s.outcome]}
                        </span>
                        <code className="mr-1.5 font-mono font-semibold whitespace-nowrap text-card-foreground">{s.id}</code>
                        {s.meaning}
                      </span>
                    </Card>
                  </div>
                </div>
                {i < labelStates.length - 1 && (
                  <div className={cn('grid grid-cols-1', columns)} data-slot="label-no">
                    <span className="flex h-4 items-center gap-1 pl-4 text-xs leading-4 text-muted-foreground @min-[560px]:justify-center @min-[560px]:pl-0">
                      <ArrowDown aria-hidden="true" className="size-3" />
                      no
                    </span>
                  </div>
                )}
              </Fragment>
            )
          })}
        </div>
      </div>
      <DiagramText items={items} lanes={(spec.groupStyle ?? 'frames') === 'lanes'} />
    </figure>
  )
}
