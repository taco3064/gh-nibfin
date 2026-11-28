// Test fakes: a fake GitHub behind a fake fetch. Only the *.test.ts files
// import this module.

import type { Blocker } from './messages'
import { createdQuery, deliveredQuery, repos, workflows } from './state'

export const token = 'github_pat_TEST_SECRET_0123456789'

export interface Answer {
  status?: number
  body?: unknown
  etag?: string
  headers?: Record<string, string>
}

export interface Recorded {
  method: string
  path: string
  headers: Record<string, string>
  body: string | null
}

export function issueJSON(number: number, extra: Record<string, unknown> = {}) {
  return {
    number,
    title: `Issue ${number}`,
    html_url: `https://github.com/x/y/issues/${number}`,
    labels: [],
    created_at: '2026-11-22T00:00:00Z',
    ...extra,
  }
}

export function runJSON(title: string, createdAt: string, extra: Record<string, unknown> = {}) {
  return {
    id: Math.floor(Date.parse(createdAt) / 1000),
    name: 'Nibfin shared runner',
    display_title: title,
    status: 'completed',
    conclusion: 'success',
    created_at: createdAt,
    html_url: `https://github.com/runs/${title.length}-${createdAt}`,
    ...extra,
  }
}

export const issuesPath = (repo: string, page = 1) => `repos/${repo}/issues?state=open&per_page=100&page=${page}`
export const runsPath = (repo: string, wf: string) => `repos/${repo}/actions/workflows/${wf}/runs?per_page=100`
export const runnersPath = (repo: string) => `repos/${repo}/actions/runners?per_page=100`
export const inProgressPath = (repo: string) => `repos/${repo}/actions/runs?status=in_progress&per_page=100`
export const jobsPath = (repo: string, id: number) => `repos/${repo}/actions/runs/${id}/jobs?filter=latest&per_page=100`
export const searchPathOf = (q: string) => `search/issues?${new URLSearchParams({ per_page: '1', q }).toString()}`

export class FakeGitHub {
  routes = new Map<string, Answer>()
  requests: Recorded[] = []
  // blockers answers the blocker queries, by lower-case "owner/repo#n".
  blockers: Record<string, Blocker[]> = {}
  graphqlErrors: { message: string; path: (string | number)[] }[] = []
  graphqlStatus = 200
  // trend answers any trend search (per_page=100), page by page: its
  // items, total_count (items.length unless set), incomplete_results, an
  // ETag, and a failure of one page.
  trend: {
    items: { html_url: string; closed_at: string }[]
    total?: number
    incomplete?: boolean
    etag?: string
    failPage?: { page: number; status: number }
  } = { items: [] }
  private n = 0

  constructor() {
    for (const repo of repos) {
      this.set(issuesPath(repo), [])
      for (const wf of workflows) this.set(runsPath(repo, wf), { total_count: 0, workflow_runs: [] })
      this.set(runnersPath(repo), { total_count: 0, runners: [] })
      this.set(inProgressPath(repo), { total_count: 0, workflow_runs: [] })
    }
    this.set(searchPathOf(createdQuery), { total_count: 50, items: [] }, null)
    this.set(searchPathOf(deliveredQuery), { total_count: 42, items: [] }, null)
  }

  // set answers path with 200 and body, under a new ETag (or none).
  set(path: string, body: unknown, etag: string | null = `W/"e${++this.n}"`, headers: Record<string, string> = {}): void {
    this.routes.set(path, { status: 200, body, etag: etag ?? undefined, headers })
  }

  fail(path: string, status: number, message: string, headers: Record<string, string> = {}): void {
    this.routes.set(path, { status, body: { message }, headers })
  }

  gets(): string[] {
    return this.requests.filter((r) => r.method === 'GET').map((r) => r.path)
  }

  graphqlCalls(): Recorded[] {
    return this.requests.filter((r) => r.path === 'graphql')
  }

  reset(): void {
    this.requests = []
  }

  fetch: typeof fetch = async (input, init) => {
    const url = String(input)
    const path = url.replace('https://api.github.com/', '')
    const headers = { ...(init?.headers as Record<string, string>) }
    const body = typeof init?.body === 'string' ? init.body : null
    this.requests.push({ method: init?.method ?? 'GET', path, headers, body })
    if (path === 'graphql') return this.graphql(JSON.parse(body ?? '{}') as { query: string; variables: Record<string, string> })
    if (!this.routes.has(path) && path.startsWith('search/issues?')) {
      const params = new URLSearchParams(path.slice('search/issues?'.length))
      if (params.get('per_page') === '100') return this.trendPage(Number(params.get('page') ?? '1'), headers)
    }
    const a = this.routes.get(path) ?? { status: 404, body: { message: 'Not Found' } }
    const h = new Headers(a.headers)
    if (a.etag) h.set('etag', a.etag)
    if (a.status === 200 && a.etag && headers['If-None-Match'] === a.etag) return new Response(null, { status: 304, headers: h })
    h.set('content-type', 'application/json')
    return new Response(JSON.stringify(a.body), { status: a.status ?? 200, headers: h })
  }

  // trendQueries lists the q of every trend page request.
  trendQueries(): string[] {
    return this.gets()
      .filter((p) => p.startsWith('search/issues?') && new URLSearchParams(p.slice('search/issues?'.length)).get('per_page') === '100')
      .map((p) => new URLSearchParams(p.slice('search/issues?'.length)).get('q') ?? '')
  }

  private trendPage(page: number, headers: Record<string, string>): Response {
    const t = this.trend
    if (t.failPage?.page === page) return new Response(JSON.stringify({ message: 'Server Error' }), { status: t.failPage.status })
    const h = new Headers({ 'content-type': 'application/json' })
    if (t.etag) {
      h.set('etag', t.etag)
      if (page === 1 && headers['If-None-Match'] === t.etag) return new Response(null, { status: 304, headers: h })
    }
    const items = t.items.slice((page - 1) * 100, page * 100)
    return new Response(JSON.stringify({ total_count: t.total ?? t.items.length, incomplete_results: t.incomplete ?? false, items }), {
      status: 200,
      headers: h,
    })
  }

  private graphql({ query, variables }: { query: string; variables: Record<string, string> }): Response {
    if (this.graphqlStatus !== 200) return new Response(JSON.stringify({ message: 'Bad credentials' }), { status: this.graphqlStatus })
    const conn = (key: string) => ({
      pageInfo: { hasNextPage: false, endCursor: null },
      nodes: (this.blockers[key] ?? []).map((b) => ({ ...b, repository: { nameWithOwner: b.repo } })),
    })
    const data: Record<string, unknown> = {}
    if ('o' in variables) {
      const num = /issue\(number: (\d+)\)/.exec(query)?.[1]
      data.repository = { issue: { blockedBy: conn(`${variables.o}/${variables.n}#${num}`.toLowerCase()) } }
    } else {
      for (let k = 0; `r${k}o` in variables; k++) {
        const start = query.indexOf(`r${k}: repository(`)
        const end = query.indexOf(`r${k + 1}: repository(`)
        const seg = query.slice(start, end < 0 ? undefined : end)
        const repo: Record<string, unknown> = {}
        for (const m of seg.matchAll(/(i\d+): issue\(number: (\d+)\)/g)) {
          repo[m[1]] = { blockedBy: conn(`${variables[`r${k}o`]}/${variables[`r${k}n`]}#${m[2]}`.toLowerCase()) }
        }
        data[`r${k}`] = repo
      }
    }
    return new Response(JSON.stringify({ data, errors: this.graphqlErrors.length ? this.graphqlErrors : undefined }), { status: 200 })
  }
}

/** No wait and no log between retries, for createGitHub in tests. */
export const quick = { sleep: async () => {}, log: () => {} }
