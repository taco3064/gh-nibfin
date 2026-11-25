// Login logic of the operation dialog (#354): the steps of the provider
// CLI's own login, read from the apply stream's login_step lines, per
// provider. The URL and the codes stay in this state only; they are never
// logged or stored.

import type { OpResult } from '@/lib/operation'
import type { StreamOutcome } from '@/lib/session'

// LoginStep mirrors one login_step line's step (op.LoginStep).
export interface LoginStep {
  provider: string
  kind: 'open_url' | 'device_code' | 'code_requested' | 'succeeded' | 'failed' | string
  url?: string
  code?: string
  message?: string
}

export type LoginStage = 'authorizing' | 'verifying' | 'done' | 'failed' | 'ended'

export interface LoginState {
  provider: string
  url?: string
  // deviceCode is the one-time code the person enters at the URL (Codex).
  deviceCode?: string
  // codeRequested is set when the CLI asks for the pasted code (Claude).
  codeRequested: boolean
  codeSent: boolean
  // exited is set once the CLI's login ended (succeeded or failed).
  exited: boolean
  stage: LoginStage
  message?: string
  // startedAt is when the first step arrived, in ms.
  startedAt: number
}

export type LoginAction =
  | { type: 'step'; step: LoginStep; at: number }
  | { type: 'codeSent'; provider: string }
  | { type: 'outcome'; outcome: StreamOutcome<OpResult> }
  | { type: 'ended'; reason: string }

export const loginTimeoutMs = 15 * 60 * 1000

export const timeoutReason = 'timed out after 15 minutes'

const settled = (s: LoginState) => s.stage === 'done' || s.stage === 'failed' || s.stage === 'ended'

function update(states: readonly LoginState[], provider: string, f: (s: LoginState) => LoginState): LoginState[] {
  return states.map((s) => (s.provider === provider ? f(s) : s))
}

// loginReducer follows each provider's login from its first step to the
// outcome. Done needs the read-back: a succeeded step only means verifying.
export function loginReducer(states: readonly LoginState[], a: LoginAction): LoginState[] {
  switch (a.type) {
    case 'step': {
      const st = a.step
      if (!st || typeof st.provider !== 'string' || st.provider === '') return states as LoginState[]
      let out = states.slice()
      if (!out.some((s) => s.provider === st.provider)) {
        out.push({ provider: st.provider, codeRequested: false, codeSent: false, exited: false, stage: 'authorizing', startedAt: a.at })
      }
      out = update(out, st.provider, (s) => {
        if (settled(s)) return s
        switch (st.kind) {
          case 'open_url':
            return { ...s, url: st.url ?? s.url, stage: s.codeSent ? s.stage : 'authorizing' }
          case 'device_code':
            return { ...s, deviceCode: st.code ?? s.deviceCode, stage: s.codeSent ? s.stage : 'authorizing' }
          case 'code_requested':
            return { ...s, codeRequested: true, stage: s.codeSent ? s.stage : 'authorizing' }
          case 'succeeded':
            return { ...s, exited: true, stage: 'verifying' }
          case 'failed':
            return { ...s, exited: true, stage: 'failed', message: st.message || 'the login failed' }
          default:
            return s
        }
      })
      return out
    }
    case 'codeSent':
      return update(states.slice(), a.provider, (s) => (settled(s) || s.exited ? { ...s, codeSent: true } : { ...s, codeSent: true, stage: 'verifying' }))
    case 'outcome': {
      const o = a.outcome
      const result = o.ok ? o.value : o.result
      return states.map((s) => {
        if (settled(s)) return s
        const item = (result?.items ?? []).find((c) => c.id === `login:${s.provider}`)
        if (item?.confirmed) return { ...s, stage: 'done', message: undefined }
        if (item) return { ...s, stage: 'failed', message: item.reason || `${s.provider} is not logged in` }
        if (!o.ok) return { ...s, stage: 'failed', message: `${o.error.code}: ${o.error.message}` }
        return { ...s, stage: 'failed', message: 'the login was not read back' }
      })
    }
    case 'ended':
      return states.map((s) => (settled(s) ? s : { ...s, stage: 'ended', message: a.reason }))
  }
}

// waiting reports whether a login waits for the person to authorize.
export function waiting(states: readonly LoginState[]): boolean {
  return states.some((s) => s.stage === 'authorizing')
}

// running reports whether a login's CLI is still running, which the timeout
// bounds: from its first step until it succeeded or failed.
export function running(s: LoginState): boolean {
  return !s.exited && (s.stage === 'authorizing' || s.stage === 'verifying')
}

// endsAt is when a running login times out, in ms.
export function endsAt(s: LoginState): number {
  return s.startedAt + loginTimeoutMs
}

// timedOut reports whether a running login has reached its timeout at now.
export function timedOut(s: LoginState, now: number): boolean {
  return running(s) && now >= endsAt(s)
}

// safeUrl returns u when it is an https URL, which is the only kind the
// dialog links; any other text is shown without a link.
export function safeUrl(u: string | undefined): string | undefined {
  if (!u) return undefined
  try {
    return new URL(u).protocol === 'https:' ? u : undefined
  } catch {
    return undefined
  }
}

// providerTitle is a provider's name as a title: claude is Claude.
export function providerTitle(p: string): string {
  return p.charAt(0).toUpperCase() + p.slice(1)
}
