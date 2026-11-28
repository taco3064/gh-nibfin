import type { DiagramSpec } from '@/app/components/diagram'

// The architecture figure of How it works (#460). It draws the page's own
// Architecture and Security bullets (content/docs/how-it-works.mdx) and adds
// nothing to them: where the workflows, route and finish runs and stage Jobs
// run, where the image comes from, and where the Secret NIBFIN_GITHUB_TOKEN
// goes. Every badge is a literal term of that page; architecture.test.ts
// checks it. Labels are kept short so the drawing stays small; the bullets
// carry the full text. The order of nodes and edges steers the layout: in
// this order no edge crosses a box or a label in either direction.

export const architecture: DiagramSpec = {
  groupStyle: 'frames',
  groups: [
    { id: 'person', title: 'You' },
    { id: 'host', title: "Owner's or Assignee's host" },
    { id: 'github', title: 'GitHub' },
  ],
  nodes: [
    { id: 'job', group: 'host', title: 'Job container', detail: 'Each stage Job, in a Podman container' },
    { id: 'login', group: 'host', title: 'Login volume', detail: "The provider CLI's own login, in a Podman volume" },
    { id: 'machine', group: 'host', title: 'Podman machine', detail: 'Windows and macOS; on Linux the Runners run on the host' },
    {
      id: 'runners',
      group: 'host',
      title: 'Runners',
      badges: ['nibfin-<user id>'],
      detail: 'Self-hosted; provider from their own configuration',
    },
    { id: 'cli', group: 'person', title: 'gh nibfin', detail: 'init, upgrade, runner add' },
    { id: 'dashboard', group: 'person', title: 'Dashboard', detail: 'gh nibfin dashboard, on 127.0.0.1 only' },
    { id: 'peer', group: 'github', title: 'Peer repository', badges: ['NIBFIN_GITHUB_TOKEN'], detail: 'Peer set of workflows' },
    { id: 'exec', group: 'github', title: 'Execution repository', badges: ['NIBFIN_GITHUB_TOKEN'], detail: 'Execution set of workflows' },
    { id: 'hosted', group: 'github', title: 'GitHub-hosted runner', detail: 'Route and finish runs of a public execution repository' },
    { id: 'image', group: 'github', title: 'GHCR image', badges: ['taco3064/nibfin'], detail: 'Pinned to a digest' },
  ],
  edges: [
    { from: 'image', to: 'job', label: 'pull by digest' },
    { from: 'login', to: 'job', label: 'mounted' },
    { from: 'hosted', to: 'exec', label: 'dispatches' },
    { from: 'exec', to: 'runners', label: 'stage Jobs; route (private)' },
    { from: 'cli', to: 'exec', label: 'init: Secret, workflows' },
    { from: 'exec', to: 'job', label: 'Secret for Nibfin, never the agent' },
    { from: 'runners', to: 'job', label: 'starts' },
    { from: 'cli', to: 'peer', label: 'init: Secret, workflows' },
    { from: 'job', to: 'exec', label: 'pushes and pull requests' },
    { from: 'job', to: 'peer', label: 'pushes and pull requests' },
    { from: 'machine', to: 'runners', label: 'runs them' },
    { from: 'peer', to: 'exec', label: 'wakes the Issue' },
    { from: 'exec', to: 'hosted', label: 'route (public)' },
  ],
}
