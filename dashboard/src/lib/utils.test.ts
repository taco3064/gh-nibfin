import { describe, expect, it } from 'vitest'

import { formatClock, formatTime } from './utils'

// The expected values come from local-time Dates, so the tests hold in any
// time zone the tests run in.
const pad = (n: number) => String(n).padStart(2, '0')

describe('formatTime', () => {
  it('shows a date and time in English in the local time zone', () => {
    const at = new Date(2026, 10, 26, 2, 14, 5)
    expect(formatTime(at.toISOString())).toBe('26 Nov 2026, 02:14')
  })

  it('shows hours from 00 to 23', () => {
    expect(formatTime(new Date(2026, 0, 1, 0, 4).toISOString())).toBe('1 Jan 2026, 00:04')
    expect(formatTime(new Date(2026, 11, 31, 23, 59).toISOString())).toBe('31 Dec 2026, 23:59')
  })

  it('reads an ISO time with an offset as the same instant', () => {
    const at = new Date(Date.UTC(2026, 10, 26, 2, 14))
    const want = `${at.getDate()} Nov 2026, ${pad(at.getHours())}:${pad(at.getMinutes())}`
    expect(formatTime('2026-11-26T02:14:00Z')).toBe(want)
  })

  it('returns text that is not a time as it is', () => {
    expect(formatTime('not a time')).toBe('not a time')
    expect(formatTime('')).toBe('')
  })
})

describe('formatClock', () => {
  it('shows the time of day with seconds in the local time zone', () => {
    const at = new Date(2026, 10, 26, 9, 18, 43)
    expect(formatClock(at)).toBe('09:18:43')
    expect(formatClock(at.getTime())).toBe('09:18:43')
  })

  it('shows midnight as 00', () => {
    expect(formatClock(new Date(2026, 10, 26, 0, 0, 0))).toBe('00:00:00')
  })
})
