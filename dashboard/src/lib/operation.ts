// Operation logic of the Dashboard operation dialog (#351): the input fields
// built from the argument schema of GET /api/commands, the plan request they
// make, and the dialog state from plan through progress to read-back. It
// names no command and no argument: the CLI definition is their only source.

import type { ApiError, PlanOutcome, StreamOutcome } from '@/lib/session'

// Command, Flag and Arg mirror one entry of GET /api/commands.
export interface Flag {
  name: string
  shorthand: string
  type: string
  default: string
  usage: string
  required: boolean
  allowed?: string[] | null
  param?: string
}

export interface Arg {
  name: string
  required: boolean
  repeatable: boolean
  allowed?: string[] | null
  param?: string
}

export interface Command {
  path: string
  api?: string
  short: string
  long: string
  flags: Flag[] | null
  args: Arg[] | null
}

// Step, Plan, Check and ProgressEvent mirror the plan, progress and
// read-back shapes of the plan and apply routes.
export interface Step {
  id: string
  action: string
  target: string
  detail?: string
  current?: string
  next?: string
  noChange: boolean
  afterStart: boolean
}

export interface Plan {
  operation: string
  repo?: string
  steps: Step[] | null
  notes?: string[] | null
  confirm: boolean
  readOnly: boolean
}

export interface Check {
  id: string
  action: string
  target: string
  confirmed: boolean
  state?: string
  reason?: string
}

export interface ProgressEvent {
  id: string
  action: string
  target: string
  phase: 'started' | 'done' | 'failed'
  code?: string
  message?: string
}

// OpResult is the part of a changing command's result the dialog reads.
export interface OpResult {
  items?: Check[] | null
  notes?: string[] | null
}

export type FieldKind = 'choice' | 'list' | 'boolean' | 'integer' | 'text'

export interface Field {
  // key is the plan request field the input fills.
  key: string
  name: string
  // cli is how the CLI spells the input: --name or <name>.
  cli: string
  kind: FieldKind
  help: string
  required: boolean
  allowed: string[]
  // multiple is set for a choice that takes several values.
  multiple: boolean
  initial: FieldValue
}

// A field's value: a string for text, integers and choices (comma-joined
// when multiple), a boolean for a boolean.
export type FieldValue = string | boolean

export type Values = Record<string, FieldValue>

function kindOf(allowed: readonly string[], repeatable: boolean, type: string | undefined): FieldKind {
  if (allowed.length > 0) return 'choice'
  if (repeatable) return 'list'
  if (type === 'bool') return 'boolean'
  if (type === 'int') return 'integer'
  return 'text'
}

function initialOf(kind: FieldKind, def: string, allowed: readonly string[]): FieldValue {
  switch (kind) {
    case 'boolean':
      return def === 'true'
    case 'integer':
      return /^-?\d+$/.test(def.trim()) ? def.trim() : ''
    case 'choice':
      return allowed.includes(def) ? def : ''
    default:
      return def
  }
}

// fieldsOf returns one field per positional argument, then per flag, that
// has a request field. CLI-only inputs have none and are left out.
export function fieldsOf(command: Command): Field[] {
  const out: Field[] = []
  for (const a of command.args ?? []) {
    if (!a.param) continue
    const allowed = a.allowed ?? []
    const kind = kindOf(allowed, a.repeatable, undefined)
    const multiple = kind === 'choice' && a.repeatable
    out.push({
      key: a.param,
      name: a.name,
      cli: a.repeatable ? `<${a.name}>...` : `<${a.name}>`,
      kind,
      help:
        kind === 'list'
          ? 'One or more values, separated by spaces, commas or new lines.'
          : a.required
            ? ''
            : "Optional. Leave it empty for the command's default.",
      required: a.required,
      allowed,
      multiple,
      initial: '',
    })
  }
  for (const f of command.flags ?? []) {
    if (!f.param) continue
    const allowed = f.allowed ?? []
    const kind = kindOf(allowed, false, f.type)
    out.push({
      key: f.param,
      name: f.name,
      cli: `--${f.name}`,
      kind,
      help: f.usage,
      required: f.required,
      allowed,
      multiple: false,
      initial: initialOf(kind, f.default, allowed),
    })
  }
  return out
}

// fieldIds are the element ids of one field's input, label, help and
// error in the dialog form.
export function fieldIds(key: string) {
  const base = `op-field-${key}`
  return { input: base, label: `${base}-label`, help: `${base}-help`, error: `${base}-error` }
}

// initialValues are the fields' defaults; a value of initial replaces the
// default of the field with its key, and keys no field has are ignored.
export function initialValues(fields: readonly Field[], initial?: Values): Values {
  return Object.fromEntries(fields.map((f) => [f.key, initial && Object.hasOwn(initial, f.key) ? initial[f.key] : f.initial]))
}

function splitList(v: string): string[] {
  return v.split(/[\s,]+/).filter((s) => s !== '')
}

export type BodyOutcome = { ok: true; body: Record<string, unknown> } | { ok: false; errors: Record<string, string> }

export const requiredText = 'Required.'
export const integerText = 'Enter a whole number.'

// requestBody builds the plan route body keyed by each field's request
// field, or the errors of the fields that are not valid. Empty optional
// fields are left out so the server's defaults apply.
export function requestBody(fields: readonly Field[], values: Values): BodyOutcome {
  const body: Record<string, unknown> = {}
  const errors: Record<string, string> = {}
  for (const f of fields) {
    const raw = values[f.key] ?? f.initial
    if (f.kind === 'boolean') {
      body[f.key] = raw === true
      continue
    }
    const v = typeof raw === 'string' ? raw.trim() : ''
    const empty = f.kind === 'list' || f.multiple ? splitList(v).length === 0 : v === ''
    if (empty) {
      if (f.required) errors[f.key] = requiredText
      continue
    }
    switch (f.kind) {
      case 'integer':
        if (!/^-?\d+$/.test(v)) errors[f.key] = integerText
        else body[f.key] = Number(v)
        break
      case 'choice': {
        const chosen = f.multiple ? splitList(v) : [v]
        const bad = chosen.find((c) => !f.allowed.includes(c))
        if (bad !== undefined) errors[f.key] = `Choose one of: ${f.allowed.join(', ')}.`
        else body[f.key] = f.multiple ? chosen : v
        break
      }
      case 'list':
        body[f.key] = splitList(v)
        break
      default:
        body[f.key] = v
    }
  }
  return Object.keys(errors).length > 0 ? { ok: false, errors } : { ok: true, body }
}

export type ItemProgress = 'waiting' | 'started' | 'done' | 'failed'

// Row is one item of the dialog: a plan step, an item a progress event
// reported that the plan did not list, or a check without a step.
export interface Row {
  id: string
  action: string
  target: string
  step?: Step
  progress: ItemProgress
  error?: ApiError
  check?: Check
}

export type Phase = 'inputs' | 'planning' | 'planFailed' | 'preview' | 'applying' | 'finished'

// Ending is how a finished dialog got its outcome.
// ended is an apply the dialog aborted while a login waited.
export type Ending = 'readOnly' | 'result' | 'error' | 'unknown' | 'ended'

export interface RunState {
  phase: Phase
  // checking is the check the plan runs now and planStarted when planning
  // began (ms); both are set only while planning.
  checking?: string
  planStarted?: number
  planId?: string
  plan?: Plan
  rows: Row[]
  // error is the plan error, or the final error of the apply or read-back.
  error?: ApiError
  // refused is an apply refused before its stream, with its HTTP status.
  refused?: { error: ApiError; status?: number }
  ending?: Ending
  result?: OpResult
  // endedReason is why the dialog aborted the apply.
  endedReason?: string
}

export type RunAction =
  | { type: 'plan'; at: number }
  | { type: 'checking'; check: string }
  | { type: 'checked'; steps: Step[] }
  | { type: 'planFailed'; error: ApiError }
  | { type: 'planned'; id: string; plan: Plan }
  | { type: 'readBack'; plan: Plan; result: OpResult; error?: ApiError }
  | { type: 'apply' }
  | { type: 'progress'; event: ProgressEvent }
  | { type: 'applied'; outcome: StreamOutcome<OpResult> }
  | { type: 'aborted'; reason: string }

export const initialRun: RunState = { phase: 'inputs', rows: [] }

function rowOf(s: Step): Row {
  return { id: s.id, action: s.action, target: s.target, step: s, progress: 'waiting' }
}

function rowsOf(plan: Plan): Row[] {
  return (plan.steps ?? []).map(rowOf)
}

// withChecked adds the items a plan reported as checked, replacing a row
// with the same id in place.
function withChecked(rows: Row[], steps: readonly Step[]): Row[] {
  const out = rows.slice()
  for (const s of steps) {
    if (!s || typeof s.id !== 'string') continue
    const i = out.findIndex((r) => r.id === s.id)
    if (i < 0) out.push(rowOf(s))
    else out[i] = rowOf(s)
  }
  return out
}

function withChecks(rows: Row[], result: OpResult | undefined): Row[] {
  const checks = result?.items ?? []
  const out = rows.map((r) => ({ ...r }))
  for (const c of checks) {
    const r = out.find((x) => x.id === c.id)
    if (r) r.check = c
    else out.push({ id: c.id, action: c.action, target: c.target, progress: 'waiting', check: c })
  }
  return out
}

function withProgress(rows: Row[], e: ProgressEvent): Row[] {
  const i = rows.findIndex((r) => r.id === e.id)
  const progress: ItemProgress = e.phase === 'started' ? 'started' : e.phase === 'done' ? 'done' : 'failed'
  const error = e.phase === 'failed' ? { code: e.code ?? 'error', message: e.message ?? 'the item failed' } : undefined
  if (i < 0) return [...rows, { id: e.id, action: e.action, target: e.target, progress, error }]
  const out = rows.slice()
  out[i] = { ...out[i], progress, error }
  return out
}

// runReducer moves the dialog from inputs through plan, preview and apply to
// its read-back.
export function runReducer(state: RunState, a: RunAction): RunState {
  switch (a.type) {
    case 'plan':
      return { ...initialRun, phase: 'planning', planStarted: a.at }
    case 'checking':
      if (state.phase !== 'planning') return state
      return { ...state, checking: a.check }
    case 'checked':
      if (state.phase !== 'planning') return state
      return { ...state, rows: withChecked(state.rows, a.steps) }
    case 'planFailed':
      return { ...initialRun, phase: 'planFailed', error: a.error }
    case 'planned':
      return { phase: 'preview', planId: a.id, plan: a.plan, rows: rowsOf(a.plan) }
    case 'readBack':
      return {
        phase: 'finished',
        plan: a.plan,
        rows: withChecks(rowsOf(a.plan), a.result),
        ending: 'readOnly',
        result: a.result,
        error: a.error,
      }
    case 'apply':
      if (state.phase !== 'preview') return state
      return { ...state, phase: 'applying', refused: undefined }
    case 'progress':
      if (state.phase !== 'applying') return state
      return { ...state, rows: withProgress(state.rows, a.event) }
    case 'aborted':
      if (state.phase !== 'applying') return state
      // An item still in progress has no known end: it is not shown running.
      return {
        ...state,
        phase: 'finished',
        ending: 'ended',
        endedReason: a.reason,
        rows: state.rows.map((r) => (r.progress === 'started' ? { ...r, progress: 'waiting' } : r)),
      }
    case 'applied': {
      if (state.phase !== 'applying') return state
      const o = a.outcome
      if (o.ok) return { ...state, phase: 'finished', ending: 'result', result: o.value, rows: withChecks(state.rows, o.value) }
      if (o.unknown) return { ...state, phase: 'finished', ending: 'unknown', error: o.error }
      if (o.status !== undefined) return { ...state, phase: 'preview', refused: { error: o.error, status: o.status } }
      return {
        ...state,
        phase: 'finished',
        ending: 'error',
        error: o.error,
        result: o.result,
        rows: withChecks(state.rows, o.result),
      }
    }
  }
}

// PlanBody is the plan route's body: a stored plan's id, or a read-only
// plan's read-back result.
export interface PlanBody {
  id?: string
  plan: Plan
  result?: OpResult
}

// AfterPlan is what the dialog does with a plan's outcome: the action to
// dispatch, the stored plan to keep for Apply or Cancel, or the stored plan
// to drop because the dialog stopped the planning.
export interface AfterPlan {
  action?: RunAction
  store?: string
  drop?: string
}

// afterPlan maps a plan's outcome to the dialog's next step. An aborted plan
// (Cancel, Escape, close or unmount) changes nothing and stores nothing; a
// plan stored before the abort arrived is dropped.
export function afterPlan(p: PlanOutcome<PlanBody>, aborted: boolean): AfterPlan {
  if (aborted) return p.ok && p.value.id ? { drop: p.value.id } : {}
  if (p.ok) {
    if (p.value.id) return { store: p.value.id, action: { type: 'planned', id: p.value.id, plan: p.value.plan } }
    return { action: { type: 'readBack', plan: p.value.plan, result: p.value.result ?? {} } }
  }
  if (p.plan && p.result) return { action: { type: 'readBack', plan: p.plan as Plan, result: p.result as OpResult, error: p.error } }
  return { action: { type: 'planFailed', error: p.error } }
}

// applyAtOnce is the stored plan to apply without a preview stop: only when
// the opener asks for none and the plan itself asks no confirmation.
export function applyAtOnce(confirm: boolean, next: AfterPlan): string | undefined {
  if (confirm || next.action?.type !== 'planned') return undefined
  return next.action.plan.confirm === false ? next.action.id : undefined
}

// elapsedSeconds is the whole seconds from start to now, never negative.
export function elapsedSeconds(start: number | undefined, now: number): number {
  return start === undefined ? 0 : Math.max(0, Math.floor((now - start) / 1000))
}

// changingCount is the number of plan items that change something.
export function changingCount(plan: Plan): number {
  return (plan.steps ?? []).filter((s) => !s.noChange).length
}

export type SummaryKind = 'confirmed' | 'notConfirmed' | 'failed' | 'unknown' | 'ended'

export interface Summary {
  kind: SummaryKind
  text: string
}

export const unknownSummary = 'The outcome is unknown: the apply ended without a result. Read the status again to see what changed.'

// endedSummary is the outcome line of an apply the dialog aborted while a
// login waited.
export function endedSummary(reason: string): string {
  return `The login was ended (${reason}). The server cancels the apply and removes the login container; items done before stay. Read the status to see the login state.`
}

// summary is the outcome line of a finished dialog. Only a read-back can
// confirm: a done progress event never counts.
export function summary(state: RunState): Summary | null {
  if (state.phase !== 'finished') return null
  if (state.ending === 'unknown') return { kind: 'unknown', text: unknownSummary }
  if (state.ending === 'ended') return { kind: 'ended', text: endedSummary(state.endedReason ?? 'ended') }
  if (!state.result) return { kind: 'failed', text: 'Failed. The items were not read back.' }
  const checks = state.result.items ?? []
  const confirmed = checks.filter((c) => c.confirmed).length
  if (state.error || confirmed < checks.length) {
    return { kind: 'notConfirmed', text: `Not confirmed: ${confirmed} of ${checks.length} items confirmed by the read-back.` }
  }
  return {
    kind: 'confirmed',
    text:
      checks.length === 0
        ? 'Confirmed by the read-back.'
        : checks.length === 1
          ? 'Confirmed: the item was read back.'
          : `Confirmed: all ${checks.length} items were read back.`,
  }
}

// readBackNotes are the notes to show after the read-back: the result's, or
// the plan's when the result has none.
export function readBackNotes(state: RunState): string[] {
  const r = state.result?.notes ?? []
  return r.length > 0 ? r : (state.plan?.notes ?? [])
}
