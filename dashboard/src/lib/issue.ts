// Issue logic of the shared Dashboard Issue element (#350): the stage from
// labels, the permission gate, the views of runs, blockers and the pending
// reason, and pause/resume through the plan and apply routes.

import { api, applyPlan, deletePlan, type ApiError } from '@/lib/session'
import type { Blocker, RepoPermission, Run, StatusIssue } from '@/lib/status'

type Fetch = typeof fetch

export type Stage = 'none' | 'shape' | 'deliver' | 'done'

export const stageLabels: Record<Stage, string> = {
  none: 'Not in Nibfin',
  shape: 'Shape',
  deliver: 'Deliver',
  done: 'Done',
}

function hasLabel(labels: readonly string[] | null | undefined, name: string): boolean {
  return (labels ?? []).some((l) => l.toLowerCase() === name)
}

// stageOf reads the Nibfin stage from the labels only. Without scope the
// Issue is not in Nibfin; otherwise delivered wins over shaped, and shaped
// over scope.
export function stageOf(labels: readonly string[] | null | undefined): Stage {
  if (!hasLabel(labels, 'scope')) return 'none'
  if (hasLabel(labels, 'delivered')) return 'done'
  if (hasLabel(labels, 'shaped')) return 'deliver'
  return 'shape'
}

export type Access = { enabled: true } | { enabled: false; reason: string }

// GitHub's RepositoryPermission values at triage or higher. TRIAGE_PLUS is
// the deprecated name of MODERATE.
const changing = new Set(['TRIAGE', 'TRIAGE_PLUS', 'MODERATE', 'WRITE', 'MAINTAIN', 'ADMIN'])
const below = new Set(['READ'])

// changeAccess says whether the account may pause or resume Issues of repo.
// GitHub still authorizes every write; this only enables the action.
export function changeAccess(access: RepoPermission, repo: string): Access {
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
