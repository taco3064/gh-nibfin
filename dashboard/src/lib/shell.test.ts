import { describe, expect, it } from 'vitest'

import { freshness, hasNoRunner, landingView, staleAfterMs } from '@/lib/shell'
import type { HostReport, PersonView, RunnerState } from '@/lib/status'

function slot(over: Partial<RunnerState> = {}): RunnerState {
  return {
    slot: 'claude-1',
    provider: 'claude',
    configured: true,
    unit: true,
    enabled: true,
    active: true,
    listed: true,
    busy: false,
    ...over,
  }
}

const host = (runners: RunnerState[] | null, code?: string): HostReport => ({
  host: code ? { error: { code, message: 'm' } } : { running: true },
  memory: { totalKB: 0, availableKB: 0 },
  runners: code ? { runners, error: { code, message: 'm' } } : { runners },
  providers: [],
})

describe('hasNoRunner', () => {
  it('is true for an empty list', () => expect(hasNoRunner(host([]))).toBe(true))
  it('is true for a null list', () => expect(hasNoRunner(host(null))).toBe(true))
  it('is true when only incomplete slots exist', () => {
    expect(hasNoRunner(host([slot({ configured: false }), slot({ slot: 'claude-2', unit: false }), slot({ slot: 'codex-1', enabled: false })]))).toBe(true)
  })
  it('is false with one complete slot', () => {
    expect(hasNoRunner(host([slot({ configured: false }), slot({ slot: 'codex-1', active: false })]))).toBe(false)
  })
  it('is true without Podman or a machine', () => {
    expect(hasNoRunner(host([], 'podman_missing'))).toBe(true)
    expect(hasNoRunner(host([], 'machine_missing'))).toBe(true)
  })
  it('is false for other read errors', () => {
    expect(hasNoRunner(host([], 'machine_stopped'))).toBe(false)
    expect(hasNoRunner(host(null, 'podman_failed'))).toBe(false)
    expect(hasNoRunner(host(null, 'unsupported_host'))).toBe(false)
  })
})

describe('landingView', () => {
  const view = (h: HostReport): PersonView => ({ host: h, latestImage: {}, githubHost: 'github.com', repos: [], truncated: false, nibfinRepos: [] })
  it('opens the runner view without a Runner', () => expect(landingView(view(host([])))).toBe('runner'))
  it('opens the person view otherwise', () => {
    expect(landingView(view(host([slot()])))).toBe('person')
    expect(landingView(view(host([], 'machine_stopped')))).toBe('person')
  })
})

describe('freshness', () => {
  it('is current until five minutes have passed', () => {
    expect(freshness(1000, 1000)).toEqual({ stale: false, staleAt: 1000 + staleAfterMs })
    expect(freshness(1000, 1000 + staleAfterMs - 1).stale).toBe(false)
    expect(freshness(1000, 1000 + staleAfterMs).stale).toBe(true)
    expect(staleAfterMs).toBe(300_000)
  })
})
