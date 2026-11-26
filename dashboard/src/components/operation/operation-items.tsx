import { ArrowRight, Check, CircleAlert, CircleDashed, CircleHelp, Clock, LoaderCircle, X } from 'lucide-react'

import { tones, type Tone } from '@/components/issue/tones'
import { Badge } from '@/components/ui/badge'
import type { Phase, Row } from '@/lib/operation'
import { cn } from '@/lib/utils'

type Icon = typeof Check

function Tag({ tone, icon: I, slot, children, spin }: { tone: Tone; icon: Icon; slot: string; children: string; spin?: boolean }) {
  return (
    <Badge variant="outline" data-badge={slot} className={cn('whitespace-normal', tones[tone])}>
      <I aria-hidden="true" className={cn(spin && 'motion-safe:animate-spin')} />
      {children}
    </Badge>
  )
}

function ProgressTag({ row }: { row: Row }) {
  switch (row.progress) {
    case 'waiting':
      return (
        <Tag tone="neutral" icon={Clock} slot="progress">
          Waiting
        </Tag>
      )
    case 'started':
      return (
        <Tag tone="info" icon={LoaderCircle} slot="progress" spin>
          In progress
        </Tag>
      )
    case 'done':
      return (
        <Tag tone="neutral" icon={Check} slot="progress">
          Done
        </Tag>
      )
    case 'failed':
      return (
        <Tag tone="danger" icon={X} slot="progress">
          Failed
        </Tag>
      )
  }
}

function ReadBackTag({ row }: { row: Row }) {
  if (!row.check) {
    return (
      <Tag tone="warning" icon={CircleHelp} slot="readback">
        Not read back
      </Tag>
    )
  }
  return row.check.confirmed ? (
    <Tag tone="success" icon={Check} slot="readback">
      Confirmed
    </Tag>
  ) : (
    <Tag tone="danger" icon={CircleAlert} slot="readback">
      Not confirmed
    </Tag>
  )
}

function Transition({ row }: { row: Row }) {
  const s = row.step
  if (!s || s.noChange || (!s.current && !s.next)) return null
  return (
    <p data-slot="transition" className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-sm">
      <span className="sr-only">State: </span>
      <span className={cn('break-words', !s.current && 'text-muted-foreground italic')}>{s.current || 'unknown'}</span>
      <ArrowRight aria-hidden="true" className="size-3.5 shrink-0 text-muted-foreground" />
      <span className="sr-only">becomes </span>
      <span className="font-medium break-words">{s.next || 'unknown'}</span>
    </p>
  )
}

function ItemRow({ row, phase }: { row: Row; phase: Phase }) {
  const s = row.step
  return (
    <li data-item={row.id} className="flex flex-col gap-1.5 py-3 first:pt-0 last:pb-0">
      <div className="flex flex-col gap-x-3 gap-y-1.5 sm:flex-row sm:items-start sm:justify-between">
        <p className="min-w-0 flex-1 break-words">
          <span className="font-medium">{row.action}</span> <span className="text-muted-foreground">{row.target}</span>
        </p>
        <div className="flex flex-wrap gap-1.5 sm:justify-end">
          {s?.noChange && (
            <Tag tone="neutral" icon={CircleDashed} slot="no-change">
              No change needed
            </Tag>
          )}
          {s?.afterStart && (
            <Tag tone="info" icon={Clock} slot="after-start">
              Known after the machine starts
            </Tag>
          )}
          {(row.progress !== 'waiting' || (phase === 'applying' && !s?.noChange && !s?.afterStart)) && <ProgressTag row={row} />}
          {phase === 'finished' && !(s?.afterStart && !row.check) && <ReadBackTag row={row} />}
        </div>
      </div>
      {s?.detail && <p className="text-sm break-words text-muted-foreground">{s.detail}</p>}
      {s?.afterStart && !row.check && (phase === 'applying' || phase === 'finished') && (
        <p data-slot="placeholder" className="text-sm break-words text-muted-foreground">
          Placeholder: the items known after the machine starts are listed on their own as they report.
        </p>
      )}
      {phase !== 'finished' && <Transition row={row} />}
      {row.error && (
        <p data-slot="item-error" className="text-sm break-words text-danger">
          <code className="font-semibold">{row.error.code}</code>: {row.error.message}
        </p>
      )}
      {phase === 'finished' && row.check && (
        <p data-slot="check" className="text-sm break-words">
          <span className="text-muted-foreground">Read back: </span>
          <span className="break-words">{row.check.state || 'no state'}</span>
          {!row.check.confirmed && row.check.reason && <span className="text-muted-foreground"> · {row.check.reason}</span>}
        </p>
      )}
    </li>
  )
}

// CheckingRow is the check a plan runs now, after the items it checked.
function CheckingRow({ check }: { check: string }) {
  return (
    <li data-slot="checking" className="flex flex-col gap-1.5 py-3 first:pt-0 last:pb-0">
      <div className="flex flex-col gap-x-3 gap-y-1.5 sm:flex-row sm:items-start sm:justify-between">
        <p className="min-w-0 flex-1 break-words">
          <span className="font-medium">{check}</span>
        </p>
        <div className="flex flex-wrap gap-1.5 sm:justify-end">
          <Tag tone="info" icon={LoaderCircle} slot="checking" spin>
            Checking
          </Tag>
        </div>
      </div>
    </li>
  )
}

// OperationItems lists the dialog's items with what the phase knows of them.
// While planning, checking names the check running now; it ends the list.
export function OperationItems({ rows, phase, label, checking }: { rows: readonly Row[]; phase: Phase; label: string; checking?: string }) {
  if (phase === 'planning') {
    if (rows.length === 0 && !checking) return null
  } else if (rows.length === 0) {
    return <p className="text-sm text-muted-foreground">The plan lists no items.</p>
  }
  return (
    <>
      <ul aria-label={label} className="divide-y divide-border-subtle">
        {rows.map((r) => (
          <ItemRow key={r.id} row={r} phase={phase} />
        ))}
        {phase === 'planning' && checking && <CheckingRow check={checking} />}
      </ul>
      {phase === 'planning' && (
        <p aria-live="polite" className="sr-only">
          {checking ? `Checking ${checking}` : ''}
        </p>
      )}
    </>
  )
}

export function Notes({ notes }: { notes: readonly string[] }) {
  if (notes.length === 0) return null
  return (
    <section aria-label="Notes" className="flex flex-col gap-1.5">
      <h3 className="text-sm font-semibold">Notes</h3>
      <ul className="flex list-disc flex-col gap-1 pl-5 text-sm break-words text-muted-foreground">
        {notes.map((n, i) => (
          <li key={i}>{n}</li>
        ))}
      </ul>
    </section>
  )
}
