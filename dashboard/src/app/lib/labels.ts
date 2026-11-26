// The label state diagram of How it works. Nibfin Core derives an Issue's stage
// from its labels and GitHub state, checking in this order (deriveStage in
// taco3064/nibfin-core src/github/stage.ts); the first match wins.

/** The labels `gh nibfin init` creates. */
export const initLabels = ['request', 'scope', 'shaped', 'delivered', 'pause', 'pending'] as const

export type InitLabel = (typeof initLabels)[number]

/** What happens to an Issue in a state. */
export type Outcome = 'waits' | 'runs' | 'done'

export const outcomeText: Record<Outcome, string> = {
  waits: 'Nothing runs',
  runs: 'A stage runs',
  done: 'Delivered',
}

export interface LabelState {
  /** The stage name Core logs: the ineligible reason or the derived stage. */
  id: string
  /** The condition, read from the Issue. */
  when: string
  /** The init labels this state reads or writes. */
  labels: InitLabel[]
  outcome: Outcome
  meaning: string
}

export const labelStates: LabelState[] = [
  {
    id: 'closed',
    when: 'The Issue is closed',
    labels: [],
    outcome: 'waits',
    meaning: 'A closed Issue never runs.',
  },
  {
    id: 'scope',
    when: 'request label, no scope label',
    labels: ['request'],
    outcome: 'runs',
    meaning:
      'The Scope stage checks whether the request is feasible and, when it is, opens scope Issues for it. pause, pending, the Assignee and Blocked by still apply.',
  },
  {
    id: 'no-scope',
    when: 'No scope or request label',
    labels: ['scope', 'request'],
    outcome: 'waits',
    meaning: 'Only Issues with scope or request belong to Nibfin.',
  },
  {
    id: 'paused',
    when: 'pause is set',
    labels: ['pause'],
    outcome: 'waits',
    meaning: 'A person stopped the Issue. Removing pause resumes it.',
  },
  {
    id: 'pending',
    when: 'pending is set',
    labels: ['pending'],
    outcome: 'waits',
    meaning: 'Nibfin asked a person to act in a marked comment. Removing pending resumes it.',
  },
  {
    id: 'multiple-assignees',
    when: 'More than one Assignee',
    labels: [],
    outcome: 'waits',
    meaning: 'Nibfin runs an Issue for exactly one Assignee.',
  },
  {
    id: 'blocked',
    when: 'Blocked by an undelivered Issue',
    labels: ['delivered'],
    outcome: 'waits',
    meaning: 'Each Blocked by Issue must be closed with delivered first.',
  },
  {
    id: 'no-assignee',
    when: 'No Assignee',
    labels: [],
    outcome: 'waits',
    meaning: 'The Assignee’s Runners do the work, so someone must be assigned.',
  },
  {
    id: 'shape',
    when: 'No shaped label',
    labels: ['shaped'],
    outcome: 'runs',
    meaning: 'Shape writes and validates the plan, publishes it and adds shaped.',
  },
  {
    id: 'deliver',
    when: 'Shaped, no pull request yet',
    labels: ['shaped'],
    outcome: 'runs',
    meaning: 'Deliver implements the Shape; after Accept passes it opens the pull request.',
  },
  {
    id: 'conflict',
    when: 'The pull request conflicts with its base',
    labels: [],
    outcome: 'runs',
    meaning: 'Deliver rebases the branch onto the base.',
  },
  {
    id: 'awaiting-ci',
    when: 'Required checks still running',
    labels: [],
    outcome: 'waits',
    meaning: 'The check results wake the Issue again.',
  },
  {
    id: 'ci-failed',
    when: 'A required check failed',
    labels: [],
    outcome: 'runs',
    meaning: 'Deliver fixes what the failed run reports.',
  },
  {
    id: 'review',
    when: 'Required checks passed',
    labels: [],
    outcome: 'runs',
    meaning: 'Review checks the pull request against the Shape, then merges it or sends problems back.',
  },
  {
    id: 'awaiting-cd',
    when: 'Merged, CD still running',
    labels: [],
    outcome: 'waits',
    meaning: 'The CD runs wake the Issue again.',
  },
  {
    id: 'cd-failed',
    when: 'A CD run failed after the merge',
    labels: [],
    outcome: 'runs',
    meaning: 'Deliver fixes it on a new branch from the latest base, in a new pull request.',
  },
  {
    id: 'done',
    when: 'Merged and CD passed',
    labels: ['delivered'],
    outcome: 'done',
    meaning: 'Nibfin comments the delivery record, adds delivered, closes the Issue and wakes the Issues it blocked.',
  },
]
