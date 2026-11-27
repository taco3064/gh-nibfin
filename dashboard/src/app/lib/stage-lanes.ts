import type { DiagramSpec } from '../components/diagram'

// The stages of How it works (#459) as a swimlane: one lane per role or
// actor, the forward hand-offs labelled with what is handed over and the
// paths back labelled with why. It says nothing the numbered list below it on
// the page does not say; that list is its text alternative.
export const stageLanes: DiagramSpec = {
  groupStyle: 'lanes',
  groups: [
    { id: 'scope', title: 'Scope' },
    { id: 'author', title: 'Shape Author' },
    { id: 'validator', title: 'Shape Validator' },
    { id: 'writer', title: 'Writer' },
    { id: 'acceptor', title: 'Acceptor' },
    { id: 'reviewer', title: 'reviewer' },
    { id: 'github', title: 'GitHub (CI, merge, CD)' },
  ],
  nodes: [
    { id: 'split', title: 'Split into Issues', detail: 'Ordered, with Blocked by', badges: ['scope'], group: 'scope' },
    { id: 'write', title: 'Write the Shape', detail: 'Investigate and plan the Issue', group: 'author' },
    { id: 'validate', title: 'Validate the Shape', detail: 'With only the sources and the Shape', group: 'validator' },
    { id: 'implement', title: 'Implement', detail: "On the Issue's branch", group: 'writer' },
    { id: 'accept', title: 'Accept', detail: 'Sees only the Issue, the Shape and the diff', group: 'acceptor' },
    { id: 'checks', title: 'Required checks', group: 'github' },
    { id: 'review', title: 'Review', detail: 'Against the Issue and the Shape', group: 'reviewer' },
    { id: 'merge', title: 'Merge', detail: 'With a method the repository allows', group: 'github' },
    { id: 'cd', title: 'CD runs', group: 'github' },
    { id: 'closed', title: 'Closed', detail: 'Wakes the Issues it blocked', badges: ['delivered'], group: 'github' },
  ],
  edges: [
    { from: 'split', to: 'write', label: 'Issue with scope' },
    { from: 'write', to: 'validate', label: 'proposed Shape' },
    { from: 'validate', to: 'write', label: 'findings' },
    { from: 'validate', to: 'implement', label: 'Shape comment and shaped' },
    { from: 'implement', to: 'write', label: 'Shape wrong, with the reason' },
    { from: 'implement', to: 'accept', label: 'branch nibfin/issue-<n>' },
    { from: 'accept', to: 'implement', label: 'defects' },
    { from: 'accept', to: 'checks', label: 'pass, pull request' },
    { from: 'checks', to: 'review', label: 'checks pass' },
    { from: 'review', to: 'implement', label: 'problems' },
    { from: 'review', to: 'merge', label: 'clean' },
    { from: 'merge', to: 'cd', label: 'merged' },
    { from: 'cd', to: 'implement', label: 'failure, new fix branch' },
    { from: 'cd', to: 'closed', label: 'pass, delivered comment' },
  ],
}
