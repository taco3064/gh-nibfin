import { useId } from 'react'
import {
  CircleAlert,
  CircleCheck,
  CircleDashed,
  CircleHelp,
  CirclePause,
  CirclePlay,
  ExternalLink,
  LoaderCircle,
  MessageSquareWarning,
  PackageCheck,
  PencilRuler,
  type LucideIcon,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  blockersView,
  changeAccess,
  pendingView,
  stageLabels,
  stageOf,
  type Change,
  type Stage,
} from '@/lib/issue'
import type { RepoPermission, StatusIssue } from '@/lib/status'
import { cn } from '@/lib/utils'

import { ChangeMessage, DisabledReason } from './change-message'
import { IssueRunsDialog } from './issue-runs-dialog'
import { tones, type Tone } from './tones'
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

const stageLook: Record<Stage, { tone: Tone; icon: LucideIcon }> = {
  none: { tone: 'neutral', icon: CircleDashed },
  shape: { tone: 'info', icon: PencilRuler },
  deliver: { tone: 'info', icon: PackageCheck },
  done: { tone: 'success', icon: CircleCheck },
}

const linkClass = 'rounded-sm text-link underline-offset-4 hover:underline'

export function ExternalMark({ label }: { label: string }) {
  return (
    <>
      <ExternalLink aria-hidden="true" className="inline size-3.5 shrink-0 align-[-0.125em]" />
      <span className="sr-only">{label}</span>
    </>
  )
}

function StageBadges({ issue }: { issue: StatusIssue }) {
  const stage = stageOf(issue.labels)
  const look = stageLook[stage]
  const StageIcon = look.icon
  const pending = issue.pending ? pendingView(issue) : null
  return (
    <div className="flex flex-wrap items-center gap-1.5">
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
          {pending.kind === 'notFound' && <span className="text-sm text-muted-foreground">Reason comment not found</span>}
        </span>
      )}
      <Badge variant="outline" data-badge="stage" data-stage={stage} className={tones[look.tone]}>
        <StageIcon aria-hidden="true" />
        <span className="sr-only">Stage: </span>
        {stageLabels[stage]}
      </Badge>
    </div>
  )
}

function Blockers({ issue }: { issue: StatusIssue }) {
  const v = blockersView(issue)
  if (v.open.length === 0 && !v.failed && !v.notRead) return null
  return (
    <div data-slot="blockers" className="flex flex-wrap items-baseline gap-x-2 gap-y-1 text-sm">
      <span className="text-muted-foreground">Blocked by</span>
      {v.open.length > 0 && (
        <ul className="contents">
          {v.open.map((b) => (
            <li key={b.url}>
              <a href={b.url} target="_blank" rel="noreferrer" title={b.title} className={cn(linkClass, 'break-all')}>
                {`${b.repo}#${b.number}`}
                <ExternalMark label={`${b.title} (opens in a new tab)`} />
              </a>
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
// labels, its GitHub link, its runs, its open blockers and pause/resume.
// It renders only from its props; the placing view applies onChanged.
export function IssueItem({ issue, repo, access, selected, onSelectedChange, onChanged, stacked = false, className }: IssueItemProps) {
  const id = useId()
  const change = useChange(onChanged)
  const gate = changeAccess(access, repo)
  // resume clears both pause and pending.
  const action = issue.pause || issue.pending ? 'resume' : 'pause'
  const running = change.running !== null
  const selectable = selected !== undefined && onSelectedChange !== undefined
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
          <StageBadges issue={issue} />
          <Blockers issue={issue} />
        </div>
        <div className={cn('flex shrink-0 flex-col gap-2', !stacked && 'sm:items-end')}>
          <div className="flex flex-wrap items-center gap-2">
            <IssueRunsDialog issue={issue} />
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
          </div>
          <div className={cn('flex max-w-full flex-col gap-2', !stacked && 'sm:max-w-xs')}>
            <DisabledReason id={`${id}-reason`} access={gate} />
            <ChangeMessage outcome={change.outcome} />
          </div>
        </div>
      </div>
    </article>
  )
}
