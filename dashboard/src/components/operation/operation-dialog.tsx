import { CircleAlert, CircleCheck, CircleHelp, LoaderCircle } from 'lucide-react'
import { useEffect, useMemo, useReducer, useRef, useState, type ReactNode } from 'react'

import { tones, type Tone } from '@/components/issue/tones'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  changingCount,
  fieldsOf,
  initialRun,
  initialValues,
  readBackNotes,
  requestBody,
  runReducer,
  summary,
  type Command,
  type FieldValue,
  type OpResult,
  type Phase,
  type Plan,
  type ProgressEvent,
  type Summary,
} from '@/lib/operation'
import { applyStream, deletePlan, planRequest, type ApiError } from '@/lib/session'
import { cn } from '@/lib/utils'

import { OperationFields } from './operation-fields'
import { Notes, OperationItems } from './operation-items'

type Fetch = typeof fetch

export interface OperationDialogProps {
  command: Command
  open: boolean
  onOpenChange: (open: boolean) => void
  // onFinished is called once the dialog closes after an apply was sent,
  // whatever its outcome, so the opener can read the status again.
  onFinished?: () => void
  fetcher?: Fetch
}

// OperationDialog runs one changing command: inputs from its schema, the
// plan's preview, confirm or cancel, the apply's progress and the read-back.
export function OperationDialog({ command, open, onOpenChange, onFinished, fetcher }: OperationDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {open && <OperationContent command={command} onClose={() => onOpenChange(false)} onFinished={onFinished} fetcher={fetcher} />}
    </Dialog>
  )
}

const stages: { label: string; phases: Phase[] }[] = [
  { label: 'Inputs', phases: ['inputs', 'planning', 'planFailed'] },
  { label: 'Preview', phases: ['preview'] },
  { label: 'Progress', phases: ['applying'] },
  { label: 'Read-back', phases: ['finished'] },
]

function Stages({ phase }: { phase: Phase }) {
  const at = stages.findIndex((s) => s.phases.includes(phase))
  return (
    <ol aria-label="Steps" className="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs">
      {stages.map((s, i) => (
        <li key={s.label} className="flex items-center gap-1.5">
          <span
            aria-current={i === at ? 'step' : undefined}
            className={cn(
              'rounded-full border px-2 py-0.5',
              i === at ? 'border-primary bg-primary font-semibold text-primary-foreground' : 'border-border text-muted-foreground',
            )}
          >
            <span className="tabular-nums">{i + 1}.</span> {s.label}
          </span>
          {i < stages.length - 1 && (
            <span aria-hidden="true" className="text-muted-foreground">
              ›
            </span>
          )}
        </li>
      ))}
    </ol>
  )
}

function Message({ tone, icon: I, slot, children, role }: { tone: Tone; icon: typeof CircleAlert; slot: string; children: ReactNode; role?: 'alert' | 'status' }) {
  return (
    <div role={role} data-slot={slot} className={cn('flex items-start gap-2 rounded-md px-3 py-2 text-sm', tones[tone])}>
      <I aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
      <div className="min-w-0 break-words">{children}</div>
    </div>
  )
}

function ErrorLine({ error }: { error: ApiError }) {
  return (
    <>
      <code className="font-semibold">{error.code}</code>: {error.message}
    </>
  )
}

const summaryLook: Record<Summary['kind'], [Tone, typeof CircleAlert]> = {
  confirmed: ['success', CircleCheck],
  notConfirmed: ['danger', CircleAlert],
  failed: ['danger', CircleAlert],
  unknown: ['warning', CircleHelp],
}

function OperationContent({
  command,
  onClose,
  onFinished,
  fetcher,
}: {
  command: Command
  onClose: () => void
  onFinished?: () => void
  fetcher?: Fetch
}) {
  const fields = useMemo(() => fieldsOf(command), [command])
  const [values, setValues] = useState(() => initialValues(fields))
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [state, dispatch] = useReducer(runReducer, initialRun)
  // stored is the id of a plan the server holds that was not applied;
  // closing the dialog drops it.
  const stored = useRef<string | null>(null)
  const applied = useRef(false)
  const finished = useRef(onFinished)
  const body = useRef<HTMLDivElement>(null)

  const fetchRef = useRef(fetcher)
  useEffect(() => {
    finished.current = onFinished
    fetchRef.current = fetcher
  })

  useEffect(() => {
    return () => {
      if (stored.current) void deletePlan(stored.current, fetchRef.current)
      stored.current = null
      if (applied.current) finished.current?.()
      applied.current = false
    }
  }, [])

  // Each new phase moves focus to the dialog body, whose controls changed.
  useEffect(() => {
    if (state.phase !== 'inputs') body.current?.focus()
  }, [state.phase])

  const phase = state.phase
  const busy = phase === 'planning' || phase === 'applying'

  const preview = async () => {
    const r = requestBody(fields, values)
    if (!r.ok) {
      setErrors(r.errors)
      return
    }
    setErrors({})
    dispatch({ type: 'plan' })
    const p = await planRequest<{ id?: string; plan: Plan; result?: OpResult }>(
      `/api/commands/${encodeURIComponent(command.api ?? '')}/plan`,
      r.body,
      fetcher,
    )
    if (p.ok) {
      if (p.value.id) {
        stored.current = p.value.id
        dispatch({ type: 'planned', id: p.value.id, plan: p.value.plan })
      } else {
        dispatch({ type: 'readBack', plan: p.value.plan, result: p.value.result ?? {} })
      }
      return
    }
    if (p.plan && p.result) {
      dispatch({ type: 'readBack', plan: p.plan as Plan, result: p.result as OpResult, error: p.error })
      return
    }
    dispatch({ type: 'planFailed', error: p.error })
  }

  const apply = async () => {
    const id = state.planId
    if (!id) return
    stored.current = null
    applied.current = true
    dispatch({ type: 'apply' })
    const o = await applyStream<OpResult>(
      id,
      true,
      (line) => {
        if (line.event === 'progress' && line.progress) dispatch({ type: 'progress', event: line.progress as ProgressEvent })
      },
      fetcher,
    )
    // A refused apply leaves the plan stored, except when it is gone.
    if (!o.ok && o.status !== undefined && o.status !== 404) stored.current = id
    dispatch({ type: 'applied', outcome: o })
  }

  const cancel = () => {
    if (stored.current) void deletePlan(stored.current, fetcher)
    stored.current = null
    onClose()
  }

  const onChange = (key: string, v: FieldValue) => {
    setValues((prev) => ({ ...prev, [key]: v }))
    if (errors[key]) setErrors((prev) => Object.fromEntries(Object.entries(prev).filter(([k]) => k !== key)))
  }

  const sum = summary(state)
  const plan = state.plan
  const changing = plan ? changingCount(plan) : 0
  const kept = plan ? (plan.steps ?? []).length - changing : 0
  const refused = state.refused
  const canRetry = refused === undefined || refused.status === 409

  return (
    <DialogContent
      showCloseButton={phase !== 'applying'}
      onEscapeKeyDown={(e) => {
        if (phase === 'applying') e.preventDefault()
      }}
      onInteractOutside={(e) => {
        if (phase === 'applying') e.preventDefault()
      }}
      data-phase={phase}
      className="flex max-h-[calc(100svh-2rem)] flex-col bg-card sm:max-w-2xl"
    >
      <DialogHeader className="gap-3 pr-8 text-left">
        <DialogTitle className="break-words">{command.path}</DialogTitle>
        <DialogDescription className="break-words">{command.short}</DialogDescription>
        {command.long && command.long !== command.short && (
          <details className="text-sm text-muted-foreground">
            <summary className="w-fit cursor-pointer rounded-sm text-foreground focus-visible:outline-3 focus-visible:outline-offset-2 focus-visible:outline-ring">
              About this command
            </summary>
            <p className="mt-2 break-words whitespace-pre-line">{command.long}</p>
          </details>
        )}
        <Stages phase={phase} />
      </DialogHeader>

      <div
        ref={body}
        tabIndex={-1}
        aria-busy={busy}
        className="-mx-6 flex min-h-0 flex-col gap-4 overflow-y-auto px-6 py-1 outline-none"
      >
        {(phase === 'inputs' || phase === 'planning' || phase === 'planFailed') && (
          <form
            id="operation-form"
            noValidate
            onSubmit={(e) => {
              e.preventDefault()
              if (phase !== 'planning') void preview()
            }}
            className="flex flex-col gap-4"
          >
            {phase === 'planFailed' && state.error && (
              <Message tone="danger" icon={CircleAlert} slot="plan-error" role="alert">
                <ErrorLine error={state.error} />
              </Message>
            )}
            <OperationFields fields={fields} values={values} errors={errors} disabled={phase === 'planning'} onChange={onChange} />
          </form>
        )}

        {phase === 'preview' && plan && (
          <>
            {refused && (
              <Message tone="danger" icon={CircleAlert} slot="refused" role="alert">
                <ErrorLine error={refused.error} />
              </Message>
            )}
            <p data-slot="plan-count" className="text-sm">
              <span className="font-semibold tabular-nums">{changing}</span> {changing === 1 ? 'item changes' : 'items change'}
              {kept > 0 && (
                <span className="text-muted-foreground">
                  , <span className="tabular-nums">{kept}</span> already in place
                </span>
              )}
              . Nothing is applied until you confirm.
            </p>
            <OperationItems rows={state.rows} phase={phase} label="Planned items" />
            <Notes notes={plan.notes ?? []} />
          </>
        )}

        {phase === 'applying' && (
          <>
            <Message tone="info" icon={LoaderCircle} slot="applying" role="status">
              Applying. The dialog stays open until the result is read back.
            </Message>
            <OperationItems rows={state.rows} phase={phase} label="Items" />
          </>
        )}

        {phase === 'finished' && sum && (
          <>
            <Message tone={summaryLook[sum.kind][0]} icon={summaryLook[sum.kind][1]} slot="summary" role="status">
              <span data-summary={sum.kind} className="font-medium">
                {state.ending === 'readOnly' && sum.kind === 'confirmed' ? `Nothing to change. ${sum.text}` : sum.text}
              </span>
            </Message>
            {state.error && sum.kind !== 'unknown' && (
              <Message tone="danger" icon={CircleAlert} slot="final-error" role="alert">
                <ErrorLine error={state.error} />
              </Message>
            )}
            {state.error && sum.kind === 'unknown' && (
              <p data-slot="final-error" className="text-sm break-words text-muted-foreground">
                <ErrorLine error={state.error} />
              </p>
            )}
            <OperationItems rows={state.rows} phase={phase} label="Items read back" />
            <Notes notes={readBackNotes(state)} />
          </>
        )}
      </div>

      <DialogFooter>
        {(phase === 'inputs' || phase === 'planning' || phase === 'planFailed') && (
          <>
            <Button variant="outline" onClick={cancel}>
              Cancel
            </Button>
            <Button type="submit" form="operation-form" disabled={phase === 'planning'}>
              {phase === 'planning' && <LoaderCircle aria-hidden="true" className="motion-safe:animate-spin" />}
              {phase === 'planning' ? 'Planning…' : 'Preview'}
            </Button>
          </>
        )}
        {phase === 'preview' &&
          (canRetry ? (
            <>
              <Button variant="outline" onClick={cancel}>
                Cancel
              </Button>
              <Button onClick={() => void apply()}>{refused ? 'Apply again' : 'Apply'}</Button>
            </>
          ) : (
            <Button variant="outline" onClick={cancel}>
              Close
            </Button>
          ))}
        {phase === 'applying' && (
          <Button disabled>
            <LoaderCircle aria-hidden="true" className="motion-safe:animate-spin" />
            Applying…
          </Button>
        )}
        {phase === 'finished' && (
          <Button variant="outline" onClick={onClose}>
            Close
          </Button>
        )}
      </DialogFooter>
    </DialogContent>
  )
}
