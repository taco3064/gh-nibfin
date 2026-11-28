import { CircleAlert, CircleCheck, CircleHelp, CircleStop, LoaderCircle } from 'lucide-react'
import { useEffect, useMemo, useReducer, useRef, useState, type ReactNode } from 'react'

import { tones, type Tone } from '@/components/issue/tones'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import {
  afterPlan,
  applyAtOnce,
  changingCount,
  elapsedSeconds,
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
  type PlanBody,
  type ProgressEvent,
  type Step,
  type Summary,
  type Values,
} from '@/lib/operation'
import { endsAt, loginReducer, running, timeoutReason, waiting, type LoginStep } from '@/lib/login'
import { applyStream, deletePlan, planStream, type ApiError } from '@/lib/session'
import { cn } from '@/lib/utils'

import { LoginPanel } from './login-panel'
import { OperationFields, type FieldInputs } from './operation-fields'
import { Notes, OperationItems } from './operation-items'

type Fetch = typeof fetch

export interface OperationDialogProps {
  command: Command
  open: boolean
  onOpenChange: (open: boolean) => void
  // onFinished is called once the dialog closes after an apply was sent,
  // whatever its outcome, so the opener can read the status again.
  onFinished?: () => void
  // initial replaces the defaults of the fields with its keys; keys no field
  // has are ignored. Openers find the keys through the schema.
  initial?: Values
  // title replaces the command path as the dialog title.
  title?: string
  // intro is shown above the inputs and stays above progress and read-back.
  intro?: ReactNode
  // inputs renders some fields in place of their default input.
  inputs?: FieldInputs
  // confirm false applies a stored plan that asks no confirmation right
  // after it is planned, with no preview stop (Proposal R9.21: resume is not
  // confirmed). A plan that asks for confirmation still stops at the
  // preview. The default is true: every plan stops at the preview.
  confirm?: boolean
  fetcher?: Fetch
}

// OperationDialog runs one changing command: inputs from its schema, the
// plan's preview, confirm or cancel, the apply's progress and the read-back.
// While a login waits for the person, closing it ends the login.
export function OperationDialog({ command, open, onOpenChange, onFinished, initial, fetcher, ...look }: OperationDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {open && <OperationContent command={command} initial={initial} onClose={() => onOpenChange(false)} onFinished={onFinished} fetcher={fetcher} {...look} />}
    </Dialog>
  )
}

const stages: { label: string; phases: Phase[] }[] = [
  { label: 'Inputs', phases: ['inputs', 'planning', 'planFailed'] },
  { label: 'Preview', phases: ['preview'] },
  { label: 'Progress', phases: ['applying'] },
  { label: 'Read-back', phases: ['finished'] },
]

function Stages({ phase, confirm }: { phase: Phase; confirm: boolean }) {
  // Without the preview stop, the preview step shows only when an apply was
  // refused and waits there.
  const shown = confirm || phase === 'preview' ? stages : stages.filter((s) => s.label !== 'Preview')
  const at = shown.findIndex((s) => s.phases.includes(phase))
  return (
    <ol aria-label="Steps" className="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs">
      {shown.map((s, i) => (
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
          {i < shown.length - 1 && (
            <span aria-hidden="true" className="text-muted-foreground">
              ›
            </span>
          )}
        </li>
      ))}
    </ol>
  )
}

function Message({
  tone,
  icon: I,
  slot,
  children,
  role,
}: {
  tone: Tone
  icon: typeof CircleAlert
  slot: string
  children: ReactNode
  role?: 'alert' | 'status'
}) {
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
  ended: ['warning', CircleStop],
}

export const endedInDialog = 'you chose End login'

function OperationContent({
  command,
  initial,
  onClose,
  onFinished,
  fetcher,
  title,
  intro,
  inputs,
  confirm = true,
}: {
  command: Command
  initial?: Values
  onClose: () => void
  onFinished?: () => void
  fetcher?: Fetch
  title?: string
  intro?: ReactNode
  inputs?: FieldInputs
  confirm?: boolean
}) {
  const fields = useMemo(() => fieldsOf(command), [command])
  const [values, setValues] = useState(() => initialValues(fields, initial))
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [state, dispatch] = useReducer(runReducer, initialRun)
  const [logins, loginDispatch] = useReducer(loginReducer, [])
  // controller aborts the running plan's or apply's request, which cancels
  // it on the server; it is set only while a plan or an apply streams.
  const controller = useRef<AbortController | null>(null)
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
      // Closing while a plan streams, or while an apply streams during a
      // login wait, ends the request.
      controller.current?.abort()
      controller.current = null
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

  // now ticks once a second while planning, for the time so far; it is
  // only on screen.
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (phase !== 'planning') return
    const t = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(t)
  }, [phase])

  const preview = async () => {
    const r = requestBody(fields, values)
    if (!r.ok) {
      setErrors(r.errors)
      return
    }
    setErrors({})
    const at = Date.now()
    setNow(at)
    dispatch({ type: 'plan', at })
    const ac = new AbortController()
    controller.current = ac
    const p = await planStream<PlanBody>(
      command.api ?? '',
      r.body,
      (line) => {
        if (ac.signal.aborted) return
        if (line.event === 'checking' && typeof line.check === 'string') dispatch({ type: 'checking', check: line.check })
        else if (line.event === 'checked' && Array.isArray(line.steps)) dispatch({ type: 'checked', steps: line.steps as Step[] })
      },
      fetcher,
      ac.signal,
    )
    if (controller.current === ac) controller.current = null
    // Cancel, Escape, close and unmount abort the plan: nothing is stored.
    const next = afterPlan(p, ac.signal.aborted)
    if (next.drop) void deletePlan(next.drop, fetchRef.current)
    if (next.store) stored.current = next.store
    if (next.action) dispatch(next.action)
    // Without the preview stop, a stored plan that asks no confirmation is
    // applied at once.
    const atOnce = applyAtOnce(confirm, next)
    if (atOnce) void apply(atOnce)
  }

  const apply = async (planId?: string) => {
    const id = planId ?? state.planId
    if (!id) return
    stored.current = null
    applied.current = true
    dispatch({ type: 'apply' })
    const ac = new AbortController()
    controller.current = ac
    const o = await applyStream<OpResult>(
      id,
      true,
      (line) => {
        if (line.event === 'progress' && line.progress) dispatch({ type: 'progress', event: line.progress as ProgressEvent })
        else if (line.event === 'login_step' && line.step) loginDispatch({ type: 'step', step: line.step as LoginStep, at: Date.now() })
      },
      fetcher,
      ac.signal,
    )
    if (controller.current === ac) controller.current = null
    // An aborted apply already has its ending, or the dialog is gone.
    if (ac.signal.aborted) return
    // A refused apply leaves the plan stored, except when it is gone.
    if (!o.ok && o.status !== undefined && o.status !== 404) stored.current = id
    loginDispatch({ type: 'outcome', outcome: o })
    dispatch({ type: 'applied', outcome: o })
  }

  // endLogin aborts the apply while a login waits, for the reason given.
  const endLogin = (reason: string) => {
    const ac = controller.current
    if (!ac) return
    controller.current = null
    dispatch({ type: 'aborted', reason })
    loginDispatch({ type: 'ended', reason })
    ac.abort()
  }
  const endRef = useRef(endLogin)
  useEffect(() => {
    endRef.current = endLogin
  })

  // The timeout: one timer per running login, until 15 minutes after its
  // first step. A login that succeeded or failed, the outcome and unmount
  // clear it.
  const phaseNow = state.phase
  useEffect(() => {
    if (phaseNow !== 'applying') return
    const timers = logins.filter(running).map((s) => setTimeout(() => endRef.current(timeoutReason), Math.max(0, endsAt(s) - Date.now())))
    return () => {
      for (const t of timers) clearTimeout(t)
    }
  }, [logins, phaseNow])

  const cancel = () => {
    // Cancel while planning stops the planning on the server.
    controller.current?.abort()
    controller.current = null
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
  // A login waiting for the person may be ended by closing; any other
  // running apply keeps the dialog open.
  const loginWait = phase === 'applying' && waiting(logins)
  const locked = phase === 'applying' && !loginWait
  const panels =
    logins.length > 0 && (phase === 'applying' || phase === 'finished') ? (
      <div className="flex flex-col gap-3" data-slot="logins">
        {logins.map((s) => (
          <LoginPanel key={s.provider} state={s} planId={state.planId} fetcher={fetcher} onCodeSent={(p) => loginDispatch({ type: 'codeSent', provider: p })} />
        ))}
      </div>
    ) : null

  return (
    <DialogContent
      showCloseButton={!locked}
      onEscapeKeyDown={(e) => {
        if (locked) e.preventDefault()
      }}
      onInteractOutside={(e) => {
        if (phase === 'applying') e.preventDefault()
      }}
      data-phase={phase}
      className="flex max-h-[calc(100svh-2rem)] flex-col bg-card sm:max-w-2xl"
    >
      <DialogHeader className="gap-3 pr-8 text-left">
        <DialogTitle className="break-words">{title ?? command.path}</DialogTitle>
        <DialogDescription className="break-words">{command.short}</DialogDescription>
        {command.long && command.long !== command.short && (
          <details className="text-sm text-muted-foreground">
            <summary className="w-fit cursor-pointer rounded-sm text-foreground focus-visible:outline-3 focus-visible:outline-offset-2 focus-visible:outline-ring">
              About this command
            </summary>
            <p className="mt-2 break-words whitespace-pre-line">{command.long}</p>
          </details>
        )}
        <Stages phase={phase} confirm={confirm} />
      </DialogHeader>

      <div ref={body} tabIndex={-1} aria-busy={busy} className="-mx-6 flex min-h-0 flex-col gap-4 overflow-y-auto px-6 py-1 outline-none">
        {intro}
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
            <OperationFields fields={fields} values={values} errors={errors} disabled={phase === 'planning'} onChange={onChange} inputs={inputs} />
          </form>
        )}

        {phase === 'planning' && (
          <section aria-label="Planning" data-slot="planning" className="flex flex-col gap-3">
            <Message tone="info" icon={LoaderCircle} slot="planning-status">
              <span className="font-medium">Planning the preview:</span>{' '}
              <span data-slot="elapsed" className="tabular-nums">
                {elapsedSeconds(state.planStarted, now)} s
              </span>{' '}
              so far. Nothing is applied while it is planned.
            </Message>
            <OperationItems rows={state.rows} phase={phase} label="Items checked so far" checking={state.checking} />
          </section>
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
              {loginWait ? 'Applying. Sign in below to continue.' : 'Applying. The dialog stays open until the result is read back.'}
            </Message>
            {panels}
            {loginWait && (
              <p data-slot="end-login-note" className="text-sm break-words text-muted-foreground">
                Ending the login cancels this {command.path}; run it again to continue.
              </p>
            )}
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
            {panels}
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
              {phase === 'planning' ? 'Planning…' : confirm ? 'Preview' : 'Submit'}
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
        {loginWait && (
          <Button variant="outline" onClick={() => endLogin(endedInDialog)} data-action="end-login">
            <CircleStop aria-hidden="true" />
            End login
          </Button>
        )}
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
