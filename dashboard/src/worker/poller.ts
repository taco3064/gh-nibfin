// One check: the due conditional reads of every probe, the follow-up reads
// after a 200, and the rebuilt LiveData. It is a function over the plain
// State and a GitHub client, so tests drive it with fake answers.

import { buildLiveData, issueKey } from './build'
import { GitHubError, noToken, searchPath, type GitHub } from './github'
import type { ApiError, Blocker, Run } from './messages'
import {
  createdQuery,
  currentErrors,
  deliveredQuery,
  repos,
  trendQuery,
  trendStart,
  workflows,
  type BlockersEntry,
  type IssueBase,
  type IssuesResult,
  type JobBase,
  type Probe,
  type RunnerBase,
  type RunsResult,
  type Source,
  type State,
  type TrendResult,
} from './state'

export const minInterval = 30_000
// missingWorkflowInterval is how long a repository without a Nibfin
// workflow waits before that workflow is probed again.
export const missingWorkflowInterval = 600_000
const perPage = 100
const issuesPerQuery = 50

export interface CheckResult {
  state: State
  changed: boolean
  errors: ApiError[]
}

function asError(e: unknown): GitHubError {
  if (e instanceof GitHubError) return e
  return new GitHubError(0, null, { code: 'github_api_failed', message: `the check failed: ${e instanceof Error ? e.message : String(e)}` })
}

function fail(s: Source, e: GitHubError, now: number): void {
  s.lastError = e.apiError
  s.errorAt = now
}

function failProbe(p: Probe<unknown>, e: GitHubError, now: number): void {
  fail(p, e, now)
  p.due = e.retryAfter !== null ? now + e.retryAfter * 1000 : 0
}

function nextDue(now: number, pollInterval: number | null): number {
  return now + Math.max(minInterval, (pollInterval ?? 0) * 1000)
}

function pass(s: Source): void {
  s.lastError = null
  s.errorAt = null
}

// Outcome is one probe's read: unchanged, new (to commit) or failed.
type Outcome<T> = { kind: 'skip' } | { kind: 'same'; poll: number | null } | { kind: 'new'; result: T; etags: (string | null)[]; poll: number | null } | { kind: 'failed'; error: GitHubError }

function commit<T>(p: Probe<T>, o: Outcome<T>, now: number): void {
  switch (o.kind) {
    case 'skip':
      return
    case 'same':
      pass(p)
      p.due = nextDue(now, o.poll)
      return
    case 'new':
      pass(p)
      p.result = o.result
      p.etags = o.etags
      p.due = nextDue(now, o.poll)
      return
    case 'failed':
      failProbe(p, o.error, now)
  }
}

// defer leaves a probe whose follow-up failed as it was, due again at the
// next check, so the next check reads the change again.
function defer(p: Probe<unknown>): void {
  p.due = 0
}

async function single<T>(gh: GitHub, p: Probe<T>, path: string, now: number, parse: (body: unknown) => T): Promise<Outcome<T>> {
  if (now < p.due) return { kind: 'skip' }
  try {
    const etag = p.result === null ? null : (p.etags[0] ?? null)
    const res = await gh.get(path, etag)
    if (res.status === 304 && p.result !== null) return { kind: 'same', poll: res.pollInterval }
    if (res.status === 304) throw new GitHubError(304, null, { code: 'github_api_failed', message: `GET ${path}: HTTP 304 without a stored answer` })
    return { kind: 'new', result: parse(res.body), etags: [res.etag], poll: res.pollInterval }
  } catch (e) {
    return { kind: 'failed', error: asError(e) }
  }
}

function obj(v: unknown): Record<string, unknown> {
  return v !== null && typeof v === 'object' ? (v as Record<string, unknown>) : {}
}

function list(v: unknown): unknown[] {
  return Array.isArray(v) ? v : []
}

function str(v: unknown): string {
  return typeof v === 'string' ? v : ''
}

function toIssueBase(item: Record<string, unknown>): IssueBase {
  return {
    number: Number(item.number),
    title: str(item.title),
    url: str(item.html_url),
    labels: list(item.labels).map((l) => (typeof l === 'string' ? l : str(obj(l).name))),
    createdAt: str(item.created_at),
  }
}

// readIssues reads every page of the open Issues up to the first short
// page, each with its own ETag. PRs are skipped, as status does.
async function readIssues(gh: GitHub, repo: string, p: Probe<IssuesResult>, now: number): Promise<Outcome<IssuesResult>> {
  if (now < p.due) return { kind: 'skip' }
  const pages: IssuesResult['pages'] = []
  const etags: (string | null)[] = []
  let changed = false
  let poll: number | null = null
  try {
    for (let page = 1; ; page++) {
      const path = `repos/${repo}/issues?state=open&per_page=${perPage}&page=${page}`
      const old = p.result?.pages[page - 1]
      const res = await gh.get(path, old ? (p.etags[page - 1] ?? null) : null)
      poll = Math.max(poll ?? 0, res.pollInterval ?? 0) || null
      let cur: IssuesResult['pages'][number]
      if (res.status === 304) {
        if (!old) throw new GitHubError(304, null, { code: 'github_api_failed', message: `GET ${path}: HTTP 304 without a stored answer` })
        cur = old
        etags.push(p.etags[page - 1] ?? null)
      } else {
        const items = list(res.body)
        cur = { issues: items.map(obj).filter((it) => !('pull_request' in it)).map(toIssueBase), full: items.length >= perPage }
        etags.push(res.etag)
        changed = true
      }
      pages.push(cur)
      if (!cur.full) break
    }
  } catch (e) {
    return { kind: 'failed', error: asError(e) }
  }
  if (pages.length !== (p.result?.pages.length ?? -1)) changed = true
  return changed ? { kind: 'new', result: { pages }, etags, poll } : { kind: 'same', poll }
}

// trendPages caps the trend read at 300 delivered Issues in 7 days, well
// inside the request budget.
export const trendPages = 3

function trendFail(what: string): GitHubError {
  return new GitHubError(200, null, { code: 'github_api_failed', message: `read the delivered trend: ${what}` })
}

// readTrend reads every delivered Issue closed since Taipei midnight 6 days
// before today, page by page in a fixed order, and keeps only their close
// times. It fails as a whole, keeping the stored result, when a page fails,
// GitHub reports incomplete results, the Issues read differ from
// total_count, or more than trendPages pages would be needed. It is read
// when due, and at once when the Taipei day has moved past the stored one.
// A stored single-page answer is asked for with its ETag.
async function readTrend(gh: GitHub, p: Probe<TrendResult>, now: number): Promise<Outcome<TrendResult>> {
  const start = trendStart(now)
  const sameDay = p.result !== null && p.result.start === start
  if (now < p.due && (p.result === null || sameDay)) return { kind: 'skip' }
  const q = trendQuery(start)
  const seen = new Set<string>()
  const closedAt: string[] = []
  const etags: (string | null)[] = []
  let poll: number | null = null
  try {
    for (let page = 1; ; page++) {
      const path = searchPath(q, { perPage, page, sort: 'created', order: 'asc' })
      const etag = page === 1 && sameDay && p.etags.length === 1 ? (p.etags[0] ?? null) : null
      const res = await gh.get(path, etag)
      poll = Math.max(poll ?? 0, res.pollInterval ?? 0) || null
      if (res.status === 304) {
        if (etag === null) throw new GitHubError(304, null, { code: 'github_api_failed', message: `GET ${path}: HTTP 304 without a stored answer` })
        return { kind: 'same', poll }
      }
      const body = obj(res.body)
      const total = body.total_count
      if (typeof total !== 'number') throw trendFail('no total_count')
      if (body.incomplete_results === true) throw trendFail('GitHub reported incomplete results')
      if (total > trendPages * perPage) throw trendFail(`${total} Issues need more than ${trendPages} pages`)
      const items = list(body.items).map(obj)
      for (const it of items) {
        const key = str(it.html_url) || `${str(it.repository_url)}#${String(it.number)}`
        if (seen.has(key)) continue
        seen.add(key)
        closedAt.push(str(it.closed_at))
      }
      etags.push(res.etag)
      if (closedAt.length >= total || items.length < perPage) {
        if (closedAt.length !== total) throw trendFail(`read ${closedAt.length} Issues, but total_count is ${total}`)
        break
      }
      if (page >= trendPages) throw trendFail(`more than ${trendPages} pages`)
    }
  } catch (e) {
    return { kind: 'failed', error: asError(e) }
  }
  return { kind: 'new', result: { start, closedAt }, etags, poll }
}

function parseRuns(repo: string) {
  return (body: unknown): RunsResult => {
    const items = list(obj(body).workflow_runs).map(obj)
    const runs: Run[] = items.map((j) => ({
      repo,
      name: str(j.display_title),
      workflow: str(j.name),
      status: str(j.status),
      conclusion: typeof j.conclusion === 'string' ? j.conclusion : null,
      createdAt: str(j.created_at),
      url: str(j.html_url),
    }))
    return { missing: false, runs, full: items.length >= perPage }
  }
}

function parseRunners(body: unknown): { runners: RunnerBase[] } {
  return {
    runners: list(obj(body).runners)
      .map(obj)
      .map((r) => ({ id: Number(r.id), name: str(r.name), status: str(r.status), busy: r.busy === true })),
  }
}

function parseInProgress(body: unknown): { ids: number[] } {
  return { ids: list(obj(body).workflow_runs).map((r) => Number(obj(r).id)) }
}

async function readJobs(gh: GitHub, repo: string, ids: number[]): Promise<JobBase[]> {
  const out: JobBase[] = []
  for (const id of ids) {
    const path = `repos/${repo}/actions/runs/${id}/jobs?filter=latest&per_page=${perPage}`
    const res = await gh.get(path)
    if (res.status !== 200) throw new GitHubError(res.status, null, { code: 'github_api_failed', message: `GET ${path}: HTTP ${res.status} without a stored answer` })
    for (const j of list(obj(res.body).jobs).map(obj)) {
      if (j.status !== 'in_progress' || typeof j.runner_id !== 'number') continue
      out.push({ runnerId: j.runner_id, status: str(j.status), workflowName: str(j.workflow_name), url: str(j.html_url) })
    }
  }
  return out
}

const blockedFields = 'pageInfo { hasNextPage endCursor } nodes { number title state url repository { nameWithOwner } }'

interface BlockedConn {
  pageInfo?: { hasNextPage?: boolean; endCursor?: string }
  nodes?: ({ number: number; title: string; state: string; url: string; repository?: { nameWithOwner?: string } } | null)[]
}

function addBlockers(entry: BlockersEntry, conn: BlockedConn): void {
  for (const n of conn.nodes ?? []) {
    if (n) entry.blockedBy.push({ repo: n.repository?.nameWithOwner ?? '', number: n.number, title: n.title, state: n.state, url: n.url } satisfies Blocker)
  }
}

function gqlFail(what: string, message: string): ApiError {
  return { code: 'github_api_failed', message: `${what}: ${message}` }
}

interface Part {
  repo: string
  issues: number[]
}

// readBlockers reads the blockers of the listed Issues in queries of at
// most 50 Issues, then the pages past the first 100 blockers of an Issue.
// It ports internal/status/graph.go. A failed request throws; an error
// path naming one Issue sets only that Issue's error.
export async function readBlockers(gh: GitHub, listed: { repo: string; issues: number[] }[]): Promise<Record<string, BlockersEntry>> {
  const out: Record<string, BlockersEntry> = {}
  const queries: Part[][] = []
  let cur: Part[] = []
  let n = 0
  for (const g of listed) {
    for (let rest = g.issues; rest.length > 0; ) {
      if (n === issuesPerQuery) {
        queries.push(cur)
        cur = []
        n = 0
      }
      const k = Math.min(issuesPerQuery - n, rest.length)
      cur.push({ repo: g.repo, issues: rest.slice(0, k) })
      rest = rest.slice(k)
      n += k
    }
  }
  if (cur.length > 0) queries.push(cur)

  const cursors = new Map<string, string>()
  for (const parts of queries) {
    const decl: string[] = []
    const body: string[] = []
    const vars: Record<string, unknown> = {}
    parts.forEach((p, k) => {
      const [owner, name] = p.repo.split('/')
      decl.push(`$r${k}o: String!`, `$r${k}n: String!`)
      vars[`r${k}o`] = owner
      vars[`r${k}n`] = name
      body.push(`r${k}: repository(owner: $r${k}o, name: $r${k}n) {${p.issues.map((num, j) => ` i${j}: issue(number: ${num}) { blockedBy(first: 100) { ${blockedFields} } }`).join('')} }`)
      for (const num of p.issues) out[issueKey(p.repo, num)] = { blockedBy: [] }
    })
    const query = `query(${decl.join(', ')}) { ${body.join(' ')} }`
    const ans = await gh.graphql(query, vars)
    const setErr = (repo: string, num: number, e: ApiError) => {
      const entry = out[issueKey(repo, num)]
      entry.error ??= e
    }
    const names = parts.map((p) => p.repo).join(', ')
    for (const item of ans.errors) {
      const [a, b] = item.path.map(String)
      const k = a?.startsWith('r') ? Number(a.slice(1)) : NaN
      const p = Number.isInteger(k) ? parts[k] : undefined
      if (!p) {
        for (const q of parts) for (const num of q.issues) setErr(q.repo, num, gqlFail(`read the blockers of ${names}`, item.message))
        continue
      }
      const j = b?.startsWith('i') ? Number(b.slice(1)) : NaN
      if (Number.isInteger(j) && j < p.issues.length) {
        setErr(p.repo, p.issues[j], gqlFail(`read the blockers of ${p.repo}#${p.issues[j]}`, item.message))
        continue
      }
      for (const num of p.issues) setErr(p.repo, num, gqlFail(`read ${p.repo}`, item.message))
    }
    parts.forEach((p, k) => {
      const repoData = ans.data?.[`r${k}`] as Record<string, { blockedBy?: BlockedConn } | null> | null | undefined
      p.issues.forEach((num, j) => {
        const entry = out[issueKey(p.repo, num)]
        if (entry.error) return
        const node = repoData?.[`i${j}`]
        if (!repoData || !node?.blockedBy) {
          entry.error = gqlFail(repoData ? `read the blockers of ${p.repo}#${num}` : `read ${p.repo}`, 'not returned')
          return
        }
        addBlockers(entry, node.blockedBy)
        if (node.blockedBy.pageInfo?.hasNextPage && node.blockedBy.pageInfo.endCursor) cursors.set(issueKey(p.repo, num), node.blockedBy.pageInfo.endCursor)
      })
    })
  }

  for (const g of listed) {
    for (const num of g.issues) {
      const entry = out[issueKey(g.repo, num)]
      let cursor = cursors.get(issueKey(g.repo, num)) ?? ''
      const [owner, name] = g.repo.split('/')
      const what = `read the blockers of ${g.repo}#${num}`
      while (cursor !== '' && !entry.error) {
        const query = `query($o: String!, $n: String!, $c: String!) { repository(owner: $o, name: $n) { issue(number: ${num}) { blockedBy(first: 100, after: $c) { ${blockedFields} } } } }`
        const after = cursor
        cursor = ''
        try {
          const ans = await gh.graphql(query, { o: owner, n: name, c: after })
          const conn = (ans.data?.repository as { issue?: { blockedBy?: BlockedConn } | null } | null | undefined)?.issue?.blockedBy
          if (ans.errors.length > 0) entry.error = gqlFail(what, ans.errors[0].message)
          else if (!conn) entry.error = gqlFail(what, 'not returned')
          else {
            addBlockers(entry, conn)
            if (conn.pageInfo?.hasNextPage && conn.pageInfo.endCursor) cursor = conn.pageInfo.endCursor
          }
        } catch (e) {
          entry.error = asError(e).apiError
        }
      }
    }
  }
  return out
}

// check runs one check at now. It returns the new state (the input is not
// changed), whether the built LiveData changed, and the current errors.
export async function check(input: State, gh: GitHub, now: number): Promise<CheckResult> {
  const state = structuredClone(input)
  if (!gh.hasToken) {
    state.fatal = noToken
    return { state, changed: false, errors: currentErrors(state) }
  }
  state.fatal = null

  const issueOutcomes: Record<string, Outcome<IssuesResult>> = {}
  for (const repo of repos) {
    const r = state.repos[repo]
    issueOutcomes[repo] = await readIssues(gh, repo, r.issues, now)
    const io = issueOutcomes[repo]
    if (io.kind === 'failed') {
      commit(r.issues, io, now)
      if (io.error.notFound) {
        r.issues.result = null
        r.issues.etags = []
      }
    }

    for (const wf of workflows) {
      const p = r.runs[wf]
      const o = await single(gh, p, `repos/${repo}/actions/workflows/${wf}/runs?per_page=${perPage}`, now, parseRuns(repo))
      if (o.kind === 'failed' && o.error.notFound) {
        pass(p)
        p.result = { missing: true, runs: [], full: false }
        p.etags = []
        p.due = now + missingWorkflowInterval
      } else {
        commit(p, o, now)
      }
    }

    const ro = await single(gh, r.runners, `repos/${repo}/actions/runners?per_page=${perPage}`, now, parseRunners)
    if (ro.kind === 'failed') commit(r.runners, ro, now)
    const runners = ro.kind === 'new' ? ro.result.runners : (r.runners.result?.runners ?? [])

    let po: Outcome<{ ids: number[] }>
    if (runners.length > 0) {
      po = await single(gh, r.inProgress, `repos/${repo}/actions/runs?status=in_progress&per_page=${perPage}`, now, parseInProgress)
    } else {
      po = { kind: 'skip' }
      pass(r.inProgress)
      r.inProgress.result = { ids: [] }
      r.inProgress.etags = []
      r.inProgress.due = 0
    }
    if (po.kind === 'failed') commit(r.inProgress, po, now)

    // A failed jobs read is retried on the next check even when its
    // triggering probes then answer 304, so its error clears.
    let jobsOk = true
    if (ro.kind === 'new' || po.kind === 'new' || r.jobs.lastError !== null) {
      const ids = po.kind === 'new' ? po.result.ids : (r.inProgress.result?.ids ?? [])
      try {
        r.jobs.result = runners.some((x) => x.busy) ? await readJobs(gh, repo, ids) : []
        pass(r.jobs)
      } catch (e) {
        jobsOk = false
        fail(r.jobs, asError(e), now)
      }
    }
    // A probe whose jobs read failed keeps its old ETag and result.
    if (ro.kind !== 'failed') {
      if (ro.kind === 'new' && !jobsOk) defer(r.runners)
      else commit(r.runners, ro, now)
    }
    if (po.kind !== 'failed') {
      if (po.kind === 'new' && !jobsOk) defer(r.inProgress)
      else commit(r.inProgress, po, now)
    }
  }

  for (const [p, q] of [
    [state.search.created, createdQuery],
    [state.search.delivered, deliveredQuery],
  ] as const) {
    const o = await single(gh, p, searchPath(q), now, (body) => {
      const total = obj(body).total_count
      if (typeof total !== 'number') throw new GitHubError(200, null, { code: 'github_api_failed', message: `search ${q}: no total_count` })
      return { total }
    })
    commit(p, o, now)
  }

  // Blockers in another repository close there without changing this
  // repository's list, so any new list re-reads the blockers of all.
  const listed = repos.map((repo) => {
    const o = issueOutcomes[repo]
    const result = o.kind === 'new' ? o.result : state.repos[repo].issues.result
    return { repo, issues: (result?.pages ?? []).flatMap((pg) => pg.issues.map((i) => i.number)) }
  })
  const anyNew = repos.some((repo) => issueOutcomes[repo].kind === 'new')
  const retry = listed.some((g) => g.issues.some((num) => state.blockers.byIssue[issueKey(g.repo, num)]?.error !== undefined || !state.blockers.byIssue[issueKey(g.repo, num)]))
  // A failed blocker query is retried on the next check even when the
  // lists then answer 304, so its error clears.
  let blockersOk = true
  if (anyNew || retry || state.blockers.lastError !== null) {
    try {
      state.blockers.byIssue = await readBlockers(gh, listed)
      pass(state.blockers)
    } catch (e) {
      blockersOk = false
      fail(state.blockers, asError(e), now)
    }
  }
  for (const repo of repos) {
    const o = issueOutcomes[repo]
    if (o.kind === 'failed') continue
    if (o.kind === 'new' && !blockersOk) defer(state.repos[repo].issues)
    else commit(state.repos[repo].issues, o, now)
  }

  // The trend runs last: a spent request budget then holds back only the
  // trend, read again on the next check.
  commit(state.search.trend, await readTrend(gh, state.search.trend, now), now)

  const errors = currentErrors(state)
  if (errors.length === 0) state.okAt = now
  const data = buildLiveData(state)
  const changed = JSON.stringify(data) !== JSON.stringify(state.data)
  if (changed) {
    state.data = data
    state.at = now
    state.seq++
  }
  return { state, changed, errors }
}
