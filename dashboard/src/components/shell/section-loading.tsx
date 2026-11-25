import { LoaderCircle } from 'lucide-react'

import { cn } from '@/lib/utils'

// SectionLoading is the loading state of one section still being read: a
// spinner, still under reduced motion, beside its text label.
export function SectionLoading({ label, className, ...rest }: { label: string; className?: string; 'data-loading'?: string }) {
  return (
    <span className={cn('inline-flex min-w-0 items-center gap-1.5 text-sm text-muted-foreground', className)} data-slot="section-loading" {...rest}>
      <LoaderCircle aria-hidden="true" className="size-3.5 shrink-0 motion-safe:animate-spin" />
      <span className="min-w-0 break-words">{label}</span>
    </span>
  )
}
