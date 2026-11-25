// Partial status reads: the sections a status read sends while others are
// still being read (the partial lines of the status read route and the
// partial events of the live stream). A view shows a partial only while no
// complete read is on screen, and each section still loading shows its own
// loading state instead of its value.

import type { HostReport, PersonView, RepoView } from '@/lib/status'

// Loading names what a read is still reading: Result keys (host,
// latestImage, issues, image, nibfinRun, permission, nibfinRepos, user,
// repos) and Issue fields (pendingComment, blockedBy, runs). It is empty
// for a complete read.
export type Loading = ReadonlySet<string>

export const complete: Loading = new Set()

// Shown is the read a view shows: its data and what of it is still loading.
export interface Shown<T> {
  data?: T
  loading: Loading
}

// isComplete reports whether shown holds a complete read.
export function isComplete<T>(shown: Shown<T>): boolean {
  return shown.data !== undefined && shown.loading.size === 0
}

// takePartial returns what to show after a partial read: the partial,
// unless a complete read is already on screen, which a partial never
// replaces (a Refresh keeps the data shown until its result arrives).
export function takePartial<T>(shown: Shown<T>, data: T, loading: readonly string[]): Shown<T> {
  if (isComplete(shown)) return shown
  return { data, loading: new Set(loading) }
}

// dropPartial returns what to show after a read failed: a complete read
// stays; a partial one goes, since its loading sections never arrive.
export function dropPartial<T>(shown: Shown<T>): Shown<T> {
  return isComplete(shown) ? shown : { loading: complete }
}

const noHost: HostReport = { host: {}, memory: { totalKB: 0, availableKB: 0 }, runners: { runners: null }, providers: null }

function obj(v: unknown): Record<string, unknown> {
  return v !== null && typeof v === 'object' ? (v as Record<string, unknown>) : {}
}

// fillRepoView gives a partial repository read the shape of a complete
// one. A missing section gets an empty value, which the view never shows:
// it shows that section's loading state.
export function fillRepoView(partial: unknown, repo: string): RepoView {
  const p = obj(partial) as Partial<RepoView>
  return {
    host: noHost,
    latestImage: {},
    image: { ref: null, latest: null },
    issues: [],
    nibfinRun: { usesNibfin: false, succeeded: false },
    permission: null,
    nibfinRepos: null,
    ...p,
    repo: typeof p.repo === 'string' ? p.repo : repo,
  }
}

// fillPersonView gives a partial person read the shape of a complete one,
// as fillRepoView does.
export function fillPersonView(partial: unknown): PersonView {
  const p = obj(partial) as Partial<PersonView>
  return {
    host: noHost,
    latestImage: {},
    repos: [],
    truncated: false,
    nibfinRepos: null,
    ...p,
    githubHost: typeof p.githubHost === 'string' ? p.githubHost : '',
  }
}

// labels name the keys of Loading for a "Still reading" line, in reading
// order.
const labels: [string, string][] = [
  ['user', 'account'],
  ['repos', 'Issues'],
  ['issues', 'Issues'],
  ['pendingComment', 'pending comments'],
  ['blockedBy', 'blockers'],
  ['permission', 'permission'],
  ['image', 'image'],
  ['nibfinRun', 'Nibfin run'],
  ['nibfinRepos', 'repository list'],
  ['host', 'host'],
  ['latestImage', 'latest image'],
  ['runs', 'runs'],
]

// loadingLabels returns the labels of the keys of only still loading, in
// reading order.
export function loadingLabels(loading: Loading, only: readonly string[]): string[] {
  const out: string[] = []
  for (const [k, label] of labels) {
    if (only.includes(k) && loading.has(k) && !out.includes(label)) out.push(label)
  }
  return out
}
