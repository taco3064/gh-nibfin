import type { LucideIcon } from 'lucide-react'

import { cn } from '@/lib/utils'

export interface Segment {
  key: string
  label: string
  count: number
  // className is the segment's token background, such as bg-info.
  className: string
  // icon marks the segment in the legend, so color never carries the
  // meaning alone.
  icon: LucideIcon
}

// StackedBar is a rounded track of proportional segments with a legend of
// their counts beside it. The track is one image whose label lists every
// segment as text; segments of zero take no room, and an empty total shows
// an empty track. It has no animation.
export function StackedBar({ label, segments, className }: { label: string; segments: Segment[]; className?: string }) {
  const total = segments.reduce((n, s) => n + s.count, 0)
  const text = `${label}: ${segments.map((s) => `${s.label} ${s.count}`).join(', ')}`
  return (
    <div className={cn('flex min-w-0 flex-col gap-2', className)} data-slot="stacked-bar">
      <div role="img" aria-label={text} className="flex h-2.5 w-full gap-0.5 overflow-hidden rounded-full border border-border bg-muted">
        {total > 0 &&
          segments
            .filter((s) => s.count > 0)
            .map((s) => <div key={s.key} data-segment={s.key} className={cn('h-full first:rounded-s-full last:rounded-e-full', s.className)} style={{ flexGrow: s.count, flexBasis: 0 }} />)}
      </div>
      <ul aria-hidden="true" className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
        {segments.map((s) => {
          const Icon = s.icon
          return (
            <li key={s.key} className="flex items-center gap-1" data-legend={s.key}>
              <span className={cn('size-2 shrink-0 rounded-full', s.className)} />
              <Icon className="size-3 shrink-0" />
              <span>{s.label}</span>
              <span className="font-semibold text-foreground tabular-nums">{s.count}</span>
            </li>
          )
        })}
      </ul>
    </div>
  )
}
