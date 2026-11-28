import { Bot, CircleCheckBig, CircleDot, GitMerge, ListChecks, PencilRuler, Rocket, Search, Settings2, UserRound, Workflow, Wrench } from 'lucide-react'

import type { DiagramSpec } from '@/app/components/diagram'

// Who acts at each step, as the How it works page describes it: agents do the
// Shape, Deliver and review work; GitHub runs the checks and CD and performs
// the merge; Nibfin itself, starting no agent, publishes, opens the pull
// request, labels, closes and wakes; a person steers with labels and reviews.
const kinds: NonNullable<DiagramSpec['kinds']> = [
  {
    id: 'agent',
    title: 'Agent work',
    description: 'The Shape Author and Validator, the Writer and Acceptor, the reviewer.',
    icon: Bot,
    tone: 'primary',
  },
  {
    id: 'github',
    title: 'GitHub',
    description: 'The required checks (CI), the merge under the repository’s rules, the CD runs.',
    icon: Workflow,
    tone: 'info',
  },
  {
    id: 'nibfin',
    title: 'Nibfin, no agent',
    description:
      'Publishes the Shape and adds shaped, opens the pull request, writes the delivered comment, adds delivered, closes the Issue and wakes the Issues it blocked.',
    icon: Settings2,
    tone: 'success',
  },
  {
    id: 'person',
    title: 'A person’s control point',
    description: 'Adds scope or request and an Assignee, can stop any step with pause, is asked when Nibfin adds pending, and gives the required reviews.',
    icon: UserRound,
    tone: 'warning',
  },
]

const steps: DiagramSpec['nodes'] = [
  {
    id: 'issue',
    title: 'Issue',
    detail: 'scope or request label, then one Assignee',
    badges: ['scope', 'request'],
    icon: CircleDot,
    kinds: ['person'],
  },
  {
    id: 'shape',
    title: 'Shape',
    detail: 'Author, then Validator, then Shape published',
    badges: ['shaped'],
    icon: PencilRuler,
    kinds: ['agent', 'nibfin'],
  },
  {
    id: 'deliver',
    title: 'Deliver',
    detail: 'Writer, then Accept, then pull request',
    icon: Wrench,
    kinds: ['agent', 'nibfin'],
  },
  { id: 'ci', title: 'CI', detail: 'required checks', icon: ListChecks, kinds: ['github'] },
  { id: 'review', title: 'Review', detail: 'review against the Shape', icon: Search, kinds: ['agent'] },
  {
    id: 'merge',
    title: 'Merge',
    detail: 'merged into the base, after required reviews',
    icon: GitMerge,
    kinds: ['github', 'person'],
  },
  { id: 'cd', title: 'CD', detail: 'CD runs pass', icon: Rocket, kinds: ['github'] },
  {
    id: 'delivered',
    title: 'Delivered',
    detail: 'delivered comment and label, then Issue closed, then downstream Issues woken',
    badges: ['delivered'],
    icon: CircleCheckBig,
    kinds: ['nibfin'],
  },
]

// issueFlow is the path of an Issue from scope to delivery on Home (#457).
export const issueFlow: DiagramSpec = {
  nodes: steps,
  edges: steps.slice(1).map((step, i) => ({ from: steps[i].id, to: step.id })),
  kinds,
  kindsTitle: 'Who acts',
  wrap: 4,
}
