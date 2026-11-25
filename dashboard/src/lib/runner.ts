// Pure logic of the runner view (#354): labels of a Runner slot's local
// service and GitHub state, memory in GiB, the slots per provider, and the
// provider an operation dialog opens with. It names no argument: the preset
// is found through the schema's allowed values.

import { fieldsOf, type Command, type Values } from '@/lib/operation'
import type { ApiError, HostReport, RunnerState } from '@/lib/status'

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
export function githubLabel(s: RunnerState, githubError?: ApiError): Label {
  if (githubError) return { text: 'GitHub not read', tone: 'warning' }
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
