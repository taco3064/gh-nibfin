// A small JSON REST client for the filer, used for both GitHub and Sentry.
// Every call is retried up to 3 times on any error; a 404 the caller asks
// about is an answer. Errors carry the method, path and status only, never a
// header or a body, so no token or untrusted text reaches the log.

export type Fetch = (input: string, init: RequestInit) => Promise<Response>

export interface ClientOptions {
  base: string // with a trailing slash, for example https://api.github.com/
  token?: string
  fetch: Fetch
  sleep: (ms: number) => Promise<void>
  headers?: Record<string, string>
  timeoutMs?: number
}

export const RETRIES = 3
export const BACKOFF_MS = [1000, 2000, 4000]
export const MAX_RETRY_AFTER_MS = 30_000

export class HttpError extends Error {
  readonly status: number // 0 when no answer came
  readonly retryAfterMs?: number

  constructor(method: string, path: string, status: number, retryAfterMs?: number) {
    super(`${method} ${path}: ${status === 0 ? 'no response' : `HTTP ${status}`}`)
    this.name = 'HttpError'
    this.status = status
    this.retryAfterMs = retryAfterMs
  }
}

export interface Answer {
  status: number
  body: unknown
  links: Record<string, string>
}

export interface Client {
  // get reads one path; with notFound it answers a 404 as null.
  get(path: string, opts?: { notFound?: boolean }): Promise<Answer | null>
  // once sends one request with no retry, for writes that create something.
  once(method: string, path: string, body?: unknown): Promise<Answer>
  // retry runs a write with the retry policy, calling check before each
  // re-send; a non-undefined check result means the write took effect.
  retry<T>(send: () => Promise<T>, check: () => Promise<T | undefined>): Promise<T>
  pages(path: string, maxPages: number): AsyncGenerator<unknown[]>
  pagesNewestFirst(path: string, maxPages: number): AsyncGenerator<unknown[]>
}

export class PageLimitError extends Error {
  constructor(path: string, maxPages: number) {
    super(`GET ${path}: more than ${maxPages} pages`)
    this.name = 'PageLimitError'
  }
}

export function parseLinks(header: string | null): Record<string, string> {
  const out: Record<string, string> = {}
  if (!header) return out
  for (const part of header.split(',')) {
    const m = /^\s*<([^>]*)>\s*;\s*rel="([^"]+)"/.exec(part)
    if (m) for (const rel of m[2].split(/\s+/)) out[rel] = m[1]
  }
  return out
}

function retryAfter(res: Response): number | undefined {
  const v = res.headers.get('retry-after')
  if (v === null || !/^\d+$/.test(v.trim())) return undefined
  return Math.min(Number(v.trim()) * 1000, MAX_RETRY_AFTER_MS)
}

export function createClient(o: ClientOptions): Client {
  // A pagination link is followed only when it stays on the same API base, so
  // the token is never sent anywhere else.
  const resolve = (path: string): { url: string; shown: string } => {
    if (/^https?:\/\//.test(path)) {
      if (!path.startsWith(o.base)) throw new Error(`link outside ${o.base}`)
      return { url: path, shown: path.slice(o.base.length) }
    }
    return { url: o.base + path, shown: path }
  }

  const send = async (method: string, path: string, body?: unknown): Promise<Answer> => {
    const { url, shown } = resolve(path)
    const headers: Record<string, string> = { Accept: 'application/json', ...o.headers }
    if (o.token) headers.Authorization = `Bearer ${o.token}`
    if (body !== undefined) headers['Content-Type'] = 'application/json'
    let res: Response
    try {
      res = await o.fetch(url, { method, headers, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(o.timeoutMs ?? 20_000) })
    } catch {
      throw new HttpError(method, shown, 0)
    }
    if (res.status < 200 || res.status >= 300) {
      await res.body?.cancel()
      throw new HttpError(method, shown, res.status, retryAfter(res))
    }
    const text = await res.text()
    let parsed: unknown = null
    if (text !== '') {
      try {
        parsed = JSON.parse(text)
      } catch {
        throw new HttpError(method, shown, res.status)
      }
    }
    return { status: res.status, body: parsed, links: parseLinks(res.headers.get('link')) }
  }

  const wait = (attempt: number, err: unknown) => {
    const after = err instanceof HttpError ? err.retryAfterMs : undefined
    return o.sleep(after ?? BACKOFF_MS[Math.min(attempt, BACKOFF_MS.length - 1)])
  }

  const retry = async <T>(run: () => Promise<T>, check: () => Promise<T | undefined>): Promise<T> => {
    for (let attempt = 0; ; attempt++) {
      try {
        return await run()
      } catch (err) {
        if (attempt >= RETRIES) throw err
        await wait(attempt, err)
        const done = await check()
        if (done !== undefined) return done
      }
    }
  }

  const get = async (path: string, opts: { notFound?: boolean } = {}): Promise<Answer | null> => {
    for (let attempt = 0; ; attempt++) {
      try {
        return await send('GET', path)
      } catch (err) {
        if (opts.notFound && err instanceof HttpError && err.status === 404) return null
        if (attempt >= RETRIES) throw err
        await wait(attempt, err)
      }
    }
  }

  const mustGet = async (path: string): Promise<Answer> => (await get(path))!

  const asList = (path: string, a: Answer): unknown[] => {
    if (!Array.isArray(a.body)) throw new Error(`GET ${resolve(path).shown}: not a list`)
    return a.body
  }

  return {
    get,
    once: send,
    retry,
    async *pages(path, maxPages) {
      let next: string | undefined = path
      for (let n = 0; next !== undefined; n++) {
        if (n >= maxPages) throw new PageLimitError(resolve(path).shown, maxPages)
        const a = await mustGet(next)
        yield asList(next, a)
        next = a.links.next
      }
    },
    async *pagesNewestFirst(path, maxPages) {
      const first = await mustGet(path)
      let next = first.links.last
      if (next === undefined) {
        yield asList(path, first)
        return
      }
      for (let n = 1; next !== undefined; n++) {
        if (n >= maxPages) throw new PageLimitError(resolve(path).shown, maxPages)
        const a = await mustGet(next)
        yield asList(next, a)
        next = a.links.prev
        // The first page is already read; do not fetch it again.
        if (next !== undefined && a.links.first !== undefined && next === a.links.first) break
      }
      yield asList(path, first)
    },
  }
}
