import { Diagram } from './diagram'
import { labelStateDiagram } from '../lib/label-flow'
import { labelStates } from '../lib/labels'

/**
 * The label state diagram: Core's checks top to bottom, each with a "yes"
 * exit to its outcome and a "no" path to the next check.
 */
export function LabelStates() {
  return (
    <Diagram
      className="mt-6"
      layout="vertical"
      readingOrder="spec"
      spec={labelStateDiagram(labelStates)}
      title="Label states, checked in order"
      description="Core checks from the top and stops at the first match: yes goes to the outcome, no goes to the next check."
    />
  )
}
