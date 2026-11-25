// Text and digits of the Summary panel.

import { ownerRepo } from '@/lib/status'

import type { RepoData, Summary } from '../../worker/messages'

export const showcaseStart: Summary['since'] = '2026-11-21T00:00:00+08:00'

// sinceLabel is the start date as "21 Nov 2026", in the showcase's time
// zone.
export function sinceLabel(since: string): string {
  return new Intl.DateTimeFormat('en-GB', { day: 'numeric', month: 'short', year: 'numeric', timeZone: 'Asia/Taipei' }).format(new Date(since))
}

// repoNames joins the repositories' names as "a, b and c".
export function repoNames(repos: RepoData[]): string {
  const names = repos.map((r) => ownerRepo(r.repo).split('/').pop() ?? r.repo)
  if (names.length <= 1) return names.join('')
  return `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`
}

// digits gives the columns of a rolling number: one per digit, with ","
// between thousands. null (not read yet) gives none.
export function digits(value: number | null): string[] {
  if (value === null || !Number.isFinite(value)) return []
  return [...Math.max(0, Math.floor(value)).toLocaleString('en-US')]
}
