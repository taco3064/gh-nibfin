import { LoaderCircle, Radio, ShieldQuestion, Unplug } from 'lucide-react'

import { tones } from '@/components/issue/tones'
import { FreshnessIndicator } from '@/components/shell/freshness-indicator'
import { Note } from '@/components/shell/note'
import { Badge } from '@/components/ui/badge'

import type { LiveFeedState } from '@/app/lib/live-feed'

// ConnectionBadge says whether the page receives updates.
function ConnectionBadge({ feed }: { feed: LiveFeedState }) {
  if (feed.connection === 'open') {
    return (
      <Badge variant="outline" data-slot="live-connection" data-state="open" className={tones.success}>
        <Radio aria-hidden="true" />
        Live
      </Badge>
    )
  }
  if (feed.connection === 'closed') {
    return (
      <Badge variant="outline" data-slot="live-connection" data-state="closed" className={tones.warning}>
        <Unplug aria-hidden="true" />
        Disconnected: reconnecting
      </Badge>
    )
  }
  return (
    <Badge variant="outline" data-slot="live-connection" data-state="connecting" className={tones.neutral}>
      <LoaderCircle aria-hidden="true" className="motion-safe:animate-spin" />
      {feed.data ? 'Reconnecting…' : 'Connecting…'}
    </Badge>
  )
}

// LiveStatus shows the connection, the time of the last good GitHub check
// with its freshness, and why the data may be out of date.
export function LiveStatus({ feed }: { feed: LiveFeedState }) {
  const error = feed.errors[0]
  return (
    <div className="flex flex-col gap-2" data-slot="live-status">
      <div role="status" className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <ConnectionBadge feed={feed} />
        {feed.checkedAt !== undefined ? (
          <FreshnessIndicator readAt={Date.parse(feed.checkedAt)} />
        ) : (
          feed.data && (
            <Badge variant="outline" data-slot="unconfirmed" className={tones.warning}>
              <ShieldQuestion aria-hidden="true" />
              Not confirmed by a GitHub check yet
            </Badge>
          )
        )}
      </div>
      {error && (
        <Note tone="warning" data-note="check-error">
          GitHub check failed: {error.code}: {error.message}
        </Note>
      )}
      {feed.connection !== 'open' && feed.data && (
        <p className="text-sm text-muted-foreground" data-note="kept">
          The data shown is kept and may be out of date.
        </p>
      )}
    </div>
  )
}
