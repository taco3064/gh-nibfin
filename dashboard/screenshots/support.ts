import { mkdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

import { expect, type Page, type Response } from '@playwright/test'

import { frame, mockLive, type LiveMock } from './live-mock'
import { personView } from './person-fixtures'

// Shared checks of the screenshot specs: the API is mocked with fictional
// bodies, CSP violations and console errors fail the spec.

export const outDir = fileURLToPath(new URL('../.screenshots/', import.meta.url))
mkdirSync(outDir, { recursive: true })

export const widths = [375, 768, 1280, 1920]
export const storageKey = 'nibfin-dashboard-theme'
export const unauthorized = 'Failed to load resource: the server responded with a status of 401'

// StatusAnswer is one answer of the mocked status read: a result, or an
// error with its HTTP status.
export type StatusAnswer = { result: unknown } | { error: { code: string; message: string }; httpStatus: number }

export interface Watch {
  violations: () => Promise<string[]>
  consoleErrors: string[]
  // statusReads counts the status requests.
  statusReads: number
  // status is the answer of the next status reads; a test may replace it.
  // A live stream opens with it too: a result is its snapshot, which counts
  // as a status read, and an error ends the stream with an error frame.
  status: StatusAnswer
  live: LiveMock
}

// prepare mocks the API and records CSP violations and console errors. The
// first exchange opens a session; later ones find the proof used. The status
// read answers status, by default a fictional person view.
export async function prepare(page: Page, status: StatusAnswer = { result: personView() }): Promise<Watch> {
  const w: Watch = {
    violations: () => page.evaluate(() => (window as unknown as { __csp: string[] }).__csp),
    consoleErrors: [],
    statusReads: 0,
    status,
    live: undefined as unknown as LiveMock,
  }
  w.live = await mockLive(page, () => {
    w.statusReads++
    const a = w.status
    if ('error' in a) return { frames: [frame({ event: 'error', source: 'status', error: a.error })], open: false }
    return { frames: [frame({ event: 'snapshot', at: new Date().toISOString(), result: a.result })], open: true }
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
  await page.route('**/api/commands/status/plan', (route) => {
    expect(route.request().headers()['x-nibfin-session']).toBe('SCREENSHOTSESSION')
    w.statusReads++
    const a = w.status
    if ('error' in a) return route.fulfill({ status: a.httpStatus, json: { error: a.error } })
    return route.fulfill({ json: { plan: { operation: 'status', readOnly: true }, result: a.result } })
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
