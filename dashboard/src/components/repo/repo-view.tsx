import { LoaderCircle } from 'lucide-react'

import { Note } from '@/components/shell/note'
import type { PickerSource } from '@/lib/repo'
import type { RepoStatus } from '@/lib/use-repo-status'

import { DependencyGraph } from './dependency-graph'
import { RepoPicker } from './repo-picker'
import { RepoSummary } from './repo-summary'

export interface RepoViewProps {
  status: RepoStatus
  // source is the picker list with the host of the read that supplied it.
  source?: PickerSource
  listReading: boolean
}

// RepoView is the repository view: the picker, then the open repository's
// summary and its dependency graph.
export function RepoView({ status, source, listReading }: RepoViewProps) {
  const { data, readError, reading } = status
  const current = data?.repo ?? status.repo
  return (
    <section aria-label="Repository" className="flex flex-col gap-4" data-view="repo">
      <RepoPicker source={source} listReading={listReading} current={current} onOpen={status.open} />
      {readError && (
        <Note tone="danger" data-note="repo-read-error">
          {data ? 'Refresh failed' : `${status.repo} could not be read`}: {readError.code}: {readError.message}
        </Note>
      )}
      {!data && reading && (
        <p role="status" data-state="repo-reading" className="flex items-center gap-2 text-sm text-muted-foreground">
          <LoaderCircle aria-hidden="true" className="size-4 motion-safe:animate-spin" />
          Reading {status.repo}…
        </p>
      )}
      {data && (
        <>
          <RepoSummary data={data} onFinished={status.refresh} />
          <DependencyGraph data={data} onChanged={status.onChanged} />
        </>
      )}
    </section>
  )
}
