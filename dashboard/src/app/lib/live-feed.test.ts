import { describe, expect, it } from 'vitest'

import type { LiveData } from '../../worker/messages'
import { initialFeed, parseEvent, reconnectDelay, reduceLive, type LiveFeedState } from './live-feed'

const data: LiveData = {
  repos: [{ repo: 'github.com/acme/app', issues: [] }],
  summary: { since: '2026-11-21T00:00:00+08:00', created: 3, delivered: 1 },
  runners: [],
}

const msg = (v: unknown) => JSON.stringify(v)
const apply = (s: LiveFeedState, ...ms: string[]) => ms.reduce(reduceLive, s)

describe('reduceLive', () => {
  it('takes a snapshot, then checked', () => {
    const s = apply(initialFeed, msg({ event: 'snapshot', at: 't1', result: data }), msg({ event: 'checked', at: 't2' }))
    expect(s.data).toEqual(data)
    expect(s.changedAt).toBe('t1')
    expect(s.checkedAt).toBe('t2')
    expect(s.errors).toEqual([])
  })

  it('keeps the data on an error and clears the error on the next checked', () => {
    const e = { event: 'error', source: 'github', error: { code: 'github_api_failed', message: 'HTTP 502' } }
    let s = apply(initialFeed, msg({ event: 'snapshot', at: 't1', result: data }), msg(e))
    expect(s.data).toEqual(data)
    expect(s.errors).toEqual([{ code: 'github_api_failed', message: 'HTTP 502' }])
    s = reduceLive(s, msg({ event: 'checked', at: 't3' }))
    expect(s.errors).toEqual([])
    expect(s.data).toEqual(data)
  })

  it('collapses duplicate errors and puts the newest first', () => {
    const a = msg({ event: 'error', source: 'github', error: { code: 'a', message: 'one' } })
    const b = msg({ event: 'error', source: 'github', error: { code: 'b', message: 'two' } })
    const s = apply(initialFeed, a, b, a)
    expect(s.errors.map((e) => e.code)).toEqual(['a', 'b'])
  })

  it('ignores malformed JSON and unknown or incomplete events', () => {
    const s = apply(
      initialFeed,
      '{',
      msg({ event: 'issue', at: 't' }),
      msg({ event: 'snapshot', at: 't', result: { repos: 1 } }),
      msg(null),
      msg({ event: 'error' }),
    )
    expect(s).toBe(initialFeed)
    expect(parseEvent('[]')).toBeUndefined()
  })

  it('keeps the data when the connection closes', () => {
    const s = apply(initialFeed, msg({ event: 'snapshot', at: 't1', result: data }))
    const closed: LiveFeedState = { ...s, connection: 'closed' }
    expect(reduceLive(closed, '').data).toEqual(data)
  })
})

describe('reconnectDelay', () => {
  it('starts at 1 s, doubles and stops at 30 s', () => {
    expect([0, 1, 2, 3, 4, 5, 6, 50].map(reconnectDelay)).toEqual([1000, 2000, 4000, 8000, 16000, 30000, 30000, 30000])
  })
})
