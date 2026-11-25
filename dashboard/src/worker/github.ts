// A read-only GitHub REST and GraphQL client for one check. The token goes
// only into the Authorization header; no error message carries a header.

import type { ApiError } from './messages'

const api = 'https://api.github.com'

// requestBudget keeps a check under the Workers Free plan's 50 external
// subrequests per invocation.
export const requestBudget = 45

// GitHubError is a failed read: a non-2xx, non-304 answer, a network
// failure, the spent request budget or a missing token.
export class GitHubError extends Error {
  readonly status: number // 0 when GitHub gave no answer
  readonly retryAfter: number | null // seconds
  readonly apiError: ApiError

  constructor(status: number, retryAfter: number | null, apiError: ApiError) {
    super(apiError.message)
    this.name = 'GitHubError'
    this.status = status
    this.retryAfter = retryAfter
    this.apiError = apiError
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
}

export const noToken: ApiError = { code: 'auth_unavailable', message: 'the Worker has no GITHUB_TOKEN secret' }

function seconds(v: string | null): number | null {
  if (v === null || v.trim() === '') return null
  const n = Number(v)
  return Number.isFinite(n) && n >= 0 ? n : null
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

// searchPath builds a search/issues path; URLSearchParams encodes the
// spaces, colons and the + of a UTC offset in q.
export function searchPath(q: string): string {
  return `search/issues?${new URLSearchParams({ per_page: '1', q }).toString()}`
}

export function createGitHub({ token, fetch: fetcher, budget = requestBudget }: GitHubOptions): GitHub {
  const hasToken = typeof token === 'string' && token !== ''
  let used = 0

  async function send(what: string, url: string, init: { method: string; etag?: string | null; body?: string }): Promise<Response> {
    if (!hasToken) throw new GitHubError(0, null, noToken)
    if (used >= budget) throw failed(what, 0, null, 'request budget of this check reached')
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
      const what = `GET ${path}`
      const res = await send(what, `${api}/${path}`, { method: 'GET', etag })
      const pollInterval = seconds(res.headers.get('x-poll-interval'))
      if (res.status === 304) return { status: 304, pollInterval }
      if (res.status < 200 || res.status > 299) {
        throw failed(what, res.status, seconds(res.headers.get('retry-after')), await githubMessage(res))
      }
      let body: unknown
      try {
        body = await res.json()
      } catch {
        throw failed(what, res.status, null, 'the answer is not JSON')
      }
      return { status: 200, etag: res.headers.get('etag'), body, date: res.headers.get('date'), pollInterval }
    },

    async graphql(query, variables) {
      const what = 'POST graphql'
      const res = await send(what, `${api}/graphql`, { method: 'POST', body: JSON.stringify({ query, variables }) })
      if (res.status < 200 || res.status > 299) {
        throw failed(what, res.status, seconds(res.headers.get('retry-after')), await githubMessage(res))
      }
      let body: { data?: unknown; errors?: unknown }
      try {
        body = (await res.json()) as typeof body
      } catch {
        throw failed(what, res.status, null, 'the answer is not JSON')
      }
      const errors: GraphQLErrorItem[] = []
      if (Array.isArray(body.errors)) {
        for (const e of body.errors as { message?: unknown; path?: unknown }[]) {
          errors.push({
            message: typeof e?.message === 'string' ? e.message : 'unknown error',
            path: Array.isArray(e?.path) ? (e.path as (string | number)[]) : [],
          })
        }
      }
      const data = body.data && typeof body.data === 'object' ? (body.data as Record<string, unknown>) : null
      return { data, errors }
    },
  }
}
