// The decision flowchart of How it works' label states (#458): Core checks
// the states top to bottom and stops at the first match, so each check has a
// "yes" exit to its outcome and a "no" path to the next check.

import type { DiagramNode, DiagramSpec } from '../components/diagram'

import { outcomeText, type LabelState, type Outcome } from './labels'

const outcomes: Outcome[] = ['waits', 'runs', 'done']

/** checkId is the id of a state's check node; the outcome node uses the state's id. */
export const checkId = (state: LabelState) => `check-${state.id}`

/**
 * labelStateDiagram builds the flowchart from the states in their order. It
 * declares each state's check and then its outcome, so a text alternative in
 * declaration order reads state by state, as the ordered list did.
 */
export function labelStateDiagram(states: LabelState[]): DiagramSpec {
  const nodes: DiagramNode[] = states.flatMap((state) => [
    { id: checkId(state), title: state.when, badges: [...state.labels], group: 'checks' },
    { id: state.id, title: state.id, detail: state.meaning, group: state.outcome },
  ])
  const edges = states.flatMap((state, i) => {
    const next = states[i + 1]
    return [{ from: checkId(state), to: state.id, label: 'yes' }, ...(next ? [{ from: checkId(state), to: checkId(next), label: 'no' }] : [])]
  })
  return {
    nodes,
    edges,
    groups: [{ id: 'checks', title: 'Checks, in order' }, ...outcomes.map((o) => ({ id: o, title: outcomeText[o] }))],
    groupStyle: 'lanes',
  }
}
