import { describe, expect, it } from 'vitest'

import { initLabels, labelStates } from './labels'

describe('labelStates', () => {
  it('follows the order Core derives a stage in', () => {
    expect(labelStates.map((state) => state.id)).toEqual([
      'closed',
      'no-scope',
      'paused',
      'pending',
      'multiple-assignees',
      'blocked',
      'no-assignee',
      'scope',
      'shape',
      'deliver',
      'conflict',
      'awaiting-ci',
      'ci-failed',
      'review',
      'awaiting-cd',
      'cd-failed',
      'done',
    ])
  })

  it('names only labels that init creates', () => {
    const known: readonly string[] = initLabels
    for (const state of labelStates) {
      for (const label of state.labels) expect(known).toContain(label)
    }
  })

  it('ends with delivered', () => {
    const last = labelStates.at(-1)
    expect(last?.outcome).toBe('done')
    expect(last?.labels).toEqual(['delivered'])
  })
})
