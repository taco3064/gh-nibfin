import { CircleAlert, CircleCheck, CircleHelp, CircleMinus, CircleSlash, TriangleAlert, type LucideIcon } from 'lucide-react'

import type { ImageKind } from '@/lib/person'

// imageIcons is the icon beside each image state's text label.
export const imageIcons: Record<ImageKind, LucideIcon> = {
  error: CircleAlert,
  none: CircleMinus,
  peer: CircleMinus,
  noPin: CircleSlash,
  latest: CircleCheck,
  notLatest: TriangleAlert,
  unknown: CircleHelp,
  otherSet: CircleHelp,
}
