import { ArrowRight } from 'lucide-react'

import { useLiveFeed } from '@/app/lib/live-feed'
import { liveUrl } from '@/app/lib/live-url'
import { LiveStatus } from '@/app/components/live/LiveStatus'
import { SummaryPanel } from '@/app/components/live/SummaryPanel'

/**
 * The Home page's live summary: the Worker stream's connection and
 * freshness, the Summary counts and a link to the Live page.
 */
export function LiveSummary() {
  const feed = useLiveFeed(liveUrl)
  return (
    <div className="not-content flex flex-col gap-4" data-slot="live-summary">
      <LiveStatus feed={feed} />
      <SummaryPanel data={feed.data} />
      <p className="m-0">
        <a
          href={`${import.meta.env.BASE_URL.replace(/\/?$/, '/')}live/`}
          className="inline-flex items-center gap-1 rounded-sm font-medium text-link underline-offset-4 hover:underline"
        >
          See the live view
          <ArrowRight aria-hidden="true" className="size-4" />
        </a>
      </p>
    </div>
  )
}
