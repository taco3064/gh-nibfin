import { describe, expect, it } from 'vitest'

import { encodedSize, MAX_DISPATCH_BYTES, MAX_TEXT } from './payload.ts'
import { webhookIssue } from './testing.ts'
import { BUDGET_MS, handle, type Deps, type Env } from './worker.ts'

const secret = 'test-client-secret'
const token = 'test-github-token'

const env: Env = { SENTRY_CLIENT_SECRET: secret, GITHUB_TOKEN: token, SENTRY_PROJECT_SLUG: 'nibfin-website', DISPATCH_REPOSITORY: 'taco3064/gh-nibfin' }

async function sign(body: string, key = secret): Promise<string> {
  const k = await crypto.subtle.importKey('raw', new TextEncoder().encode(key), { name: 'HMAC', hash: 'SHA-256' }, false, ['sign'])
  const mac = new Uint8Array(await crypto.subtle.sign('HMAC', k, new TextEncoder().encode(body)))
  return Array.from(mac, (b) => b.toString(16).padStart(2, '0')).join('')
}

function event(action = 'created', issue: Record<string, unknown> = webhookIssue()): string {
  return JSON.stringify({ action, installation: { uuid: 'x' }, data: { issue }, actor: { type: 'application' } })
}

async function request(body: string, opts: { resource?: string; signature?: string | null; method?: string } = {}): Promise<Request> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    'Sentry-Hook-Resource': opts.resource ?? 'issue',
    'Sentry-Hook-Timestamp': '1764205200',
  }
  const sig = opts.signature === undefined ? await sign(body) : opts.signature
  if (sig !== null) headers['Sentry-Hook-Signature'] = sig
  const method = opts.method ?? 'POST'
  return new Request('https://nibfin-sentry-relay.example.workers.dev/', { method, headers, body: method === 'GET' || method === 'HEAD' ? undefined : body })
}

interface Sent {
  url: string
  init: RequestInit
}

function deps(answers: (number | 'throw')[] = [204], clock = { t: 0, step: 0 }) {
  const sent: Sent[] = []
  const logs: string[] = []
  const d: Deps = {
    fetch: async (url, init) => {
      sent.push({ url, init })
      clock.t += clock.step
      const a = answers[Math.min(sent.length - 1, answers.length - 1)]
      if (a === 'throw') throw new Error('network down')
      return new Response(a === 204 ? null : '{"message":"Bad credentials"}', { status: a })
    },
    log: (line) => logs.push(line),
    now: () => clock.t,
  }
  return { d, sent, logs }
}

function noLeak(logs: string[], ...values: string[]) {
  for (const line of logs) for (const v of values) expect(line).not.toContain(v)
}

describe('handle', () => {
  it('forwards a signed created issue as one sentry-issue dispatch', async () => {
    const body = event()
    const { d, sent, logs } = deps()
    const res = await handle(await request(body), env, d)
    expect(res.status).toBe(200)
    expect(sent).toHaveLength(1)
    expect(sent[0].url).toBe('https://api.github.com/repos/taco3064/gh-nibfin/dispatches')
    const h = sent[0].init.headers as Record<string, string>
    expect(h.Authorization).toBe(`Bearer ${token}`)
    expect(h.Accept).toBe('application/vnd.github+json')
    expect(h['X-GitHub-Api-Version']).toBe('2022-11-28')
    const out = JSON.parse(sent[0].init.body as string)
    expect(out.event_type).toBe('sentry-issue')
    expect(Object.keys(out.client_payload)).toEqual(['issue'])
    expect(Object.keys(out.client_payload.issue).sort()).toEqual([
      'action',
      'count',
      'culprit',
      'firstSeen',
      'id',
      'lastSeen',
      'level',
      'permalink',
      'shortId',
      'title',
      'userCount',
    ])
    expect(sent[0].init.body).not.toContain('secret-looking')
    expect(logs).toEqual([])
  })

  it('forwards unresolved too', async () => {
    const { d, sent } = deps()
    expect((await handle(await request(event('unresolved')), env, d)).status).toBe(200)
    expect(JSON.parse(sent[0].init.body as string).client_payload.issue.action).toBe('unresolved')
  })

  it('forwards the same normalized payload whether numeric fields are strings or numbers', async () => {
    const a = deps()
    const b = deps()
    expect((await handle(await request(event('created', webhookIssue({ id: '55', count: '7', userCount: '3' }))), env, a.d)).status).toBe(200)
    expect((await handle(await request(event('created', webhookIssue({ id: 55, count: 7, userCount: 3 }))), env, b.d)).status).toBe(200)
    expect(a.sent[0].init.body).toBe(b.sent[0].init.body)
    expect(JSON.parse(a.sent[0].init.body as string).client_payload.issue).toMatchObject({ id: '55', count: '7', userCount: 3 })
  })

  it('answers 400 to a malformed numeric field', async () => {
    const { d, sent, logs } = deps()
    expect((await handle(await request(event('created', webhookIssue({ count: -3 }))), env, d)).status).toBe(400)
    expect(sent).toHaveLength(0)
    expect(logs).toEqual(['rejected: issue fields'])
  })

  it('rejects a wrong, malformed or missing signature with 401', async () => {
    const body = event()
    for (const signature of [
      await sign(body, 'other-secret'),
      await sign(body + ' '),
      'sha256=' + (await sign(body)),
      (await sign(body)).toUpperCase(),
      'zz',
      null,
    ]) {
      const { d, sent, logs } = deps()
      const res = await handle(await request(body, { signature }), env, d)
      expect(res.status).toBe(401)
      expect(sent).toHaveLength(0)
      expect(logs).toEqual(['rejected: signature'])
      noLeak(logs, secret, body, signature ?? '\u0000')
    }
  })

  it('rejects every request without a client secret and logs only that', async () => {
    for (const e of [
      { ...env, SENTRY_CLIENT_SECRET: undefined },
      { ...env, SENTRY_CLIENT_SECRET: '' },
    ]) {
      const { d, sent, logs } = deps()
      const body = event()
      expect((await handle(await request(body), e, d)).status).toBe(401)
      expect(sent).toHaveLength(0)
      expect(logs).toEqual(['rejected: no client secret set'])
    }
  })

  it('rejects methods other than POST with 401', async () => {
    for (const method of ['GET', 'PUT', 'DELETE']) {
      const { d, sent } = deps()
      expect((await handle(await request(event(), { method }), env, d)).status).toBe(401)
      expect(sent).toHaveLength(0)
    }
  })

  it('answers 400 to a signed body that is not a webhook', async () => {
    for (const body of ['not json', '[]', JSON.stringify({ action: 'created', data: {} })]) {
      const { d, sent, logs } = deps()
      expect((await handle(await request(body), env, d)).status).toBe(400)
      expect(sent).toHaveLength(0)
      expect(logs).toEqual(['rejected: body'])
    }
  })

  it('ignores other resources, actions and projects with 200', async () => {
    const cases: [Request, string][] = [
      [await request(event(), { resource: 'event_alert' }), 'ignored: resource'],
      [await request(JSON.stringify({ action: 'created', data: { installation: {} } }), { resource: 'installation' }), 'ignored: resource'],
      [await request(event('resolved')), 'ignored: action'],
      [await request(event('assigned')), 'ignored: action'],
      [await request(event('archived')), 'ignored: action'],
      [await request(event('created', webhookIssue({ project: { slug: 'other' } }))), 'ignored: project'],
      [await request(event('created', webhookIssue({ project: undefined }))), 'ignored: project'],
    ]
    for (const [req, line] of cases) {
      const { d, sent, logs } = deps()
      expect((await handle(req, env, d)).status).toBe(200)
      expect(sent).toHaveLength(0)
      expect(logs).toEqual([line])
    }
  })

  it('retries a failed dispatch and answers 502 when every attempt fails', async () => {
    const { d, sent, logs } = deps([401])
    const body = event()
    expect((await handle(await request(body), env, d)).status).toBe(502)
    expect(sent).toHaveLength(4)
    expect(logs).toEqual(['dispatch failed: 401'])
    noLeak(logs, token, secret, 'Bad credentials')
  })

  it('succeeds when a retry is accepted', async () => {
    const { d, sent } = deps([500, 'throw', 204])
    expect((await handle(await request(event()), env, d)).status).toBe(200)
    expect(sent).toHaveLength(3)
  })

  it('reports no response when the dispatch throws', async () => {
    const { d, logs } = deps(['throw'])
    expect((await handle(await request(event()), env, d)).status).toBe(502)
    expect(logs).toEqual(['dispatch failed: no response'])
  })

  it('answers 502 without a token', async () => {
    const { d, sent, logs } = deps()
    expect((await handle(await request(event()), { ...env, GITHUB_TOKEN: undefined }, d)).status).toBe(502)
    expect(sent).toHaveLength(0)
    expect(logs).toEqual(['dispatch failed: no token'])
  })

  it('stops retrying when the time budget is spent', async () => {
    const clock = { t: 0, step: BUDGET_MS / 2 }
    const { d, sent } = deps([503], clock)
    expect((await handle(await request(event()), env, d)).status).toBe(502)
    expect(sent).toHaveLength(2)
    expect(sent[0].init.signal).toBeInstanceOf(AbortSignal)
  })

  it('keeps the dispatch under the size limit with capped text', async () => {
    const long = '\u0001"'.repeat(10_000)
    const { d, sent } = deps()
    expect((await handle(await request(event('created', webhookIssue({ title: long, culprit: long }))), env, d)).status).toBe(200)
    const out = JSON.parse(sent[0].init.body as string)
    expect(Array.from(out.client_payload.issue.title as string)).toHaveLength(MAX_TEXT)
    expect(encodedSize(out)).toBeLessThan(MAX_DISPATCH_BYTES)
  })
})
