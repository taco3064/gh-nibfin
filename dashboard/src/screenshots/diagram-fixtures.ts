// Fictional examples of the shared site diagram (#456, #457), used only by
// its tests and the screenshot harness (diagram.html). No site page imports them.

import { Brush, Cog, Hand, Hourglass, Scissors } from 'lucide-react'

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

// pipelineExample marks its nodes with kinds and icons and wraps its LR
// drawing into rows of three ranks.
const pipelineKinds: DiagramSpec['kinds'] = [
  { id: 'hands', title: 'Handwork', description: 'Done at the bench by hand.', icon: Hand, tone: 'primary' },
  { id: 'machine', title: 'A machine', description: 'The saw, the sander or the oven.', icon: Cog, tone: 'info' },
  { id: 'wait', title: 'Waiting for it to dry', icon: Hourglass, tone: 'warning' },
]

export const pipelineExample: DiagramSpec = {
  nodes: [
    { id: 'cut', title: 'Cut', detail: 'Saw the parts to size', icon: Scissors, kinds: ['machine', 'hands'] },
    { id: 'sand', title: 'Sand', detail: 'Smooth every edge', kinds: ['machine'] },
    { id: 'glue', title: 'Glue', detail: 'Clamp the frame', badges: ['clamps'], icon: Hand, kinds: ['hands'] },
    { id: 'dry', title: 'Dry', detail: 'Overnight on the rack', icon: Hourglass, kinds: ['wait'] },
    { id: 'paint', title: 'Paint', detail: 'Two thin coats', icon: Brush, kinds: ['hands', 'wait'] },
    { id: 'shelf', title: 'On the shelf', badges: ['done'], kinds: ['hands'] },
  ],
  edges: [
    { from: 'cut', to: 'sand' },
    { from: 'sand', to: 'glue' },
    { from: 'glue', to: 'dry', label: 'next day' },
    { from: 'dry', to: 'paint' },
    { from: 'paint', to: 'shelf' },
  ],
  kinds: pipelineKinds,
  kindsTitle: 'Who works',
  wrap: 3,
}
