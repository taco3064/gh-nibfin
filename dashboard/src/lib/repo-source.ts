// The data source of the repository view and its read-only switch. The
// Dashboard supplies its status read (useRepoStatus); the site supplies the
// public live data of the Worker, which has no write and reads only a
// repository's Issues.

import { createContext, useContext } from 'react'

import type { Change } from '@/lib/issue'
import type { Loading } from '@/lib/partial'
import type { ApiError } from '@/lib/session'
import type { RepoView } from '@/lib/status'

// RepoViewData is what the repository view needs of a repository: repo and
// issues always; the Dashboard's read adds image, nibfinRun, permission and
// the rest, which the view shows only when present.
export type RepoViewData = Pick<RepoView, 'repo' | 'issues'> & Partial<Omit<RepoView, 'repo' | 'issues'>>

export interface RepoSource {
  // repo is the value sent for the open repository, as entered or picked.
  repo?: string
  data?: RepoViewData
  // loading names what of data is still being read; empty once complete.
  loading: Loading
  // readAt is when the shown data was read, in ms.
  readAt?: number
  reading: boolean
  readError?: ApiError
  open: (repo: string) => void
  refresh: () => void
  // onChanged applies confirmed changes; an empty list (unknown result)
  // reads the repository again.
  onChanged: (changes: Change[]) => void
}

// ReadOnlyContext is true where the view offers no operation: no pause,
// resume, init, upgrade, selection or manual entry. Run-history dialogs
// and GitHub links stay.
export const ReadOnlyContext = createContext(false)

export function useReadOnly(): boolean {
  return useContext(ReadOnlyContext)
}
