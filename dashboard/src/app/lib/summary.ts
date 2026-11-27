// Text and digits of the Summary panel.

import { ownerRepo } from '@/lib/status'

import type { RepoData, Summary, TrendDay } from '../../worker/messages'

export const showcaseStart: Summary['since'] = '2026-11-21T00:00:00+08:00'

// sinceLabel is the start date as "21 Nov 2026", in the showcase's time
// zone.
export function sinceLabel(since: string): string {
  return new Intl.DateTimeFormat('en-GB', { day: 'numeric', month: 'short', year: 'numeric', timeZone: 'Asia/Taipei' }).format(new Date(since))
}

// repoNames joins the repositories' names as "a, b and c".
export function repoNames(repos: RepoData[]): string {
  const names = repos.map((r) => ownerRepo(r.repo).split('/').pop() ?? r.repo)
  if (names.length <= 1) return names.join('')
  return `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`
}

// digits gives the columns of a rolling number: one per digit, with ","
// between thousands. null (not read yet) gives none.
export function digits(value: number | null): string[] {
  if (value === null || !Number.isFinite(value)) return []
  return [...Math.max(0, Math.floor(value)).toLocaleString('en-US')]
}

// trendBarPercent is a trend bar's height as a percentage of the 7-day
// maximum. A day with deliveries keeps a visible bar; a day without any
// has none (the chart shows a baseline).
export function trendBarPercent(value: number, max: number): number {
  if (!(value > 0) || !(max > 0)) return 0
  return Math.max(8, Math.round((Math.min(value, max) / max) * 100))
}

const dayParts = new Intl.DateTimeFormat('en-GB', { weekday: 'short', day: 'numeric', month: 'short', timeZone: 'Asia/Taipei' })

// trendDay names a trend date (YYYY-MM-DD, an Asia/Taipei day) in English,
// whatever the viewer's locale: weekday "Thu", day "27", label "Thu 27"
// and long "Thu 27 Nov".
export function trendDay(date: string): { weekday: string; day: string; label: string; long: string } {
  const at = new Date(`${date}T12:00:00+08:00`)
  if (Number.isNaN(at.getTime())) return { weekday: '', day: date, label: date, long: date }
  const parts = Object.fromEntries(dayParts.formatToParts(at).map((p) => [p.type, p.value]))
  const weekday = parts.weekday ?? ''
  const day = parts.day ?? ''
  return { weekday, day, label: `${weekday} ${day}`, long: `${weekday} ${day} ${parts.month ?? ''}` }
}

// trendDayText reads one day of the trend aloud: "Thu 27 Nov: 3 delivered".
export function trendDayText(day: TrendDay): string {
  return `${trendDay(day.date).long}: ${day.delivered} delivered`
}

// trendTotal adds up the 7 days.
export function trendTotal(days: TrendDay[]): number {
  return days.reduce((n, d) => n + d.delivered, 0)
}
