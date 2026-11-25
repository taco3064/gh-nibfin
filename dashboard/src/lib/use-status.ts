import { useCallback, useEffect, useRef, useState } from 'react'

import type { Change } from '@/lib/issue'
import { openLive, type LiveEvent } from '@/lib/live'
import { applyPersonChanges, replaceIssue } from '@/lib/person'
import { complete, dropPartial, fillPersonView, takePartial, type Loading, type Shown } from '@/lib/partial'
import { openSession, readProof, readStatus, type ApiError } from '@/lib/session'
import type { PersonView } from '@/lib/status'

export type Connection =
  | { state: 'no-proof' }
  | { state: 'connecting' }
  | { state: 'connected' }
  | { state: 'failed'; error: ApiError }

// Live is the live stream: connecting until its first snapshot, on while it
// runs, off with the reason once it ended.
export type Live = { state: 'connecting' } | { state: 'on' } | { state: 'off'; error: ApiError }

// Podman is whether the server follows this host's Job events.
export interface Podman {
  watching: boolean
  error?: ApiError
}

export interface Status {
  connection: Connection
  // data is the last person view read, or the part of the first read done
  // so far; a failed refresh keeps a complete read.
  data?: PersonView
  // loading names what of data is still being read; empty once complete.
  loading: Loading
  // readAt is the time in ms the data is known current at: when its read
  // began, or the last GitHub check that found nothing new.
  readAt?: number
  reading: boolean
  readError?: ApiError
  live: Live
  podman?: Podman
  // checkError is the last failed GitHub check, until a check succeeds.
  checkError?: ApiError
  // liveReadError is the last failed live re-read, until the next one.
  liveReadError?: ApiError
  // refresh reconnects an ended stream, whose snapshot is the refresh, or
  // reads the status once.
  refresh: () => void
  // onChanged applies confirmed changes; an empty list (unknown result)
  // reads the status again.
  onChanged: (changes: Change[]) => void
}

// The proof is read once per page load, before the first render.
const proof = readProof(location.hash, history)

const time = (at: string) => {
  const t = Date.parse(at)
  return Number.isNaN(t) ? Date.now() : t
}

// useStatus opens the session, then the live stream, which keeps the data
// current. When the stream ends before its first snapshot, the status is
// read once instead. Until the first read is complete, each section shows as
// it arrives. Nothing reconnects by itself; Refresh does.
export function useStatus(): Status {
  const [connection, setConnection] = useState<Connection>(proof ? { state: 'connecting' } : { state: 'no-proof' })
  const [shown, setShown] = useState<Shown<PersonView>>({ loading: complete })
  const setData = useCallback((f: (d: PersonView) => PersonView) => setShown((s) => (s.data ? { ...s, data: f(s.data) } : s)), [])
  const [readAt, setReadAt] = useState<number>()
  const [reading, setReading] = useState(false)
  const [readError, setReadError] = useState<ApiError>()
  const [live, setLive] = useState<Live>({ state: 'connecting' })
  const [podman, setPodman] = useState<Podman>()
  const [checkError, setCheckError] = useState<ApiError>()
  const [liveReadError, setLiveReadError] = useState<ApiError>()
  const busy = useRef(false)
  const mounted = useRef(true)
  const liveOff = useRef(false)
  const stream = useRef<AbortController | null>(null)

  const read = useCallback(() => {
    if (busy.current) return
    busy.current = true
    setReading(true)
    const sent = Date.now()
    const onPartial = (result: unknown, loading: string[]) => {
      if (mounted.current) setShown((s) => takePartial(s, fillPersonView(result), loading))
    }
    void readStatus<PersonView>('', onPartial).then((r) => {
      busy.current = false
      if (!mounted.current) return
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

  const connect = useCallback(() => {
    stream.current?.abort()
    const ctl = new AbortController()
    stream.current = ctl
    liveOff.current = false
    let snapshot = false
    setLive({ state: 'connecting' })
    setReading(true)
    const onEvent = (e: LiveEvent) => {
      if (ctl.signal.aborted || !mounted.current) return
      switch (e.event) {
        case 'partial':
          if (!snapshot) setShown((s) => takePartial(s, fillPersonView(e.result), e.loading))
          break
        case 'snapshot':
          if (!snapshot) {
            snapshot = true
            setLive({ state: 'on' })
            setReading(false)
          }
          setShown({ data: e.result, loading: complete })
          setReadAt(time(e.at))
          setReadError(undefined)
          setLiveReadError(undefined)
          break
        case 'issue':
          setData((d) => replaceIssue(d, e.repo, e.issue))
          setLiveReadError(undefined)
          break
        case 'checked':
          setReadAt((t) => Math.max(t ?? 0, time(e.at)))
          setCheckError(undefined)
          break
        case 'podman':
          setPodman({ watching: e.watching, ...(e.error ? { error: e.error } : {}) })
          break
        case 'error':
          if (e.source === 'github') setCheckError(e.error)
          else setLiveReadError(e.error)
          break
      }
    }
    void openLive(onEvent, ctl.signal).then((end) => {
      if (end.reason === 'aborted' || stream.current !== ctl || !mounted.current) return
      stream.current = null
      liveOff.current = true
      setLive({ state: 'off', error: end.error })
      setLiveReadError(undefined)
      setCheckError(undefined)
      setPodman(undefined)
      setReading(false)
      // The page works as without the stream: one read.
      if (!snapshot) read()
    })
  }, [read, setData])

  useEffect(() => {
    mounted.current = true
    if (!proof) return
    let active = true
    void openSession(proof).then((opened) => {
      if (!active) return
      if (!opened.ok) {
        setConnection({ state: 'failed', error: opened.error })
        return
      }
      setConnection({ state: 'connected' })
      connect()
    })
    return () => {
      active = false
      mounted.current = false
      stream.current?.abort()
      stream.current = null
    }
  }, [connect])

  const refresh = useCallback(() => {
    if (liveOff.current) connect()
    else read()
  }, [connect, read])

  const onChanged = useCallback(
    (changes: Change[]) => {
      if (changes.length === 0) refresh()
      else setData((d) => applyPersonChanges(d, changes))
    },
    [refresh, setData],
  )

  return { connection, data: shown.data, loading: shown.loading, readAt, reading, readError, live, podman, checkError, liveReadError, refresh, onChanged }
}
