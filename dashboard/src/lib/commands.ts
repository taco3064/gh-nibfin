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
