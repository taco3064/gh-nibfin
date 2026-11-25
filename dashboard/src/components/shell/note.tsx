import type { ReactNode } from 'react'
import { CircleAlert, Info, TriangleAlert, type LucideIcon } from 'lucide-react'

import { tones } from '@/components/issue/tones'
import { cn } from '@/lib/utils'

type NoteTone = 'info' | 'warning' | 'danger'

const icons: Record<NoteTone, LucideIcon> = { info: Info, warning: TriangleAlert, danger: CircleAlert }

// Note is a labelled message line: an icon and text on a token tone. danger
// notes are alerts; the others are plain text.
export function Note({ tone, children, className, ...rest }: { tone: NoteTone; children: ReactNode; className?: string; 'data-note'?: string }) {
  const Icon = icons[tone]
  return (
    <div
      role={tone === 'danger' ? 'alert' : undefined}
      data-slot="note"
      className={cn('flex items-start gap-2 rounded-md border px-3 py-2 text-sm', tones[tone], className)}
      {...rest}
    >
      <Icon aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
      <div className="min-w-0 break-words">{children}</div>
    </div>
  )
}
