import { useEffect, useState } from 'react'

import { apiGet, type ApiError, type Outcome } from '@/lib/session'
import type { Activity } from '@/lib/status'

export type ActivityState =
  | { state: 'loading'; activity?: Activity }
  | { state: 'loaded'; activity: Activity }
  | { state: 'failed'; error: ApiError }

// useActivity reads the Runner activity when the view mounts and again
// whenever read changes: the view passes its person-view data, so a newer
// status read reads the activity again. It only reads and never drives
// anything; a newer read wins over an older one still in flight. While a
// newer read runs, the last activity stays shown.
export function useActivity(read: unknown): ActivityState {
  const [got, setGot] = useState<{ read: unknown; outcome: Outcome<Activity> } | null>(null)
  useEffect(() => {
    let live = true
    void apiGet<Activity>('/api/runner-activity').then((outcome) => {
      if (live) setGot({ read, outcome })
    })
    return () => {
      live = false
    }
  }, [read])
  if (!got) return { state: 'loading' }
  if (!got.outcome.ok) return { state: 'failed', error: got.outcome.error }
  return got.read === read ? { state: 'loaded', activity: got.outcome.value } : { state: 'loading', activity: got.outcome.value }
}

// useNow is the time in ms, renewed every step ms, for elapsed-time labels.
// It only re-renders; it reads nothing.
export function useNow(step = 60_000): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), step)
    return () => clearInterval(id)
  }, [step])
  return now
}
