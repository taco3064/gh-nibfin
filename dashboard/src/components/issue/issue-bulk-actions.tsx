import { useId } from 'react'
import { CirclePause, CirclePlay, LoaderCircle } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { bulkAccess, type Action, type Change, type ChangeOutcome } from '@/lib/issue'
import type { RepoPermission, StatusIssue } from '@/lib/status'
import { cn } from '@/lib/utils'

import { ChangeMessage, DisabledReason } from './change-message'
import { useChange } from './use-change'

export interface SelectedIssue {
  issue: StatusIssue
  repo: string
  access: RepoPermission
}

export interface IssueBulkActionsProps {
  selected: readonly SelectedIssue[]
  onChanged: (changes: Change[]) => void
  className?: string
}

function doneText(o: ChangeOutcome | null): string | undefined {
  if (o?.state !== 'done') return undefined
  const paused = o.changes.filter((c) => c.result === 'paused').length
  const resumed = o.changes.length - paused
  const parts = [paused && `paused ${paused}`, resumed && `resumed ${resumed}`].filter(Boolean)
  return parts.length ? `Confirmed: ${parts.join(', ')}.` : undefined
}

// IssueBulkActions pauses or resumes every selected Issue in one operation.
export function IssueBulkActions({ selected, onChanged, className }: IssueBulkActionsProps) {
  const id = useId()
  const change = useChange(onChanged)
  const gate = bulkAccess(selected)
  const running = change.running !== null
  const urls = selected.map((s) => s.issue.url)
  const button = (action: Action, idle: string, busy: string) => {
    const busyHere = change.running === action
    const Icon = busyHere ? LoaderCircle : action === 'pause' ? CirclePause : CirclePlay
    return (
      <Button
        variant="outline"
        size="sm"
        data-action={`${action}-selected`}
        disabled={!gate.enabled || running}
        aria-describedby={gate.enabled ? undefined : `${id}-reason`}
        onClick={() => void change.run(action, urls)}
      >
        <Icon aria-hidden="true" className={cn(busyHere && 'motion-safe:animate-spin')} />
        {busyHere ? busy : idle}
      </Button>
    )
  }
  return (
    <section
      aria-label="Selected Issues"
      className={cn('flex flex-col gap-2 rounded-lg border border-border-subtle bg-card p-3 text-card-foreground', className)}
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm font-medium tabular-nums" data-slot="selected-count">
          {`${selected.length} selected`}
        </p>
        <div className="flex flex-wrap gap-2">
          {button('pause', 'Pause selected', 'Pausing…')}
          {button('resume', 'Resume selected', 'Resuming…')}
        </div>
      </div>
      <DisabledReason id={`${id}-reason`} access={gate} />
      <ChangeMessage outcome={change.outcome} done={doneText(change.outcome)} />
    </section>
  )
}
