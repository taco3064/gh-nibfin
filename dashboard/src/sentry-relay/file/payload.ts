// The repository_dispatch contract between the Sentry relay Worker and the
// filer. The Worker builds it from a Sentry `issue` webhook; the filer parses
// it again before use. Plain TypeScript with no runtime-specific types, so
// both the Worker and Node import it.

export const EVENT_TYPE = 'sentry-issue'

// GitHub's limit is "less than 64KB"; the caps below keep the body far under it.
export const MAX_DISPATCH_BYTES = 60_000

export const MAX_TEXT = 512
export const TRUNCATED = '… (truncated)'

export const ACTIONS = ['created', 'unresolved'] as const
export const LEVELS = ['fatal', 'error', 'warning', 'info', 'debug', 'sample'] as const

export type Action = (typeof ACTIONS)[number]
export type Level = (typeof LEVELS)[number]

export interface SentryIssue {
  id: string
  shortId: string
  action: Action
  level: Level
  title: string
  culprit: string
  permalink: string
  count: string
  userCount: number
  firstSeen: string
  lastSeen: string
}

export interface SentryDispatch {
  event_type: typeof EVENT_TYPE
  client_payload: { issue: SentryIssue }
}

const FIELDS = ['id', 'shortId', 'action', 'level', 'title', 'culprit', 'permalink', 'count', 'userCount', 'firstSeen', 'lastSeen'] as const

const SHORT_ID = /^[A-Z0-9][A-Z0-9-]{0,63}$/
const ISO_UTC = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?(Z|\+00:00)$/
const ID_MAX = 20
const COUNT_MAX = 15

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

// digits accepts a digit string or a non-negative safe integer and returns its
// decimal string, or null. Sentry's docs show id and count as strings and
// userCount as a number only in an example, so either form is accepted.
export function digits(value: unknown, maxLen: number): string | null {
  let s: string
  if (typeof value === 'number') {
    if (!Number.isSafeInteger(value) || value < 0) return null
    s = String(value)
  } else if (typeof value === 'string' && /^[0-9]+$/.test(value)) {
    s = value
  } else {
    return null
  }
  return s.length <= maxLen ? s : null
}

// cap shortens text over MAX_TEXT characters and marks the cut.
export function cap(s: string): string {
  const chars = Array.from(s)
  if (chars.length <= MAX_TEXT) return s
  return chars.slice(0, MAX_TEXT - Array.from(TRUNCATED).length).join('') + TRUNCATED
}

function textLength(s: string): number {
  return Array.from(s).length
}

// isSentryURL accepts an https URL on sentry.io or one of its subdomains.
export function isSentryURL(s: string): boolean {
  // Only URL characters with no space, angle bracket or backtick, so the
  // filer can put it in a Markdown autolink as it is.
  if (s.length > MAX_TEXT || !/^[A-Za-z0-9._~:/?#[\]@!$&'()*+,;=%-]+$/.test(s)) return false
  let u: URL
  try {
    u = new URL(s)
  } catch {
    return false
  }
  if (u.protocol !== 'https:' || u.username !== '' || u.password !== '' || u.port !== '') return false
  return u.hostname === 'sentry.io' || u.hostname.endsWith('.sentry.io')
}

function isTimestamp(v: unknown): v is string {
  return typeof v === 'string' && v.length <= 40 && ISO_UTC.test(v) && !Number.isNaN(Date.parse(v))
}

function oneOf<T extends string>(list: readonly T[], v: unknown): v is T {
  return typeof v === 'string' && (list as readonly string[]).includes(v)
}

// buildPayload picks the dispatch fields from a webhook's data.issue, or
// returns null when one of them is missing or malformed.
export function buildPayload(action: unknown, issue: unknown): SentryDispatch | null {
  if (!isRecord(issue) || !oneOf(ACTIONS, action)) return null
  const id = digits(issue.id, ID_MAX)
  const count = digits(issue.count, COUNT_MAX)
  const users = digits(issue.userCount, COUNT_MAX)
  const userCount = users === null ? NaN : Number(users)
  const permalink = typeof issue.permalink === 'string' ? issue.permalink : issue.web_url
  const culprit = issue.culprit ?? ''
  if (
    id === null ||
    count === null ||
    !Number.isSafeInteger(userCount) ||
    typeof issue.shortId !== 'string' ||
    !SHORT_ID.test(issue.shortId) ||
    !oneOf(LEVELS, issue.level) ||
    typeof issue.title !== 'string' ||
    typeof culprit !== 'string' ||
    typeof permalink !== 'string' ||
    !isSentryURL(permalink) ||
    !isTimestamp(issue.firstSeen) ||
    !isTimestamp(issue.lastSeen)
  ) {
    return null
  }
  const out: SentryDispatch = {
    event_type: EVENT_TYPE,
    client_payload: {
      issue: {
        id,
        shortId: issue.shortId,
        action,
        level: issue.level,
        title: cap(issue.title),
        culprit: cap(culprit),
        permalink,
        count,
        userCount,
        firstSeen: issue.firstSeen,
        lastSeen: issue.lastSeen,
      },
    },
  }
  return encodedSize(out) < MAX_DISPATCH_BYTES ? out : null
}

export function encodedSize(v: unknown): number {
  return new TextEncoder().encode(JSON.stringify(v)).length
}

// parsePayload checks a dispatch's client_payload field by field. It accepts
// only the normalized form buildPayload produces.
export function parsePayload(clientPayload: unknown): SentryIssue | Error {
  if (!isRecord(clientPayload)) return new Error('client_payload: not an object')
  const extra = Object.keys(clientPayload).filter((k) => k !== 'issue')
  if (extra.length > 0) return new Error(`client_payload: unexpected key ${JSON.stringify(extra[0]).slice(0, 80)}`)
  const issue = clientPayload.issue
  if (!isRecord(issue)) return new Error('issue: not an object')
  const unknownKey = Object.keys(issue).find((k) => !(FIELDS as readonly string[]).includes(k))
  if (unknownKey !== undefined) return new Error(`issue: unexpected key ${JSON.stringify(unknownKey).slice(0, 80)}`)
  const bad = (field: string) => new Error(`issue.${field}: missing or malformed`)
  if (typeof issue.id !== 'string' || digits(issue.id, ID_MAX) === null) return bad('id')
  if (typeof issue.shortId !== 'string' || !SHORT_ID.test(issue.shortId)) return bad('shortId')
  if (!oneOf(ACTIONS, issue.action)) return bad('action')
  if (!oneOf(LEVELS, issue.level)) return bad('level')
  if (typeof issue.title !== 'string' || textLength(issue.title) > MAX_TEXT) return bad('title')
  if (typeof issue.culprit !== 'string' || textLength(issue.culprit) > MAX_TEXT) return bad('culprit')
  if (typeof issue.permalink !== 'string' || !isSentryURL(issue.permalink)) return bad('permalink')
  if (typeof issue.count !== 'string' || digits(issue.count, COUNT_MAX) === null) return bad('count')
  if (typeof issue.userCount !== 'number' || !Number.isSafeInteger(issue.userCount) || issue.userCount < 0) return bad('userCount')
  if (!isTimestamp(issue.firstSeen)) return bad('firstSeen')
  if (!isTimestamp(issue.lastSeen)) return bad('lastSeen')
  return {
    id: issue.id,
    shortId: issue.shortId,
    action: issue.action,
    level: issue.level,
    title: issue.title,
    culprit: issue.culprit,
    permalink: issue.permalink,
    count: issue.count,
    userCount: issue.userCount,
    firstSeen: issue.firstSeen,
    lastSeen: issue.lastSeen,
  }
}
