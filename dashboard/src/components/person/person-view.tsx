import { useState } from 'react'

import { IssueBulkActions, type SelectedIssue } from '@/components/issue'
import { Note } from '@/components/shell/note'
import { Button } from '@/components/ui/button'
import { SectionLoading } from '@/components/shell/section-loading'
import { accessOf, type Change } from '@/lib/issue'
import { useLoading } from '@/lib/loading-context'
import { filterNeedsYou, keepSelection } from '@/lib/person'
import type { PersonView as PersonData } from '@/lib/status'

import { RepoGroup } from './repo-group'
import { SummaryTiles } from './summary-tiles'

export interface PersonViewProps {
  data: PersonData
  onChanged: (changes: Change[]) => void
  // onShowRunners opens the Runner view from the Runners tile.
  onShowRunners?: () => void
}

// PersonView lists the open Issues with scope or request assigned to the
// current gh account, grouped by repository in the order of the data, under
// summary tiles. The Needs you tile limits the list to the Issues that wait on
// a person (needsYou); the tiles always count the whole read.
export function PersonView({ data, onChanged, onShowRunners }: PersonViewProps) {
  const loading = useLoading()
  const listing = loading.has('repos')
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const [needsYouOnly, setNeedsYouOnly] = useState(false)
  const shown = needsYouOnly ? filterNeedsYou(data) : data
  // A new read, or turning the filter on, drops the selected Issues the
  // list no longer shows, so bulk actions never act on hidden Issues.
  const [seen, setSeen] = useState({ data, needsYouOnly })
  if (seen.data !== data || seen.needsYouOnly !== needsYouOnly) {
    setSeen({ data, needsYouOnly })
    setSelected((prev) => keepSelection(prev, shown))
  }

  const repos = shown.repos ?? []
  const shownCount = repos.reduce((n, r) => n + (r.issues?.length ?? 0), 0)
  const chosen: SelectedIssue[] = repos.flatMap((r) =>
    (r.issues ?? [])
      .filter((i) => selected.has(i.url))
      .map((issue) => ({
        issue,
        repo: r.repo,
        access: accessOf(r, loading),
      })),
  )
  // The run legend shows once, above the first run strip: in the first group
  // that lists Issues, once the runs are read.
  const legendAt = loading.has('runs') ? -1 : repos.findIndex((r) => (r.issues?.length ?? 0) > 0)
  const onSelect = (url: string, on: boolean) =>
    setSelected((prev) => {
      const next = new Set(prev)
      if (on) next.add(url)
      else next.delete(url)
      return next
    })

  return (
    <section aria-labelledby="person-heading" className="flex flex-col gap-4" data-view="person">
      <div className="flex flex-col gap-1">
        <h2 id="person-heading" className="text-lg font-semibold tracking-tight">
          Your open Issues
        </h2>
        {loading.has('user') ? (
          <SectionLoading label="Reading your account…" data-loading="user" />
        ) : data.userError ? (
          <Note tone="danger">
            Account could not be read: {data.userError.code}: {data.userError.message}
          </Note>
        ) : (
          <p className="text-sm break-words text-muted-foreground">
            Open Issues with scope or request assigned to <span className="font-medium text-foreground">{data.user}</span> on{' '}
            <span className="font-medium text-foreground">{data.githubHost}</span>
          </p>
        )}
      </div>
      <SummaryTiles data={data} needsYouOnly={needsYouOnly} onNeedsYouOnly={setNeedsYouOnly} onShowRunners={onShowRunners} />
      {data.issuesError && (
        <Note tone="danger" data-note="issues-error">
          Issue search failed: {data.issuesError.code}: {data.issuesError.message}
        </Note>
      )}
      {data.truncated && (
        <Note tone="warning" data-note="truncated">
          GitHub's search returns at most 1,000 Issues; some may be missing.
        </Note>
      )}
      {listing && <SectionLoading label="Reading your Issues…" data-loading="repos" />}
      {needsYouOnly && !listing && (
        <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border-subtle bg-muted px-3 py-2 text-sm" data-slot="filter">
          <p role="status" className="min-w-0 break-words">
            Showing {shownCount} {shownCount === 1 ? 'Issue that needs' : 'Issues that need'} you
          </p>
          <Button variant="outline" size="sm" onClick={() => setNeedsYouOnly(false)} data-action="clear-filter">
            Clear
          </Button>
        </div>
      )}
      {needsYouOnly && !listing && repos.length === 0 && !data.issuesError && (
        <p className="rounded-md border border-dashed border-border-subtle px-4 py-8 text-center text-sm text-muted-foreground" data-empty="needs-you">
          No listed Issue waits on you.
        </p>
      )}
      {!needsYouOnly && !listing && repos.length === 0 && !data.issuesError && (
        <p className="rounded-md border border-dashed border-border-subtle px-4 py-8 text-center text-sm text-muted-foreground" data-empty="issues">
          No open Issues with scope or request are assigned to you.
        </p>
      )}
      {chosen.length > 0 && <IssueBulkActions selected={chosen} onChanged={onChanged} className="sticky top-2 z-10 shadow-sm" />}
      {repos.map((r, i) => (
        <RepoGroup
          key={r.repo}
          legend={i === legendAt}
          group={r}
          githubHost={data.githubHost}
          latestImage={data.latestImage}
          selected={selected}
          onSelect={onSelect}
          onChanged={onChanged}
        />
      ))}
    </section>
  )
}
