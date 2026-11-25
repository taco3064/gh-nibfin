import { describe, expect, it } from 'vitest'

import { loginReducer, loginTimeoutMs, providerTitle, running, safeUrl, timedOut, waiting, type LoginState, type LoginStep } from './login'
import type { OpResult } from './operation'
import type { StreamOutcome } from './session'

const step = (provider: string, kind: string, extra: Partial<LoginStep> = {}): LoginStep => ({ provider, kind, ...extra })

function run(actions: Parameters<typeof loginReducer>[1][]): LoginState[] {
  return actions.reduce<LoginState[]>((s, a) => loginReducer(s, a), [])
}

const item = (provider: string, confirmed: boolean, reason?: string) => ({
  id: `login:${provider}`,
  action: 'log in',
  target: provider,
  confirmed,
  state: confirmed ? 'logged in' : 'not logged in',
  ...(reason && { reason }),
})

const ok = (items: OpResult['items']): StreamOutcome<OpResult> => ({ ok: true, value: { items } })

describe('loginReducer', () => {
  it('follows the Claude sequence to Done after the read-back', () => {
    let s = run([
      { type: 'step', step: step('claude', 'open_url', { url: 'https://claude.ai/oauth/x' }), at: 1000 },
      { type: 'step', step: step('claude', 'code_requested'), at: 2000 },
    ])
    expect(s).toEqual([
      { provider: 'claude', url: 'https://claude.ai/oauth/x', codeRequested: true, codeSent: false, exited: false, stage: 'authorizing', startedAt: 1000 },
    ])
    expect(waiting(s)).toBe(true)
    s = loginReducer(s, { type: 'codeSent', provider: 'claude' })
    expect(s[0]).toMatchObject({ codeSent: true, stage: 'verifying' })
    expect(waiting(s)).toBe(false)
    expect(running(s[0])).toBe(true)
    s = loginReducer(s, { type: 'step', step: step('claude', 'succeeded'), at: 3000 })
    expect(s[0]).toMatchObject({ stage: 'verifying', exited: true })
    expect(running(s[0])).toBe(false)
    s = loginReducer(s, { type: 'outcome', outcome: ok([item('claude', true)]) })
    expect(s[0].stage).toBe('done')
  })

  it('follows the Codex sequence to Done', () => {
    let s = run([
      { type: 'step', step: step('codex', 'open_url', { url: 'https://auth.openai.com/codex/device' }), at: 5 },
      { type: 'step', step: step('codex', 'device_code', { code: 'ABCD-EFGH1' }), at: 6 },
    ])
    expect(s[0]).toMatchObject({ deviceCode: 'ABCD-EFGH1', codeRequested: false, stage: 'authorizing', startedAt: 5 })
    s = loginReducer(s, { type: 'step', step: step('codex', 'succeeded'), at: 7 })
    expect(s[0].stage).toBe('verifying')
    s = loginReducer(s, { type: 'outcome', outcome: ok([item('codex', true)]) })
    expect(s[0].stage).toBe('done')
  })

  it('keeps a failed step and its message through the outcome', () => {
    let s = run([
      { type: 'step', step: step('claude', 'open_url', { url: 'https://x' }), at: 1 },
      { type: 'step', step: step('claude', 'failed', { message: 'claude login exited with status 1' }), at: 2 },
    ])
    expect(s[0]).toMatchObject({ stage: 'failed', message: 'claude login exited with status 1' })
    s = loginReducer(s, { type: 'outcome', outcome: { ok: false, error: { code: 'login_failed', message: 'x' } } })
    expect(s[0]).toMatchObject({ stage: 'failed', message: 'claude login exited with status 1' })
  })

  it('fails a login the read-back does not confirm', () => {
    const s = run([
      { type: 'step', step: step('codex', 'device_code', { code: 'A' }), at: 1 },
      { type: 'step', step: step('codex', 'succeeded'), at: 2 },
      {
        type: 'outcome',
        outcome: { ok: false, error: { code: 'not_logged_in', message: 'codex is not logged in after login' }, result: { items: [item('codex', false, 'codex is not logged in')] } },
      },
    ])
    expect(s[0]).toMatchObject({ stage: 'failed', message: 'codex is not logged in' })
  })

  it('fails with the error when there is no result, and without a read-back item', () => {
    const base: Parameters<typeof loginReducer>[1][] = [{ type: 'step', step: step('codex', 'succeeded'), at: 1 }]
    expect(run([...base, { type: 'outcome', outcome: { ok: false, error: { code: 'network_error', message: 'm' }, unknown: true } }])[0]).toMatchObject({
      stage: 'failed',
      message: 'network_error: m',
    })
    expect(run([...base, { type: 'outcome', outcome: ok([]) }])[0]).toMatchObject({ stage: 'failed', message: 'the login was not read back' })
  })

  it('ends the running logins and keeps the settled ones', () => {
    const s = run([
      { type: 'step', step: step('claude', 'failed', { message: 'x' }), at: 1 },
      { type: 'step', step: step('codex', 'device_code', { code: 'A' }), at: 2 },
      { type: 'ended', reason: 'timed out after 15 minutes' },
      { type: 'outcome', outcome: { ok: false, error: { code: 'network_error', message: 'aborted' }, unknown: true } },
    ])
    expect(s.map((x) => [x.provider, x.stage, x.message])).toEqual([
      ['claude', 'failed', 'x'],
      ['codex', 'ended', 'timed out after 15 minutes'],
    ])
    expect(waiting(s)).toBe(false)
  })

  it('ignores a step without a provider', () => {
    expect(run([{ type: 'step', step: {} as LoginStep, at: 1 }])).toEqual([])
  })
})

describe('timedOut', () => {
  const s = run([{ type: 'step', step: step('claude', 'code_requested'), at: 10_000 }])[0]
  it('is false just under 15 minutes', () => expect(timedOut(s, 10_000 + loginTimeoutMs - 1)).toBe(false))
  it('is true at 15 minutes', () => expect(timedOut(s, 10_000 + loginTimeoutMs)).toBe(true))
  it('is false once the CLI exited', () => {
    const done = loginReducer([s], { type: 'step', step: step('claude', 'succeeded'), at: 11_000 })[0]
    expect(timedOut(done, 10_000 + loginTimeoutMs)).toBe(false)
  })
  it('is 15 minutes', () => expect(loginTimeoutMs).toBe(900_000))
})

describe('safeUrl', () => {
  it('links https', () => expect(safeUrl('https://claude.ai/oauth/authorize?x=1')).toBe('https://claude.ai/oauth/authorize?x=1'))
  it.each(['http://claude.ai/x', 'javascript:alert(1)', 'not a url', '', undefined])('rejects %j', (u) => expect(safeUrl(u)).toBeUndefined())
})

describe('providerTitle', () => {
  it('capitalizes', () => expect(providerTitle('codex')).toBe('Codex'))
})
