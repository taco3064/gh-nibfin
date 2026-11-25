// Pure logic of the Dashboard shell: the views, the landing rule and the
// freshness of the data.

import type { HostReport, PersonView } from '@/lib/status'

export type View = 'person' | 'repo' | 'runner'

export const views: readonly View[] = ['person', 'repo', 'runner']

export const viewLabels: Record<View, string> = {
  person: 'Person',
  repo: 'Repositories',
  runner: 'Runners',
}

// hostAbsent are the runners.error codes that mean this host has no Podman
// or no machine, so no Runner slot can exist on it.
const hostAbsent = ['podman_missing', 'machine_missing']

// hasNoRunner reports whether this host has no complete Runner slot. A slot
// is complete when it is configured, has its unit and is enabled; an
// incomplete slot is what an interrupted runner add left. Any other read
// error means Runners may exist, so it is not "no Runner".
export function hasNoRunner(host: HostReport): boolean {
  const r = host.runners
  if (r.error) return hostAbsent.includes(r.error.code)
  return !(r.runners ?? []).some((s) => s.configured && s.unit && s.enabled)
}

// landingView is the view the shell opens on: the runner view when this host
// has no Runner yet, the person view otherwise.
export function landingView(view: PersonView): View {
  return hasNoRunner(view.host) ? 'runner' : 'person'
}

export const staleAfterMs = 5 * 60 * 1000

export interface Freshness {
  stale: boolean
  // staleAt is the time in ms the data turns stale.
  staleAt: number
}

// freshness tells whether data read at readAt is stale at now (both in ms).
export function freshness(readAt: number, now: number): Freshness {
  return { stale: now - readAt >= staleAfterMs, staleAt: readAt + staleAfterMs }
}
