import { describe, expect, it } from 'vitest'

import type { Command } from './operation'
import { clock, elapsed, freeFraction, gib, githubLabel, hourTicks, jobLabel, laneBars, periodLabel, providerPreset, serviceLabel, slotsByProvider, tileState } from './runner'
import type { HostReport, RunnerState } from './status'

function slot(over: Partial<RunnerState> = {}): RunnerState {
  return { slot: 'claude-1', provider: 'claude', configured: true, unit: true, enabled: true, active: true, listed: true, status: 'online', busy: false, ...over }
}

describe('gib', () => {
  it('shows one decimal', () => {
    expect(gib(16_303_136)).toBe('15.5 GiB')
    expect(gib(0)).toBe('0.0 GiB')
  })
})

describe('serviceLabel', () => {
  it('is active', () => expect(serviceLabel(slot())).toEqual({ text: 'Service active', tone: 'success' }))
  it('is inactive', () => expect(serviceLabel(slot({ active: false }))).toEqual({ text: 'Service inactive', tone: 'danger' }))
  it('names what an incomplete slot misses', () => {
    expect(serviceLabel(slot({ configured: false, unit: false, enabled: false, active: false })).text).toBe('Incomplete: not configured, no unit, not enabled')
    expect(serviceLabel(slot({ enabled: false })).text).toBe('Incomplete: not enabled')
  })
})

describe('githubLabel', () => {
  it('is online and busy', () => expect(githubLabel(slot({ busy: true })).text).toBe('Online · Busy'))
  it('is online and idle', () => expect(githubLabel(slot()).text).toBe('Online · Idle'))
  it('is offline', () => expect(githubLabel(slot({ status: 'offline' })).text).toBe('Offline'))
  it('is not listed', () => expect(githubLabel(slot({ listed: false, status: undefined })).text).toBe('Not listed on GitHub'))
  it('is not read with a GitHub error', () => expect(githubLabel(slot(), { code: 'github_error', message: 'm' }).text).toBe('GitHub not read'))
})

describe('slotsByProvider', () => {
  it('groups by the report providers first, then other slot providers', () => {
    const r: HostReport = {
      host: {},
      memory: { totalKB: 0, availableKB: 0 },
      runners: { runners: [slot({ slot: 'x-1', provider: 'x' }), slot(), slot({ slot: 'claude-2' })] },
      providers: [
        { provider: 'claude', loggedIn: true },
        { provider: 'codex', loggedIn: false },
      ],
    }
    const g = slotsByProvider(r)
    expect([...g.keys()]).toEqual(['claude', 'codex', 'x'])
    expect(g.get('claude')!.map((s) => s.slot)).toEqual(['claude-1', 'claude-2'])
    expect(g.get('codex')).toEqual([])
  })
})

describe('providerPreset', () => {
  const cmd = (args: Command['args'], flags: Command['flags'] = []): Command => ({ path: 'p', api: 'p', short: '', long: '', args, flags })
  it('fills the first field allowing the provider', () => {
    const c = cmd([{ name: 'provider', required: true, repeatable: false, allowed: ['claude', 'codex'], param: 'provider' }])
    expect(providerPreset(c, 'codex')).toEqual({ provider: 'codex' })
  })
  it('is empty without a matching field', () => {
    expect(providerPreset(cmd([{ name: 'name', required: true, repeatable: false, param: 'name' }]), 'codex')).toEqual({})
    expect(providerPreset(cmd([{ name: 'provider', required: true, repeatable: false, allowed: ['claude'], param: 'provider' }]), 'codex')).toEqual({})
  })
})

describe('githubLabel without admin', () => {
  it('says GitHub needs admin', () => expect(githubLabel(slot({ listed: false, status: undefined }), undefined, true).text).toBe('GitHub needs admin'))
})

describe('tileState', () => {
  it('is busy and idle from GitHub', () => {
    expect(tileState(slot({ busy: true }), true)).toEqual({ state: 'busy', github: true })
    expect(tileState(slot(), true)).toEqual({ state: 'idle', github: true })
  })
  it('is offline from GitHub even with an active service', () => {
    expect(tileState(slot({ status: 'offline' }), true)).toEqual({ state: 'offline', github: true })
  })
  it('is busy from a current Job when GitHub says idle or was not read', () => {
    expect(tileState(slot(), true, { source: 'github' }).state).toBe('busy')
    expect(tileState(slot({ listed: false, status: undefined }), false, { source: 'container' })).toEqual({ state: 'busy', github: false })
  })
  it('uses the local service when GitHub was not read', () => {
    expect(tileState(slot({ listed: false, status: undefined }), false)).toEqual({ state: 'idle', github: false })
    expect(tileState(slot({ active: false }), false)).toEqual({ state: 'offline', github: false })
    expect(tileState(slot({ enabled: false }), false).state).toBe('offline')
    // An unlisted slot is local even when the list was read.
    expect(tileState(slot({ listed: false }), true)).toEqual({ state: 'idle', github: false })
  })
})

describe('elapsed', () => {
  const now = Date.parse('2026-11-26T12:00:00Z')
  it('shows hours and minutes', () => expect(elapsed('2026-11-26T10:55:00Z', now)).toBe('1h 05m'))
  it('shows minutes', () => expect(elapsed('2026-11-26T11:57:00Z', now)).toBe('3m'))
  it('shows under a minute', () => expect(elapsed('2026-11-26T11:59:30Z', now)).toBe('<1m'))
  it('is empty without a start', () => {
    expect(elapsed(undefined, now)).toBe('')
    expect(elapsed('nope', now)).toBe('')
  })
})

describe('laneBars', () => {
  const from = '2026-11-25T12:00:00Z'
  const to = '2026-11-26T12:00:00Z'
  it('places periods as fractions of the window', () => {
    const bars = laneBars([{ start: '2026-11-26T00:00:00Z', end: '2026-11-26T06:00:00Z', failed: false }], from, to)
    expect(bars).toHaveLength(1)
    expect(bars[0].x).toBeCloseTo(0.5)
    expect(bars[0].width).toBeCloseTo(0.25)
  })
  it('clips to the window and runs an open period to the end', () => {
    const bars = laneBars(
      [
        { start: '2026-11-25T06:00:00Z', end: '2026-11-25T18:00:00Z', failed: true },
        { start: '2026-11-26T06:00:00Z', failed: false },
      ],
      from,
      to,
    )
    expect(bars[0].x).toBe(0)
    expect(bars[0].width).toBeCloseTo(0.25)
    expect(bars[1].x).toBeCloseTo(0.75)
    expect(bars[1].width).toBeCloseTo(0.25)
  })
  it('drops periods outside the window and keeps a minimum width', () => {
    const bars = laneBars(
      [
        { start: '2026-11-24T06:00:00Z', end: '2026-11-24T07:00:00Z', failed: false },
        { start: '2026-11-26T06:00:00Z', end: '2026-11-26T06:00:01Z', failed: false },
        { start: 'bad', failed: false },
      ],
      from,
      to,
    )
    expect(bars).toHaveLength(1)
    expect(bars[0].width).toBe(0.004)
  })
  it('is empty for an unreadable window', () => expect(laneBars([{ start: from, failed: false }], 'x', to)).toEqual([]))
})

describe('hourTicks', () => {
  it('marks whole hours divisible by the step, inside the window', () => {
    const ticks = hourTicks('2026-11-25T12:30:00Z', '2026-11-26T12:30:00Z')
    expect(ticks).toHaveLength(4)
    for (const t of ticks) {
      expect(new Date(t.at).getHours() % 6).toBe(0)
      expect(new Date(t.at).getMinutes()).toBe(0)
      expect(t.x).toBeGreaterThan(0)
      expect(t.x).toBeLessThanOrEqual(1)
    }
  })
})

describe('clock', () => {
  it('shows 24-hour time', () => expect(clock(new Date(2026, 10, 26, 15, 4).getTime())).toBe('15:04'))
})

describe('freeFraction', () => {
  it('is the free share', () => expect(freeFraction({ totalKB: 16_000_000, availableKB: 4_000_000 })).toBe(0.25))
  it('is bounded', () => expect(freeFraction({ totalKB: 10, availableKB: 20 })).toBe(1))
  it('is null without a total', () => expect(freeFraction({ totalKB: 0, availableKB: 0 })).toBeNull())
})

describe('periodLabel', () => {
  it('names the Issue, stage, times and failure', () => {
    const start = new Date(2026, 10, 26, 8, 1).toISOString()
    const end = new Date(2026, 10, 26, 8, 30).toISOString()
    expect(periodLabel({ start, end, failed: true, repo: 'acme/app', issue: 5, stage: 'shape' })).toBe('acme/app#5 · shape · 08:01–08:30 · failed')
    expect(periodLabel({ start, failed: false })).toBe('an unknown Issue · 08:01–now')
    expect(jobLabel({ repo: 'acme/app' })).toBe('an unknown Issue')
  })
})
