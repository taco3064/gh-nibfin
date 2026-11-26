import { ArrowDown, ArrowRight } from 'lucide-react'

import { cn } from '@/lib/utils'

interface Step {
  title: string
  steps: string[]
  labels?: string[]
}

const flow: Step[] = [
  { title: 'Issue', steps: ['scope or request label', 'one Assignee'], labels: ['scope', 'request'] },
  { title: 'Shape', steps: ['Author', 'Validator', 'Shape published'], labels: ['shaped'] },
  { title: 'Deliver', steps: ['Writer', 'Accept', 'pull request'] },
  { title: 'CI', steps: ['required checks'] },
  { title: 'Review', steps: ['review against the Shape'] },
  { title: 'Merge', steps: ['merged into the base'] },
  { title: 'CD', steps: ['CD runs pass'] },
  { title: 'Delivered', steps: ['Issue closed', 'downstream Issues woken'], labels: ['delivered'] },
]

const perRow = 4

/** The path of an Issue from scope to delivery, as a numbered list with decorative arrows. */
export function IssueFlow() {
  return (
    <ol className="not-content m-0 grid list-none gap-x-8 gap-y-6 p-0 lg:grid-cols-4" aria-label="From Issue to merge">
      {flow.map((step, index) => {
        const last = index === flow.length - 1
        return (
          <li
            key={step.title}
            className="relative flex flex-col gap-2 rounded-lg border border-border-subtle bg-card p-4 text-card-foreground"
          >
            <span className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Step {index + 1}</span>
            <span className="font-heading text-lg font-semibold">{step.title}</span>
            <span className="text-sm text-muted-foreground">
              {step.steps.map((part, partIndex) => (
                <span key={part}>
                  {partIndex > 0 && <span aria-hidden="true"> → </span>}
                  {partIndex > 0 && <span className="sr-only">, then </span>}
                  {part}
                </span>
              ))}
            </span>
            {step.labels && (
              <span className="flex flex-wrap gap-1">
                {step.labels.map((label) => (
                  <code key={label} className="rounded-sm bg-muted px-1.5 py-0.5 text-xs">
                    {label}
                  </code>
                ))}
              </span>
            )}
            {!last && (
              <>
                <ArrowDown
                  aria-hidden="true"
                  className="absolute -bottom-6 left-1/2 size-5 -translate-x-1/2 text-muted-foreground lg:hidden"
                />
                <ArrowRight
                  aria-hidden="true"
                  className={cn(
                    'absolute top-1/2 -right-7 hidden size-5 -translate-y-1/2 text-muted-foreground',
                    (index + 1) % perRow !== 0 && 'lg:block',
                  )}
                />
              </>
            )}
          </li>
        )
      })}
    </ol>
  )
}
