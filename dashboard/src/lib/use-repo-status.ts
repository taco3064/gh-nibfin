import { useCallback, useEffect, useRef, useState } from 'react'

import { applyChanges, type Change } from '@/lib/issue'
import { complete, dropPartial, fillRepoView, takePartial, type Shown } from '@/lib/partial'
import { Sequence } from '@/lib/repo'
import type { RepoSource } from '@/lib/repo-source'
import { readStatus, type ApiError } from '@/lib/session'
import type { RepoView } from '@/lib/status'

// RepoStatus is the Dashboard's source of the repository view. data is the
// last read of repo, or the part of the first read done so far; a failed
// refresh keeps a complete read. readAt is when the request that read data
// was sent, unset while data is partial.
export interface RepoStatus extends RepoSource {
  data?: RepoView
}

// useRepoStatus reads the repository view of status for the chosen
// repository through the status read route, once on open() and again on
// refresh(), showing each section as it arrives until the first read is
// complete. An answer for a repository that is no longer the chosen one is
// dropped and its request cancelled. Nothing is stored.
export function useRepoStatus(): RepoStatus {
  const [repo, setRepo] = useState<string>()
  const [shown, setShown] = useState<Shown<RepoView>>({ loading: complete })
  const [readAt, setReadAt] = useState<number>()
  const [reading, setReading] = useState(false)
  const [readError, setReadError] = useState<ApiError>()
  const seq = useRef(new Sequence())
  const current = useRef<string | undefined>(undefined)
  const request = useRef<AbortController | null>(null)

  const read = useCallback((target: string) => {
    const n = seq.current.next()
    request.current?.abort()
    const ctl = new AbortController()
    request.current = ctl
    setReading(true)
    const sent = Date.now()
    const onPartial = (result: unknown, loading: string[]) => {
      if (!seq.current.current(n)) return
      setShown((s) => takePartial(s, fillRepoView(result, target), loading))
    }
    void readStatus<RepoView>(target, onPartial, fetch, ctl.signal).then((r) => {
      if (!seq.current.current(n)) return
      request.current = null
      setReading(false)
      if (!r.ok) {
        setReadError(r.error)
        setShown(dropPartial)
      } else if (r.value === undefined || r.value === null) {
        setReadError({ code: 'error', message: 'the status read returned no result' })
        setShown(dropPartial)
      } else {
        setShown({ data: r.value, loading: complete })
        setReadAt(sent)
        setReadError(undefined)
      }
    })
  }, [])

  const open = useCallback(
    (target: string) => {
      current.current = target
      setRepo(target)
      setShown({ loading: complete })
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
    return () => {
      s.drop()
      request.current?.abort()
    }
  }, [])

  const onChanged = useCallback(
    (changes: Change[]) => {
      if (changes.length === 0) refresh()
      else setShown((s) => (s.data?.issues ? { ...s, data: { ...s.data, issues: applyChanges(s.data.issues, changes) } } : s))
    },
    [refresh],
  )

  return { repo, data: shown.data, loading: shown.loading, readAt, reading, readError, open, refresh, onChanged }
}
