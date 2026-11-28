// Issue logic of the shared Dashboard Issue element (#350): the stage from
// labels, the permission gate, the views of runs, blockers and the pending
// reason, and pause/resume through the plan and apply routes.

import { api, applyPlan, deletePlan, type ApiError } from '@/lib/session'
import type { Blocker, Repair, RepairState, RepoPermission, Run, StatusIssue } from '@/lib/status'
import type { Loading } from '@/lib/partial'

type Fetch = typeof fetch

export type Stage = 'none' | 'scope' | 'shape' | 'deliver' | 'done'

export const stageLabels: Record<Stage, string> = {
  none: 'Not in Nibfin',
  scope: 'Scope',
  shape: 'Shape',
  deliver: 'Deliver',
  done: 'Done',
}

function hasLabel(labels: readonly string[] | null | undefined, name: string): boolean {
  return (labels ?? []).some((l) => l.toLowerCase() === name)
}

// stageOf reads the Nibfin stage from the labels only. Without scope an
// Issue with request is in the Scope stage and any other is not in Nibfin;
// with scope, delivered wins over shaped, and shaped over scope.
export function stageOf(labels: readonly string[] | null | undefined): Stage {
  if (!hasLabel(labels, 'scope')) return hasLabel(labels, 'request') ? 'scope' : 'none'
  if (hasLabel(labels, 'delivered')) return 'done'
  if (hasLabel(labels, 'shaped')) return 'deliver'
  return 'shape'
}

export type Access = { enabled: true } | { enabled: false; reason: string }

// GitHub's RepositoryPermission values at triage or higher. TRIAGE_PLUS is
// the deprecated name of MODERATE.
const changing = new Set(['TRIAGE', 'TRIAGE_PLUS', 'MODERATE', 'WRITE', 'MAINTAIN', 'ADMIN'])
const below = new Set(['READ'])

// accessOf is the account's access on one repository as read: its
// permission, or that it could not be read or is still being read.
export function accessOf(p: RepoPermission, loading: Loading): RepoPermission {
  if (loading.has('permission')) return { permission: null, permissionLoading: true }
  return { permission: p.permission, ...(p.permissionError ? { permissionError: p.permissionError } : {}) }
}

// changeAccess says whether the account may pause or resume Issues of repo.
// GitHub still authorizes every write; this only enables the action.
export function changeAccess(access: RepoPermission, repo: string): Access {
  if (access.permissionLoading) return { enabled: false, reason: `Reading the permission on ${repo}…` }
  if (access.permissionError) {
    const e = access.permissionError
    return { enabled: false, reason: `Permission on ${repo} could not be read: ${e.code}: ${e.message}.` }
  }
  const p = access.permission
  if (p === null || p === undefined) {
    return { enabled: false, reason: `GitHub reported no permission for this account on ${repo}.` }
  }
  const v = p.toUpperCase()
  if (changing.has(v)) return { enabled: true }
  if (below.has(v)) {
    return { enabled: false, reason: `Needs triage permission or higher; this account has ${v} on ${repo}.` }
  }
  return { enabled: false, reason: `Unknown permission ${p} on ${repo}.` }
}

// bulkAccess is enabled only when every selected Issue's repository is; it
// names each repository that is not.
export function bulkAccess(selected: readonly { repo: string; access: RepoPermission }[]): Access {
  if (selected.length === 0) return { enabled: false, reason: 'Select one or more Issues.' }
  const reasons = new Map<string, string>()
  for (const s of selected) {
    const a = changeAccess(s.access, s.repo)
    if (!a.enabled && !reasons.has(s.repo)) reasons.set(s.repo, a.reason)
  }
  if (reasons.size === 0) return { enabled: true }
  return { enabled: false, reason: [...reasons.values()].join(' ') }
}

export type RunsNote = { kind: 'failed'; error: ApiError } | { kind: 'truncated' } | { kind: 'notRead' }

export interface RunsView {
  // runs are newest first.
  runs: Run[]
  notes: RunsNote[]
  // empty is true only when the runs were read in full and there are none.
  empty: boolean
}

export function runsView(issue: StatusIssue): RunsView {
  const runs = [...(issue.runs ?? [])].sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt))
  const notes: RunsNote[] = []
  if (issue.runsError) notes.push({ kind: 'failed', error: issue.runsError })
  else if (issue.runs === undefined || issue.runs === null) notes.push({ kind: 'notRead' })
  if (issue.runsTruncated) notes.push({ kind: 'truncated' })
  return { runs, notes, empty: runs.length === 0 && notes.length === 0 }
}

export type StepState = 'complete' | 'current' | 'upcoming'

export interface StageStep {
  stage: Exclude<Stage, 'none'>
  label: string
  state: StepState
}

const trackStages = ['scope', 'shape', 'deliver', 'done'] as const

// stageSteps is the Scope → Shape → Deliver → Done track of a stage. Steps before
// the stage are complete and the stage is current; Done is current once
// delivered. Without scope every step is upcoming.
export function stageSteps(stage: Stage): StageStep[] {
  const at = stage === 'none' ? -1 : trackStages.indexOf(stage)
  return trackStages.map((s, i) => ({
    stage: s,
    label: stageLabels[s],
    state: at < 0 || i > at ? 'upcoming' : i === at ? 'current' : 'complete',
  }))
}

export type RunBucket = 'succeeded' | 'failed' | 'skipped' | 'running'

export const runBuckets: readonly RunBucket[] = ['succeeded', 'failed', 'skipped', 'running']

export const runBucketLabels: Record<RunBucket, string> = {
  succeeded: 'Succeeded',
  failed: 'Failed',
  skipped: 'Skipped or cancelled',
  running: 'Running',
}

// runBucket groups a run by its conclusion for the run strip: not finished
// (queued and waiting included) is running; success succeeded; failure,
// timed_out and startup_failure failed; everything else skipped or
// cancelled. The exact conclusion stays in each bar's name.
export function runBucket(run: Pick<Run, 'conclusion'>): RunBucket {
  switch (run.conclusion) {
    case null:
      return 'running'
    case 'success':
      return 'succeeded'
    case 'failure':
    case 'timed_out':
    case 'startup_failure':
      return 'failed'
    default:
      return 'skipped'
  }
}

// recentRuns is the newest max runs of the Issue, oldest first.
export function recentRuns(issue: StatusIssue, max = 20): Run[] {
  return runsView(issue).runs.slice(0, max).reverse()
}

export interface BlockersView {
  open: Blocker[]
  failed?: ApiError
  notRead: boolean
}

// blockersView keeps the open blockers. Zero open blockers that were read
// show nothing; a failed or missing read is labelled.
export function blockersView(issue: StatusIssue): BlockersView {
  const open = (issue.blockedBy ?? []).filter((b) => b.state.toUpperCase() === 'OPEN')
  if (issue.blockedByError) return { open, failed: issue.blockedByError, notRead: false }
  return { open, notRead: issue.blockedBy === undefined || issue.blockedBy === null }
}

export type PendingView = { kind: 'link'; url: string } | { kind: 'failed'; error: ApiError } | { kind: 'notFound' }

export function pendingView(issue: StatusIssue): PendingView {
  if (issue.pendingComment) return { kind: 'link', url: issue.pendingComment }
  if (issue.pendingCommentError) return { kind: 'failed', error: issue.pendingCommentError }
  return { kind: 'notFound' }
}

export type RepairTone = 'info' | 'warning'

export interface RepairLook {
  // state is the record's state; unknown for a state this Dashboard does
  // not know, which is shown but never answered.
  state: RepairState | 'unknown'
  label: string
  tone: RepairTone
}

export const repairLabels: Record<RepairState, string> = {
  repairing: 'Repairing',
  choose: 'Waiting for your choice',
  act: 'Needs your action',
}

// repairLook is the badge of a repair state (Proposal R9.52). Repairing is
// Nibfin's own work; the other two wait on a person.
export function repairLook(repair: Pick<Repair, 'state'>): RepairLook {
  const s = repair.state
  if (s === 'repairing') return { state: s, label: repairLabels[s], tone: 'info' }
  if (s === 'choose' || s === 'act') return { state: s, label: repairLabels[s], tone: 'warning' }
  return { state: 'unknown', label: `Unknown repair state ${String(s)}`, tone: 'warning' }
}

// answerable says whether the Repair dialog is offered: the two states
// that wait on a person (Proposal R9.53).
export function answerable(issue: StatusIssue): boolean {
  const s = issue.pending ? issue.repair?.state : undefined
  return s === 'choose' || s === 'act'
}

// pendingLabel is the text of a pending Issue's flag: its repair state when
// it was read, or Pending.
export function pendingLabel(issue: StatusIssue): string {
  return issue.repair ? repairLook(issue.repair).label : 'Pending'
}

// recommendedOption is the number of the recommended option of a choose
// record, or else its first option's; undefined without options.
export function recommendedOption(repair: Repair): number | undefined {
  const options = repair.options ?? []
  return (options.find((o) => o.recommended) ?? options[0])?.number
}

// safeLink returns url only when it is an absolute http or https URL. The
// repair record's links are partly agent-written; any other scheme is shown
// as text, never as a link.
export function safeLink(url: string | undefined): string | undefined {
  if (!url) return undefined
  try {
    const u = new URL(url)
    return u.protocol === 'https:' || u.protocol === 'http:' ? u.href : undefined
  } catch {
    return undefined
  }
}

export type LatestRunView = { kind: 'run'; run: Run } | { kind: 'failed'; error: ApiError } | { kind: 'notRead' } | { kind: 'none' }

// latestRun is the Issue's newest Nibfin run, or why there is none to link.
export function latestRun(issue: StatusIssue): LatestRunView {
  const v = runsView(issue)
  if (v.runs[0]) return { kind: 'run', run: v.runs[0] }
  const failed = v.notes.find((n) => n.kind === 'failed')
  if (failed) return { kind: 'failed', error: failed.error }
  if (v.notes.some((n) => n.kind === 'notRead')) return { kind: 'notRead' }
  return { kind: 'none' }
}

export type ChangeResult = 'paused' | 'resumed'

export interface Change {
  url: string
  result: ChangeResult
}

// applyChanges returns issues with the confirmed changes applied: paused adds
// pause; resumed removes pause and pending. Other Issues are kept as they are.
export function applyChanges<T extends StatusIssue>(issues: readonly T[], changes: readonly Change[]): T[] {
  const byURL = new Map(changes.map((c) => [c.url.toLowerCase(), c.result]))
  return issues.map((is) => {
    const r = byURL.get(is.url.toLowerCase())
    if (!r) return is
    const labels = is.labels ?? []
    if (r === 'paused') {
      return { ...is, pause: true, labels: hasLabel(labels, 'pause') ? labels : [...labels, 'pause'] }
    }
    return {
      ...is,
      pause: false,
      pending: false,
      labels: labels.filter((l) => !['pause', 'pending'].includes(l.toLowerCase())),
    }
  })
}

export type Action = 'pause' | 'resume'

export type ChangeOutcome =
  | { state: 'done'; changes: Change[] }
  | { state: 'failed'; error: ApiError }
  // unknown: the apply was sent but its result never arrived; the labels may
  // or may not have changed.
  | { state: 'unknown'; error: ApiError }

// changeIssues pauses or resumes urls in one operation: a plan, then an apply
// without confirmation, whose read-back decides the result.
export async function changeIssues(action: Action, urls: readonly string[], fetcher: Fetch = fetch): Promise<ChangeOutcome> {
  const planned = await api<{ id?: string }>(`/api/commands/${action}/plan`, { issues: urls }, fetcher)
  if (!planned.ok) return { state: 'failed', error: planned.error }
  const id = planned.value.id
  if (!id) return { state: 'failed', error: { code: 'error', message: `the ${action} plan returned no id` } }

  const applied = await applyPlan<Record<string, unknown>>(id, fetcher)
  if (!applied.ok) {
    if (applied.unknown) return { state: 'unknown', error: applied.error }
    // A refused apply (operation_running, confirmation_required) leaves the
    // plan stored; drop it.
    if (applied.status !== undefined || applied.error.code === 'confirmation_required') await deletePlan(id, fetcher)
    return { state: 'failed', error: applied.error }
  }
  const changes: Change[] = []
  for (const [url, result] of Object.entries(applied.value ?? {})) {
    if (result === 'paused' || result === 'resumed') changes.push({ url, result })
  }
  return { state: 'done', changes }
}
