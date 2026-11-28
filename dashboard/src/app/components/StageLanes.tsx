import { stageLanes } from '@/app/lib/stage-lanes'

import { Diagram } from './diagram'

/**
 * The stage swimlane of How it works. It is drawn top to bottom where its box
 * is wide enough for that drawing; otherwise it shows the stacked layout, one
 * block per stage with its role and hand-offs.
 */
export function StageLanes() {
  return (
    <Diagram
      spec={stageLanes}
      layout="vertical"
      title="The stages, by role"
      description="Arrows down hand the work on; arrows back show where it returns. The list below describes the same stages."
    />
  )
}
