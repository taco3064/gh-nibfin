// fileRequest turns one sentry-issue dispatch into a `request` Issue, or one
// comment on the open request with the same title. Every write carries this
// run's marker, so a re-run attempt of the same run finds what the earlier
// attempt wrote instead of writing it twice.

import type { SentryIssue } from '../payload.ts'
import { commentBody, hasMarker, issueBody, title } from './body.ts'
import type { Client } from './http.ts'
import { verify, type Verification } from './verify.ts'

export const ASSIGNEE = 'taco3064'
export const DAILY_CAP = 5
export const MAX_PAGES = 50
const TITLE_PREFIX = 'Website error: '

export interface FileDeps {
  repository: string
  runId: string
  github: Client
  sentry?: Client
  now: () => number
}

export type Decision = 'filed' | 'already filed' | 'commented' | 'already commented' | 'daily cap reached'

export interface Outcome {
  decision: Decision
  number?: number
  url?: string
  verification?: Verification
  problems: string[] // read-back mismatches; non-empty fails the run
}

interface GhIssue {
  number: number
  title: string
  state: string
  body: string | null
  html_url: string
  created_at: string
  labels: { name: string }[]
  assignees: { login: string }[]
  pull_request?: unknown
}

function isIssue(v: unknown): v is GhIssue {
  if (typeof v !== 'object' || v === null) return false
  const o = v as Record<string, unknown>
  return typeof o.number === 'number' && typeof o.title === 'string' && typeof o.state === 'string'
}

function labelNames(i: GhIssue): string[] {
  return Array.isArray(i.labels) ? i.labels.map((l) => (typeof l === 'string' ? l : l?.name)).filter((n): n is string => typeof n === 'string') : []
}

// openRequests lists the open requests with exactly this title, lowest number first.
async function openRequests(deps: FileDeps, wanted: string): Promise<GhIssue[]> {
  const found: GhIssue[] = []
  for await (const page of deps.github.pages(`repos/${deps.repository}/issues?state=open&labels=request&per_page=100`, MAX_PAGES)) {
    for (const i of page) if (isIssue(i) && i.pull_request === undefined && i.title === wanted) found.push(i)
  }
  return found.sort((a, b) => a.number - b.number)
}

async function commentedByRun(deps: FileDeps, number: number): Promise<boolean> {
  // Newest page first: an earlier attempt's comment is among the latest. No
  // time bound; reaching the page limit throws, so the run never guesses.
  for await (const page of deps.github.pagesNewestFirst(`repos/${deps.repository}/issues/${number}/comments?per_page=100`, MAX_PAGES)) {
    if (page.some((c) => typeof c === 'object' && c !== null && hasMarker((c as { body?: unknown }).body, deps.runId))) return true
  }
  return false
}

async function readIssue(deps: FileDeps, number: number): Promise<GhIssue> {
  const body = (await deps.github.get(`repos/${deps.repository}/issues/${number}`))!.body
  if (!isIssue(body)) throw new Error(`GET repos/${deps.repository}/issues/${number}: unexpected answer`)
  return body
}

function startOfUTCDay(ms: number): string {
  const d = new Date(ms)
  return new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate())).toISOString()
}

async function filedToday(deps: FileDeps): Promise<number> {
  const since = startOfUTCDay(deps.now())
  let n = 0
  for await (const page of deps.github.pages(`repos/${deps.repository}/issues?state=all&labels=request&creator=${ASSIGNEE}&since=${since}&per_page=100`, MAX_PAGES)) {
    for (const i of page) {
      if (isIssue(i) && i.pull_request === undefined && i.title.startsWith(TITLE_PREFIX) && typeof i.created_at === 'string' && i.created_at >= since) n++
    }
  }
  return n
}

function readBack(i: GhIssue, wanted: string, pause: boolean): string[] {
  const problems: string[] = []
  const labels = labelNames(i)
  if (i.title !== wanted) problems.push('title differs')
  if (i.state !== 'open') problems.push('not open')
  if (!labels.includes('request')) problems.push('no request label')
  if (pause && !labels.includes('pause')) problems.push('no pause label')
  if (!Array.isArray(i.assignees) || !i.assignees.some((a) => a?.login === ASSIGNEE)) problems.push(`not assigned to ${ASSIGNEE}`)
  return problems
}

export async function fileRequest(issue: SentryIssue, deps: FileDeps): Promise<Outcome> {
  const wanted = title(issue)
  const repo = deps.repository
  let verification: Verification | undefined
  const verified = async () => (verification ??= await verify(issue, { repository: repo, github: deps.github, sentry: deps.sentry }))

  const requests = await openRequests(deps, wanted)
  const filed = requests.find((i) => hasMarker(i.body, deps.runId))
  if (filed !== undefined) {
    return { decision: 'already filed', number: filed.number, url: filed.html_url, problems: readBack(await readIssue(deps, filed.number), wanted, false) }
  }
  const open = requests[0]
  if (open !== undefined) {
    if (await commentedByRun(deps, open.number)) {
      return { decision: 'already commented', number: open.number, url: open.html_url, problems: [] }
    }
    const v = await verified()
    const text = commentBody(issue, v, deps.runId)
    await deps.github.retry(
      () => deps.github.once('POST', `repos/${repo}/issues/${open.number}/comments`, { body: text }),
      async () => ((await commentedByRun(deps, open.number)) ? { status: 201, body: null, links: {} } : undefined),
    )
    const now = await readIssue(deps, open.number)
    if (now.state === 'open') {
      const problems = labelNames(now).includes('request') ? [] : ['no request label']
      return { decision: 'commented', number: open.number, url: open.html_url, verification: v, problems }
    }
    // Closed between the list and the comment: file a new request below.
  }

  if ((await filedToday(deps)) >= DAILY_CAP) return { decision: 'daily cap reached', problems: [] }

  const v = await verified()
  const pause = !v.verified
  const created = await deps.github.retry(
    async () => {
      const a = await deps.github.once('POST', `repos/${repo}/issues`, {
        title: wanted,
        body: issueBody(issue, v, deps.runId),
        labels: pause ? ['request', 'pause'] : ['request'],
        assignees: [ASSIGNEE],
      })
      if (!isIssue(a.body)) throw new Error(`POST repos/${repo}/issues: unexpected answer`)
      return a.body
    },
    async () => {
      return (await openRequests(deps, wanted)).find((i) => hasMarker(i.body, deps.runId))
    },
  )
  return { decision: 'filed', number: created.number, url: created.html_url, verification: v, problems: readBack(await readIssue(deps, created.number), wanted, pause) }
}

export function summaryLine(issue: SentryIssue, o: Outcome): string {
  const v = o.verification
  const check = v === undefined ? '' : v.verified ? '; verified' : `; unverified: ${v.failed}`
  if (o.decision === 'daily cap reached') return `daily cap of ${DAILY_CAP} reached; not filed: ${title(issue)} ${issue.permalink}`
  return `#${o.number} ${o.url} ${o.decision}${check}${o.problems.length > 0 ? `; read-back failed: ${o.problems.join(', ')}` : ''}`
}
