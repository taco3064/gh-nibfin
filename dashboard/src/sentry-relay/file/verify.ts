// verify decides whether a Sentry error came from a build this repository
// deployed: the latest event's release is a main commit with a green Website
// push run, and every in-app frame resolved through the source maps to a
// dashboard/src/ file that exists at that commit. Anything else, including a
// missing token or a Sentry API error, is unverified; verify never throws.

import type { SentryIssue } from '../payload.ts'
import type { Client } from './http.ts'

export const SENTRY_ORG = 'na-43k'
export const MAX_FRAMES = 50

export interface Frame {
  path: string
  line: number
  column?: number
}

export type Verification = { verified: true; release: string; frames: Frame[] } | { verified: false; release?: string; failed: string }

export interface VerifyDeps {
  repository: string // OWNER/REPO
  github: Client
  sentry?: Client // undefined without SENTRY_API_TOKEN
}

const SHA = /^[0-9a-f]{40}$/
const REPO_PATH = /^dashboard\/src\/[A-Za-z0-9._@/-]+$/
// The website is served under this Pages base path.
const SITE_BASE = 'gh-nibfin/'

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

function list(v: unknown): unknown[] {
  return Array.isArray(v) ? v : []
}

// normalizePath maps a resolved frame filename to a repository path, or null.
// How Sentry shows a frame resolved through Debug-ID source maps is not
// documented, so the accepted forms live here alone.
export function normalizePath(name: string): string | null {
  let p = name.trim()
  if (/^https?:\/\//i.test(p)) return null
  p = p.replace(/^(webpack|app):\/\/[^/]*\//, '').replace(/^(webpack|app):\/\//, '')
  p = p.replace(/^~\//, '').replace(/^\/+/, '')
  for (;;) {
    const before = p
    p = p.replace(/^\.\.?\//, '')
    if (p.startsWith(SITE_BASE)) p = p.slice(SITE_BASE.length)
    if (p === before) break
  }
  if (p.startsWith('src/')) p = 'dashboard/' + p
  if (!REPO_PATH.test(p)) return null
  if (p.split('/').some((seg) => seg === '..' || seg === '.' || seg === '')) return null
  return p
}

function fail(failed: string, release?: string): Verification {
  return { verified: false, failed, release }
}

interface RawFrame {
  frame: Record<string, unknown>
  raw?: Record<string, unknown>
}

// inAppFrames collects the in-app frames of every exception entry with the
// raw (minified) frame at the same index, when Sentry kept one.
export function inAppFrames(event: Record<string, unknown>): RawFrame[] {
  const out: RawFrame[] = []
  for (const entry of list(event.entries)) {
    if (!isRecord(entry) || entry.type !== 'exception' || !isRecord(entry.data)) continue
    for (const value of list(entry.data.values)) {
      if (!isRecord(value)) continue
      const frames = isRecord(value.stacktrace) ? list(value.stacktrace.frames) : []
      const raws = isRecord(value.rawStacktrace) ? list(value.rawStacktrace.frames) : []
      frames.forEach((frame, i) => {
        if (isRecord(frame) && frame.inApp === true) out.push({ frame, raw: isRecord(raws[i]) ? raws[i] : undefined })
      })
    }
  }
  return out
}

function frameName(f: Record<string, unknown>): string | undefined {
  if (typeof f.filename === 'string' && f.filename !== '') return f.filename
  if (typeof f.absPath === 'string' && f.absPath !== '') return f.absPath
  return undefined
}

function positive(v: unknown): v is number {
  return typeof v === 'number' && Number.isSafeInteger(v) && v > 0
}

async function check(issue: SentryIssue, deps: VerifyDeps): Promise<Verification> {
  if (!deps.sentry) return fail('sentry token: not set')
  let event: unknown
  try {
    event = (await deps.sentry.get(`organizations/${SENTRY_ORG}/issues/${issue.id}/events/latest/`))!.body
  } catch (err) {
    const status = isRecord(err) && typeof err.status === 'number' ? err.status : 0
    return fail(`sentry api: ${status === 0 ? 'no response' : `HTTP ${status}`}`)
  }
  if (!isRecord(event)) return fail('sentry api: unexpected answer')

  const rel = event.release
  const version = isRecord(rel) ? rel.version : rel
  if (typeof version !== 'string' || !SHA.test(version)) return fail('release: missing or not a full commit SHA')
  const sha = version
  const repo = deps.repository

  const runs = (await deps.github.get(`repos/${repo}/actions/workflows/website.yml/runs?head_sha=${sha}&branch=main&event=push&status=success&per_page=1`))!.body
  if (!isRecord(runs) || typeof runs.total_count !== 'number' || runs.total_count < 1) return fail('release: no successful Website run on main', sha)

  for (const e of list(event.errors)) {
    const type = isRecord(e) && typeof e.type === 'string' ? e.type : ''
    if (type.startsWith('js_')) return fail(`frames: source map processing error ${type.replace(/[^a-z0-9_]/gi, '').slice(0, 60)}`, sha)
  }

  const frames = inAppFrames(event).slice(0, MAX_FRAMES)
  if (frames.length === 0) return fail('frames: no in-app frame', sha)

  const resolved: Frame[] = []
  for (const [i, { frame, raw }] of frames.entries()) {
    const name = frameName(frame)
    const rawName = raw ? frameName(raw) : undefined
    const path = name === undefined || name === rawName ? null : normalizePath(name)
    if (path === null || !positive(frame.lineNo)) return fail(`frames: ${i} not resolved to dashboard/src/`, sha)
    resolved.push({ path, line: frame.lineNo, ...(positive(frame.colNo) ? { column: frame.colNo } : {}) })
  }

  const tree = (await deps.github.get(`repos/${repo}/git/trees/${sha}?recursive=1`))!.body
  if (!isRecord(tree) || !Array.isArray(tree.tree)) return fail('frames: unexpected tree answer', sha)
  if (tree.truncated === true) return fail('frames: tree truncated', sha)
  const blobs = new Set(tree.tree.filter((t): t is Record<string, unknown> => isRecord(t) && t.type === 'blob' && typeof t.path === 'string').map((t) => t.path as string))
  for (const f of resolved) {
    if (!blobs.has(f.path)) return fail(`frames: ${f.path} not in the repository at ${sha.slice(0, 7)}`, sha)
  }

  const seen = new Set<string>()
  const unique = resolved.filter((f) => {
    const key = `${f.path}:${f.line}:${f.column ?? ''}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
  return { verified: true, release: sha, frames: unique }
}

export async function verify(issue: SentryIssue, deps: VerifyDeps): Promise<Verification> {
  try {
    return await check(issue, deps)
  } catch (err) {
    // A GitHub error while checking: name the call, never a body.
    const msg = err instanceof Error ? err.message.slice(0, 200) : 'unknown error'
    return fail(`check failed: ${msg}`)
  }
}
