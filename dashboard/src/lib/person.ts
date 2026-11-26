// Pure logic of the person view: the image state of a repository and the
// updates of its Issues, and the counts of its summary tiles.

import { applyChanges, runBucket, stageOf, type Change, type RunBucket } from '@/lib/issue'
import type { ApiError, HostReport, LatestImage, PersonView, RepoImage, StatusIssue } from '@/lib/status'

export type ImageTone = 'neutral' | 'success' | 'warning' | 'danger'

export type ImageKind = 'error' | 'none' | 'peer' | 'noPin' | 'latest' | 'notLatest' | 'unknown' | 'otherSet'

export interface ImageState {
  kind: ImageKind
  tone: ImageTone
  label: string
  detail?: string
}

const text = (e: ApiError) => `${e.code}: ${e.message}`

// imageState describes whether a repository's workflows use the latest
// image. Every state is text; nothing is diagnosed beyond the data.
export function imageState(image: RepoImage, latest: LatestImage): ImageState {
  if (image.error) return { kind: 'error', tone: 'danger', label: 'Image could not be read', detail: text(image.error) }
  switch (image.set) {
    case 'none':
      return { kind: 'none', tone: 'neutral', label: 'No Nibfin workflows' }
    case 'peer':
      return { kind: 'peer', tone: 'neutral', label: 'Peer set: no image pin' }
    case 'execution':
      if (image.ref === null) return { kind: 'noPin', tone: 'warning', label: 'No valid image pin' }
      if (image.latest === true) return { kind: 'latest', tone: 'success', label: 'Latest image' }
      if (image.latest === false) return { kind: 'notLatest', tone: 'warning', label: 'Not the latest image' }
      return {
        kind: 'unknown',
        tone: 'neutral',
        label: 'Latest image unknown',
        ...(latest.error ? { detail: text(latest.error) } : {}),
      }
    default:
      return { kind: 'otherSet', tone: 'neutral', label: `Unknown workflow set ${image.set ?? ''}`.trim() }
  }
}

// applyPersonChanges returns view with the confirmed changes applied to the
// Issues of every repository.
export function applyPersonChanges(view: PersonView, changes: readonly Change[]): PersonView {
  if (changes.length === 0 || !view.repos) return view
  return { ...view, repos: view.repos.map((r) => (r.issues ? { ...r, issues: applyChanges(r.issues, changes) } : r)) }
}

// keepSelection drops the selected Issue URLs that view no longer lists.
export function keepSelection(selected: ReadonlySet<string>, view: PersonView): Set<string> {
  const listed = new Set((view.repos ?? []).flatMap((r) => (r.issues ?? []).map((i) => i.url)))
  return new Set([...selected].filter((u) => listed.has(u)))
}

// replaceIssue returns view with repo's Issue of the same URL replaced by
// issue; a view that does not list it is returned unchanged.
export function replaceIssue(view: PersonView, repo: string, issue: StatusIssue): PersonView {
  let found = false
  const repos = (view.repos ?? []).map((r) => {
    if (r.repo.toLowerCase() !== repo.toLowerCase() || !r.issues?.some((i) => i.url === issue.url)) return r
    found = true
    return { ...r, issues: r.issues.map((i) => (i.url === issue.url ? issue : i)) }
  })
  return found ? { ...view, repos } : view
}

// needsYou reports whether an Issue waits on a person: it has pending or
// pause.
export function needsYou(issue: StatusIssue): boolean {
  return issue.pending || issue.pause
}

function listed(view: PersonView): StatusIssue[] {
  return (view.repos ?? []).flatMap((r) => r.issues ?? [])
}

export interface StageCounts {
  total: number
  shape: number
  deliver: number
  // other is every listed Issue in neither stage, such as one already
  // labelled delivered, so the parts always add up to total.
  other: number
}

// stageCounts counts the listed Issues by their stage, from labels only.
export function stageCounts(view: PersonView): StageCounts {
  const out: StageCounts = { total: 0, shape: 0, deliver: 0, other: 0 }
  for (const i of listed(view)) {
    out.total++
    const s = stageOf(i.labels)
    if (s === 'shape') out.shape++
    else if (s === 'deliver') out.deliver++
    else out.other++
  }
  return out
}

// needsYouCount is the number of listed Issues with pending or pause.
export function needsYouCount(view: PersonView): number {
  return listed(view).filter(needsYou).length
}

export interface RunCounts extends Record<RunBucket, number> {
  total: number
  // incomplete is true when the runs of some Issue failed to read, were
  // truncated or were not read: the counts are then a lower bound.
  incomplete: boolean
}

// runCounts counts the conclusions of the listed Issues' runs in the data.
export function runCounts(view: PersonView): RunCounts {
  const out: RunCounts = { total: 0, running: 0, succeeded: 0, failed: 0, skipped: 0, incomplete: false }
  for (const i of listed(view)) {
    if (i.runsError || i.runsTruncated || i.runs === undefined || i.runs === null) out.incomplete = true
    for (const r of i.runs ?? []) {
      out.total++
      out[runBucket(r)]++
    }
  }
  return out
}

export interface RunnerCounts {
  // github says whether online and busy come from GitHub's Runner list:
  // 'error' when it could not be read, 'needs-admin' when the gh login is
  // not admin of the Runners' repository. Only 'read' counts are measured.
  github: 'read' | 'error' | 'needs-admin'
  online: number
  busy: number
  // usedKB and totalKB are the host's memory in use and in total; null when
  // the memory was not read.
  usedKB: number | null
  totalKB: number | null
}

// runnerCounts counts this host's Runner slots the way the runner view
// labels them: online when GitHub says online, busy when also busy.
export function runnerCounts(host: HostReport): RunnerCounts {
  const r = host.runners
  const slots = r.runners ?? []
  const online = slots.filter((s) => s.status === 'online')
  const m = host.memory
  const read = !m.error && m.totalKB > 0
  return {
    // A read error wins over needs-admin, as in the runner view.
    github: r.githubError ? 'error' : r.githubNeedsAdmin ? 'needs-admin' : 'read',
    online: online.length,
    busy: online.filter((s) => s.busy).length,
    usedKB: read ? Math.max(0, m.totalKB - m.availableKB) : null,
    totalKB: read ? m.totalKB : null,
  }
}

// filterNeedsYou keeps only the Issues with pending or pause, and drops the
// repositories left without one.
export function filterNeedsYou(view: PersonView): PersonView {
  const repos = (view.repos ?? [])
    .map((r) => ({ ...r, issues: (r.issues ?? []).filter(needsYou) }))
    .filter((r) => r.issues.length > 0)
  return { ...view, repos }
}
