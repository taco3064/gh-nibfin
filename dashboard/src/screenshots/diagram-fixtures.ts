// Fictional example of the shared site diagram (#456), used only by its
// tests and the screenshot harness (diagram.html). No site page imports it.

import type { DiagramSpec } from '@/app/components/diagram'

const nodes: DiagramSpec['nodes'] = [
  { id: 'idea', title: 'Idea', detail: 'Someone writes down a wish', badges: ['draft'], group: 'plan' },
  { id: 'sketch', title: 'Sketch', detail: 'Rough shapes on paper', group: 'plan' },
  { id: 'notes', title: 'Notes', detail: 'Questions to answer first', group: 'plan' },
  { id: 'build', title: 'Build the model', detail: 'Glue, paint and patience', badges: ['wip', 'workshop'], group: 'make' },
  { id: 'check', title: 'Check', detail: 'Does it stand on its own?', group: 'make' },
  { id: 'show', title: 'Show it', detail: 'Put it on the shelf', badges: ['done'], group: 'make' },
  { id: 'archive', title: 'Archive the notes' },
]

const edges: DiagramSpec['edges'] = [
  { from: 'idea', to: 'sketch' },
  { from: 'idea', to: 'notes', label: 'unclear' },
  { from: 'sketch', to: 'build' },
  { from: 'notes', to: 'build', label: 'answered' },
  { from: 'build', to: 'check' },
  { from: 'check', to: 'show', label: 'stands' },
  { from: 'check', to: 'archive' },
]

const groups: DiagramSpec['groups'] = [
  { id: 'plan', title: 'Plan at the desk' },
  { id: 'make', title: 'Make in the workshop' },
]

export const framesExample: DiagramSpec = { nodes, edges, groups, groupStyle: 'frames' }
export const lanesExample: DiagramSpec = { nodes, edges, groups, groupStyle: 'lanes' }
