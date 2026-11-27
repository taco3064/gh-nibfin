import { describe, expect, it } from 'vitest'

import { layoutDiagram } from '../components/diagram'

import { checkId, labelStateDiagram } from './label-flow'
import { labelStates } from './labels'

describe('labelStateDiagram', () => {
  const spec = labelStateDiagram(labelStates)

  it('declares each state as its check, then its outcome, in order', () => {
    expect(spec.nodes.map((n) => n.id)).toEqual(labelStates.flatMap((s) => [`check-${s.id}`, s.id]))
  })

  it('keeps the wording of labels.ts', () => {
    const byId = new Map(spec.nodes.map((n) => [n.id, n]))
    for (const s of labelStates) {
      const check = byId.get(checkId(s))!
      expect(check.title).toBe(s.when)
      expect(check.badges).toEqual(s.labels)
      expect(check.detail).toBeUndefined()
      const outcome = byId.get(s.id)!
      expect(outcome.title).toBe(s.id)
      expect(outcome.detail).toBe(s.meaning)
    }
  })

  it('puts checks in their lane and each outcome in the lane of its kind', () => {
    expect(spec.groupStyle).toBe('lanes')
    expect(spec.groups).toEqual([
      { id: 'checks', title: 'Checks, in order' },
      { id: 'waits', title: 'Nothing runs' },
      { id: 'runs', title: 'A stage runs' },
      { id: 'done', title: 'Delivered' },
    ])
    const byId = new Map(spec.nodes.map((n) => [n.id, n]))
    for (const s of labelStates) {
      expect(byId.get(checkId(s))!.group).toBe('checks')
      expect(byId.get(s.id)!.group).toBe(s.outcome)
    }
    expect(spec.nodes.filter((n) => n.group === 'done').map((n) => n.id)).toEqual(['done'])
  })

  it('gives each check one yes exit to its outcome and chains the checks with no', () => {
    for (const [i, s] of labelStates.entries()) {
      const out = spec.edges.filter((e) => e.from === checkId(s))
      expect(out.filter((e) => e.label === 'yes')).toEqual([{ from: checkId(s), to: s.id, label: 'yes' }])
      const no = out.filter((e) => e.label === 'no')
      if (i === labelStates.length - 1) expect(no).toEqual([])
      else expect(no).toEqual([{ from: checkId(s), to: checkId(labelStates[i + 1]), label: 'no' }])
    }
    expect(spec.edges).toHaveLength(2 * labelStates.length - 1)
    expect(spec.edges.filter((e) => labelStates.some((s) => s.id === e.from))).toEqual([])
  })

  it('places every check below the previous one when drawn top to bottom', () => {
    const p = layoutDiagram(spec, 'TB')
    const y = labelStates.map((s) => p.nodes.find((n) => n.id === checkId(s))!.y)
    for (let i = 1; i < y.length; i++) expect(y[i]).toBeGreaterThan(y[i - 1])
    // Each outcome is below its check.
    for (const s of labelStates) {
      expect(p.nodes.find((n) => n.id === s.id)!.y).toBeGreaterThan(p.nodes.find((n) => n.id === checkId(s))!.y)
    }
  })
})
