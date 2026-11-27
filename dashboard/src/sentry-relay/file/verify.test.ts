import { describe, expect, it } from 'vitest'

import { Fake, frame, GITHUB, REPO, SENTRY, SHA, sentryEvent, sentryIssue } from './fake.ts'
import { createClient } from './http.ts'
import { normalizePath, verify } from './verify.ts'

const sleep = async () => {}

function setup(fake: Fake, token = true) {
  const github = createClient({ base: GITHUB, token: 'gh', fetch: fake.fetch, sleep })
  const sentry = token ? createClient({ base: SENTRY, token: 'st', fetch: fake.fetch, sleep }) : undefined
  return { repository: REPO, github, sentry }
}

describe('normalizePath', () => {
  it.each([
    ['../../src/components/diagram.tsx', 'dashboard/src/components/diagram.tsx'],
    ['./src/lib/format.ts', 'dashboard/src/lib/format.ts'],
    ['src/lib/format.ts', 'dashboard/src/lib/format.ts'],
    ['webpack://gh-nibfin-dashboard/src/lib/format.ts', 'dashboard/src/lib/format.ts'],
    ['webpack:///./src/lib/format.ts', 'dashboard/src/lib/format.ts'],
    ['app:///gh-nibfin/src/lib/format.ts', 'dashboard/src/lib/format.ts'],
    ['~/gh-nibfin/src/lib/format.ts', 'dashboard/src/lib/format.ts'],
    ['dashboard/src/lib/format.ts', 'dashboard/src/lib/format.ts'],
  ])('maps %s', (input, want) => {
    expect(normalizePath(input)).toBe(want)
  })

  it.each([
    'https://taco3064.github.io/gh-nibfin/_astro/index.abc.js',
    'http://localhost/src/x.ts',
    '../node_modules/react/index.js',
    'src/../../etc/passwd',
    'src//x.ts',
    'src/a b.ts',
    'dashboard/internal/x.go',
    '',
  ])('rejects %s', (input) => {
    expect(normalizePath(input)).toBeNull()
  })
})

describe('verify', () => {
  it('verifies a release with a green run and resolved frames', async () => {
    const fake = new Fake()
    fake.event = sentryEvent([frame('../../src/components/diagram.tsx', 42), frame('../../src/lib/format.ts', 7), frame('../../src/components/diagram.tsx', 42)])
    expect(await verify(sentryIssue(), setup(fake))).toEqual({
      verified: true,
      release: SHA,
      frames: [
        { path: 'dashboard/src/components/diagram.tsx', line: 42, column: 5 },
        { path: 'dashboard/src/lib/format.ts', line: 7, column: 5 },
      ],
    })
    expect(fake.requests.find((r) => r.url.startsWith(SENTRY))).toMatchObject({ url: `${SENTRY}organizations/na-43k/issues/1234567890/events/latest/`, auth: 'Bearer st' })
  })

  it('accepts a release given as a plain string', async () => {
    const fake = new Fake()
    fake.event = sentryEvent([frame('src/lib/format.ts')], { release: SHA })
    expect((await verify(sentryIssue(), setup(fake))).verified).toBe(true)
  })

  const unverified: [string, (f: Fake) => void, string][] = [
    ['no release', (f) => (f.event = sentryEvent([frame('src/lib/format.ts')], { release: null })), 'release: missing or not a full commit SHA'],
    ['a short release', (f) => (f.event = sentryEvent([frame('src/lib/format.ts')], { release: { version: 'abc1234' } })), 'release: missing or not a full commit SHA'],
    ['a named release', (f) => (f.event = sentryEvent([frame('src/lib/format.ts')], { release: { version: 'website@1.0.0' } })), 'release: missing or not a full commit SHA'],
    ['no green Website run', (f) => f.greenRuns.clear(), 'release: no successful Website run on main'],
    ['a source map error', (f) => (f.event = sentryEvent([frame('src/lib/format.ts')], { errors: [{ type: 'js_invalid_sourcemap_location' }] })), 'frames: source map processing error js_invalid_sourcemap_location'],
    ['no in-app frame', (f) => (f.event = sentryEvent([])), 'frames: no in-app frame'],
    ['an unresolved frame (raw URL)', (f) => (f.event = sentryEvent([frame('src/lib/format.ts'), frame('https://taco3064.github.io/gh-nibfin/_astro/index.abc123.js')])), 'frames: 1 not resolved to dashboard/src/'],
    ['a frame equal to its raw frame', (f) => (f.event = sentryEvent([frame('src/lib/format.ts', 3, 'src/lib/format.ts')])), 'frames: 0 not resolved to dashboard/src/'],
    ['a frame outside the repository', (f) => (f.event = sentryEvent([frame('../node_modules/react/index.js')])), 'frames: 0 not resolved to dashboard/src/'],
    ['a frame without a line', (f) => (f.event = sentryEvent([frame('src/lib/format.ts', 0)])), 'frames: 0 not resolved to dashboard/src/'],
    ['a path missing at the release', (f) => (f.event = sentryEvent([frame('src/lib/gone.ts')])), `frames: dashboard/src/lib/gone.ts not in the repository at ${SHA.slice(0, 7)}`],
    ['a truncated tree', (f) => f.trees.set(SHA, { paths: ['dashboard/src/lib/format.ts'], truncated: true }), 'frames: tree truncated'],
    ['a Sentry API error', (f) => (f.sentryStatus = 403), 'sentry api: HTTP 403'],
    ['an unexpected Sentry answer', (f) => (f.event = []), 'sentry api: unexpected answer'],
  ]

  it.each(unverified)('is unverified with %s', async (_, arrange, failed) => {
    const fake = new Fake()
    arrange(fake)
    const v = await verify(sentryIssue(), setup(fake))
    expect(v.verified).toBe(false)
    expect(v.verified ? '' : v.failed).toBe(failed)
  })

  it('is unverified without a Sentry token and calls nothing', async () => {
    const fake = new Fake()
    expect(await verify(sentryIssue(), setup(fake, false))).toEqual({ verified: false, failed: 'sentry token: not set' })
    expect(fake.requests).toHaveLength(0)
  })

  it('retries a Sentry error before giving up and never shows its body', async () => {
    const fake = new Fake()
    fake.sentryStatus = 500
    const v = await verify(sentryIssue(), setup(fake))
    expect(fake.requests.filter((r) => r.url.startsWith(SENTRY))).toHaveLength(4)
    expect(JSON.stringify(v)).not.toContain('secret detail')
  })

  it('turns a GitHub failure into unverified instead of throwing', async () => {
    const fake = new Fake()
    fake.trees.clear()
    const v = await verify(sentryIssue(), setup(fake))
    expect(v).toMatchObject({ verified: false, failed: `check failed: GET repos/${REPO}/git/trees/${SHA}?recursive=1: HTTP 404` })
  })
})
