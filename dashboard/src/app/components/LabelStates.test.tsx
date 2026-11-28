import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { labelStates, outcomeText } from '@/app/lib/labels'

import { LabelStates } from './LabelStates'

// React escapes text in HTML; the list items are compared as text.
const unescape = (s: string) =>
  s.replaceAll('<!-- -->', '').replaceAll('&#x27;', "'").replaceAll('&quot;', '"').replaceAll('&lt;', '<').replaceAll('&gt;', '>').replaceAll('&amp;', '&')

describe('LabelStates', () => {
  const html = renderToStaticMarkup(<LabelStates />)
  const list = html.match(/<ol class="sr-only">(.*)<\/ol>/)![1]
  const items = [...list.matchAll(/<li>(.*?)<\/li>/g)].map((m) => unescape(m[1]))

  it('lists every state in labelStates order, each check followed by its outcome', () => {
    expect(items).toHaveLength(2 * labelStates.length)
    for (const [i, s] of labelStates.entries()) {
      const check = items[2 * i]
      const outcome = items[2 * i + 1]
      expect(check.startsWith(`${s.when}.`)).toBe(true)
      expect(check).toContain('Lane: Checks, in order.')
      for (const label of s.labels) expect(check).toContain(label)
      if (s.labels.length) expect(check).toContain(`Labels: ${s.labels.join(', ')}.`)
      // The check names its own outcome as the yes target, then the next check.
      const next = labelStates[i + 1]
      expect(check).toContain(next ? `Leads to ${s.id} (yes), to ${next.when} (no).` : `Leads to ${s.id} (yes).`)
      expect(outcome).toBe(`${s.id}: ${s.meaning} Lane: ${outcomeText[s.outcome]}.`)
    }
  })

  it('shows one row per state, hidden from assistive technology, without drawings or inline styles', () => {
    expect(html).toMatch(/^<figure[^>]*data-slot="diagram"/)
    expect(html).toContain('<figcaption')
    expect(html).toContain('Label states, checked in order')
    expect(html).not.toMatch(/<svg[^>]*data-direction=/)
    expect(html).not.toContain('data-slot="diagram-stacked"')
    expect(html.match(/data-slot="diagram-rows"/g)).toHaveLength(1)
    expect(html).toMatch(/<div aria-hidden="true" class="@container[^"]*" data-slot="diagram-rows">/)
    expect(html).not.toMatch(/\sstyle="/)
  })

  it('lists every state in order with its check, labels, outcome id, meaning and outcome text', () => {
    const rows = html
      .split('<ol class="sr-only">')[0]
      .split('data-slot="label-state"')
      .slice(1)
      .map((r) => unescape(r.split('data-slot="label-no"')[0].replace(/<[^>]*>/g, ' ')))
    expect(rows).toHaveLength(labelStates.length)
    const order = [...html.matchAll(/data-state="([^"]*)"/g)].map((m) => m[1])
    expect(order).toEqual(labelStates.map((s) => s.id))
    for (const [i, s] of labelStates.entries()) {
      const row = rows[i]
      expect(row).toContain(s.when)
      for (const label of s.labels) expect(row).toContain(` ${label} `)
      expect(row).toContain(' yes ')
      expect(row).toContain(` ${s.id} `)
      expect(row).toContain(s.meaning)
      expect(row).toContain(outcomeText[s.outcome])
      expect(html).toContain(`data-block="${s.id}" data-outcome="${s.outcome}"`)
      expect(html).toContain(`data-block="check-${s.id}"`)
    }
  })

  it('leads each check but the last to the next with a no arrow', () => {
    expect(html.match(/data-slot="label-no"/g)).toHaveLength(labelStates.length - 1)
    // The last state's row ends the figure's rows: no arrow follows it.
    const last = html.split('data-slot="label-state"').at(-1)!
    expect(last).not.toContain('data-slot="label-no"')
  })
})
