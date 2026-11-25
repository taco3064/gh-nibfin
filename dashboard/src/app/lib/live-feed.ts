// The site's read of the public live data Worker: one WebSocket per island
// that sends nothing, keeps the last snapshot through a dropped connection
// and reconnects by itself.

import { useEffect, useState } from 'react'

import type { ApiError, LiveData, LiveEvent } from '../../worker/messages'

export type Connection = 'connecting' | 'open' | 'closed'

export interface LiveFeedState {
  connection: Connection
  // data is the last snapshot; it stays through errors and closes.
  data?: LiveData
  // changedAt is the time of the last snapshot, checkedAt of the last good
  // GitHub check.
  changedAt?: string
  checkedAt?: string
  // errors are the GitHub check errors since the last good check, newest
  // first.
  errors: ApiError[]
  closeReason?: string
}

export const initialFeed: LiveFeedState = { connection: 'connecting', errors: [] }

function isObject(v: unknown): v is Record<string, unknown> {
  return v !== null && typeof v === 'object'
}

function isLiveData(v: unknown): v is LiveData {
  return isObject(v) && Array.isArray(v.repos) && isObject(v.summary) && Array.isArray(v.runners)
}

function isError(v: unknown): v is ApiError {
  return isObject(v) && typeof v.code === 'string' && typeof v.message === 'string'
}

// parseEvent reads one text message as a LiveEvent, or undefined for
// malformed JSON or an unknown event.
export function parseEvent(message: string): LiveEvent | undefined {
  let v: unknown
  try {
    v = JSON.parse(message)
  } catch {
    return undefined
  }
  if (!isObject(v)) return undefined
  if (v.event === 'snapshot' && typeof v.at === 'string' && isLiveData(v.result)) return { event: 'snapshot', at: v.at, result: v.result }
  if (v.event === 'checked' && typeof v.at === 'string') return { event: 'checked', at: v.at }
  if (v.event === 'error' && isError(v.error)) return { event: 'error', source: 'github', error: { code: v.error.code, message: v.error.message } }
  return undefined
}

// reduceLive applies one message: a snapshot replaces the data, checked
// confirms it and clears the errors, an error is added and keeps the data.
// Other messages change nothing.
export function reduceLive(state: LiveFeedState, message: string): LiveFeedState {
  const e = parseEvent(message)
  if (!e) return state
  switch (e.event) {
    case 'snapshot':
      return { ...state, data: e.result, changedAt: e.at }
    case 'checked':
      return { ...state, checkedAt: e.at, errors: [] }
    case 'error': {
      const rest = state.errors.filter((x) => x.code !== e.error.code || x.message !== e.error.message)
      return { ...state, errors: [e.error, ...rest] }
    }
  }
}

// reconnectDelay is the wait before reconnect attempt n (from 0): 1 s,
// doubling, at most 30 s.
export function reconnectDelay(attempt: number): number {
  return Math.min(30_000, 1000 * 2 ** Math.max(0, Math.min(attempt, 5)))
}

// useLiveFeed opens one WebSocket to url. A closed socket keeps the data,
// and is opened again after reconnectDelay, or at once when the browser
// comes back online. Unmounting closes it and cancels the retry.
export function useLiveFeed(url: string): LiveFeedState {
  const [state, setState] = useState<LiveFeedState>(initialFeed)
  useEffect(() => {
    let socket: WebSocket | null = null
    let timer: ReturnType<typeof setTimeout> | undefined
    let attempt = 0
    let stopped = false

    const connect = () => {
      clearTimeout(timer)
      timer = undefined
      if (stopped) return
      setState((s) => ({ ...s, connection: 'connecting' }))
      const ws = new WebSocket(url)
      socket = ws
      ws.onopen = () => {
        attempt = 0
        setState((s) => ({ ...s, connection: 'open', closeReason: undefined }))
      }
      ws.onmessage = (m: MessageEvent) => {
        if (typeof m.data === 'string') setState((s) => reduceLive(s, m.data as string))
      }
      ws.onclose = (c: CloseEvent) => {
        if (socket !== ws || stopped) return
        socket = null
        setState((s) => ({ ...s, connection: 'closed', closeReason: c.reason || undefined }))
        timer = setTimeout(connect, reconnectDelay(attempt++))
      }
    }
    const online = () => {
      if (socket === null) connect()
    }

    window.addEventListener('online', online)
    connect()
    return () => {
      stopped = true
      clearTimeout(timer)
      window.removeEventListener('online', online)
      socket?.close()
      socket = null
    }
  }, [url])
  return state
}
