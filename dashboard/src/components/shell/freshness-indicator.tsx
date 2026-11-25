import { useEffect, useState } from 'react'
import { Clock, History } from 'lucide-react'

import { tones } from '@/components/issue/tones'
import { Badge } from '@/components/ui/badge'
import { freshness } from '@/lib/shell'

// FreshnessIndicator shows the time of the data and whether it is stale.
// One timer re-renders it when the data turns stale; it never reads.
export function FreshnessIndicator({ readAt }: { readAt?: number }) {
  // expired is the readAt whose stale time has passed.
  const [expired, setExpired] = useState<number>()
  useEffect(() => {
    if (readAt === undefined) return
    const { staleAt } = freshness(readAt, Date.now())
    const t = setTimeout(() => setExpired(readAt), Math.max(0, staleAt - Date.now()))
    return () => clearTimeout(t)
  }, [readAt])

  if (readAt === undefined) {
    return (
      <p data-slot="freshness" data-state="none" className="text-sm text-muted-foreground">
        No data yet
      </p>
    )
  }
  const stale = expired === readAt
  const at = new Date(readAt)
  return (
    <p data-slot="freshness" data-state={stale ? 'stale' : 'current'} className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
      <span className="text-muted-foreground">
        Data from <time dateTime={at.toISOString()}>{at.toLocaleTimeString()}</time>
      </span>
      {stale ? (
        <Badge variant="outline" className={tones.warning}>
          <History aria-hidden="true" />
          Stale: over 5 minutes old
        </Badge>
      ) : (
        <Badge variant="outline" className={tones.success}>
          <Clock aria-hidden="true" />
          Current
        </Badge>
      )}
    </p>
  )
}
