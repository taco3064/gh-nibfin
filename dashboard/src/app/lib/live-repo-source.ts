// The live data as the source of the shared repository view: the three
// repositories of the Worker, read-only. The site holds no other read.

import { useMemo, useState } from 'react'

import { complete } from '@/lib/partial'
import type { PickerSource } from '@/lib/repo'
import type { RepoSource, RepoViewData } from '@/lib/repo-source'
import { ownerRepo, repoHost } from '@/lib/status'

import type { LiveData } from '@/worker/messages'

export interface LiveRepoView {
  // data is the chosen repository, or the first one.
  data?: RepoViewData
  // source lists every repository of the live data for the picker.
  source?: PickerSource
}

// liveRepoSource picks the repository chosen (HOST/OWNER/REPO, compared
// without case), or else the first, and lists all of them for the picker.
// The Worker reads only public repositories.
export function liveRepoSource(data: LiveData | undefined, chosen?: string): LiveRepoView {
  if (!data) return {}
  const want = chosen?.toLowerCase()
  const r = data.repos.find((x) => x.repo.toLowerCase() === want) ?? data.repos[0]
  const host = data.repos.length > 0 ? repoHost(data.repos[0].repo) : 'github.com'
  const source: PickerSource = {
    host,
    repos: data.repos.map((x) => ({
      repo: ownerRepo(x.repo),
      url: `https://${repoHost(x.repo) || host}/${ownerRepo(x.repo)}`,
      visibility: 'PUBLIC',
      permission: null,
      execution: null,
    })),
  }
  if (!r) return { source }
  const view: RepoViewData = { repo: r.repo, issues: r.issues, ...(r.issuesError ? { issuesError: r.issuesError } : {}) }
  return { data: view, source }
}

const noop = () => {}

// useLiveRepoSource keeps the chosen repository in memory only. refresh and
// onChanged do nothing: the read-only view has no control that calls them,
// and new data arrives with each snapshot.
export function useLiveRepoSource(data: LiveData | undefined, checkedAt?: string): { status: RepoSource; source?: PickerSource } {
  const [chosen, setChosen] = useState<string>()
  const view = useMemo(() => liveRepoSource(data, chosen), [data, chosen])
  const readAt = checkedAt === undefined ? undefined : Date.parse(checkedAt)
  return {
    status: { repo: view.data?.repo, data: view.data, loading: complete, readAt, reading: false, open: setChosen, refresh: noop, onChanged: noop },
    source: view.source,
  }
}
