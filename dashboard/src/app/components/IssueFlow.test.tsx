import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { IssueFlow } from './IssueFlow'

// The Home flow (#457): the same eight steps as before, with who acts at each.
const html = renderToStaticMarkup(<IssueFlow />)
const text = (s: string) => s.replaceAll('<!-- -->', '').replace(/<[^>]+>/g, '')
const items = [...html.match(/<ol class="sr-only">(.*)<\/ol>/)![1].matchAll(/<li>(.*?)<\/li>/g)].map((m) => text(m[1]))
const legend = [...html.matchAll(/<li[^>]*data-kind="([^"]+)"[^>]*>(.*?)<\/li>/g)].map((m) => ({ id: m[1], text: text(m[2]) }))

describe('IssueFlow', () => {
  it('is a diagram figure with its caption', () => {
    expect(html).toMatch(/^<figure[^>]*data-slot="diagram"/)
    expect(html).toContain('Who acts at each step')
    expect(html).toContain('pause')
    expect(html).toContain('pending')
  })

  it('lists the eight steps in order with their sub-steps, labels and who acts', () => {
    expect(items).toEqual([
      'Issue: scope or request label, then one Assignee. Labels: scope, request. Who acts: A person’s control point. Leads to Shape.',
      'Shape: Author, then Validator, then Shape published. Labels: shaped. Who acts: Agent work; Nibfin, no agent. Leads to Deliver.',
      'Deliver: Writer, then Accept, then pull request. Who acts: Agent work; Nibfin, no agent. Leads to CI.',
      'CI: required checks. Who acts: GitHub. Leads to Review.',
      'Review: review against the Shape. Who acts: Agent work. Leads to Merge.',
      'Merge: merged into the base, after required reviews. Who acts: GitHub; A person’s control point. Leads to CD.',
      'CD: CD runs pass. Who acts: GitHub. Leads to Delivered.',
      'Delivered: delivered comment and label, then Issue closed, then downstream Issues woken. Labels: delivered. Who acts: Nibfin, no agent.',
    ])
  })

  it('credits only Nibfin, without an agent, with labelling, closing and waking', () => {
    const delivered = items.at(-1)!
    expect(delivered).toContain('Who acts: Nibfin, no agent.')
    expect(delivered).not.toContain('GitHub')
    expect(delivered).not.toContain('Agent work')
  })

  it('explains the four kinds in the legend', () => {
    expect(legend.map((k) => k.id)).toEqual(['agent', 'github', 'nibfin', 'person'])
    const [agent, github, nibfin, person] = legend.map((k) => k.text)
    expect(agent).toContain('Shape Author and Validator')
    expect(agent).toContain('Writer and Acceptor')
    expect(agent).toContain('reviewer')
    expect(github).toMatch(/checks.*merge.*CD/)
    for (const word of ['clos', 'label', 'wake', 'delivered', 'shaped']) expect(github).not.toContain(word)
    for (const word of ['shaped', 'pull request', 'delivered', 'closes', 'wakes']) expect(nibfin).toContain(word)
    for (const word of ['scope', 'request', 'Assignee', 'pause', 'pending', 'required reviews']) expect(person).toContain(word)
  })
})
