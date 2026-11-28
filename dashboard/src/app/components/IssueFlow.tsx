import { Diagram } from './diagram'
import { issueFlow } from '@/app/lib/issue-flow'

/** The path of an Issue from scope to delivery, as a pipeline diagram with who acts at each step. */
export function IssueFlow() {
  return (
    <Diagram
      flow
      spec={issueFlow}
      title="Who acts at each step"
      description="Eight steps, each marked with who acts. A person can stop any step with pause, and Nibfin waits for a person whenever it adds pending."
    />
  )
}
