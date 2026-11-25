import { useEffect, useState } from 'react'
import { CircleAlert, CircleCheck, Info, LoaderCircle, Monitor, Moon, Sun, type LucideIcon } from 'lucide-react'

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { api, openSession, readProof, type ApiError } from '@/lib/session'
import { readPreference, storePreference, type ThemePreference } from '@/lib/theme'
import { cn } from '@/lib/utils'

type Connection =
  | { state: 'no-proof' }
  | { state: 'connecting' }
  | { state: 'connected' }
  | { state: 'failed'; error: ApiError }

const proofInvalidMessage =
  'This session URL was already used or is invalid. If you did not open it yourself, stop gh nibfin dashboard (Ctrl-C).'

// The proof is read once per page load, before the first render.
const proof = readProof(location.hash, history)

export function App() {
  const connection = useConnection()
  return (
    <div className="flex min-h-svh flex-col">
      <header className="border-b border-border-subtle bg-card">
        <div className="mx-auto flex w-full max-w-5xl flex-wrap items-center justify-between gap-3 px-4 py-3 sm:px-6">
          <h1 className="text-lg font-semibold tracking-tight">gh nibfin dashboard</h1>
          <ThemeControl />
        </div>
      </header>
      <main className="mx-auto w-full max-w-5xl flex-1 px-4 py-6 sm:px-6 sm:py-10">
        <ConnectionCard connection={connection} />
      </main>
    </div>
  )
}

function useConnection(): Connection {
  const [connection, setConnection] = useState<Connection>(proof ? { state: 'connecting' } : { state: 'no-proof' })
  useEffect(() => {
    if (!proof) return
    let live = true
    void (async () => {
      const opened = await openSession(proof)
      const checked = opened.ok ? await api<{ result?: unknown }>('/api/commands/status/plan', {}) : opened
      if (!live) return
      if (!checked.ok) setConnection({ state: 'failed', error: checked.error })
      else if (checked.value.result === undefined) {
        setConnection({ state: 'failed', error: { code: 'error', message: 'the status read returned no result' } })
      } else setConnection({ state: 'connected' })
    })()
    return () => {
      live = false
    }
  }, [])
  return connection
}

const themeOptions: { value: ThemePreference; label: string; icon: LucideIcon }[] = [
  { value: 'dark', label: 'Dark', icon: Moon },
  { value: 'light', label: 'Light', icon: Sun },
  { value: 'system', label: 'System', icon: Monitor },
]

function ThemeControl() {
  const [pref, setPref] = useState<ThemePreference>(readPreference)
  return (
    <ToggleGroup
      type="single"
      variant="outline"
      size="sm"
      aria-label="Theme"
      value={pref}
      onValueChange={(v) => {
        // Radix sends '' when the selected item is pressed again; keep the choice.
        if (!v) return
        const next = v as ThemePreference
        setPref(next)
        storePreference(next)
      }}
    >
      {themeOptions.map(({ value, label, icon: Icon }) => (
        <ToggleGroupItem key={value} value={value} className="border-border">
          <Icon aria-hidden="true" />
          {label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}

const tones = {
  info: 'bg-info-bg text-info',
  success: 'bg-success-bg text-success',
  danger: 'bg-danger-bg text-danger',
}

function describe(c: Connection): { tone: keyof typeof tones; icon: LucideIcon; label: string; detail: string; spin?: boolean } {
  switch (c.state) {
    case 'no-proof':
      return { tone: 'info', icon: Info, label: 'No session', detail: 'Open the session URL printed by gh nibfin dashboard.' }
    case 'connecting':
      return { tone: 'info', icon: LoaderCircle, label: 'Connecting', detail: 'Opening the session…', spin: true }
    case 'connected':
      return { tone: 'success', icon: CircleCheck, label: 'Connected', detail: 'The session is open and the API answers.' }
    case 'failed':
      if (c.error.code === 'proof_invalid') {
        return { tone: 'danger', icon: CircleAlert, label: 'Session URL used', detail: proofInvalidMessage }
      }
      return { tone: 'danger', icon: CircleAlert, label: 'Not connected', detail: `${c.error.code}: ${c.error.message}` }
  }
}

function ConnectionCard({ connection }: { connection: Connection }) {
  const d = describe(connection)
  const Icon = d.icon
  return (
    <Card className="border-border-subtle">
      <CardHeader>
        <CardTitle>
          <h2 className="text-base">Connection</h2>
        </CardTitle>
        <CardDescription>This page talks only to the local gh nibfin dashboard server.</CardDescription>
      </CardHeader>
      <CardContent>
        <div
          role="status"
          data-state={connection.state}
          className={cn('flex items-start gap-3 rounded-md px-4 py-3', tones[d.tone])}
        >
          <Icon aria-hidden="true" className={cn('mt-0.5 size-5 shrink-0', d.spin && 'motion-safe:animate-spin')} />
          <div className="min-w-0">
            <p className="font-semibold">{d.label}</p>
            <p className="text-sm break-words">{d.detail}</p>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
