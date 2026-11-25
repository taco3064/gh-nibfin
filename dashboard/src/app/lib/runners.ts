// The runner cards of the Live page. A card names a runner only by its
// place in the list: runner names include host names and are never shown.

import type { ApiError, Job, RepoRunners } from '../../worker/messages'

export type RunnerState = 'busy' | 'idle' | 'offline' | 'other'

export interface RunnerCard {
  key: string
  // label is "Runner N", by the runner's place in the Worker's order.
  label: string
  state: RunnerState
  // status is GitHub's status as sent.
  status: string
  job: Job | null
}

// runnerState: offline is GitHub's offline; busy and idle apply to an
// online runner; any other status stays as it is.
export function runnerState(status: string, busy: boolean): RunnerState {
  if (status === 'offline') return 'offline'
  if (status === 'online') return busy ? 'busy' : 'idle'
  return 'other'
}

// runnerCards flattens the runners of every repository in the Worker's
// order. A card carries only the public fields, even when a message still
// has others.
export function runnerCards(runners: RepoRunners[]): RunnerCard[] {
  const out: RunnerCard[] = []
  for (const r of runners) {
    for (const rn of r.runners) {
      out.push({
        key: `${r.repo}:${rn.id}`,
        label: `Runner ${out.length + 1}`,
        state: runnerState(rn.status, rn.busy),
        status: rn.status,
        job: rn.busy ? rn.job : null,
      })
    }
  }
  return out
}

export interface Counts {
  total: number
  busy: number
  idle: number
  offline: number
  other: number
}

export function capacityCounts(cards: RunnerCard[]): Counts {
  const c: Counts = { total: cards.length, busy: 0, idle: 0, offline: 0, other: 0 }
  for (const card of cards) c[card.state]++
  return c
}

// runnerErrors lists the repositories whose runners could not be read.
export function runnerErrors(runners: RepoRunners[]): { repo: string; error: ApiError }[] {
  return runners.flatMap((r) => (r.error ? [{ repo: r.repo, error: r.error }] : []))
}

const ownerRepoPattern = /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/

// safeUrl keeps an https URL from the data, or gives undefined.
export function safeUrl(url: string | null | undefined): string | undefined {
  if (!url || !URL.canParse(url)) return undefined
  return new URL(url).protocol === 'https:' ? url : undefined
}

export type JobTarget = { kind: 'issue'; label: string; url: string } | { kind: 'ref'; label: string } | { kind: 'title'; label: string }

// jobTarget names what a job works on: its Issue, linked on GitHub only
// for a valid OWNER/REPO and number; else its ref; else its run title.
export function jobTarget(job: Job): JobTarget {
  if (job.repo !== null && ownerRepoPattern.test(job.repo) && job.issue !== null && Number.isSafeInteger(job.issue) && job.issue > 0) {
    return { kind: 'issue', label: `${job.repo}#${job.issue}`, url: `https://github.com/${job.repo}/issues/${job.issue}` }
  }
  if (job.ref !== null) return { kind: 'ref', label: job.repo ? `${job.repo}@${job.ref}` : job.ref }
  return { kind: 'title', label: job.title }
}
