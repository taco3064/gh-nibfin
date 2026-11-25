import { CircleAlert, CircleHelp, Lock } from 'lucide-react'

import type { Access, ChangeOutcome } from '@/lib/issue'
import { cn } from '@/lib/utils'

import { tones } from './tones'

// unknownText is shown when the apply's result never arrived.
export const unknownText = 'The result is unknown; reload the status to see the labels.'

// DisabledReason shows why pause and resume are not available, as visible
// text: a disabled button gets no hover, so a tooltip would never show.
export function DisabledReason({ id, access }: { id: string; access: Access }) {
  if (access.enabled) return null
  return (
    <p id={id} data-slot="disabled-reason" className="flex items-start gap-1.5 text-sm break-words text-muted-foreground">
      <Lock aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
      <span className="min-w-0">{access.reason}</span>
    </p>
  )
}

// ChangeMessage shows a failed or unknown pause or resume. A confirmed
// change shows through the badges, which follow the read-back.
export function ChangeMessage({ outcome, done }: { outcome: ChangeOutcome | null; done?: string }) {
  if (!outcome) return null
  if (outcome.state === 'done') {
    if (!done) return null
    return (
      <p role="status" data-state="done" className="text-sm text-muted-foreground">
        {done}
      </p>
    )
  }
  const unknown = outcome.state === 'unknown'
  const Icon = unknown ? CircleHelp : CircleAlert
  return (
    <div
      role="alert"
      data-state={outcome.state}
      className={cn('flex items-start gap-2 rounded-md px-3 py-2 text-sm', unknown ? tones.warning : tones.danger)}
    >
      <Icon aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
      <p className="min-w-0 break-words">
        {unknown ? (
          <>
            {unknownText} <span>({outcome.error.code}: {outcome.error.message})</span>
          </>
        ) : (
          <>
            <code className="font-semibold">{outcome.error.code}</code>: {outcome.error.message}
          </>
        )}
      </p>
    </div>
  )
}
