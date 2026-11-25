import { useCallback, useEffect, useRef, useState } from 'react'

import type { Change } from '@/lib/issue'
import { applyPersonChanges } from '@/lib/person'
import { api, openSession, readProof, type ApiError } from '@/lib/session'
import type { PersonView } from '@/lib/status'

export type Connection =
  | { state: 'no-proof' }
  | { state: 'connecting' }
  | { state: 'connected' }
  | { state: 'failed'; error: ApiError }

export interface Status {
  connection: Connection
  // data is the last person view read; a failed refresh keeps it.
  data?: PersonView
  // readAt is when the request that read data was sent, in ms.
  readAt?: number
  reading: boolean
  readError?: ApiError
  refresh: () => void
  // onChanged applies confirmed changes; an empty list (unknown result)
  // reads the status again.
  onChanged: (changes: Change[]) => void
}

// The proof is read once per page load, before the first render.
const proof = readProof(location.hash, history)

// useStatus opens the session, then reads the person-view status once. It
// reads again only on refresh() or after an unknown change result.
export function useStatus(): Status {
  const [connection, setConnection] = useState<Connection>(proof ? { state: 'connecting' } : { state: 'no-proof' })
  const [data, setData] = useState<PersonView>()
  const [readAt, setReadAt] = useState<number>()
  const [reading, setReading] = useState(false)
  const [readError, setReadError] = useState<ApiError>()
  const busy = useRef(false)
  const mounted = useRef(true)

  const refresh = useCallback(() => {
    if (busy.current) return
    busy.current = true
    setReading(true)
    const sent = Date.now()
    void api<{ result?: PersonView }>('/api/commands/status/plan', {}).then((r) => {
      busy.current = false
      if (!mounted.current) return
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

  useEffect(() => {
    mounted.current = true
    if (!proof) return
    let live = true
    void openSession(proof).then((opened) => {
      if (!live) return
      if (!opened.ok) {
        setConnection({ state: 'failed', error: opened.error })
        return
      }
      setConnection({ state: 'connected' })
      refresh()
    })
    return () => {
      live = false
      mounted.current = false
    }
  }, [refresh])

  const onChanged = useCallback(
    (changes: Change[]) => {
      if (changes.length === 0) refresh()
      else setData((d) => (d ? applyPersonChanges(d, changes) : d))
    },
    [refresh],
  )

  return { connection, data, readAt, reading, readError, refresh, onChanged }
}
