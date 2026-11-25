import { afterEach, describe, expect, it, vi } from 'vitest'

import { openLive, parseEvents, type LiveEvent } from './live'
import { openSession, resetSession } from './session'

afterEach(() => resetSession())

describe('parseEvents', () => {
  it('dispatches on a blank line and keeps the rest', () => {
    expect(parseEvents('data: {"a":1}\n\ndata: {"b"')).toEqual({ events: ['{"a":1}'], rest: 'data: {"b"' })
    expect(parseEvents('data: {"b"')).toEqual({ events: [], rest: 'data: {"b"' })
  })

  it('reads several events, comments, CRLF and multi-line data', () => {
    const { events, rest } = parseEvents(': hello\n\ndata: 1\n\ndata: a\r\ndata:b\r\n\r\nevent: x\nid: 2\ndata: 3\n\n')
    expect(events).toEqual(['1', 'a\nb', '3'])
    expect(rest).toBe('')
  })

  it('keeps an event whose blank line has not arrived', () => {
    const first = parseEvents('data: {"x":')
    const second = parseEvents(first.rest + '1}\n')
    expect(second.events).toEqual([])
    expect(parseEvents(second.rest + '\n').events).toEqual(['{"x":1}'])
  })
})

function streamOf(chunks: string[]): ReadableStream<Uint8Array> {
  const enc = new TextEncoder()
  return new ReadableStream({
    start(c) {
      for (const ch of chunks) c.enqueue(enc.encode(ch))
      c.close()
    },
  })
}

async function withSession() {
  await openSession('P', vi.fn(async () => new Response(JSON.stringify({ session: 'S1' }), { status: 200 })))
}

describe('openLive', () => {
  it('sends the session header, not a URL token, and reads split chunks', async () => {
    await withSession()
    const f = vi.fn(
      async () =>
        new Response(streamOf(['data: {"event":"snap', 'shot","at":"2026-11-25T10:00:00Z","result":{}}\n', '\ndata: {"event":"checked","at":"x"}\n\n']), {
          status: 200,
          headers: { 'Content-Type': 'text/event-stream' },
        }),
    )
    const got: LiveEvent[] = []
    const end = await openLive((e) => got.push(e), new AbortController().signal, f)
    const [url, init] = f.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('/api/live')
    expect(init.headers).toEqual({ 'X-Nibfin-Session': 'S1' })
    expect(got.map((e) => e.event)).toEqual(['snapshot', 'checked'])
    expect(end).toEqual({ reason: 'closed', error: { code: 'stream_closed', message: 'the live stream ended' } })
  })

  it('ends with the last status error the server sent', async () => {
    await withSession()
    const body = 'data: {"event":"error","source":"status","error":{"code":"auth_unavailable","message":"no login"}}\n\n'
    const end = await openLive(() => {}, new AbortController().signal, vi.fn(async () => new Response(streamOf([body]), { status: 200 })))
    expect(end).toEqual({ reason: 'closed', error: { code: 'auth_unavailable', message: 'no login' } })
  })

  it('reports a refused request with the server error', async () => {
    await withSession()
    const f = vi.fn(async () => new Response(JSON.stringify({ error: { code: 'not_found', message: 'no live' } }), { status: 404 }))
    expect(await openLive(() => {}, new AbortController().signal, f)).toEqual({
      reason: 'http',
      error: { code: 'not_found', message: 'no live' },
    })
  })

  it('reports a network failure and an abort', async () => {
    await withSession()
    const f = vi.fn(async () => {
      throw new TypeError('offline')
    })
    expect((await openLive(() => {}, new AbortController().signal, f)).reason).toBe('network')
    const ctl = new AbortController()
    ctl.abort()
    expect((await openLive(() => {}, ctl.signal, f)).reason).toBe('aborted')
  })

  it('needs a session', async () => {
    const f = vi.fn()
    expect((await openLive(() => {}, new AbortController().signal, f)).error.code).toBe('session_required')
    expect(f).not.toHaveBeenCalled()
  })
})
