import { mkdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

import { expect, type Page, type Response, type Route } from '@playwright/test'

import { commands } from './command-fixtures'
import { frame, mockLive, type LiveMock } from './live-mock'
import { personView, runnerActivity } from './person-fixtures'

// Shared checks of the screenshot specs: the API is mocked with fictional
// bodies, CSP violations and console errors fail the spec.

export const outDir = fileURLToPath(new URL('../.screenshots/', import.meta.url))
mkdirSync(outDir, { recursive: true })

export const widths = [375, 768, 1280, 1920]
export const storageKey = 'nibfin-dashboard-theme'
export const unauthorized = 'Failed to load resource: the server responded with a status of 401'

// StatusAnswer is one answer of the mocked status read: a result, or an
// error with its HTTP status.
// partials, when given, come first: the status read route's partial lines,
// or the live stream's partial frames before its snapshot.
export type StatusAnswer =
  | { result: unknown; partials?: PartialRead[] }
  | { error: { code: string; message: string }; httpStatus: number }

// PartialRead is one partial read: the sections read so far and those still
// loading.
export interface PartialRead {
  result: unknown
  loading: string[]
}

export interface Watch {
  violations: () => Promise<string[]>
  consoleErrors: string[]
  // statusReads counts the status requests.
  statusReads: number
  // status is the answer of the next person-view status reads (no repo in
  // the body); a test may replace it. A live stream opens with it too: a
  // result is its snapshot, which counts as a status read, and an error ends
  // the stream with an error frame.
  status: StatusAnswer
  live: LiveMock
  // repoStatus answers the repository-view reads by the body's repo; a repo
  // without an entry answers invalid_repo.
  repoStatus: Record<string, StatusAnswer>
  // statusBodies are the request bodies of the status read route, in order.
  statusBodies: Record<string, unknown>[]
  // activity answers GET /api/runner-activity: a body, or an error with
  // its HTTP status; activityReads counts the reads.
  activity: { result: unknown } | { error: { code: string; message: string }; httpStatus: number }
  activityReads: number
}

// prepare mocks the API and records CSP violations and console errors. The
// first exchange opens a session; later ones find the proof used. The status
// read answers status, by default a fictional person view, or the entry of
// repoStatus for the body's repo.
export async function prepare(page: Page, status: StatusAnswer = { result: personView() }): Promise<Watch> {
  const w: Watch = {
    violations: () => page.evaluate(() => (window as unknown as { __csp: string[] }).__csp),
    consoleErrors: [],
    statusReads: 0,
    status,
    live: undefined as unknown as LiveMock,
    repoStatus: {},
    statusBodies: [],
    activity: { result: runnerActivity() },
    activityReads: 0,
  }
  w.live = await mockLive(page, () => {
    w.statusReads++
    const a = w.status
    if ('error' in a) return { frames: [frame({ event: 'error', source: 'status', error: a.error })], open: false }
    const at = new Date().toISOString()
    const partials = (a.partials ?? []).map((p) => frame({ event: 'partial', at, result: p.result, loading: p.loading }))
    return { frames: [...partials, frame({ event: 'snapshot', at, result: a.result })], open: true }
  })
  let exchanges = 0
  await page.route('**/api/session', (route) => {
    exchanges++
    return exchanges === 1
      ? route.fulfill({ json: { session: 'SCREENSHOTSESSION' } })
      : route.fulfill({
          status: 401,
          json: {
            error: {
              code: 'proof_invalid',
              message: 'the session URL is invalid or was already used; restart gh nibfin dashboard for a new one',
            },
          },
        })
  })
  await page.route('**/api/runner-activity', (route) => {
    expect(route.request().headers()['x-nibfin-session']).toBe('SCREENSHOTSESSION')
    w.activityReads++
    const a = w.activity
    return 'error' in a ? route.fulfill({ status: a.httpStatus, json: { error: a.error } }) : route.fulfill({ json: a.result })
  })
  // The command schema the runner view reads; a spec may route it again.
  await page.route('**/api/commands', (route) => route.fulfill({ json: { commands } }))
  await page.route('**/api/commands/status/read', (route) => {
    expect(route.request().headers()['x-nibfin-session']).toBe('SCREENSHOTSESSION')
    w.statusReads++
    const body = (route.request().postDataJSON() ?? {}) as Record<string, unknown>
    w.statusBodies.push(body)
    const a: StatusAnswer =
      typeof body.repo === 'string'
        ? (w.repoStatus[body.repo] ?? {
            error: { code: 'invalid_repo', message: `${body.repo} is not a repository; use [HOST/]OWNER/REPO` },
            httpStatus: 400,
          })
        : w.status
    if ('error' in a) return route.fulfill({ status: a.httpStatus, json: { error: a.error } })
    const lines = [...(a.partials ?? []).map((p) => ({ event: 'partial', result: p.result, loading: p.loading })), { event: 'result', result: a.result }]
    return route.fulfill({ contentType: 'application/x-ndjson', body: lines.map((l) => `${JSON.stringify(l)}\n`).join('') })
  })
  await page.addInitScript(() => {
    const w = window as unknown as { __csp: string[] }
    w.__csp = []
    document.addEventListener('securitypolicyviolation', (e) => {
      w.__csp.push(`${e.violatedDirective} ${e.blockedURI}`)
    })
  })
  page.on('console', (m) => {
    if (m.type() === 'error') w.consoleErrors.push(m.text())
  })
  page.on('pageerror', (e) => w.consoleErrors.push(e.message))
  return w
}

export async function expectClean(w: Watch, allowed: string[] = []) {
  expect(await w.violations()).toEqual([])
  expect(w.consoleErrors.filter((e) => !allowed.some((a) => e.startsWith(a)))).toEqual([])
}

export async function shot(page: Page, name: string, fullPage = true) {
  await page.screenshot({ path: `${outDir}${name}.png`, fullPage, animations: 'disabled' })
}

// expectNonce checks that the page carries the fresh style-src nonce of its
// response in meta[property=csp-nonce].
export async function expectNonce(page: Page, res: Response | null) {
  const csp = res?.headers()['content-security-policy'] ?? ''
  const m = /style-src 'self' 'nonce-([A-Z2-7]+)';/.exec(csp)
  expect(m, csp).not.toBeNull()
  expect(csp).toContain("script-src 'self';")
  const meta = await page.evaluate(() => document.querySelector<HTMLMetaElement>('meta[property=csp-nonce]')?.nonce)
  expect(meta).toBe(m![1])
  return m![1]
}

// mockHeldReads lets a spec answer the status read route itself, line by
// line, on a stream that stays open: once holdReads(page, true) runs, the
// page's next status reads are answered in the page and pushRead sends
// their lines. Before that, and after holdReads(page, false), reads go to
// prepare's mock. Call it before the page loads.
export async function mockHeldReads(page: Page) {
  await page.addInitScript(() => {
    const w = window as unknown as { __nibfinHold?: boolean; __nibfinRead?: ReadableStreamDefaultController<Uint8Array> }
    const real = window.fetch.bind(window)
    window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
      if (!w.__nibfinHold || new URL(url, location.href).pathname !== '/api/commands/status/read') return real(input, init)
      let ctl: ReadableStreamDefaultController<Uint8Array> | undefined
      const body = new ReadableStream<Uint8Array>({
        start(c) {
          ctl = c
          w.__nibfinRead = c
        },
      })
      init?.signal?.addEventListener('abort', () => {
        try {
          ctl?.error(new DOMException('aborted', 'AbortError'))
        } catch {
          // Already closed.
        }
      })
      return new Response(body, { status: 200, headers: { 'Content-Type': 'application/x-ndjson' } })
    }
  })
}

export async function holdReads(page: Page, on: boolean) {
  await page.evaluate((v) => {
    ;(window as unknown as { __nibfinHold?: boolean }).__nibfinHold = v
  }, on)
}

// pushRead sends lines on the held status read, and closes it with end.
export async function pushRead(page: Page, lines: unknown[], end = false) {
  await page.evaluate(
    ([ls, e]) => {
      const c = (window as unknown as { __nibfinRead?: ReadableStreamDefaultController<Uint8Array> }).__nibfinRead
      const enc = new TextEncoder()
      for (const l of ls as string[]) c?.enqueue(enc.encode(l))
      if (e) c?.close()
    },
    [lines.map((l) => `${JSON.stringify(l)}\n`), end] as const,
  )
}

// PlanAnswer is a plan route's body: a stored plan, a read-only plan with
// its result, or an error that may carry both.
export interface PlanAnswer {
  id?: string
  plan?: { steps?: { target?: string; [k: string]: unknown }[] | null; readOnly?: boolean; [k: string]: unknown }
  result?: unknown
  error?: unknown
}

// planStreamLines are the planning stream of answer: a checking and a
// checked line per step, the read-back check of a read-only plan, then the
// final line with the plan route's body.
export function planStreamLines(answer: PlanAnswer): string[] {
  const lines: unknown[] = []
  for (const s of answer.plan?.steps ?? []) {
    lines.push({ event: 'checking', check: s.target ?? '' }, { event: 'checked', steps: [s] })
  }
  if (answer.plan?.readOnly) lines.push({ event: 'checking', check: 'read back' })
  lines.push({ event: answer.error ? 'error' : answer.id ? 'planned' : 'result', ...answer })
  return lines.map((l) => JSON.stringify(l))
}

// fulfillPlanStream answers a planning stream route with answer.
export function fulfillPlanStream(route: Route, answer: PlanAnswer): Promise<void> {
  return route.fulfill({ status: 200, contentType: 'application/x-ndjson', body: planStreamLines(answer).join('\n') + '\n' })
}

// gutterSelector lists the content and controls that keep the page's side
// gutter: inputs, buttons, tabs, links, cards, the dependency graph's border
// box and an open dialog panel. Full-bleed containers (html, body, header,
// main, tab panels) and the dialog overlay are not in it.
const gutterSelector = [
  'input',
  'textarea',
  'select',
  'button',
  '[role=tab]',
  'a',
  'article',
  '[data-slot=card]',
  '[data-slot=graph-region]',
  '[role=dialog]',
].join(',')

// gutterOffenders lists the visible elements of gutterSelector that come
// closer than gutter px to either side of the viewport. Elements inside a
// horizontally scrolling ancestor (the graph's content) and sr-only elements
// are skipped. Each entry names the element and its left and right edges.
export async function gutterOffenders(page: Page, gutter = 16): Promise<string[]> {
  return page.evaluate(
    ([sel, g]) => {
      const tolerance = 0.5
      const width = document.documentElement.clientWidth
      const scrollsX = (el: Element) => {
        const s = getComputedStyle(el)
        return (s.overflowX === 'auto' || s.overflowX === 'scroll') && el.scrollWidth > el.clientWidth
      }
      const insideScroller = (el: Element) => {
        for (let p = el.parentElement; p && p !== document.documentElement; p = p.parentElement) if (scrollsX(p)) return true
        return false
      }
      const name = (el: Element) => {
        const slot = el.getAttribute('data-slot')
        const text = (el.getAttribute('aria-label') ?? el.textContent ?? '').trim().slice(0, 40)
        return `${el.tagName.toLowerCase()}${slot ? `[data-slot=${slot}]` : ''} "${text}"`
      }
      const out: string[] = []
      for (const el of document.querySelectorAll(sel as string)) {
        if (!el.checkVisibility({ visibilityProperty: true, opacityProperty: false })) continue
        const r = el.getBoundingClientRect()
        if (r.width <= 1 || r.height <= 1) continue
        if (insideScroller(el)) continue
        if (r.left < (g as number) - tolerance || r.right > width - (g as number) + tolerance) {
          out.push(`${name(el)} left=${r.left.toFixed(1)} right=${(width - r.right).toFixed(1)}`)
        }
      }
      return out
    },
    [gutterSelector, gutter] as const,
  )
}

// expectGutter checks that the page has no horizontal scroll and that every
// element of gutterSelector keeps the side gutter.
export async function expectGutter(page: Page, gutter = 16) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  expect(await gutterOffenders(page, gutter)).toEqual([])
}
