import { History } from 'lucide-react'

import { Note } from '@/components/shell/note'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { clock, hourTicks, laneBars, periodLabel, type Bar } from '@/lib/runner'
import type { Activity, RunnerState } from '@/lib/status'
import { cn, formatTime } from '@/lib/utils'

import type { TimelineState } from './use-activity'

// BusyTimeline is the "Last 24 hours" card: one lane per Runner slot with
// its busy periods from the execution repository's jobs, failed ones in
// the danger color. It shows only a full read's history: while only the
// Runners' current Jobs are in, it says it is still reading. When the jobs
// could not be read it says why and draws no lanes; when the repository
// had more runs than GitHub lists, it says how far back the lanes reach.
export function BusyTimeline({ slots, activity }: { slots: RunnerState[]; activity: TimelineState }) {
  const a = activity.state === 'failed' ? undefined : activity.activity
  const error = activity.state === 'failed' ? activity.error : a?.jobsError
  return (
    <Card className="gap-4 border-border-subtle py-4" data-card="timeline" role="region" aria-labelledby="timeline-title">
      <CardHeader className="px-4 sm:px-6">
        <CardTitle>
          <h2 id="timeline-title" className="flex items-center gap-2 text-base">
            <History aria-hidden="true" className="size-5 shrink-0" />
            Last 24 hours
          </h2>
        </CardTitle>
        <CardDescription>When each Runner was busy, from the execution repository&apos;s jobs.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4 px-4 sm:px-6">
        {error ? (
          <Note tone="warning" data-note="jobs-error">
            The last 24 hours are not available; this view shows the local state only. {error.code}: {error.message}
          </Note>
        ) : !a ? (
          <p className="text-sm text-muted-foreground" data-slot="timeline-loading">
            Reading the jobs of the last 24 hours…
          </p>
        ) : (
          <>
            {a.coversFrom && (
              <Note tone="info" data-note="covers-from">
                The lanes reach back only to <time dateTime={a.coversFrom}>{formatTime(a.coversFrom)}</time>: the execution repository had more runs in the last
                24 hours than GitHub lists.
              </Note>
            )}
            <Lanes slots={slots} activity={a} />
          </>
        )}
      </CardContent>
    </Card>
  )
}

function Lanes({ slots, activity: a }: { slots: RunnerState[]; activity: Activity }) {
  const ticks = hourTicks(a.from, a.to)
  const bySlot = new Map(a.runners.map((r) => [r.slot, r]))
  return (
    <>
      <Legend />
      <ul className="flex flex-col gap-3" aria-label="Runner lanes">
        {slots.map((s) => {
          const bars = laneBars(bySlot.get(s.slot)?.periods ?? [], a.from, a.to)
          return (
            <li key={s.slot} data-lane={s.slot} className="grid gap-1.5 sm:grid-cols-[9rem_1fr] sm:items-center sm:gap-3">
              <p className="min-w-0 truncate text-sm">
                <span className="font-medium">{s.slot}</span>
                <span className="text-muted-foreground">
                  {' '}
                  · {bars.length} {bars.length === 1 ? 'Job' : 'Jobs'}
                </span>
              </p>
              <Lane bars={bars} ticks={ticks.map((t) => t.x)} />
            </li>
          )
        })}
      </ul>
      <div className="grid sm:grid-cols-[9rem_1fr] sm:gap-3" aria-hidden="true">
        <span className="hidden sm:block" />
        <div className="relative h-4 text-xs text-muted-foreground tabular-nums" data-slot="axis">
          {ticks.map((t) => (
            <span
              key={t.at}
              className={cn('absolute top-0 whitespace-nowrap', t.x < 0.06 ? '' : t.x > 0.94 ? '-translate-x-full' : '-translate-x-1/2')}
              style={{ left: `${t.x * 100}%` }}
            >
              {clock(t.at)}
            </span>
          ))}
        </div>
      </div>
    </>
  )
}

function Lane({ bars, ticks }: { bars: Bar[]; ticks: number[] }) {
  return (
    <div className="relative h-7 rounded-md border border-border-subtle bg-muted/50">
      {ticks.map((x) => (
        <span key={x} aria-hidden="true" className="absolute inset-y-0 w-px bg-border-subtle" style={{ left: `${x * 100}%` }} />
      ))}
      {bars.length === 0 && <span className="sr-only">No Jobs</span>}
      {bars.map((b, i) => {
        const label = periodLabel(b.period)
        const cls = cn('absolute inset-y-1 rounded-sm', b.period.failed ? 'bg-danger' : 'bg-primary')
        const style = { left: `${b.x * 100}%`, width: `${b.width * 100}%`, minWidth: '3px' }
        return b.period.url ? (
          <a
            key={i}
            href={b.period.url}
            target="_blank"
            rel="noreferrer"
            aria-label={label}
            title={label}
            data-period={b.period.failed ? 'failed' : 'busy'}
            className={cls}
            style={style}
          />
        ) : (
          <span key={i} role="img" aria-label={label} title={label} data-period={b.period.failed ? 'failed' : 'busy'} className={cls} style={style} />
        )
      })}
    </div>
  )
}

function Legend() {
  return (
    <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground" aria-label="Legend">
      <li className="flex items-center gap-1.5">
        <span aria-hidden="true" className="inline-block h-2.5 w-4 rounded-sm bg-primary" />
        Busy
      </li>
      <li className="flex items-center gap-1.5">
        <span aria-hidden="true" className="inline-block h-2.5 w-4 rounded-sm bg-danger" />
        Failed or timed out
      </li>
    </ul>
  )
}
