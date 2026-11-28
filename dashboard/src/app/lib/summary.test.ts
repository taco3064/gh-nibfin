import { describe, expect, it } from 'vitest'

import { digits, repoNames, showcaseStart, sinceLabel, trendBarPercent, trendDay, trendDayText, trendTotal } from './summary'

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

describe('delivered trend', () => {
  it('scales a bar to the 7-day maximum, keeping a small day visible', () => {
    expect(trendBarPercent(10, 10)).toBe(100)
    expect(trendBarPercent(5, 10)).toBe(50)
    expect(trendBarPercent(1, 100)).toBe(8)
    expect(trendBarPercent(0, 10)).toBe(0)
    expect(trendBarPercent(0, 0)).toBe(0)
  })

  it('names a Taipei day in English', () => {
    expect(trendDay('2026-11-27')).toEqual({ weekday: 'Fri', day: '27', label: 'Fri 27', long: 'Fri 27 Nov' })
    expect(trendDay('bad').label).toBe('bad')
  })

  it('reads a day and the total as text', () => {
    expect(trendDayText({ date: '2026-11-26', delivered: 3 })).toBe('Thu 26 Nov: 3 delivered')
    expect(
      trendTotal([
        { date: '2026-11-26', delivered: 3 },
        { date: '2026-11-27', delivered: 4 },
      ]),
    ).toBe(7)
  })
})
