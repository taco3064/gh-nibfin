import { describe, expect, it } from 'vitest'

import { buildPayload, cap, digits, encodedSize, MAX_DISPATCH_BYTES, MAX_TEXT, parsePayload, TRUNCATED } from './payload.ts'
import { webhookIssue } from './testing.ts'

describe('digits', () => {
  it('normalizes digit strings and non-negative safe integers', () => {
    expect(digits('42', 5)).toBe('42')
    expect(digits(42, 5)).toBe('42')
    expect(digits(0, 5)).toBe('0')
  })

  it('rejects negatives, fractions, other strings, long values and other types', () => {
    for (const v of [-1, 1.5, NaN, Infinity, 2 ** 53, '', '-1', '1.5', '1e3', ' 1', '0x1', '123456', null, undefined, true, {}, [1]]) {
      expect(digits(v, 5), String(v)).toBeNull()
    }
  })
})

describe('buildPayload', () => {
  it('picks only the eleven fields under issue', () => {
    const out = buildPayload('created', webhookIssue())
    expect(out).toEqual({
      event_type: 'sentry-issue',
      client_payload: {
        issue: {
          id: '1234567890',
          shortId: 'NIBFIN-WEBSITE-1A',
          action: 'created',
          level: 'error',
          title: "TypeError: Cannot read properties of undefined (reading 'map')",
          culprit: 'renderDiagram(src/components/diagram)',
          permalink: 'https://na-43k.sentry.io/issues/1234567890/',
          count: '3',
          userCount: 2,
          firstSeen: '2026-11-27T01:02:03.456000Z',
          lastSeen: '2026-11-27T04:05:06.789000Z',
        },
      },
    })
  })

  it('gives the same result for numeric fields sent as strings or numbers', () => {
    const asStrings = buildPayload('created', webhookIssue({ id: '77', count: '9', userCount: '4' }))
    const asNumbers = buildPayload('created', webhookIssue({ id: 77, count: 9, userCount: 4 }))
    expect(asStrings).not.toBeNull()
    expect(asStrings).toEqual(asNumbers)
    expect(asNumbers?.client_payload.issue).toMatchObject({ id: '77', count: '9', userCount: 4 })
  })

  it('rejects malformed numeric fields', () => {
    for (const bad of [{ id: -1 }, { id: '12a' }, { id: '1'.repeat(21) }, { count: 1.5 }, { count: null }, { userCount: '-2' }, { userCount: '9'.repeat(16) }, { userCount: true }]) {
      expect(buildPayload('created', webhookIssue(bad)), JSON.stringify(bad)).toBeNull()
    }
  })

  it('rejects other malformed fields and actions', () => {
    for (const bad of [{ shortId: 'lower-case' }, { level: 'loud' }, { title: 3 }, { permalink: 'http://na-43k.sentry.io/x', web_url: undefined }, { permalink: 'https://sentry.io.evil.example/x', web_url: undefined }, { permalink: 'https://na-43k.sentry.io/a b', web_url: undefined }, { permalink: 'https://na-43k.sentry.io/a>', web_url: undefined }, { firstSeen: 'yesterday' }, { lastSeen: '2026-11-27T04:05:06+08:00' }]) {
      expect(buildPayload('created', webhookIssue(bad)), JSON.stringify(bad)).toBeNull()
    }
    expect(buildPayload('resolved', webhookIssue())).toBeNull()
    expect(buildPayload('created', 'not an object')).toBeNull()
  })

  it('falls back to web_url and turns a missing culprit into an empty string', () => {
    const out = buildPayload('unresolved', webhookIssue({ permalink: undefined, culprit: null }))
    expect(out?.client_payload.issue).toMatchObject({ action: 'unresolved', permalink: 'https://na-43k.sentry.io/issues/1234567890/?web', culprit: '' })
  })

  it('caps title and culprit and stays under the dispatch size limit', () => {
    const long = '`'.repeat(5000) + '🙂'.repeat(5000)
    const out = buildPayload('created', webhookIssue({ title: long, culprit: long }))
    expect(out).not.toBeNull()
    const issue = out!.client_payload.issue
    expect(Array.from(issue.title)).toHaveLength(MAX_TEXT)
    expect(issue.title.endsWith(TRUNCATED)).toBe(true)
    expect(Array.from(issue.culprit)).toHaveLength(MAX_TEXT)
    expect(encodedSize(out)).toBeLessThan(MAX_DISPATCH_BYTES)
  })

  it('keeps the encoded body under the limit with every field at its cap', () => {
    const wide = '\u0001'.repeat(MAX_TEXT) // JSON escapes each as six bytes, the widest form
    const out = buildPayload('created', webhookIssue({ id: '9'.repeat(20), shortId: 'A'.repeat(64), title: wide, culprit: wide, permalink: `https://sentry.io/${'a'.repeat(MAX_TEXT - 18)}`, count: '9'.repeat(15), userCount: 999_999_999_999_999 }))
    expect(out).not.toBeNull()
    expect(encodedSize(out)).toBeLessThan(MAX_DISPATCH_BYTES)
  })
})

describe('cap', () => {
  it('leaves short text alone', () => {
    expect(cap('short')).toBe('short')
  })
})

describe('parsePayload', () => {
  const good = () => buildPayload('created', webhookIssue())!.client_payload

  it('round-trips a buildPayload result', () => {
    expect(parsePayload(good())).toEqual(good().issue)
  })

  it('rejects each malformed field', () => {
    const cases: Record<string, unknown> = {
      id: 1234567890,
      shortId: 'a',
      action: 'resolved',
      level: 'loud',
      title: 'x'.repeat(MAX_TEXT + 1),
      culprit: null,
      permalink: 'https://example.com/',
      count: 3,
      userCount: '2',
      firstSeen: 'never',
      lastSeen: 12,
    }
    for (const [field, value] of Object.entries(cases)) {
      const p = good()
      ;(p.issue as unknown as Record<string, unknown>)[field] = value
      const err = parsePayload(p)
      expect(err, field).toBeInstanceOf(Error)
      expect((err as Error).message).toBe(`issue.${field}: missing or malformed`)
    }
  })

  it('rejects extra keys and non-objects', () => {
    expect(parsePayload({ ...good(), more: 1 })).toBeInstanceOf(Error)
    expect(parsePayload({ issue: { ...good().issue, extra: 1 } })).toBeInstanceOf(Error)
    expect(parsePayload(null)).toBeInstanceOf(Error)
    expect(parsePayload({ issue: [] })).toBeInstanceOf(Error)
  })
})
