import { describe, expect, it } from 'vitest'

import type { Command } from './operation'
import { gib, githubLabel, providerPreset, serviceLabel, slotsByProvider } from './runner'
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
