import { describe, expect, it } from 'vitest'

import { pickOption, repairLabels, replying, typeReply } from './repair-reply'

describe('the Repair dialog sends one of an option and a reply', () => {
  it('reads a reply only when it has text', () => {
    expect(replying({ option: '2', reply: '' })).toBe(false)
    expect(replying({ option: '2', reply: ' \n\t' })).toBe(false)
    expect(replying({ option: '2' })).toBe(false)
    expect(replying({ option: '', reply: ' x ' })).toBe(true)
  })

  it('picking a card sets the option and clears the reply', () => {
    expect(pickOption('3')).toEqual({ option: '3', reply: '' })
  })

  it('typing a reply clears the option and a blank reply brings the last card back', () => {
    // The recommended card is picked when the dialog opens.
    let values: Record<string, string> = { option: '2', reply: '' }
    let last = '2'
    const type = (text: string) => {
      const r = typeReply(text, values, last)
      values = { ...values, ...(r.changes as Record<string, string>) }
      last = r.last
    }
    values = { ...values, ...(pickOption('3') as Record<string, string>) }
    last = '3'
    type('K')
    expect(values).toEqual({ option: '', reply: 'K' })
    expect(last).toBe('3')
    type('Keep it')
    expect(values).toEqual({ option: '', reply: 'Keep it' })
    expect(last).toBe('3')
    type('  ')
    expect(values).toEqual({ option: '3', reply: '  ' })
    type('')
    expect(values).toEqual({ option: '3', reply: '' })
  })

  it('names the reply actions and keeps the default for an option', () => {
    expect(repairLabels({ option: '2', reply: '' })).toEqual({})
    expect(repairLabels({ option: '', reply: 'Keep it' })).toEqual({ submit: 'Preview reply', apply: 'Send reply' })
  })
})
