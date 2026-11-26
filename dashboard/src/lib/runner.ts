// Pure logic of the runner view (#354): labels of a Runner slot's local
// service and GitHub state, memory in GiB, the slots per provider, and the
// provider an operation dialog opens with. It names no argument: the preset
// is found through the schema's allowed values.

import { fieldsOf, type Command, type Values } from '@/lib/operation'
import type { ApiError, CurrentJob, HostReport, Period, RunnerState } from '@/lib/status'

export type LabelTone = 'neutral' | 'info' | 'success' | 'warning' | 'danger'

export interface Label {
  text: string
  tone: LabelTone
}

// gib shows kB as GiB with one decimal.
export function gib(kb: number): string {
  return `${(kb / 1024 / 1024).toFixed(1)} GiB`
}

// serviceLabel is a slot's local systemd service: active or inactive for a
// complete slot, or what an incomplete slot misses.
export function serviceLabel(s: RunnerState): Label {
  const missing = [!s.configured && 'not configured', !s.unit && 'no unit', !s.enabled && 'not enabled'].filter(
    (m): m is string => typeof m === 'string',
  )
  if (missing.length > 0) return { text: `Incomplete: ${missing.join(', ')}`, tone: 'warning' }
  return s.active ? { text: 'Service active', tone: 'success' } : { text: 'Service inactive', tone: 'danger' }
}

// githubLabel is GitHub's view of a slot, joined by its name.
export function githubLabel(s: RunnerState, githubError?: ApiError, needsAdmin = false): Label {
  if (githubError) return { text: 'GitHub not read', tone: 'warning' }
  if (needsAdmin) return { text: 'GitHub needs admin', tone: 'neutral' }
  if (!s.listed) return { text: 'Not listed on GitHub', tone: 'warning' }
  if (s.status === 'online') return s.busy ? { text: 'Online · Busy', tone: 'info' } : { text: 'Online · Idle', tone: 'success' }
  if (s.status === 'offline' || !s.status) return { text: 'Offline', tone: 'danger' }
  return { text: s.status, tone: 'neutral' }
}

// slotsByProvider groups the slots by provider: the providers of the report
// first, in data order, then any other provider a slot names.
export function slotsByProvider(report: HostReport): Map<string, RunnerState[]> {
  const out = new Map<string, RunnerState[]>()
  for (const p of report.providers ?? []) out.set(p.provider, [])
  for (const s of report.runners.runners ?? []) {
    const list = out.get(s.provider)
    if (list) list.push(s)
    else out.set(s.provider, [s])
  }
  return out
}

// providerPreset is the initial value that picks provider in command's
// dialog: the first field whose allowed values hold it. A command without
// such a field gets no preset.
export function providerPreset(command: Command, provider: string): Values {
  const f = fieldsOf(command).find((x) => x.allowed.includes(provider))
  return f ? { [f.key]: provider } : {}
}

// TileState is a Runner tile's state ring. github is true when online and
// offline came from GitHub's Runner list, false when only from the host.
export interface TileState {
  state: 'idle' | 'busy' | 'offline'
  github: boolean
}

// tileState is a slot's tile: offline from GitHub, or from the host when
// GitHub's list was not read (no admin or an error); busy from GitHub or a
// current Job; idle otherwise.
export function tileState(s: RunnerState, githubRead: boolean, current?: CurrentJob): TileState {
  const github = githubRead && s.listed
  const complete = s.configured && s.unit && s.enabled
  const offline = github ? s.status !== 'online' : !complete || !s.active
  if (offline) return { state: 'offline', github }
  if ((github && s.busy) || current) return { state: 'busy', github }
  return { state: 'idle', github }
}

// elapsed shows the time from an ISO start to now as "1h 05m", "3m" or
// "<1m"; "" for an unreadable start.
export function elapsed(start: string | undefined, now: number): string {
  const t = start ? Date.parse(start) : NaN
  if (Number.isNaN(t)) return ''
  const m = Math.max(0, Math.floor((now - t) / 60_000))
  if (m < 1) return '<1m'
  if (m < 60) return `${m}m`
  return `${Math.floor(m / 60)}h ${String(m % 60).padStart(2, '0')}m`
}

// Bar is one period on a lane, as fractions of the window.
export interface Bar {
  x: number
  width: number
  period: Period
}

// laneBars places each period on the window [from, to]: clipped to it, an
// open period runs to "to". A period outside the window or unreadable is
// dropped; a very short one keeps a visible minimum width.
export function laneBars(periods: Period[], from: string, to: string, minWidth = 0.004): Bar[] {
  const a = Date.parse(from)
  const b = Date.parse(to)
  if (Number.isNaN(a) || Number.isNaN(b) || b <= a) return []
  const span = b - a
  const out: Bar[] = []
  for (const p of periods) {
    const s = Math.max(a, Date.parse(p.start))
    const e = Math.min(b, p.end ? Date.parse(p.end) : b)
    if (Number.isNaN(s) || Number.isNaN(e) || e < s || s >= b || e <= a) continue
    const x = (s - a) / span
    out.push({ x, width: Math.min(1 - x, Math.max(minWidth, (e - s) / span)), period: p })
  }
  return out
}

// Tick is one time-axis mark, as a fraction of the window.
export interface Tick {
  x: number
  at: number
}

// hourTicks are the marks every step hours, on whole local hours, inside
// the window.
export function hourTicks(from: string, to: string, step = 6): Tick[] {
  const a = Date.parse(from)
  const b = Date.parse(to)
  if (Number.isNaN(a) || Number.isNaN(b) || b <= a) return []
  const d = new Date(a)
  d.setMinutes(0, 0, 0)
  if (d.getTime() < a) d.setHours(d.getHours() + 1)
  while (d.getHours() % step !== 0) d.setHours(d.getHours() + 1)
  const out: Tick[] = []
  for (; d.getTime() <= b; d.setHours(d.getHours() + step)) out.push({ x: (d.getTime() - a) / (b - a), at: d.getTime() })
  return out
}

// clock shows a time as 24-hour HH:MM in the viewer's time zone.
export function clock(ms: number): string {
  return new Date(ms).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })
}

// freeFraction is the share of free memory, 0 to 1, or null when the total
// is unknown.
export function freeFraction(m: { totalKB: number; availableKB: number }): number | null {
  if (!(m.totalKB > 0)) return null
  return Math.min(1, Math.max(0, m.availableKB / m.totalKB))
}

// jobLabel names a Job: its Issue, or "an unknown Issue".
export function jobLabel(j: { repo?: string; issue?: number }): string {
  return j.repo && j.issue ? `${j.repo}#${j.issue}` : 'an unknown Issue'
}

// periodLabel is a period's accessible name: Issue, stage, start–end and
// whether it failed.
export function periodLabel(p: Period): string {
  const end = p.end ? clock(Date.parse(p.end)) : 'now'
  const parts = [jobLabel(p), p.stage, `${clock(Date.parse(p.start))}–${end}`, p.failed ? 'failed' : undefined]
  return parts.filter(Boolean).join(' · ')
}
