import { describe, expect, it } from 'vitest'

import { createGitHub } from './github'
import { createHub, type Socket, type Store } from './hub'
import type { LiveEvent } from './messages'
import { minInterval } from './poller'
import type { State } from './state'
import { FakeGitHub, issueJSON, issuesPath, token } from './testing'

const t0 = Date.parse('2026-11-25T00:00:00Z')
const cli = 'taco3064/gh-nibfin'

class FakeSocket implements Socket {
  sent: string[] = []
  closed = false
  broken = false
  send(message: string): void {
    if (this.broken) throw new Error('socket gone')
    this.sent.push(message)
  }
  close(): void {
    this.closed = true
  }
  events(): LiveEvent[] {
    return this.sent.map((m) => JSON.parse(m) as LiveEvent)
  }
}

class FakeStore implements Store {
  state: State | undefined
  saved: string[] = []
  alarm: number | null = null
  alarmSetBeforeFetch: boolean | null = null
  clients: FakeSocket[] = []
  async load() {
    return this.state === undefined ? undefined : structuredClone(this.state)
  }
  async save(state: State) {
    this.state = structuredClone(state)
    this.saved.push(JSON.stringify(state))
  }
  sockets() {
    return this.clients
  }
  async getAlarm() {
    return this.alarm
  }
  async setAlarm(ms: number) {
    this.alarm = ms
  }
}

function setup(opts: { token?: string } = {}) {
  const fake = new FakeGitHub()
  const store = new FakeStore()
  let now = t0
  const fetches: number[] = []
  const hub = createHub({
    store,
    now: () => now,
    github: () =>
      createGitHub({
        token: opts.token ?? token,
        fetch: (input, init) => {
          fetches.push(store.alarm ?? -1)
          return fake.fetch(input, init)
        },
      }),
  })
  return { fake, store, hub, fetches, tick: (ms = minInterval) => (now += ms), now: () => now }
}

const kinds = (s: FakeSocket) => s.events().map((e) => e.event)

describe('hub', () => {
  it('schedules a check when a page connects to an idle Worker', async () => {
    const { store, hub } = setup()
    const s = new FakeSocket()
    store.clients.push(s)
    await hub.connect(s)
    expect(s.sent).toEqual([])
    expect(store.alarm).toBe(t0)
  })

  it('greets a page with the stored snapshot and checked, or the last error', async () => {
    const { fake, store, hub, tick } = setup()
    const a = new FakeSocket()
    store.clients.push(a)
    await hub.alarm()
    const b = new FakeSocket()
    await hub.connect(b)
    expect(kinds(b)).toEqual(['snapshot', 'checked'])
    expect(b.events()[1]).toEqual({ event: 'checked', at: new Date(t0).toISOString() })

    fake.fail(issuesPath(cli), 500, 'Server Error')
    tick()
    await hub.alarm()
    const c = new FakeSocket()
    await hub.connect(c)
    expect(kinds(c)).toEqual(['snapshot', 'error'])
    expect(c.events()[1]).toMatchObject({ event: 'error', source: 'github', error: { code: 'github_api_failed' } })
  })

  it('broadcasts the same snapshot text to every page on a change, and only checked otherwise', async () => {
    const { fake, store, hub, tick } = setup()
    const a = new FakeSocket()
    const b = new FakeSocket()
    store.clients.push(a, b)
    await hub.alarm()
    expect(kinds(a)).toEqual(['snapshot', 'checked'])
    expect(a.sent).toEqual(b.sent)

    tick()
    await hub.alarm()
    expect(kinds(a)).toEqual(['snapshot', 'checked', 'checked'])
    expect(a.events()[2]).toEqual({ event: 'checked', at: new Date(t0 + minInterval).toISOString() })

    fake.set(issuesPath(cli), [issueJSON(8)])
    tick()
    await hub.alarm()
    expect(kinds(b)).toEqual(['snapshot', 'checked', 'checked', 'snapshot', 'checked'])
    expect(a.sent).toEqual(b.sent)
    const snap = a.events()[3]
    expect(snap.event === 'snapshot' && snap.result.repos[1].issues?.[0].number).toBe(8)
    expect(store.state?.sentSeq).toBe(store.state?.seq)
  })

  it('sends error and keeps the old snapshot when the check fails', async () => {
    const { fake, store, hub, tick } = setup()
    const a = new FakeSocket()
    store.clients.push(a)
    fake.set(issuesPath(cli), [issueJSON(1)])
    await hub.alarm()
    fake.fail(issuesPath(cli), 403, 'Resource not accessible by personal access token')
    tick()
    await hub.alarm()
    expect(kinds(a)).toEqual(['snapshot', 'checked', 'error'])
    expect(store.state?.data?.repos[1].issues?.map((i) => i.number)).toEqual([1])
  })

  it('stops polling while no page is connected', async () => {
    const { store, hub, fetches } = setup()
    await hub.alarm()
    expect(store.alarm).toBeNull()
    expect(fetches).toEqual([])
  })

  it('schedules the next alarm before calling GitHub', async () => {
    const { store, hub, fetches } = setup()
    store.clients.push(new FakeSocket())
    await hub.alarm()
    expect(fetches.length).toBeGreaterThan(0)
    expect(new Set(fetches)).toEqual(new Set([t0 + minInterval]))
    expect(store.alarm).toBe(t0 + minInterval)
  })

  it('keeps the next alarm when the check throws', async () => {
    const { store } = setup()
    const s = new FakeSocket()
    store.clients.push(s)
    const hub = createHub({
      store,
      now: () => t0,
      github: () => {
        throw new Error('boom')
      },
    })
    await hub.alarm()
    expect(store.alarm).toBe(t0 + minInterval)
    expect(s.events()).toEqual([{ event: 'error', source: 'github', error: { code: 'github_api_failed', message: 'the check failed: boom' } }])
  })

  it('sends a saved snapshot again when the last send did not happen', async () => {
    const { store, hub } = setup()
    const a = new FakeSocket()
    store.clients.push(a)
    await hub.alarm()
    const s = store.state as State
    store.state = { ...s, seq: s.seq + 1 }
    const b = new FakeSocket()
    store.clients.push(b)
    await hub.alarm()
    expect(kinds(b)[0]).toBe('snapshot')
    expect(store.state?.sentSeq).toBe(store.state?.seq)
  })

  it('never sends a runner name kept in a snapshot stored before names were dropped', async () => {
    const { store, hub } = setup()
    const a = new FakeSocket()
    store.clients.push(a)
    await hub.alarm()
    const s = store.state as State
    const data = structuredClone(s.data) as NonNullable<State['data']>
    data.runners[0].runners = [{ id: 7, name: 'host-1', status: 'online', busy: false, job: null } as (typeof data.runners)[0]['runners'][0]]
    store.state = { ...s, data, seq: s.seq + 1 }

    const b = new FakeSocket()
    await hub.connect(b)
    const c = new FakeSocket()
    store.clients.push(c)
    await hub.alarm()
    for (const sock of [b, c]) {
      const snap = sock.events().find((e) => e.event === 'snapshot')
      expect(snap?.event === 'snapshot' && snap.result.runners[0].runners).toEqual([{ id: 7, status: 'online', busy: false, job: null }])
      expect(sock.sent.join('')).not.toContain('host-1')
    }
  })

  it('closes a page whose send throws and still reaches the others', async () => {
    const { store, hub } = setup()
    const bad = new FakeSocket()
    bad.broken = true
    const good = new FakeSocket()
    store.clients.push(bad, good)
    await hub.alarm()
    expect(bad.closed).toBe(true)
    expect(kinds(good)).toEqual(['snapshot', 'checked'])
  })

  it('sends auth_unavailable without a token and makes no request', async () => {
    const { store, hub, fetches } = setup({ token: '' })
    const a = new FakeSocket()
    store.clients.push(a)
    await hub.alarm()
    expect(fetches).toEqual([])
    expect(a.events()).toEqual([{ event: 'error', source: 'github', error: { code: 'auth_unavailable', message: 'the Worker has no GITHUB_TOKEN secret' } }])
    expect(store.alarm).toBe(t0 + minInterval)
  })

  it('sends only valid LiveEvent JSON and never the token', async () => {
    const { fake, store, hub, tick } = setup()
    const a = new FakeSocket()
    store.clients.push(a)
    fake.set(issuesPath(cli), [issueJSON(1)])
    await hub.alarm()
    fake.fail(issuesPath(cli), 401, 'Bad credentials')
    tick()
    await hub.alarm()
    await hub.connect(a)
    expect(a.sent.length).toBeGreaterThan(3)
    for (const m of a.sent) {
      const e = JSON.parse(m) as LiveEvent
      expect(['snapshot', 'checked', 'error']).toContain(e.event)
      if (e.event === 'error') expect(e.source).toBe('github')
      if (e.event !== 'error') expect(Number.isNaN(Date.parse(e.at))).toBe(false)
      expect(m).not.toContain(token)
    }
    for (const saved of store.saved) expect(saved).not.toContain(token)
  })
})
