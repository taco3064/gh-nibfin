import { describe, expect, it } from 'vitest'

import { digits, repoNames, showcaseStart, sinceLabel } from './summary'

describe('digits', () => {
  it('gives one column per digit with thousands separators', () => {
    expect(digits(0)).toEqual(['0'])
    expect(digits(7)).toEqual(['7'])
    expect(digits(42)).toEqual(['4', '2'])
    expect(digits(1000)).toEqual(['1', ',', '0', '0', '0'])
  })

  it('gives none before the first read', () => {
    expect(digits(null)).toEqual([])
  })
})

describe('summary text', () => {
  it('names the start date in the showcase time zone', () => {
    expect(sinceLabel(showcaseStart)).toBe('21 Nov 2026')
  })

  it('joins the repository names', () => {
    const r = (repo: string) => ({ repo, issues: [] })
    expect(repoNames([r('github.com/a/nibfin-core'), r('github.com/a/gh-nibfin'), r('github.com/a/nibfin-e2e')])).toBe('nibfin-core, gh-nibfin and nibfin-e2e')
    expect(repoNames([r('github.com/a/one')])).toBe('one')
  })
})
