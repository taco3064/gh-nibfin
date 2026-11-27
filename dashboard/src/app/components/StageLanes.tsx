import { stageLanes } from '../lib/stage-lanes'

import { Diagram } from './diagram'

/** The stage swimlane of How it works, drawn top to bottom at every width. */
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
