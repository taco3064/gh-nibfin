// The Worker's stored state: per probe its ETags, last parsed result, due
// time and last error, and the LiveData last built from them. It is plain
// JSON, kept under one Durable Object storage key.

import type { ApiError, Blocker, LiveData, Run } from './messages'

export const repos = ['taco3064/nibfin-core', 'taco3064/gh-nibfin', 'taco3064/nibfin-e2e'] as const
export type Repo = (typeof repos)[number]

export const workflows = ['nibfin-issue.yml', 'nibfin-shared-runner.yml'] as const
export type Workflow = (typeof workflows)[number]

export const since = '2026-11-21T00:00:00+08:00'

// The Summary queries, each over the three repositories. delivered has no
// created: qualifier: an Issue created before the date and closed with
// delivered after it counts.
export const createdQuery = `repo:taco3064/nibfin-core repo:taco3064/gh-nibfin repo:taco3064/nibfin-e2e is:issue created:>=${since}`
export const deliveredQuery = `repo:taco3064/nibfin-core repo:taco3064/gh-nibfin repo:taco3064/nibfin-e2e is:issue is:closed label:delivered closed:>=${since}`

// trendDays is the length of the delivered trend, today included.
export const trendDays = 7

// trendQuery is deliveredQuery closed since Taipei midnight of start
// (YYYY-MM-DD), the trend's first day.
export function trendQuery(start: string): string {
  return `repo:taco3064/nibfin-core repo:taco3064/gh-nibfin repo:taco3064/nibfin-e2e is:issue is:closed label:delivered closed:>=${start}T00:00:00+08:00`
}

const taipeiOffset = 8 * 3600_000 // Asia/Taipei is UTC+8, without daylight saving time

// taipeiDate is the Asia/Taipei date of an instant as YYYY-MM-DD, or "" for
// an invalid time.
export function taipeiDate(ms: number): string {
  if (!Number.isFinite(ms)) return ''
  return new Date(ms + taipeiOffset).toISOString().slice(0, 10)
}

// addDays moves a YYYY-MM-DD date by n days.
export function addDays(date: string, n: number): string {
  return new Date(Date.parse(`${date}T00:00:00Z`) + n * 86_400_000).toISOString().slice(0, 10)
}

// trendStart is the first day of the trend that ends today in Asia/Taipei.
export function trendStart(now: number): string {
  return addDays(taipeiDate(now), -(trendDays - 1))
}

// Source is anything a check reads: its last error and when it happened.
export interface Source {
  lastError: ApiError | null
  errorAt: number | null
}

// Probe is a conditional read. etags holds one ETag per page.
export interface Probe<T> extends Source {
  etags: (string | null)[]
  result: T | null
  due: number // epoch ms
}

export interface IssueBase {
  number: number
  title: string
  url: string
  labels: string[]
  createdAt: string
}

export interface IssuesResult {
  pages: { issues: IssueBase[]; full: boolean }[]
}

export interface RunsResult {
  missing: boolean // the workflow does not exist in the repository
  runs: Run[]
  full: boolean // the page held 100 runs, so older runs were not read
}

export interface RunnerBase {
  id: number
  name: string
  status: string
  busy: boolean
}

export interface JobBase {
  runnerId: number | null
  status: string
  workflowName: string
  url: string
}

// TrendResult holds the close time of every delivered Issue closed since
// Taipei midnight of start, read over every page of the trend search.
export interface TrendResult {
  start: string
  closedAt: string[]
}

export interface BlockersEntry {
  blockedBy: Blocker[]
  error?: ApiError
}

export interface RepoState {
  issues: Probe<IssuesResult>
  runs: Record<Workflow, Probe<RunsResult>>
  runners: Probe<{ runners: RunnerBase[] }>
  inProgress: Probe<{ ids: number[] }>
  jobs: Source & { result: JobBase[] }
}

export interface State {
  version: 1
  repos: Record<string, RepoState>
  search: { created: Probe<{ total: number }>; delivered: Probe<{ total: number }>; trend: Probe<TrendResult> }
  blockers: Source & { byIssue: Record<string, BlockersEntry> }
  fatal: ApiError | null // a failure no probe could start past, the missing token
  okAt: number | null // the last check every source passed
  data: LiveData | null
  at: number | null // when data last changed
  seq: number // increments with each new data
  sentSeq: number // the seq last broadcast
}

function probe<T>(): Probe<T> {
  return { etags: [], result: null, due: 0, lastError: null, errorAt: null }
}

export function initialState(): State {
  const r: Record<string, RepoState> = {}
  for (const repo of repos) {
    r[repo] = {
      issues: probe(),
      runs: { 'nibfin-issue.yml': probe(), 'nibfin-shared-runner.yml': probe() },
      runners: probe(),
      inProgress: probe(),
      jobs: { result: [], lastError: null, errorAt: null },
    }
  }
  return {
    version: 1,
    repos: r,
    search: { created: probe(), delivered: probe(), trend: probe() },
    blockers: { byIssue: {}, lastError: null, errorAt: null },
    fatal: null,
    okAt: null,
    data: null,
    at: null,
    seq: 0,
    sentSeq: 0,
  }
}

// loadedState fills in what a state stored by an older Worker lacks, so a
// Worker deployed over old storage reads it: the trend probe, added later.
export function loadedState(state: State): State {
  if (!state.search.trend) state.search.trend = probe()
  return state
}

// sources lists every source of the state in a fixed order.
export function sources(state: State): Source[] {
  const out: Source[] = []
  for (const repo of repos) {
    const r = state.repos[repo]
    out.push(r.issues, ...workflows.map((wf) => r.runs[wf]), r.runners, r.inProgress, r.jobs)
  }
  out.push(state.search.created, state.search.delivered, state.blockers, state.search.trend)
  return out
}

// currentErrors returns the state's errors, the newest first.
export function currentErrors(state: State): ApiError[] {
  if (state.fatal) return [state.fatal]
  return sources(state)
    .filter((s) => s.lastError !== null)
    .sort((a, b) => (b.errorAt ?? 0) - (a.errorAt ?? 0))
    .map((s) => s.lastError as ApiError)
}
