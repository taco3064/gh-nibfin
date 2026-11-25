// The argument schema of every runnable command, read from GET /api/commands
// (#347). Openers find a command's entry by its api name.

import type { Command } from '@/lib/operation'
import { apiGet, type Outcome } from '@/lib/session'

type Fetch = typeof fetch

export async function loadCommands(fetcher: Fetch = fetch): Promise<Outcome<Command[]>> {
  const r = await apiGet<{ commands?: Command[] | null }>('/api/commands', fetcher)
  if (!r.ok) return r
  return { ok: true, value: r.value.commands ?? [] }
}

// commandByApi returns the command with the plan route name api, if any.
export function commandByApi(commands: readonly Command[], api: string): Command | undefined {
  return commands.find((c) => c.api === api)
}

let cached: Promise<Outcome<Command[]>> | null = null

// cachedCommands loads the schema once per page; a failed load is read
// again on the next call.
export function cachedCommands(fetcher: Fetch = fetch): Promise<Outcome<Command[]>> {
  cached ??= loadCommands(fetcher).then((r) => {
    if (!r.ok) cached = null
    return r
  })
  return cached
}

// resetCommands forgets the cached schema; for tests only.
export function resetCommands(): void {
  cached = null
}
