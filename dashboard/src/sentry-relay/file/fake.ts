// An in-memory GitHub and Sentry for the filer tests. It serves only the
// endpoints the filer calls, with GitHub's Link pagination.

import { buildPayload, type SentryIssue } from '../payload.ts'
import { webhookIssue } from '../testing.ts'
import type { Fetch } from './http.ts'

export const REPO = 'taco3064/gh-nibfin'
export const SHA = 'a'.repeat(40)
export const GITHUB = 'https://api.github.com/'
export const SENTRY = 'https://de.sentry.io/api/0/'

export interface FakeIssue {
  number: number
  title: string
  state: 'open' | 'closed'
  body: string
  labels: string[]
  assignees: string[]
  created_at: string
  user: string
  pull_request?: boolean
}

export interface FakeComment {
  id: number
  body: string
  created_at: string
}

// before: fail without effect; after: apply the effect, then fail.
export type Fault = 'before' | 'after'

export function sentryIssue(overrides: Partial<SentryIssue> = {}): SentryIssue {
  return { ...buildPayload('created', webhookIssue())!.client_payload.issue, ...overrides }
}

export function frame(filename: string, lineNo = 10, raw = 'https://taco3064.github.io/gh-nibfin/_astro/index.abc123.js') {
  return { frame: { filename, absPath: filename, inApp: true, lineNo, colNo: 5, function: 'render' }, raw: { filename: raw, absPath: raw, inApp: true, lineNo: 1, colNo: 9999 } }
}

export function sentryEvent(frames: ReturnType<typeof frame>[], opts: { release?: unknown; errors?: unknown[] } = {}) {
  return {
    eventID: 'e1',
    release: 'release' in opts ? opts.release : { version: SHA },
    errors: opts.errors ?? [],
    entries: [
      { type: 'breadcrumbs', data: { values: [] } },
      {
        type: 'exception',
        data: {
          values: [
            {
              type: 'TypeError',
              stacktrace: { frames: [{ filename: 'https://cdn.example/lib.js', inApp: false, lineNo: 1 }, ...frames.map((f) => f.frame)] },
              rawStacktrace: { frames: [{ filename: 'https://cdn.example/lib.js', inApp: false, lineNo: 1 }, ...frames.map((f) => f.raw)] },
            },
          ],
        },
      },
    ],
  }
}

export class Fake {
  issues: FakeIssue[] = []
  comments = new Map<number, FakeComment[]>()
  greenRuns = new Set<string>([SHA])
  trees = new Map<string, { paths: string[]; truncated?: boolean }>([[SHA, { paths: ['dashboard/src/app/pages/index.astro', 'dashboard/src/components/diagram.tsx', 'dashboard/src/lib/format.ts'] }]])
  event: unknown = sentryEvent([frame('../../src/components/diagram.tsx', 42)])
  sentryStatus = 200
  faults = new Map<string, Fault[]>()
  requests: { method: string; url: string; auth?: string }[] = []
  perPage = 100
  now = '2026-11-27T10:00:00Z'
  private nextNumber = 500
  private nextComment = 1

  open(title: string, opts: Partial<FakeIssue> = {}): FakeIssue {
    const i: FakeIssue = { number: this.nextNumber++, title, state: 'open', body: 'opened by hand', labels: ['request'], assignees: ['taco3064'], created_at: '2026-11-20T00:00:00Z', user: 'taco3064', ...opts }
    this.issues.push(i)
    return i
  }

  comment(number: number, body: string, created_at = '2026-11-21T00:00:00Z') {
    const list = this.comments.get(number) ?? []
    list.push({ id: this.nextComment++, body, created_at })
    this.comments.set(number, list)
  }

  fault(key: string, ...modes: Fault[]) {
    this.faults.set(key, [...(this.faults.get(key) ?? []), ...modes])
  }

  posts(suffix: string): number {
    return this.requests.filter((r) => r.method === 'POST' && r.url.endsWith(suffix)).length
  }

  private takeFault(key: string): Fault | undefined {
    const q = this.faults.get(key)
    return q?.shift()
  }

  private ghIssue(i: FakeIssue) {
    return {
      number: i.number,
      title: i.title,
      state: i.state,
      body: i.body,
      html_url: `https://github.com/${REPO}/issues/${i.number}`,
      created_at: i.created_at,
      labels: i.labels.map((name) => ({ name })),
      assignees: i.assignees.map((login) => ({ login })),
      user: { login: i.user },
      ...(i.pull_request ? { pull_request: {} } : {}),
    }
  }

  private page<T>(url: URL, items: T[]): Response {
    const per = Number(url.searchParams.get('per_page') ?? '30')
    const pageNo = Number(url.searchParams.get('page') ?? '1')
    const last = Math.max(1, Math.ceil(items.length / per))
    const link = (n: number, rel: string) => {
      const u = new URL(url)
      u.searchParams.set('page', String(n))
      return `<${u}>; rel="${rel}"`
    }
    const links: string[] = []
    if (pageNo > 1) links.push(link(1, 'first'), link(pageNo - 1, 'prev'))
    if (pageNo < last) links.push(link(pageNo + 1, 'next'), link(last, 'last'))
    return json(items.slice((pageNo - 1) * per, pageNo * per), 200, links.length > 0 ? { link: links.join(', ') } : {})
  }

  fetch: Fetch = async (input, init) => {
    const method = init.method ?? 'GET'
    const auth = (init.headers as Record<string, string>)?.Authorization
    this.requests.push({ method, url: input, auth })
    const url = new URL(input)
    if (input.startsWith(SENTRY)) {
      if (this.sentryStatus !== 200) return json({ detail: 'secret detail' }, this.sentryStatus)
      return json(this.event)
    }
    if (!input.startsWith(`${GITHUB}repos/${REPO}/`)) return json({ message: 'Not Found' }, 404)
    const path = url.pathname.slice(`/repos/${REPO}/`.length)
    const key = `${method} ${path}`
    const fault = this.takeFault(key)
    if (fault === 'before') return json({ message: 'Server Error' }, 502)
    const done = (r: Response) => (fault === 'after' ? json({ message: 'Server Error' }, 502) : r)

    let m: RegExpExecArray | null
    if (path === 'issues' && method === 'GET') {
      const state = url.searchParams.get('state')
      const label = url.searchParams.get('labels')
      const creator = url.searchParams.get('creator')
      const since = url.searchParams.get('since')
      const list = this.issues
        .filter((i) => (state === 'all' || i.state === state) && (!label || i.labels.includes(label)) && (!creator || i.user === creator) && (!since || i.created_at >= since))
        .sort((a, b) => b.number - a.number)
        .map((i) => this.ghIssue(i))
      return this.page(url, list)
    }
    if (path === 'issues' && method === 'POST') {
      const b = JSON.parse(init.body as string)
      const i = this.open(b.title, { body: b.body, labels: b.labels, assignees: b.assignees, created_at: this.now })
      return done(json(this.ghIssue(i), 201))
    }
    if ((m = /^issues\/(\d+)$/.exec(path)) && method === 'GET') {
      const i = this.issues.find((x) => x.number === Number(m![1]))
      return i ? json(this.ghIssue(i)) : json({ message: 'Not Found' }, 404)
    }
    if ((m = /^issues\/(\d+)\/comments$/.exec(path))) {
      const n = Number(m[1])
      if (method === 'POST') {
        this.comment(n, JSON.parse(init.body as string).body, this.now)
        return done(json({ id: 1 }, 201))
      }
      return this.page(url, this.comments.get(n) ?? [])
    }
    if (path === 'actions/workflows/website.yml/runs') {
      const sha = url.searchParams.get('head_sha') ?? ''
      const ok = url.searchParams.get('branch') === 'main' && url.searchParams.get('event') === 'push' && url.searchParams.get('status') === 'success' && this.greenRuns.has(sha)
      return json({ total_count: ok ? 1 : 0, workflow_runs: [] })
    }
    if ((m = /^git\/trees\/([0-9a-f]{40})$/.exec(path))) {
      const t = this.trees.get(m[1])
      if (!t) return json({ message: 'Not Found' }, 404)
      return json({ sha: m[1], truncated: t.truncated ?? false, tree: [{ path: 'dashboard/src', type: 'tree' }, ...t.paths.map((p) => ({ path: p, type: 'blob' }))] })
    }
    return json({ message: 'Not Found' }, 404)
  }
}

function json(body: unknown, status = 200, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json', ...headers } })
}
