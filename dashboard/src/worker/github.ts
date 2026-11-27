// A read-only GitHub REST and GraphQL client for one check. The token goes
// only into the Authorization header; no error message carries a header.
// Every call follows the retry rule of all GitHub API calls (Proposal
// R4.37–R4.39): on any error it is sent again up to 3 times, whatever the
// error is, before the caller sees the last error unchanged. A GET 404 or
// 410 and a GraphQL answer with only NOT_FOUND errors ("absent"), and a 304,
// are answers, not errors.

import type { ApiError } from './messages'

const api = 'https://api.github.com'

// requestBudget keeps a check under the Workers Free plan's 50 external
// subrequests per invocation.
export const requestBudget = 45

// retries is the number of retries after the first attempt.
export const retries = 3

// maxWait bounds one wait taken from Retry-After or x-ratelimit-reset, in
// seconds. It is short because a check runs in one alarm, and each retry
// counts against the request budget like any request.
const maxWait = 10

// GitHubError is a failed read: a non-2xx, non-304 answer, a network
// failure, the spent request budget or a missing token.
export class GitHubError extends Error {
  readonly status: number // 0 when GitHub gave no answer
  readonly retryAfter: number | null // seconds
  readonly apiError: ApiError
  // final is an error another attempt cannot change: no token or the spent
  // request budget.
  readonly final: boolean

  constructor(status: number, retryAfter: number | null, apiError: ApiError, final = false) {
    super(apiError.message)
    this.name = 'GitHubError'
    this.status = status
    this.retryAfter = retryAfter
    this.apiError = apiError
    this.final = final
  }

  get notFound(): boolean {
    return this.status === 404
  }
}

export interface GetOk {
  status: 200
  etag: string | null
  body: unknown
  date: string | null
  pollInterval: number | null // seconds
}

export interface GetNotModified {
  status: 304
  pollInterval: number | null
}

export interface GraphQLErrorItem {
  message: string
  path: (string | number)[]
  type: string | null
}

export interface GraphQLAnswer {
  data: Record<string, unknown> | null
  errors: GraphQLErrorItem[]
}

export interface GitHub {
  readonly hasToken: boolean
  get(path: string, etag?: string | null): Promise<GetOk | GetNotModified>
  graphql(query: string, variables: Record<string, unknown>): Promise<GraphQLAnswer>
}

export interface GitHubOptions {
  token: string | undefined
  fetch: typeof fetch
  budget?: number
  // sleep waits between retries; tests replace it.
  sleep?: (ms: number) => Promise<void>
  // log receives one line per retry; tests replace it.
  log?: (line: string) => void
}

export const noToken: ApiError = { code: 'auth_unavailable', message: 'the Worker has no GITHUB_TOKEN secret' }

function seconds(v: string | null): number | null {
  if (v === null || v.trim() === '') return null
  const n = Number(v)
  return Number.isFinite(n) && n >= 0 ? n : null
}

// askedWait is the wait GitHub asks for, in seconds: Retry-After, or the
// time to x-ratelimit-reset when x-ratelimit-remaining is 0; null when none.
export function askedWait(h: Headers, now = Date.now()): number | null {
  const after = seconds(h.get('retry-after'))
  if (after !== null) return after
  if (h.get('x-ratelimit-remaining') !== '0') return null
  const reset = seconds(h.get('x-ratelimit-reset'))
  return reset === null ? null : Math.max(0, reset - Math.floor(now / 1000))
}

function failed(what: string, status: number, retryAfter: number | null, detail: string): GitHubError {
  const message = status > 0 ? `${what}: HTTP ${status}${detail ? `: ${detail}` : ''}` : `${what}: ${detail}`
  return new GitHubError(status, retryAfter, { code: 'github_api_failed', message })
}

async function githubMessage(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { message?: unknown } | null
    return typeof body?.message === 'string' ? body.message : ''
  } catch {
    return ''
  }
}

export interface SearchOptions {
  perPage?: number // 1 by default: only total_count is read
  page?: number
  sort?: string
  order?: 'asc' | 'desc'
}

// searchPath builds a search/issues path; URLSearchParams encodes the
// spaces, colons and the + of a UTC offset in q.
export function searchPath(q: string, { perPage = 1, page, sort, order }: SearchOptions = {}): string {
  const params = new URLSearchParams({ per_page: String(perPage) })
  if (page !== undefined) params.set('page', String(page))
  if (sort !== undefined) params.set('sort', sort)
  if (order !== undefined) params.set('order', order)
  params.set('q', q)
  return `search/issues?${params.toString()}`
}

const realSleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms))

// waitSeconds is the wait GitHub asked for (at most maxWait), otherwise 1, 2, 4.
export function waitSeconds(attempt: number, retryAfter: number | null): number {
  return retryAfter === null ? 2 ** (attempt - 1) : Math.min(retryAfter, maxWait)
}

export function createGitHub({
  token,
  fetch: fetcher,
  budget = requestBudget,
  sleep = realSleep,
  log = (line) => console.log(line),
}: GitHubOptions): GitHub {
  const hasToken = typeof token === 'string' && token !== ''
  let used = 0

  // retrying runs once up to 1 + retries times. A thrown GitHubError is
  // retried unless it is final or an absent answer; an answer is retried
  // while again() names a failure. The retry line names the call and the
  // attempt, never a query string, body or header.
  async function retrying<T>(
    what: string,
    once: () => Promise<T>,
    absent: (e: GitHubError) => boolean,
    again: (t: T) => string | null,
  ): Promise<T> {
    for (let attempt = 1; ; attempt++) {
      let reason: string | null
      let retryAfter: number | null = null
      try {
        const answer = await once()
        reason = attempt > retries ? null : again(answer)
        if (reason === null) return answer
      } catch (e) {
        if (!(e instanceof GitHubError) || e.final || absent(e) || attempt > retries) throw e
        reason = e.status > 0 ? `HTTP ${e.status}` : 'no response'
        retryAfter = e.retryAfter
      }
      log(`nibfin: GitHub API ${what} failed (${reason}); retry ${attempt} of ${retries}`)
      await sleep(waitSeconds(attempt, retryAfter) * 1000)
    }
  }

  async function send(what: string, url: string, init: { method: string; etag?: string | null; body?: string }): Promise<Response> {
    if (!hasToken) throw new GitHubError(0, null, noToken, true)
    if (used >= budget) {
      const e = failed(what, 0, null, 'request budget of this check reached')
      throw new GitHubError(e.status, null, e.apiError, true)
    }
    used++
    const headers: Record<string, string> = {
      Authorization: `Bearer ${token}`,
      Accept: 'application/vnd.github+json',
      'X-GitHub-Api-Version': '2022-11-28',
      'User-Agent': 'nibfin-live',
    }
    if (init.etag) headers['If-None-Match'] = init.etag
    if (init.body !== undefined) headers['Content-Type'] = 'application/json'
    try {
      return await fetcher(url, { method: init.method, headers, body: init.body })
    } catch (e) {
      throw failed(what, 0, null, `the request failed: ${e instanceof Error ? e.message : String(e)}`)
    }
  }

  return {
    hasToken,

    async get(path, etag) {
      const absent = (e: GitHubError) => e.status === 404 || e.status === 410
      return retrying(`GET /${path.split('?')[0]}`, () => getOnce(path, etag), absent, () => null)
    },

    async graphql(query, variables) {
      // NOT_FOUND alone is an answer ("absent"), like a GET 404.
      const again = (a: GraphQLAnswer) => (a.errors.some((e) => e.type !== 'NOT_FOUND') ? 'GraphQL error' : null)
      return retrying('POST /graphql', () => graphqlOnce(query, variables), () => false, again)
    },
  }

  async function getOnce(path: string, etag: string | null | undefined): Promise<GetOk | GetNotModified> {
    const what = `GET ${path}`
    const res = await send(what, `${api}/${path}`, { method: 'GET', etag })
    const pollInterval = seconds(res.headers.get('x-poll-interval'))
    if (res.status === 304) return { status: 304, pollInterval }
    if (res.status < 200 || res.status > 299) {
      throw failed(what, res.status, askedWait(res.headers), await githubMessage(res))
    }
    let body: unknown
    try {
      body = await res.json()
    } catch {
      throw failed(what, res.status, null, 'the answer is not JSON')
    }
    return { status: 200, etag: res.headers.get('etag'), body, date: res.headers.get('date'), pollInterval }
  }

  async function graphqlOnce(query: string, variables: Record<string, unknown>): Promise<GraphQLAnswer> {
    const what = 'POST graphql'
    const res = await send(what, `${api}/graphql`, { method: 'POST', body: JSON.stringify({ query, variables }) })
    if (res.status < 200 || res.status > 299) {
      throw failed(what, res.status, askedWait(res.headers), await githubMessage(res))
    }
    let body: { data?: unknown; errors?: unknown }
    try {
      body = (await res.json()) as typeof body
    } catch {
      throw failed(what, res.status, null, 'the answer is not JSON')
    }
    const errors: GraphQLErrorItem[] = []
    if (Array.isArray(body.errors)) {
      for (const e of body.errors as { message?: unknown; path?: unknown; type?: unknown }[]) {
        errors.push({
          message: typeof e?.message === 'string' ? e.message : 'unknown error',
          path: Array.isArray(e?.path) ? (e.path as (string | number)[]) : [],
          type: typeof e?.type === 'string' ? e.type : null,
        })
      }
    }
    const data = body.data && typeof body.data === 'object' ? (body.data as Record<string, unknown>) : null
    return { data, errors }
  }
}
