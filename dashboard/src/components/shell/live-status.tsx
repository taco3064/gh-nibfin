import { Radio, Unplug } from 'lucide-react'

import { tones } from '@/components/issue/tones'
import { Note } from '@/components/shell/note'
import { Badge } from '@/components/ui/badge'
import { hasNoRunner } from '@/lib/shell'
import type { ApiError, PersonView } from '@/lib/status'
import type { Live, Podman } from '@/lib/use-status'

const text = (e: ApiError) => `${e.code}: ${e.message}`

// LiveBadge says whether the page updates by itself. Nothing shows while
// the stream connects.
export function LiveBadge({ live }: { live: Live }) {
  if (live.state === 'connecting') return null
  if (live.state === 'on') {
    return (
      <Badge variant="outline" data-slot="live" data-state="on" className={tones.success}>
        <Radio aria-hidden="true" />
        Live
      </Badge>
    )
  }
  return (
    <Badge variant="outline" data-slot="live" data-state="off" className={tones.warning}>
      <Unplug aria-hidden="true" />
      Live updates off
    </Badge>
  )
}

// LiveNotes name why live updates stopped or are partial.
export function LiveNotes({
  live,
  podman,
  checkError,
  liveReadError,
  data,
}: {
  live: Live
  podman?: Podman
  checkError?: ApiError
  liveReadError?: ApiError
  data?: PersonView
}) {
  return (
    <>
      {live.state === 'off' && (
        <Note tone="warning" data-note="live-off">
          Live updates stopped: {text(live.error)}. Use Refresh to reconnect.
        </Note>
      )}
      {checkError && (
        <Note tone="warning" data-note="check-error">
          GitHub check failed: {text(checkError)}
        </Note>
      )}
      {liveReadError && (
        <Note tone="warning" data-note="live-read-error">
          Live re-read failed: {text(liveReadError)}. The data shown is kept.
        </Note>
      )}
      {live.state === 'on' && podman && !podman.watching && data && !hasNoRunner(data.host) && (
        <p data-note="podman" className="text-sm break-words text-muted-foreground">
          Local Job events are not watched{podman.error ? `: ${text(podman.error)}` : ''}; GitHub is still checked.
        </p>
      )}
    </>
  )
}
