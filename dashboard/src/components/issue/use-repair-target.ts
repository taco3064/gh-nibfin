import { useEffect, useState } from 'react'

import { cachedCommands, commandByApi } from '@/lib/commands'
import { recommendedOption } from '@/lib/issue'
import { fieldsOf, type Command } from '@/lib/operation'
import type { ApiError } from '@/lib/session'
import type { Repair } from '@/lib/status'

// The resume command's request fields the Repair dialog fills.
export const issuesKey = 'issues'
export const optionKey = 'option'

// RepairTarget is what the Repair button opens: for a choose record the
// resume command that answers options and the option to preselect, or why
// it cannot be answered here.
export type RepairTarget =
  | { ready: true; command?: Command; recommended?: number }
  | { ready: false; reason: string }

type Load = { state: 'loading' } | { state: 'loaded'; commands: Command[] } | { state: 'failed'; error: ApiError }

// useRepairTarget reads the command schema, once per page, only for an
// answerable choose record; the advice of an act record needs no command.
export function useRepairTarget(repair: Repair | undefined, enabled: boolean): RepairTarget {
  const choose = enabled && repair?.state === 'choose'
  const [load, setLoad] = useState<Load>({ state: 'loading' })
  useEffect(() => {
    if (!choose) return
    let live = true
    void cachedCommands().then((r) => {
      if (live) setLoad(r.ok ? { state: 'loaded', commands: r.value } : { state: 'failed', error: r.error })
    })
    return () => {
      live = false
    }
  }, [choose])
  if (!choose || !repair) return { ready: true }
  const recommended = recommendedOption(repair)
  if (recommended === undefined) return { ready: false, reason: 'The repair record lists no options.' }
  if (load.state === 'loading') return { ready: false, reason: 'Reading the commands…' }
  if (load.state === 'failed') return { ready: false, reason: `Commands could not be loaded: ${load.error.code}: ${load.error.message}` }
  const command = commandByApi(load.commands, 'resume')
  if (!command) return { ready: false, reason: 'The server offers no resume command.' }
  const keys = fieldsOf(command).map((f) => f.key)
  if (!keys.includes(optionKey) || !keys.includes(issuesKey)) return { ready: false, reason: 'The server’s resume command takes no option.' }
  return { ready: true, command, recommended }
}
