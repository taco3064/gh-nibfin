import { describe, expect, it } from 'vitest'

import { createGitHub, GitHubError, requestBudget, searchPath } from './github'
import { FakeGitHub, token, quick } from './testing'
import { askedWait, waitSeconds } from './github'

const path = 'repos/taco3064/gh-nibfin/issues?state=open&per_page=100&page=1'

describe('createGitHub', () => {
  it('sends the read headers and If-None-Match only with a known ETag', async () => {
    const fake = new FakeGitHub()
    fake.set(path, [{ number: 1 }], 'W/"a"')
    const gh = createGitHub({ token, fetch: fake.fetch, ...quick })
    const first = await gh.get(path)
    expect(first).toMatchObject({ status: 200, etag: 'W/"a"', body: [{ number: 1 }] })
    const h = fake.requests[0].headers
    expect(h.Authorization).toBe(`Bearer ${token}`)
    expect(h.Accept).toBe('application/vnd.github+json')
    expect(h['X-GitHub-Api-Version']).toBe('2022-11-28')
    expect(h['User-Agent']).toBe('nibfin-live')
    expect('If-None-Match' in h).toBe(false)
    const again = await gh.get(path, 'W/"a"')
    expect(again.status).toBe(304)
    expect(fake.requests[1].headers['If-None-Match']).toBe('W/"a"')
  })

  it('reads x-poll-interval from 200 and 304 answers', async () => {
    const fake = new FakeGitHub()
    fake.set(path, [], 'W/"a"', { 'x-poll-interval': '120' })
    const gh = createGitHub({ token, fetch: fake.fetch, ...quick })
    expect((await gh.get(path)).pollInterval).toBe(120)
    expect(await gh.get(path, 'W/"a"')).toEqual({ status: 304, pollInterval: 120 })
  })

  it('turns a failure into a GitHubError with Retry-After and no token', async () => {
    const fake = new FakeGitHub()
    fake.fail(path, 403, 'API rate limit exceeded', { 'retry-after': '60' })
    const gh = createGitHub({ token, fetch: fake.fetch, ...quick })
    const err = await gh.get(path).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(GitHubError)
    const g = err as GitHubError
    expect(g.status).toBe(403)
    expect(g.retryAfter).toBe(60)
    expect(g.notFound).toBe(false)
    expect(g.apiError).toEqual({ code: 'github_api_failed', message: `GET ${path}: HTTP 403: API rate limit exceeded` })
    expect(JSON.stringify(g.apiError)).not.toContain(token)
  })

  it('tells a 404 apart', async () => {
    const fake = new FakeGitHub()
    const gh = createGitHub({ token, fetch: fake.fetch, ...quick })
    const err = (await gh.get('repos/x/y/actions/workflows/none.yml/runs').catch((e: unknown) => e)) as GitHubError
    expect(err.notFound).toBe(true)
  })

  it('turns a network failure into a GitHubError', async () => {
    const gh = createGitHub({
      ...quick,
      token,
      fetch: async () => {
        throw new TypeError('connection reset')
      },
    })
    const err = (await gh.get(path).catch((e: unknown) => e)) as GitHubError
    expect(err.status).toBe(0)
    expect(err.apiError.message).toContain('connection reset')
  })

  it('stops at the request budget without fetching', async () => {
    const fake = new FakeGitHub()
    const gh = createGitHub({ token, fetch: fake.fetch, ...quick })
    for (let i = 0; i < requestBudget; i++) await gh.get(path)
    const err = (await gh.get(path).catch((e: unknown) => e)) as GitHubError
    expect(err.apiError.message).toContain('request budget of this check reached')
    expect(fake.requests).toHaveLength(requestBudget)
    await expect(gh.graphql('query { viewer { login } }', {})).rejects.toBeInstanceOf(GitHubError)
    expect(fake.requests).toHaveLength(requestBudget)
  })

  it('makes no request without a token', async () => {
    const fake = new FakeGitHub()
    for (const t of [undefined, '']) {
      const gh = createGitHub({ token: t, fetch: fake.fetch, ...quick })
      expect(gh.hasToken).toBe(false)
      const err = (await gh.get(path).catch((e: unknown) => e)) as GitHubError
      expect(err.apiError).toEqual({ code: 'auth_unavailable', message: 'the Worker has no GITHUB_TOKEN secret' })
    }
    expect(fake.requests).toHaveLength(0)
  })

  it('returns GraphQL data with the error paths', async () => {
    const fake = new FakeGitHub()
    fake.graphqlErrors = [{ message: 'Could not resolve', path: ['r0', 'i1'] }]
    const gh = createGitHub({ token, fetch: fake.fetch, ...quick })
    const ans = await gh.graphql('query($r0o: String!, $r0n: String!) { r0: repository(owner: $r0o, name: $r0n) { i0: issue(number: 3) { x } } }', { r0o: 'a', r0n: 'b' })
    expect(ans.errors).toEqual([{ message: 'Could not resolve', path: ['r0', 'i1'], type: null }])
    expect(ans.data).toHaveProperty('r0')
    expect(fake.requests[0].method).toBe('POST')
    expect(fake.requests[0].headers.Authorization).toBe(`Bearer ${token}`)
  })

  it('retries any error 3 times, waiting as GitHub asks, then fails as before', async () => {
    const fake = new FakeGitHub()
    fake.fail('repos/x/y/pulls', 422, 'Validation Failed', { 'retry-after': '7' })
    const waits: number[] = []
    const lines: string[] = []
    const gh = createGitHub({ token, fetch: fake.fetch, sleep: async (ms) => void waits.push(ms), log: (l) => void lines.push(l) })
    const err = (await gh.get('repos/x/y/pulls').catch((e: unknown) => e)) as GitHubError
    expect(err.status).toBe(422)
    expect(fake.gets()).toHaveLength(4)
    expect(waits).toEqual([7000, 7000, 7000])
    expect(lines).toEqual([1, 2, 3].map((n) => `nibfin: GitHub API GET /repos/x/y/pulls failed (HTTP 422); retry ${n} of 3`))
  })

  it('succeeds on the third retry and does not retry an absent 404', async () => {
    let n = 0
    const fake = new FakeGitHub()
    const gh = createGitHub({
      ...quick,
      token,
      fetch: async (input, init) => {
        if (++n <= 3) throw new TypeError('connection reset')
        return fake.fetch(input, init)
      },
    })
    expect((await gh.get(path)).status).toBe(200)
    expect(n).toBe(4)
    const fake404 = new FakeGitHub()
    const gh404 = createGitHub({ token, fetch: fake404.fetch, ...quick })
    await gh404.get('repos/x/y/none').catch(() => null)
    expect(fake404.gets()).toHaveLength(1)
  })

  it('retries a GraphQL error but not NOT_FOUND alone', async () => {
    const fake = new FakeGitHub()
    fake.graphqlErrors = [{ message: 'Something went wrong', path: [] }]
    const gh = createGitHub({ token, fetch: fake.fetch, ...quick })
    const ans = await gh.graphql('query { x }', {})
    expect(ans.errors).toHaveLength(1)
    expect(fake.graphqlCalls()).toHaveLength(4)
    const absent = new FakeGitHub()
    absent.graphqlErrors = [{ message: 'Could not resolve', path: ['r0'], type: 'NOT_FOUND' } as never]
    const gh2 = createGitHub({ token, fetch: absent.fetch, ...quick })
    await gh2.graphql('query { x }', {})
    expect(absent.graphqlCalls()).toHaveLength(1)
  })

  it('does not retry a missing token or a spent budget', async () => {
    const fake = new FakeGitHub()
    const gh = createGitHub({ token, fetch: fake.fetch, budget: 1, ...quick })
    fake.fail('repos/x/y/a', 500, 'boom')
    const err = (await gh.get('repos/x/y/a').catch((e: unknown) => e)) as GitHubError
    expect(err.apiError.message).toContain('request budget')
    expect(fake.gets()).toHaveLength(1)
  })

  it('waits 1, 2 and 4 seconds, or Retry-After up to a minute', () => {
    expect([1, 2, 3].map((a) => waitSeconds(a, null))).toEqual([1, 2, 4])
    expect(waitSeconds(1, 3600)).toBe(10)
  })

  it('reads the wait from Retry-After or the rate limit reset', () => {
    const now = 1_000_000
    expect(askedWait(new Headers({ 'retry-after': '7' }), now)).toBe(7)
    expect(askedWait(new Headers({ 'x-ratelimit-remaining': '0', 'x-ratelimit-reset': '1005' }), now)).toBe(5)
    expect(askedWait(new Headers({ 'x-ratelimit-remaining': '0', 'x-ratelimit-reset': '990' }), now)).toBe(0)
    expect(askedWait(new Headers({ 'x-ratelimit-remaining': '3', 'x-ratelimit-reset': '1005' }), now)).toBe(null)
    expect(askedWait(new Headers({ 'x-ratelimit-remaining': '0' }), now)).toBe(null)
    expect(askedWait(new Headers(), now)).toBe(null)
  })

  it('encodes a search query', () => {
    expect(searchPath('repo:a/b is:issue created:>=2026-11-21T00:00:00+08:00')).toBe('search/issues?per_page=1&q=repo%3Aa%2Fb+is%3Aissue+created%3A%3E%3D2026-11-21T00%3A00%3A00%2B08%3A00')
  })

  it('adds a page size, page and sort to a search path', () => {
    expect(searchPath('is:issue', { perPage: 100, page: 2, sort: 'created', order: 'asc' })).toBe('search/issues?per_page=100&page=2&sort=created&order=asc&q=is%3Aissue')
  })
})
