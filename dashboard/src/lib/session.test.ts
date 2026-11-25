import { afterEach, describe, expect, it, vi } from 'vitest'

import { api, applyPlan, openSession, readProof, resetSession } from './session'

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
