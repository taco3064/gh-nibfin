import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { labelStates, outcomeText } from '../lib/labels'

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

  it('draws one top-to-bottom figure without inline styles', () => {
    expect(html).toMatch(/^<figure[^>]*data-slot="diagram"/)
    expect(html).toContain('Label states, checked in order')
    const svgs = html.match(/<svg[^>]*>/g) ?? []
    expect(svgs).toHaveLength(1)
    expect(svgs[0]).toContain('data-direction="TB"')
    expect(svgs[0]).toContain('aria-hidden="true"')
    expect(html).not.toMatch(/\sstyle="/)
  })
})
