import { useState } from 'react'

import { changeIssues, type Action, type Change, type ChangeOutcome } from '@/lib/issue'

export interface ChangeState {
  running: Action | null
  outcome: ChangeOutcome | null
  run: (action: Action, urls: readonly string[]) => Promise<void>
}

// useChange runs one pause or resume at a time and keeps its last outcome.
// Confirmed changes go to onChanged; an unknown outcome sends an empty list
// so the placing view can read the status again.
export function useChange(onChanged: (changes: Change[]) => void): ChangeState {
  const [running, setRunning] = useState<Action | null>(null)
  const [outcome, setOutcome] = useState<ChangeOutcome | null>(null)
  const run = async (action: Action, urls: readonly string[]) => {
    setRunning(action)
    setOutcome(null)
    const o = await changeIssues(action, urls)
    setRunning(null)
    setOutcome(o)
    if (o.state === 'done') onChanged(o.changes)
    else if (o.state === 'unknown') onChanged([])
  }
  return { running, outcome, run }
}
