import { afterEach, describe, expect, it, vi } from 'vitest'

import { api, apiGet, applyPlan, applyStream, openSession, planRequest, readProof, readStatus, resetSession, sendCode } from './session'

vi.stubGlobal('location', { pathname: '/', search: '' })

function reply(status: number, body: unknown) {
  return vi.fn(async () => new Response(JSON.stringify(body), { status }))
}

afterEach(() => resetSession())

describe('readProof', () => {
  it('returns the proof and clears the fragment', () => {
    const history = { replaceState: vi.fn() }
    expect(readProof('#proof=ABC234', history)).toBe('ABC234')
    expect(history.replaceState).toHaveBeenCalledWith(null, '', '/')
  })

  it.each(['', '#', '#proof=', '#other=ABC', '#proof=ABC&x=1'])('ignores %j', (hash) => {
    const history = { replaceState: vi.fn() }
    expect(readProof(hash, history)).toBeNull()
    expect(history.replaceState).not.toHaveBeenCalled()
  })

  it.each(['#proof=abc', '#proof=AB01', '#proof=AB8'])('rejects the wrong alphabet in %j', (hash) => {
    expect(readProof(hash)).toBeNull()
  })
})

describe('openSession', () => {
  it('exchanges the proof and sends the session with API calls', async () => {
    const f = reply(200, { session: 'S1' })
    expect(await openSession('P', f)).toEqual({ ok: true, value: 'S1' })
    expect(f).toHaveBeenCalledWith('/api/session', expect.objectContaining({ method: 'POST', headers: { 'X-Nibfin-Proof': 'P' } }))

    const g = reply(200, { plan: {}, result: {} })
    expect(await api('/api/commands/status/plan', {}, g)).toEqual({ ok: true, value: { plan: {}, result: {} } })
    const init = g.mock.calls[0] as unknown as [string, RequestInit]
    expect(init[1].headers).toMatchObject({ 'X-Nibfin-Session': 'S1' })
    expect(init[1].body).toBe('{}')
  })

  it('returns proof_invalid', async () => {
    const f = reply(401, { error: { code: 'proof_invalid', message: 'used' } })
    expect(await openSession('P', f)).toEqual({ ok: false, error: { code: 'proof_invalid', message: 'used' } })
    expect(await api('/api/x', {}, reply(200, {}))).toMatchObject({ ok: false, error: { code: 'session_required' } })
  })

  it('reports a network error', async () => {
    const f = vi.fn(async () => {
      throw new TypeError('Failed to fetch')
    })
    const r = await openSession('P', f)
    expect(r).toMatchObject({ ok: false, error: { code: 'network_error' } })
  })

  it('exchanges the proof only once on repeated calls', async () => {
    const f = reply(200, { session: 'S1' })
    const [a, b] = await Promise.all([openSession('P', f), openSession('P', f)])
    expect(await openSession('P', f)).toEqual(a)
    expect(b).toEqual(a)
    expect(f).toHaveBeenCalledTimes(1)
  })
})

describe('applyPlan', () => {
  function ndjson(text: string, status = 200) {
    return vi.fn(async () => new Response(text, { status, headers: { 'Content-Type': 'application/x-ndjson' } }))
  }

  async function opened() {
    await openSession('P', reply(200, { session: 'S1' }))
  }

  it('returns the last result line and skips login steps', async () => {
    await opened()
    const f = ndjson('{"event":"login_step","step":{}}\n{"event":"result","result":{"a":"paused"}}\n')
    expect(await applyPlan('ID', f)).toEqual({ ok: true, value: { a: 'paused' } })
    const init = f.mock.calls[0] as unknown as [string, RequestInit]
    expect(init[0]).toBe('/api/plans/ID/apply')
    expect(init[1].headers).toMatchObject({ 'X-Nibfin-Session': 'S1' })
    expect(init[1].body).toBe('{"confirm":false}')
  })

  it('returns an error line', async () => {
    await opened()
    expect(await applyPlan('ID', ndjson('{"event":"error","error":{"code":"not_applied","message":"m"}}\n'))).toEqual({
      ok: false,
      error: { code: 'not_applied', message: 'm' },
    })
  })

  it('is unknown without a final line', async () => {
    await opened()
    expect(await applyPlan('ID', ndjson('{"event":"login_step","step":{}}\n{"event":"resu'))).toMatchObject({
      ok: false,
      unknown: true,
      error: { code: 'stream_incomplete' },
    })
  })

  it('returns a non-2xx JSON error with its status', async () => {
    await opened()
    expect(await applyPlan('ID', reply(409, { error: { code: 'operation_running', message: 'busy' } }))).toEqual({
      ok: false,
      status: 409,
      error: { code: 'operation_running', message: 'busy' },
    })
  })

  it('needs a session', async () => {
    expect(await applyPlan('ID', ndjson(''))).toMatchObject({ ok: false, error: { code: 'session_required' } })
  })
})

describe('applyStream', () => {
  async function opened() {
    await openSession('P', reply(200, { session: 'S1' }))
  }

  // chunked answers the text in the given chunks, which may split lines.
  function chunked(chunks: string[], fail?: Error) {
    return vi.fn(async () => {
      const enc = new TextEncoder()
      let i = 0
      const body = new ReadableStream<Uint8Array>({
        pull(c) {
          if (i < chunks.length) c.enqueue(enc.encode(chunks[i++]))
          else if (fail) c.error(fail)
          else c.close()
        },
      })
      return new Response(body, { status: 200, headers: { 'Content-Type': 'application/x-ndjson' } })
    })
  }

  const progress = (id: string, phase: string) => JSON.stringify({ event: 'progress', progress: { id, action: 'a', target: 't', phase } })

  it('hands each line to onEvent across split chunks and returns the result', async () => {
    await opened()
    const text = `${progress('one', 'started')}\n${progress('one', 'done')}\n{"event":"login_step","step":{}}\n{"event":"result","result":{"items":[]}}\n`
    const f = chunked([text.slice(0, 7), text.slice(7, 60), text.slice(60, 61), text.slice(61)])
    const seen: string[] = []
    const r = await applyStream('ID', true, (l) => seen.push(`${l.event}:${(l.progress as { phase?: string } | undefined)?.phase ?? ''}`), f)
    expect(r).toEqual({ ok: true, value: { items: [] } })
    expect(seen).toEqual(['progress:started', 'progress:done', 'login_step:', 'result:'])
    const init = f.mock.calls[0] as unknown as [string, RequestInit]
    expect(init[1].body).toBe('{"confirm":true}')
  })

  it('keeps the result of an error line', async () => {
    await opened()
    const line = '{"event":"error","error":{"code":"runner_busy","message":"run it again"},"result":{"items":[{"id":"a"}]}}'
    expect(await applyStream('ID', true, () => {}, chunked([line]))).toEqual({
      ok: false,
      error: { code: 'runner_busy', message: 'run it again' },
      result: { items: [{ id: 'a' }] },
    })
    expect(await applyStream('ID', true, () => {}, chunked(['{"event":"error","error":{"code":"x","message":"m"}}\n']))).toEqual({
      ok: false,
      error: { code: 'x', message: 'm' },
    })
  })

  it('is unknown when the stream ends without a final line or breaks', async () => {
    await opened()
    const seen: unknown[] = []
    expect(await applyStream('ID', true, (l) => seen.push(l), chunked([`${progress('one', 'done')}\n{"event":"res`]))).toMatchObject({
      ok: false,
      unknown: true,
      error: { code: 'stream_incomplete' },
    })
    expect(seen).toHaveLength(1)
    expect(await applyStream('ID', true, () => {}, chunked([`${progress('one', 'started')}\n`], new Error('reset')))).toMatchObject({
      ok: false,
      unknown: true,
      error: { code: 'network_error' },
    })
  })

  it('returns a non-2xx answer with its status and reads no stream', async () => {
    await opened()
    const seen: unknown[] = []
    expect(await applyStream('ID', true, (l) => seen.push(l), reply(404, { error: { code: 'not_found', message: 'no plan' } }))).toEqual({
      ok: false,
      status: 404,
      error: { code: 'not_found', message: 'no plan' },
    })
    expect(seen).toEqual([])
  })
})

describe('applyPlan keeps its outcomes', () => {
  it('drops the result of an error line', async () => {
    await openSession('P', reply(200, { session: 'S1' }))
    const f = vi.fn(async () => new Response('{"event":"error","error":{"code":"x","message":"m"},"result":{"a":1}}\n', { status: 200 }))
    expect(await applyPlan('ID', f)).toEqual({ ok: false, error: { code: 'x', message: 'm' } })
  })
})

describe('planRequest', () => {
  it('keeps plan and result on a 422', async () => {
    await openSession('P', reply(200, { session: 'S1' }))
    const f = reply(422, { error: { code: 'read_failed', message: 'm' }, plan: { operation: 'upgrade' }, result: { items: [] } })
    expect(await planRequest('/api/commands/upgrade/plan', { repo: 'a/b' }, f)).toEqual({
      ok: false,
      status: 422,
      error: { code: 'read_failed', message: 'm' },
      plan: { operation: 'upgrade' },
      result: { items: [] },
    })
    const init = f.mock.calls[0] as unknown as [string, RequestInit]
    expect(init[1].body).toBe('{"repo":"a/b"}')
    // The api helper keeps dropping them.
    expect(await api('/api/commands/upgrade/plan', {}, f)).toEqual({ ok: false, error: { code: 'read_failed', message: 'm' } })
  })

  it('returns a plan', async () => {
    await openSession('P', reply(200, { session: 'S1' }))
    expect(await planRequest('/x', {}, reply(200, { id: 'P1', plan: {} }))).toEqual({ ok: true, value: { id: 'P1', plan: {} } })
  })
})

describe('apiGet', () => {
  it('reads with the session', async () => {
    await openSession('P', reply(200, { session: 'S1' }))
    const f = reply(200, { commands: [] })
    expect(await apiGet('/api/commands', f)).toEqual({ ok: true, value: { commands: [] } })
    const init = f.mock.calls[0] as unknown as [string, RequestInit]
    expect(init[1]).toMatchObject({ method: 'GET', headers: { 'X-Nibfin-Session': 'S1' } })
  })

  it('needs a session', async () => {
    expect(await apiGet('/api/commands', reply(200, {}))).toMatchObject({ ok: false, error: { code: 'session_required' } })
  })
})

describe('sendCode', () => {
  async function opened() {
    await openSession('P', reply(200, { session: 'S1' }))
  }

  it('is ok on 204 and posts the code once with the session', async () => {
    await opened()
    const f = vi.fn(async () => new Response(null, { status: 204 }))
    expect(await sendCode('ID 1', 'the-code', f)).toEqual({ ok: true })
    expect(f).toHaveBeenCalledTimes(1)
    const [path, init] = f.mock.calls[0] as unknown as [string, RequestInit]
    expect(path).toBe('/api/plans/ID%201/code')
    expect(init.method).toBe('POST')
    expect(init.headers).toMatchObject({ 'X-Nibfin-Session': 'S1' })
    expect(init.body).toBe('{"code":"the-code"}')
  })

  it('returns the error body and status on 409', async () => {
    await opened()
    const r = await sendCode('ID', 'c', reply(409, { error: { code: 'invalid_argument', message: 'no code requested' } }))
    expect(r).toEqual({ ok: false, error: { code: 'invalid_argument', message: 'no code requested' }, status: 409 })
  })

  it('reports a network error', async () => {
    await opened()
    const r = await sendCode(
      'ID',
      'c',
      vi.fn(async () => {
        throw new TypeError('Failed to fetch')
      }),
    )
    expect(r).toMatchObject({ ok: false, error: { code: 'network_error' } })
  })

  it('needs a session', async () => {
    expect(await sendCode('ID', 'c', reply(204, {}))).toMatchObject({ ok: false, error: { code: 'session_required' } })
  })
})

describe('applyStream abort', () => {
  it('passes the signal and an abort mid-stream comes out as unknown', async () => {
    await openSession('P', reply(200, { session: 'S1' }))
    const ac = new AbortController()
    const enc = new TextEncoder()
    const f = vi.fn(async (_path: RequestInfo | URL, init?: RequestInit) => {
      const body = new ReadableStream<Uint8Array>({
        start(c) {
          c.enqueue(enc.encode('{"event":"login_step","step":{"provider":"claude","kind":"code_requested"}}\n'))
          init?.signal?.addEventListener('abort', () => c.error(init.signal?.reason))
        },
      })
      return new Response(body, { status: 200 })
    })
    const seen: string[] = []
    const p = applyStream('ID', true, (l) => {
      seen.push(l.event ?? '')
      ac.abort()
    }, f, ac.signal)
    const r = await p
    expect((f.mock.calls[0] as unknown as [string, RequestInit])[1].signal).toBe(ac.signal)
    expect(seen).toEqual(['login_step'])
    expect(r).toMatchObject({ ok: false, unknown: true, error: { code: 'network_error' } })
  })
})

describe('readStatus', () => {
  it('posts the repository to the status read route and hands each partial to onPartial', async () => {
    await openSession('P', reply(200, { session: 'S1' }))
    const lines = [
      '{"event":"partial","result":{"repo":"github.com/o/r","issues":[]},"loading":["image","runs"]}',
      '{"event":"partial","result":{"repo":"github.com/o/r","issues":[],"image":{"set":"none"}},"loading":["runs"]}',
      '{"event":"result","result":{"repo":"github.com/o/r","done":true}}',
    ]
    const f = vi.fn(async () => new Response(`${lines.join('\n')}\n`, { status: 200 }))
    const seen: [unknown, string[]][] = []
    const r = await readStatus('o/r', (result, loading) => seen.push([result, loading]), f)
    expect(r).toEqual({ ok: true, value: { repo: 'github.com/o/r', done: true } })
    expect(seen).toEqual([
      [{ repo: 'github.com/o/r', issues: [] }, ['image', 'runs']],
      [{ repo: 'github.com/o/r', issues: [], image: { set: 'none' } }, ['runs']],
    ])
    const [path, init] = f.mock.calls[0] as unknown as [string, RequestInit]
    expect(path).toBe('/api/commands/status/read')
    expect(init.body).toBe('{"repo":"o/r"}')
    expect((init.headers as Record<string, string>)['X-Nibfin-Session']).toBe('S1')
  })

  it('returns an error line as the failure and skips malformed partials', async () => {
    await openSession('P', reply(200, { session: 'S1' }))
    const text = '{"event":"partial","loading":["host"]}\n{"event":"error","error":{"code":"auth_unavailable","message":"no login"}}\n'
    const seen: unknown[] = []
    const r = await readStatus('', (result) => seen.push(result), vi.fn(async () => new Response(text, { status: 200 })))
    expect(r).toEqual({ ok: false, error: { code: 'auth_unavailable', message: 'no login' } })
    expect(seen).toEqual([])
  })
})
