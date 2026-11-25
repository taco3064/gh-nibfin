import { useEffect, useState } from 'react'

import { cachedCommands } from '@/lib/commands'
import type { Command } from '@/lib/operation'
import type { ApiError } from '@/lib/session'

export type CommandsState = { state: 'loading' } | { state: 'loaded'; commands: Command[] } | { state: 'failed'; error: ApiError }

// useCommands reads the command schema, once per page.
export function useCommands(): CommandsState {
  const [s, setS] = useState<CommandsState>({ state: 'loading' })
  useEffect(() => {
    let live = true
    void cachedCommands().then((r) => {
      if (!live) return
      setS(r.ok ? { state: 'loaded', commands: r.value } : { state: 'failed', error: r.error })
    })
    return () => {
      live = false
    }
  }, [])
  return s
}
