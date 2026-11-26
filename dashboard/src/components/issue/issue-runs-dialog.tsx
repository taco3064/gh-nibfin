import type { ReactNode } from 'react'
import { CircleAlert, CircleHelp, ExternalLink, History, TriangleAlert } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { runsView, type RunsNote } from '@/lib/issue'
import type { Run, StatusIssue } from '@/lib/status'
import { cn, formatTime } from '@/lib/utils'

import { tones, type Tone } from './tones'

export const truncatedText = 'Older runs may be missing: only the newest runs were read.'

function words(v: string): string {
  return v.replaceAll('_', ' ')
}

function statusTone(status: string): Tone {
  return status === 'completed' ? 'neutral' : 'info'
}

function conclusionTone(c: string | null): Tone {
  switch (c) {
    case null:
      return 'info'
    case 'success':
      return 'success'
    case 'failure':
    case 'timed_out':
    case 'startup_failure':
      return 'danger'
    case 'action_required':
      return 'warning'
    default:
      return 'neutral'
  }
}

function Note({ note }: { note: RunsNote }) {
  const [tone, Icon, text]: [Tone, typeof CircleAlert, string] =
    note.kind === 'failed'
      ? ['danger', CircleAlert, `Runs could not be read in full: ${note.error.code}: ${note.error.message}`]
      : note.kind === 'truncated'
        ? ['warning', TriangleAlert, truncatedText]
        : ['warning', CircleHelp, 'Runs were not read for this Issue.']
  return (
    <p data-note={note.kind} className={cn('flex items-start gap-2 rounded-md px-3 py-2 text-sm break-words', tones[tone])}>
      <Icon aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
      <span className="min-w-0">{text}</span>
    </p>
  )
}

function RunRow({ run }: { run: Run }) {
  return (
    <li className="flex flex-col gap-2 py-3 first:pt-0 last:pb-0">
      <div className="flex flex-wrap items-start justify-between gap-x-3 gap-y-1">
        <p className="min-w-0 flex-1 font-medium break-words">{run.name}</p>
        <a
          href={run.url}
          target="_blank"
          rel="noreferrer"
          className="inline-flex shrink-0 items-center gap-1 rounded-sm text-sm text-link underline-offset-4 hover:underline"
        >
          Open run
          <ExternalLink aria-hidden="true" className="size-3.5" />
          <span className="sr-only">{`${run.name} on GitHub (opens in a new tab)`}</span>
        </a>
      </div>
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm text-muted-foreground">
        <Badge variant="outline" className={tones[statusTone(run.status)]}>
          <span className="sr-only">Status: </span>
          {words(run.status)}
        </Badge>
        <Badge variant="outline" className={tones[conclusionTone(run.conclusion)]}>
          <span className="sr-only">Conclusion: </span>
          {run.conclusion === null ? 'running' : words(run.conclusion)}
        </Badge>
        <time dateTime={run.createdAt}>{formatTime(run.createdAt)}</time>
        <span className="break-all">{run.repo}</span>
      </div>
    </li>
  )
}

// IssueRunsDialog lists the Nibfin runs of one Issue from the status data.
// trigger, when given, is the element that opens it (it must take a ref and
// its props, as DialogTrigger asChild passes them); by default a "Runs"
// button with the count.
export function IssueRunsDialog({ issue, trigger }: { issue: StatusIssue; trigger?: ReactNode }) {
  const v = runsView(issue)
  const count = v.runs.length
  return (
    <Dialog>
      <DialogTrigger asChild>
        {trigger ?? (
          <Button variant="outline" size="sm">
            <History aria-hidden="true" />
            Runs
            {count > 0 && <span className="tabular-nums text-muted-foreground">{count}</span>}
            {v.notes.some((n) => n.kind !== 'truncated') && (
              <>
                <CircleAlert aria-hidden="true" className="text-warning" />
                <span className="sr-only">(incomplete)</span>
              </>
            )}
          </Button>
        )}
      </DialogTrigger>
      <DialogContent className="flex max-h-[calc(100svh-2rem)] flex-col bg-card sm:max-w-xl">
        <DialogHeader className="pr-8 text-left">
          <DialogTitle>{`Nibfin runs of #${issue.number}`}</DialogTitle>
          <DialogDescription className="break-words">{issue.title}</DialogDescription>
        </DialogHeader>
        <div className="-mx-6 flex min-h-0 flex-col gap-3 overflow-y-auto px-6">
          {v.notes.map((n) => (
            <Note key={n.kind} note={n} />
          ))}
          {v.empty && (
            <p data-note="empty" className="text-sm text-muted-foreground">
              No Nibfin runs
            </p>
          )}
          {count > 0 && (
            <ul aria-label="Runs, newest first" className="divide-y divide-border-subtle">
              {v.runs.map((r) => (
                <RunRow key={r.url} run={r} />
              ))}
            </ul>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
