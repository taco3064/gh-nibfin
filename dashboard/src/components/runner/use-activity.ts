import { useEffect, useState } from 'react'

import { cyclesView, rollCycle, type ActivityCycle, type ActivityCycles, type ActivityView } from '@/lib/runner'
import { apiGet, type Outcome } from '@/lib/session'
import type { Activity } from '@/lib/status'

export type { ActivityView, TimelineState } from '@/lib/runner'

// useActivity reads the Runner activity when the view mounts and again
// whenever read changes: the view passes its person-view data, so a newer
// status read reads the activity again. Each cycle asks for the Runners'
// current Jobs first and the full read with the 24-hour history next; the
// server answers the current part without waiting for the history and
// joins a read already in flight. It only reads and never drives anything;
// answers of an older cycle still in flight are dropped. Until the newer
// cycle answers, the last answers stay shown (see activityView).
export function useActivity(read: unknown): ActivityView {
  const [got, setGot] = useState<ActivityCycles | null>(null)
  useEffect(() => {
    let live = true
    const take = (part: keyof ActivityCycle) => (outcome: Outcome<Activity>) => {
      if (live) setGot((g) => rollCycle(g, read, part, outcome))
    }
    void apiGet<Activity>('/api/runner-activity?part=current').then(take('current'))
    void apiGet<Activity>('/api/runner-activity').then(take('history'))
    return () => {
      live = false
    }
  }, [read])
  return cyclesView(got, read)
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
