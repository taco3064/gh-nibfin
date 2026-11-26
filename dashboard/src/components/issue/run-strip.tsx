import { CircleAlert } from 'lucide-react'

import { recentRuns, runBucket, runBucketLabels, runBuckets, runsView, type RunBucket } from '@/lib/issue'
import type { Run, StatusIssue } from '@/lib/status'
import { cn, formatTime } from '@/lib/utils'

import { IssueRunsDialog } from './issue-runs-dialog'

// Token colors of the run buckets. Each bar also carries its name, and
// the legend names every color; skipped or cancelled bars are shorter so
// they differ in shape too.
const barClass: Record<RunBucket, string> = {
  succeeded: 'h-full bg-success',
  failed: 'h-full bg-danger',
  skipped: 'h-1/2 bg-muted-foreground',
  running: 'h-full bg-info motion-safe:animate-pulse',
}

function words(v: string): string {
  return v.replaceAll('_', ' ')
}

// barName is a run's accessible name: its name, its exact conclusion (or
// its status while it runs) and when it started.
function barName(run: Run): string {
  const state = run.conclusion === null ? words(run.status) : words(run.conclusion)
  return `${run.name}, ${state}, ${formatTime(run.createdAt)}`
}

// RunStrip shows the Issue's newest runs as bars, oldest left, colored by
// conclusion. Selecting it opens the run-history dialog. The bars stay
// outside the button, which would flatten them, so screen readers reach
// each bar; the button covers the strip for pointer and keyboard.
export function RunStrip({ issue }: { issue: StatusIssue }) {
  const v = runsView(issue)
  const runs = recentRuns(issue)
  const incomplete = v.notes.some((n) => n.kind !== 'truncated')
  const count = v.runs.length
  return (
    <div
      data-slot="run-strip"
      className="relative inline-flex h-8 max-w-full items-center gap-2 rounded-md border border-border bg-background px-2.5 shadow-xs has-[button:hover]:bg-accent dark:bg-input/30 has-[button:hover]:dark:bg-input/50"
    >
      {runs.length > 0 ? (
        <ol aria-label={`Recent runs of #${issue.number}, oldest first`} className="m-0 flex h-4 list-none items-end gap-0.5 p-0">
          {runs.map((r) => {
            const bucket = runBucket(r)
            return (
              <li key={r.url} className="flex h-full items-end">
                <span role="img" aria-label={barName(r)} data-run={bucket} className={cn('block w-1.5 rounded-[1px]', barClass[bucket])} />
              </li>
            )
          })}
        </ol>
      ) : (
        <span className="text-sm whitespace-nowrap text-muted-foreground">No runs</span>
      )}
      {incomplete && (
        <span className="inline-flex items-center" data-slot="runs-incomplete">
          <CircleAlert aria-hidden="true" className="size-4 text-warning" />
          <span className="sr-only">(incomplete)</span>
        </span>
      )}
      <IssueRunsDialog
        issue={issue}
        trigger={
          <button
            type="button"
            aria-label={`Run history of #${issue.number} (${count} ${count === 1 ? 'run' : 'runs'}${incomplete ? ', incomplete' : ''})`}
            data-action="runs"
            className="absolute inset-0 cursor-pointer rounded-md outline-none focus-visible:outline-3 focus-visible:outline-offset-2 focus-visible:outline-ring focus-visible:outline-solid"
          />
        }
      />
    </div>
  )
}

// RunLegend names the colors of the run strip once per list.
export function RunLegend({ className }: { className?: string }) {
  return (
    <ul aria-label="Run colors" data-slot="run-legend" className={cn('m-0 flex list-none flex-wrap items-center gap-x-3 gap-y-1 p-0 text-xs text-muted-foreground', className)}>
      {runBuckets.map((b) => (
        <li key={b} className="inline-flex items-center gap-1.5">
          <span aria-hidden="true" className="flex h-3 items-end">
            <span className={cn('block w-1.5 rounded-[1px]', barClass[b])} />
          </span>
          {runBucketLabels[b]}
        </li>
      ))}
    </ul>
  )
}
