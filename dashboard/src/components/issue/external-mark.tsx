import { ExternalLink } from 'lucide-react'

// ExternalMark is the new-tab mark of a link, with its text for screen
// readers.
export function ExternalMark({ label }: { label: string }) {
  return (
    <>
      <ExternalLink aria-hidden="true" className="inline size-3.5 shrink-0 align-[-0.125em]" />
      <span className="sr-only">{label}</span>
    </>
  )
}
