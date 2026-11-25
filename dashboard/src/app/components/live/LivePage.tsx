import type { ReactNode } from 'react'

import { RepoView } from '@/components/repo/repo-view'

import { useLiveFeed } from '../../lib/live-feed'
import { useLiveRepoSource } from '../../lib/live-repo-source'
import { liveUrl } from '../../lib/live-url'
import { Capacity } from './Capacity'
import { LiveStatus } from './LiveStatus'
import { SummaryPanel } from './SummaryPanel'

function Section({ id, title, children }: { id: string; title: string; children: ReactNode }) {
  return (
    <section aria-labelledby={id} className="flex flex-col gap-4">
      <h2 id={id} className="m-0 text-2xl font-semibold tracking-tight">
        {title}
      </h2>
      {children}
    </section>
  )
}

// LivePage is the public, read-only live view of Nibfin's own
// repositories, updated by the Worker stream without a reload.
export function LivePage() {
  const feed = useLiveFeed(liveUrl)
  const { status, source } = useLiveRepoSource(feed.data, feed.checkedAt)
  return (
    <div className="not-content flex flex-col gap-10" data-slot="live-page">
      <LiveStatus feed={feed} />
      <Section id="live-summary" title="Summary">
        <SummaryPanel data={feed.data} />
      </Section>
      <Section id="live-capacity" title="Capacity">
        <Capacity runners={feed.data?.runners} />
      </Section>
      <Section id="live-repos" title="Repositories">
        <RepoView status={status} source={source} listReading={feed.connection !== 'closed' && !feed.data} readOnly />
      </Section>
    </div>
  )
}
