import { UserRoundCheck } from 'lucide-react'

import { safeLink } from '@/lib/issue'
import type { NeedsPerson as NeedsPersonData } from '@/lib/status'
import { cn } from '@/lib/utils'

import { ExternalMark } from './external-mark'

const linkClass = 'rounded-sm text-link underline-offset-4 hover:underline'

// NeedsPerson lists the parts of a repair decision that Core reports it did
// not apply (Proposal R4.51), such as a label, CI or closing the Issue: a
// person does them. It only shows them; the items are agent-written and
// shown only as text, and the link to Core's comment only when it is
// http(s).
export function NeedsPerson({ data, className }: { data: NeedsPersonData; className?: string }) {
  if (data.items.length === 0) return null
  const comment = safeLink(data.comment)
  return (
    <section
      aria-label="Needs a person"
      data-slot="needs-person"
      className={cn('flex min-w-0 flex-col gap-1.5 rounded-md border border-border-subtle bg-muted/40 px-3 py-2 text-sm', className)}
    >
      <p className="flex items-start gap-2 font-medium">
        <UserRoundCheck aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-warning" />
        <span className="min-w-0 break-words">Needs a person: Nibfin did not do these</span>
      </p>
      <ul className="flex list-disc flex-col gap-1 pl-5">
        {data.items.map((item, i) => (
          <li key={i} data-slot="needs-person-item" className="break-words">
            {item}
          </li>
        ))}
      </ul>
      {comment && (
        <a href={comment} target="_blank" rel="noreferrer noopener" className={cn(linkClass, 'w-fit')}>
          Nibfin’s note <ExternalMark label="(opens in a new tab)" />
        </a>
      )}
    </section>
  )
}
