import type { Page } from '@playwright/test'

// A controllable mock of GET /api/live. Playwright's route.fulfill ends a
// body at once, so the page's fetch of /api/live is answered in the page by
// an init script whose stream stays open; the spec pushes frames into it.

// LiveAnswer is how the next stream opens: frames to send first, and
// whether the stream then stays open; or a refused request.
export type LiveAnswer = { frames: string[]; open: boolean } | { httpStatus: number; error: { code: string; message: string } }

export interface LiveMock {
  // opens counts the streams the page opened; sessions their headers.
  opens: number
  sessions: (string | null)[]
  urls: string[]
  // answer decides each new stream; hold, when set, delays it.
  answer: () => LiveAnswer
  hold?: Promise<void>
}

export const frame = (v: unknown) => `data: ${JSON.stringify(v)}\n\n`

export async function mockLive(page: Page, answer: () => LiveAnswer): Promise<LiveMock> {
  const m: LiveMock = { opens: 0, sessions: [], urls: [], answer }
  await page.exposeFunction('__nibfinLiveOpen', async (url: string, session: string | null) => {
    m.opens++
    m.urls.push(url)
    m.sessions.push(session)
    if (m.hold) await m.hold
    return m.answer()
  })
  await page.addInitScript(() => {
    const w = window as unknown as {
      __nibfinLiveOpen: (url: string, session: string | null) => Promise<LiveAnswer>
      __nibfinLive?: ReadableStreamDefaultController<Uint8Array>
    }
    const real = window.fetch.bind(window)
    window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
      if (new URL(url, location.href).pathname !== '/api/live') return real(input, init)
      const headers = new Headers(init?.headers)
      const a = await w.__nibfinLiveOpen(url, headers.get('X-Nibfin-Session'))
      if ('httpStatus' in a) {
        return new Response(JSON.stringify({ error: a.error }), { status: a.httpStatus, headers: { 'Content-Type': 'application/json' } })
      }
      const enc = new TextEncoder()
      let ctl: ReadableStreamDefaultController<Uint8Array> | undefined
      const body = new ReadableStream<Uint8Array>({
        start(c) {
          ctl = c
          for (const f of a.frames) c.enqueue(enc.encode(f))
          if (a.open) w.__nibfinLive = c
          else c.close()
        },
      })
      init?.signal?.addEventListener('abort', () => {
        try {
          ctl?.error(new DOMException('aborted', 'AbortError'))
        } catch {
          // Already closed.
        }
        if (w.__nibfinLive === ctl) w.__nibfinLive = undefined
      })
      return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } })
    }
  })
  return m
}

// push sends frames on the open stream.
export async function push(page: Page, ...events: unknown[]) {
  await page.evaluate((fs) => {
    const c = (window as unknown as { __nibfinLive?: ReadableStreamDefaultController<Uint8Array> }).__nibfinLive
    const enc = new TextEncoder()
    for (const f of fs) c?.enqueue(enc.encode(f))
  }, events.map(frame))
}

// endStream closes the open stream.
export async function endStream(page: Page) {
  await page.evaluate(() => {
    const w = window as unknown as { __nibfinLive?: ReadableStreamDefaultController<Uint8Array> }
    w.__nibfinLive?.close()
    w.__nibfinLive = undefined
  })
}
