import { useState } from 'react'

import { IssueBulkActions, type SelectedIssue } from '@/components/issue'
import { Note } from '@/components/shell/note'
import { SectionLoading } from '@/components/shell/section-loading'
import { accessOf, type Change } from '@/lib/issue'
import { useLoading } from '@/lib/loading-context'
import { keepSelection } from '@/lib/person'
import type { PersonView as PersonData } from '@/lib/status'

import { RepoGroup } from './repo-group'

export interface PersonViewProps {
  data: PersonData
  onChanged: (changes: Change[]) => void
}

// PersonView lists the open Issues with scope or request assigned to the
// current gh account, grouped by repository in the order of the data.
export function PersonView({ data, onChanged }: PersonViewProps) {
  const loading = useLoading()
  const listing = loading.has('repos')
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  // A new read drops the selected Issues it no longer lists.
  const [seen, setSeen] = useState(data)
  if (seen !== data) {
    setSeen(data)
    setSelected((prev) => keepSelection(prev, data))
  }

  const repos = data.repos ?? []
  const chosen: SelectedIssue[] = repos.flatMap((r) =>
    (r.issues ?? [])
      .filter((i) => selected.has(i.url))
      .map((issue) => ({
        issue,
        repo: r.repo,
        access: accessOf(r, loading),
      })),
  )
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
      {!listing && repos.length === 0 && !data.issuesError && (
        <p className="rounded-md border border-dashed border-border-subtle px-4 py-8 text-center text-sm text-muted-foreground" data-empty="issues">
          No open Issues with scope or request are assigned to you.
        </p>
      )}
      {chosen.length > 0 && (
        <IssueBulkActions selected={chosen} onChanged={onChanged} className="sticky top-2 z-10 shadow-sm" />
      )}
      {repos.map((r) => (
        <RepoGroup
          key={r.repo}
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
