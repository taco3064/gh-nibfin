import { useId } from 'react'
import { CircleAlert, CircleHelp, CirclePause, CirclePlay, LoaderCircle, MessageSquareWarning } from 'lucide-react'

import { SectionLoading } from '@/components/shell/section-loading'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  answerable,
  blockersView,
  changeAccess,
  pendingView,
  safeLink,
  stageOf,
  type Change,
} from '@/lib/issue'
import { useLoading } from '@/lib/loading-context'
import { useReadOnly } from '@/lib/repo-source'
import type { RepoPermission, StatusIssue } from '@/lib/status'
import { cn } from '@/lib/utils'

import { ChangeMessage, DisabledReason } from './change-message'
import { ExternalMark } from './external-mark'
import { NeedsPerson } from './needs-person'
import { RepairAction, RepairBadge } from './repair-dialog'
import { useRepairTarget } from './use-repair-target'
import { RunStrip } from './run-strip'
import { StageTrack } from './stage-track'
import { tones } from './tones'
import { useChange } from './use-change'

export interface IssueItemProps {
  issue: StatusIssue
  // repo is OWNER/REPO of the Issue.
  repo: string
  access: RepoPermission
  // The selection checkbox shows only when both are given.
  selected?: boolean
  onSelectedChange?: (selected: boolean) => void
  // onChanged gets the confirmed changes of a pause or resume; an empty
  // list when the result is unknown and the status should be read again.
  onChanged: (changes: Change[]) => void
  // stacked keeps the column layout at every width, for narrow places such
  // as a graph column; by default it turns into a row from the sm width.
  stacked?: boolean
  className?: string
}

const linkClass = 'rounded-sm text-link underline-offset-4 hover:underline'

function StageMarkers({ issue }: { issue: StatusIssue }) {
  const loading = useLoading()
  const repair = issue.pending ? issue.repair : undefined
  const pending = issue.pending && !repair ? pendingView(issue) : null
  const comment = safeLink(issue.pendingComment)
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
        <StageTrack stage={stageOf(issue.labels)} />
        {issue.pause && (
          <Badge variant="outline" data-badge="paused" className={tones.warning}>
            <CirclePause aria-hidden="true" />
            Paused
          </Badge>
        )}
        {pending && (
          <span data-badge="pending" className="inline-flex flex-wrap items-center gap-x-1.5 gap-y-1">
            <Badge variant="outline" className={tones.warning}>
              <MessageSquareWarning aria-hidden="true" />
              Pending
            </Badge>
            {pending.kind === 'link' && (
              <a href={pending.url} target="_blank" rel="noreferrer" className={cn(linkClass, 'text-sm')}>
                Reason <ExternalMark label={`comment of #${issue.number} (opens in a new tab)`} />
              </a>
            )}
            {pending.kind === 'failed' && (
              <span className="text-sm break-words text-danger">
                {`Reason comment could not be read: ${pending.error.code}: ${pending.error.message}`}
              </span>
            )}
            {pending.kind === 'notFound' && loading.has('pendingComment') && <SectionLoading label="Reading the reason…" data-loading="pendingComment" />}
            {pending.kind === 'notFound' && !loading.has('pendingComment') && <span className="text-sm text-muted-foreground">Reason comment not found</span>}
          </span>
        )}
        {repair && (
          <span data-badge="pending" className="inline-flex flex-wrap items-center gap-x-1.5 gap-y-1">
            <RepairBadge repair={repair} />
            {comment && (
              <a href={comment} target="_blank" rel="noreferrer" className={cn(linkClass, 'text-sm')}>
                Comment <ExternalMark label={`pending comment of #${issue.number} (opens in a new tab)`} />
              </a>
            )}
          </span>
        )}
      </div>
      {repair && (
        <p data-slot="repair-line" className="text-sm break-words text-muted-foreground">
          {repair.reason}
        </p>
      )}
    </div>
  )
}

function Blockers({ issue }: { issue: StatusIssue }) {
  const loading = useLoading()
  if (loading.has('blockedBy')) {
    return (
      <div data-slot="blockers" className="text-sm">
        <SectionLoading label="Reading blockers…" data-loading="blockedBy" />
      </div>
    )
  }
  const v = blockersView(issue)
  if (v.open.length === 0 && !v.failed && !v.notRead) return null
  return (
    <div data-slot="blockers" className="flex flex-wrap items-center gap-x-2 gap-y-1.5 text-sm">
      <span className="text-muted-foreground">Waiting on</span>
      {v.open.length > 0 && (
        <ul className="contents">
          {v.open.map((b) => (
            <li key={b.url} className="max-w-full min-w-0">
              <Badge variant="outline" asChild className="max-w-full border-border text-sm font-normal whitespace-normal text-link hover:bg-accent">
                <a href={b.url} target="_blank" rel="noreferrer" title={b.title} data-slot="blocker-chip">
                  <span className="break-all">{`${b.repo}#${b.number}`}</span>
                  <ExternalMark label={`${b.title} (opens in a new tab)`} />
                </a>
              </Badge>
            </li>
          ))}
        </ul>
      )}
      {v.failed && (
        <span className="min-w-0 break-words text-danger">
          <CircleAlert aria-hidden="true" className="mr-1 inline size-3.5 align-[-0.125em]" />
          {`could not be read: ${v.failed.code}: ${v.failed.message}`}
        </span>
      )}
      {v.notRead && (
        <span className="text-warning">
          <CircleHelp aria-hidden="true" className="mr-1 inline size-3.5 align-[-0.125em]" />
          not read
        </span>
      )}
    </div>
  )
}

// IssueItem is the shared Dashboard element of one Issue: its stage from
// labels, its GitHub link, its runs, its open blockers, pause/resume and,
// for a pending Issue that waits on a person, the Repair dialog, and the
// parts of a repair decision Core left for a person.
// It renders only from its props; the placing view applies onChanged.
// Read-only, it offers no selection, pause, resume or Repair; its runs dialog,
// links and the parts left for a person stay.
export function IssueItem({ issue, repo, access, selected, onSelectedChange, onChanged, stacked = false, className }: IssueItemProps) {
  const id = useId()
  const loading = useLoading()
  const readOnly = useReadOnly()
  const change = useChange(onChanged)
  const gate = changeAccess(access, repo)
  const repairOn = !readOnly && answerable(issue) && issue.repair !== undefined
  const target = useRepairTarget(issue.repair, repairOn)
  // resume clears both pause and pending.
  const action = issue.pause || issue.pending ? 'resume' : 'pause'
  const running = change.running !== null
  const selectable = !readOnly && selected !== undefined && onSelectedChange !== undefined
  const ActionIcon = running ? LoaderCircle : action === 'pause' ? CirclePause : CirclePlay
  const label = running
    ? change.running === 'pause'
      ? 'Pausing…'
      : 'Resuming…'
    : action === 'pause'
      ? 'Pause'
      : 'Resume'

  return (
    <article
      aria-labelledby={`${id}-title`}
      data-issue={issue.url}
      className={cn('flex gap-3 rounded-lg border border-border-subtle bg-card p-4 text-card-foreground', className)}
    >
      {selectable && (
        <Checkbox
          className="mt-1"
          checked={selected}
          onCheckedChange={(v) => onSelectedChange(v === true)}
          aria-label={`Select #${issue.number}`}
        />
      )}
      <div className={cn('flex min-w-0 flex-1 flex-col gap-3', !stacked && 'sm:flex-row sm:items-start sm:justify-between')}>
        <div className="flex min-w-0 flex-col gap-2">
          <h3 id={`${id}-title`} className="text-base leading-snug font-semibold">
            <a href={issue.url} target="_blank" rel="noreferrer" className={cn(linkClass, 'text-foreground')}>
              <span className="text-muted-foreground tabular-nums">{`#${issue.number}`}</span> {issue.title}{' '}
              <ExternalMark label="(opens on GitHub in a new tab)" />
            </a>
          </h3>
          <span className="sr-only">{repo}</span>
          <StageMarkers issue={issue} />
          <Blockers issue={issue} />
          {issue.needsPerson && <NeedsPerson data={issue.needsPerson} />}
        </div>
        <div className={cn('flex shrink-0 flex-col gap-2', !stacked && 'sm:items-end')}>
          <div className="flex flex-wrap items-center gap-2">
            {loading.has('runs') ? <SectionLoading label="Reading runs…" data-loading="runs" /> : <RunStrip issue={issue} />}
            {!readOnly && (
              <Button
                variant="outline"
                size="sm"
                data-action={action}
                disabled={!gate.enabled || running}
                aria-describedby={gate.enabled ? undefined : `${id}-reason`}
                onClick={() => void change.run(action, [issue.url])}
              >
                <ActionIcon aria-hidden="true" className={cn(running && 'motion-safe:animate-spin')} />
                {label}
              </Button>
            )}
            {repairOn && issue.repair && (
              <RepairAction
                issue={issue}
                repair={issue.repair}
                gate={gate}
                target={target}
                describedBy={gate.enabled ? `${id}-repair` : `${id}-reason`}
                onChanged={() => onChanged([])}
              />
            )}
          </div>
          {!readOnly && (
            <div className={cn('flex max-w-full flex-col gap-2', !stacked && 'sm:max-w-xs')}>
              {/* While the permission is read, the reason is for screen readers only:
                  every row would repeat it. */}
              {access.permissionLoading && !gate.enabled ? (
                <span id={`${id}-reason`} className="sr-only">
                  {gate.reason}
                </span>
              ) : (
                <DisabledReason id={`${id}-reason`} access={gate} />
              )}
              {repairOn && gate.enabled && !target.ready && (
                <p id={`${id}-repair`} data-slot="repair-unavailable" className="text-sm break-words text-muted-foreground">
                  {target.reason}
                </p>
              )}
              <ChangeMessage outcome={change.outcome} />
            </div>
          )}
        </div>
      </div>
    </article>
  )
}
