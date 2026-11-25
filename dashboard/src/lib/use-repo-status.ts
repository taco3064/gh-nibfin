import { useCallback, useEffect, useRef, useState } from 'react'

import { applyChanges, type Change } from '@/lib/issue'
import { Sequence } from '@/lib/repo'
import { api, type ApiError } from '@/lib/session'
import type { RepoView } from '@/lib/status'

export interface RepoStatus {
  // repo is the value sent for the open repository, as entered or picked.
  repo?: string
  // data is the last read of repo; a failed refresh keeps it.
  data?: RepoView
  // readAt is when the request that read data was sent, in ms.
  readAt?: number
  reading: boolean
  readError?: ApiError
  open: (repo: string) => void
  refresh: () => void
  // onChanged applies confirmed changes; an empty list (unknown result)
  // reads the repository again.
  onChanged: (changes: Change[]) => void
}

// useRepoStatus reads the repository view of status for the chosen
// repository, once on open() and again on refresh(). An answer for a
// repository that is no longer the chosen one is dropped. Nothing is stored.
export function useRepoStatus(): RepoStatus {
  const [repo, setRepo] = useState<string>()
  const [data, setData] = useState<RepoView>()
  const [readAt, setReadAt] = useState<number>()
  const [reading, setReading] = useState(false)
  const [readError, setReadError] = useState<ApiError>()
  const seq = useRef(new Sequence())
  const current = useRef<string | undefined>(undefined)

  const read = useCallback((target: string) => {
    const n = seq.current.next()
    setReading(true)
    const sent = Date.now()
    void api<{ result?: RepoView }>('/api/commands/status/plan', { repo: target }).then((r) => {
      if (!seq.current.current(n)) return
      setReading(false)
      if (!r.ok) setReadError(r.error)
      else if (r.value.result === undefined) setReadError({ code: 'error', message: 'the status read returned no result' })
      else {
        setData(r.value.result)
        setReadAt(sent)
        setReadError(undefined)
      }
    })
  }, [])

  const open = useCallback(
    (target: string) => {
      current.current = target
      setRepo(target)
      setData(undefined)
      setReadAt(undefined)
      setReadError(undefined)
      read(target)
    },
    [read],
  )

  const refresh = useCallback(() => {
    if (current.current !== undefined) read(current.current)
  }, [read])

  useEffect(() => {
    const s = seq.current
    return () => s.drop()
  }, [])

  const onChanged = useCallback(
    (changes: Change[]) => {
      if (changes.length === 0) refresh()
      else setData((d) => (d?.issues ? { ...d, issues: applyChanges(d.issues, changes) } : d))
    },
    [refresh],
  )

  return { repo, data, readAt, reading, readError, open, refresh, onChanged }
}
