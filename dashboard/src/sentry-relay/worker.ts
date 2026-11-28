// The Sentry relay Worker: a Sentry internal integration's signed `issue`
// webhook becomes a repository_dispatch of type sentry-issue. Only POSTs
// signed with SENTRY_CLIENT_SECRET are read. Logs carry fixed reasons and
// status numbers only: never the secret, the signature, the token or the body.

import { buildPayload, type SentryDispatch } from '@/sentry-relay/file/payload.ts'

export interface Env {
  SENTRY_CLIENT_SECRET?: string
  GITHUB_TOKEN?: string
  SENTRY_PROJECT_SLUG: string
  DISPATCH_REPOSITORY: string
}

export interface Deps {
  fetch: (input: string, init: RequestInit) => Promise<Response>
  log: (line: string) => void
  now: () => number
}

// Sentry wants an answer within 1 s; dispatch attempts stop at this budget.
export const BUDGET_MS = 800
export const ATTEMPTS = 4 // the first try and up to 3 retries

const SIGNATURE = /^[0-9a-f]{64}$/
const REPOSITORY = /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/

function hexBytes(hex: string): Uint8Array<ArrayBuffer> {
  const out = new Uint8Array(hex.length / 2)
  for (let i = 0; i < out.length; i++) out[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16)
  return out
}

async function signatureValid(secret: string, signature: string | null, raw: ArrayBuffer): Promise<boolean> {
  if (signature === null || !SIGNATURE.test(signature)) return false
  const key = await crypto.subtle.importKey('raw', new TextEncoder().encode(secret), { name: 'HMAC', hash: 'SHA-256' }, false, ['verify'])
  return crypto.subtle.verify('HMAC', key, hexBytes(signature), raw)
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

function answer(status: number): Response {
  return new Response(null, { status })
}

// dispatch sends the event, retrying while the time budget allows. It returns
// the last status, 0 when no answer came, or -1 without a token.
async function dispatch(env: Env, body: SentryDispatch, deps: Deps, start: number): Promise<number> {
  if (!env.GITHUB_TOKEN || !REPOSITORY.test(env.DISPATCH_REPOSITORY)) return -1
  const url = `https://api.github.com/repos/${env.DISPATCH_REPOSITORY}/dispatches`
  const text = JSON.stringify(body)
  let last = 0
  for (let attempt = 0; attempt < ATTEMPTS; attempt++) {
    const left = BUDGET_MS - (deps.now() - start)
    if (left <= 0) break
    try {
      const res = await deps.fetch(url, {
        method: 'POST',
        headers: {
          Authorization: `Bearer ${env.GITHUB_TOKEN}`,
          Accept: 'application/vnd.github+json',
          'X-GitHub-Api-Version': '2022-11-28',
          'Content-Type': 'application/json',
          'User-Agent': 'nibfin-sentry-relay',
        },
        body: text,
        signal: AbortSignal.timeout(left),
      })
      last = res.status
      if (res.status === 204) return 204
    } catch {
      last = 0
    }
  }
  return last
}

export async function handle(request: Request, env: Env, deps: Deps): Promise<Response> {
  const start = deps.now()
  if (request.method !== 'POST') return answer(401)
  if (!env.SENTRY_CLIENT_SECRET) {
    deps.log('rejected: no client secret set')
    return answer(401)
  }
  const raw = await request.arrayBuffer()
  if (!(await signatureValid(env.SENTRY_CLIENT_SECRET, request.headers.get('Sentry-Hook-Signature'), raw))) {
    deps.log('rejected: signature')
    return answer(401)
  }
  let body: unknown
  try {
    body = JSON.parse(new TextDecoder().decode(raw))
  } catch {
    body = undefined
  }
  if (!isRecord(body)) {
    deps.log('rejected: body')
    return answer(400)
  }
  // Other resources (installation, event_alert, …) carry other bodies.
  if (request.headers.get('Sentry-Hook-Resource') !== 'issue') {
    deps.log('ignored: resource')
    return answer(200)
  }
  if (typeof body.action !== 'string' || !isRecord(body.data) || !isRecord(body.data.issue)) {
    deps.log('rejected: body')
    return answer(400)
  }
  const issue = body.data.issue
  if (body.action !== 'created' && body.action !== 'unresolved') {
    deps.log('ignored: action')
    return answer(200)
  }
  const project = isRecord(issue.project) ? issue.project.slug : undefined
  if (project !== env.SENTRY_PROJECT_SLUG) {
    deps.log('ignored: project')
    return answer(200)
  }
  const payload = buildPayload(body.action, issue)
  if (payload === null) {
    deps.log('rejected: issue fields')
    return answer(400)
  }
  const status = await dispatch(env, payload, deps, start)
  if (status === 204) return answer(200)
  deps.log(`dispatch failed: ${status === -1 ? 'no token' : status === 0 ? 'no response' : status}`)
  return answer(502)
}

export default {
  fetch(request, env): Promise<Response> {
    return handle(request, env, { fetch: (input, init) => fetch(input, init), log: (line) => console.log(line), now: () => Date.now() })
  },
} satisfies ExportedHandler<Env>
