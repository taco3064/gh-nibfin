import { useId } from 'react'
import { CircleCheck, CircleDot } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import type { Blocker } from '@/lib/status'
import { cn } from '@/lib/utils'

import { ExternalMark } from './issue-item'
import { tones } from './tones'

export interface BlockerItemProps {
  blocker: Blocker
  // blocks names the shown Issues this one blocks, such as #12.
  blocks: string[]
  className?: string
}

const linkClass = 'rounded-sm text-link underline-offset-4 hover:underline'

// BlockerItem is the reduced Issue element of a blocker outside the listed
// Issues: closed, in another repository or otherwise not listed. Only its
// reference was read, so it shows no stage and offers no pause or resume.
export function BlockerItem({ blocker, blocks, className }: BlockerItemProps) {
  const id = useId()
  const closed = blocker.state.toUpperCase() === 'CLOSED'
  const StateIcon = closed ? CircleCheck : CircleDot
  return (
    <article
      aria-labelledby={`${id}-title`}
      data-blocker={blocker.url}
      className={cn('flex flex-col gap-2 rounded-lg border border-border-subtle bg-card p-4 text-card-foreground', className)}
    >
      <h3 id={`${id}-title`} className="text-base leading-snug font-semibold">
        <a href={blocker.url} target="_blank" rel="noreferrer" className={cn(linkClass, 'text-foreground')}>
          <span className="break-all text-muted-foreground tabular-nums">{`${blocker.repo}#${blocker.number}`}</span>{' '}
          {blocker.title} <ExternalMark label="(opens on GitHub in a new tab)" />
        </a>
      </h3>
      <div className="flex flex-wrap items-center gap-1.5">
        <Badge variant="outline" data-badge="state" data-state={closed ? 'closed' : 'open'} className={closed ? tones.success : tones.info}>
          <StateIcon aria-hidden="true" />
          {closed ? 'Closed' : 'Open'}
        </Badge>
      </div>
      <p className="text-sm text-muted-foreground">Not listed here: no stage or pause shown</p>
      {blocks.length > 0 && (
        <p data-slot="blocks" className="text-sm break-words">
          <span className="text-muted-foreground">Blocks</span> {blocks.join(', ')}
        </p>
      )}
    </article>
  )
}
