import { CircleCheckBig, CircleDot, type LucideIcon } from 'lucide-react'

import { Note } from '@/components/shell/note'

import type { LiveData } from '../../../worker/messages'
import { repoNames, showcaseStart, sinceLabel } from '../../lib/summary'
import { RollingNumber } from './RollingNumber'

function Stat({ icon: Icon, label, value, slot }: { icon: LucideIcon; label: string; value: number | null; slot: string }) {
  return (
    <div className="flex min-w-0 flex-col gap-2 rounded-lg border border-border-subtle bg-card p-4 text-card-foreground sm:p-5" data-slot={slot}>
      <dt className="flex items-center gap-2 text-sm text-muted-foreground">
        <Icon aria-hidden="true" className="size-4 shrink-0" />
        {label}
      </dt>
      <dd className="m-0">
        <RollingNumber value={value} className="font-heading text-4xl font-semibold tracking-tight sm:text-5xl" />
      </dd>
    </div>
  )
}

// SummaryPanel counts the Issues created since the showcase start and those
// delivered automatically since then, over the Worker's repositories.
export function SummaryPanel({ data }: { data?: LiveData }) {
  const summary = data?.summary
  const since = sinceLabel(summary?.since ?? showcaseStart)
  return (
    <div className="flex flex-col gap-3" data-slot="summary">
      <dl className="m-0 grid gap-3 sm:grid-cols-2">
        <Stat icon={CircleDot} label={`Issues since ${since}`} value={summary?.created ?? null} slot="summary-created" />
        <Stat icon={CircleCheckBig} label="Delivered automatically" value={summary?.delivered ?? null} slot="summary-delivered" />
      </dl>
      <p className="m-0 text-sm text-muted-foreground">
        Since {since}
        {data && data.repos.length > 0 ? ` across ${repoNames(data.repos)}` : ''}. Delivered counts Issues closed with the{' '}
        <code className="font-mono text-xs">delivered</code> label.
      </p>
      {summary?.error && (
        <Note tone="warning" data-note="summary-error">
          Summary could not be read: {summary.error.code}: {summary.error.message}
        </Note>
      )}
    </div>
  )
}
