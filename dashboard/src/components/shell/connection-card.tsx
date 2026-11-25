import { CircleAlert, CircleCheck, Info, LoaderCircle, type LucideIcon } from 'lucide-react'

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import type { Connection } from '@/lib/use-status'
import { cn } from '@/lib/utils'

const proofInvalidMessage =
  'This session URL was already used or is invalid. If you did not open it yourself, stop gh nibfin dashboard (Ctrl-C).'

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

// ConnectionCard shows the session state until the session is open.
export function ConnectionCard({ connection }: { connection: Connection }) {
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
