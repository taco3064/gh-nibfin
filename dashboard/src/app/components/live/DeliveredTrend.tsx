import type { TrendDay } from '../../../worker/messages'
import { trendBarPercent, trendDay, trendDayText, trendTotal } from '../../lib/summary'

// DeliveredTrend draws the delivered count of each of the last 7 Taipei
// days as small bars, today last. Each day's count and date stay as text:
// visible above and below its bar, and one sentence per day for screen
// readers. Bars grow in under motion-safe only; with reduced motion they
// show at their final height.
export function DeliveredTrend({ days }: { days: TrendDay[] }) {
  const max = Math.max(0, ...days.map((d) => d.delivered))
  const total = trendTotal(days)
  return (
    <figure className="m-0 flex min-w-0 flex-col gap-2" data-slot="delivered-trend">
      <figcaption className="text-xs text-muted-foreground">
        Delivered per day, last 7 days <span className="whitespace-nowrap">({total} in total)</span>
      </figcaption>
      <ol className="m-0 grid list-none grid-cols-7 gap-1 p-0 sm:gap-1.5">
        {days.map((d, i) => {
          const day = trendDay(d.date)
          const pct = trendBarPercent(d.delivered, max)
          const today = i === days.length - 1
          return (
            <li key={d.date} className="flex min-w-0 flex-col items-center gap-1" data-slot="trend-day" data-date={d.date} data-value={d.delivered}>
              <span className="sr-only">
                {trendDayText(d)}
                {today ? ' (today)' : ''}
              </span>
              <span aria-hidden="true" className="text-xs font-semibold tabular-nums text-foreground">
                {d.delivered}
              </span>
              <span aria-hidden="true" className="flex h-14 w-full max-w-8 items-end border-b border-border">
                {pct > 0 && (
                  <span
                    data-slot="trend-bar"
                    className="block w-full origin-bottom rounded-t-sm bg-primary motion-safe:animate-grow"
                    style={{ height: `${pct}%` }}
                  />
                )}
              </span>
              <span aria-hidden="true" className="flex flex-col items-center text-[0.6875rem] leading-tight text-muted-foreground">
                <span className={today ? 'font-semibold text-foreground' : undefined}>{day.weekday}</span>
                <span className="tabular-nums">{day.day}</span>
              </span>
            </li>
          )
        })}
      </ol>
    </figure>
  )
}
