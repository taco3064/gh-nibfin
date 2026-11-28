import { describe, expect, it } from 'vitest'

import { BACKOFF_MS, createClient, HttpError, MAX_RETRY_AFTER_MS, parseLinks } from './http.ts'

const base = 'https://api.github.com/'

function client(answers: (() => Response)[], slept: number[] = []) {
  const urls: string[] = []
  const c = createClient({
    base,
    token: 't0ken',
    fetch: async (url) => {
      urls.push(url)
      const a = answers[Math.min(urls.length - 1, answers.length - 1)]
      return a()
    },
    sleep: async (ms) => {
      slept.push(ms)
    },
  })
  return { c, urls }
}

const ok =
  (body: unknown, headers: Record<string, string> = {}) =>
  () =>
    new Response(JSON.stringify(body), { status: 200, headers })
const status =
  (s: number, headers: Record<string, string> = {}) =>
  () =>
    new Response('{"message":"secret body"}', { status: s, headers })

describe('createClient', () => {
  it('retries any error up to 3 times with backoff', async () => {
    const slept: number[] = []
    const { c, urls } = client([status(500), status(403), status(422), ok({ a: 1 })], slept)
    expect((await c.get('x'))!.body).toEqual({ a: 1 })
    expect(urls).toHaveLength(4)
    expect(slept).toEqual(BACKOFF_MS)
  })

  it('throws after the third retry with method, path and status only', async () => {
    const { c, urls } = client([status(500)])
    const err = await c.get('repos/a/b').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(HttpError)
    expect((err as HttpError).message).toBe('GET repos/a/b: HTTP 500')
    expect((err as HttpError).message).not.toContain('secret')
    expect(urls).toHaveLength(4)
  })

  it('answers a 404 as null when asked, without retrying', async () => {
    const { c, urls } = client([status(404)])
    expect(await c.get('x', { notFound: true })).toBeNull()
    expect(urls).toHaveLength(1)
  })

  it('honors Retry-After up to a cap', async () => {
    const slept: number[] = []
    const { c } = client([status(429, { 'retry-after': '3' }), status(429, { 'retry-after': '9999' }), ok({})], slept)
    await c.get('x')
    expect(slept).toEqual([3000, MAX_RETRY_AFTER_MS])
  })

  it('never follows a link off its base', async () => {
    const { c, urls } = client([ok([1], { link: '<https://evil.example/x?page=2>; rel="next"' })])
    const pages: unknown[][] = []
    await expect(
      (async () => {
        for await (const p of c.pages('x', 5)) pages.push(p)
      })(),
    ).rejects.toThrow('link outside')
    expect(urls).toEqual([`${base}x`])
  })

  it('checks before re-sending a write', async () => {
    const { c, urls } = client([status(502)])
    let checks = 0
    const out = await c.retry(
      () => c.once('POST', 'x', {}),
      async () => (++checks === 2 ? { status: 201, body: 'found', links: {} } : undefined),
    )
    expect(out.body).toBe('found')
    expect(urls).toHaveLength(2)
  })
})

describe('parseLinks', () => {
  it('reads GitHub Link headers', () => {
    expect(parseLinks('<https://api.github.com/x?page=2>; rel="next", <https://api.github.com/x?page=5>; rel="last"')).toEqual({
      next: 'https://api.github.com/x?page=2',
      last: 'https://api.github.com/x?page=5',
    })
    expect(parseLinks(null)).toEqual({})
  })
})
