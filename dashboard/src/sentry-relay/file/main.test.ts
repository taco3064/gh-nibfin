import { describe, expect, it } from 'vitest'

import { Fake, REPO, sentryIssue } from './fake.ts'
import { main } from './main.ts'

function run(fake: Fake, payload: unknown, env: Record<string, string | undefined> = {}) {
  const lines: string[] = []
  const code = main({
    env: {
      GITHUB_REPOSITORY: REPO,
      GITHUB_RUN_ID: '77',
      GITHUB_EVENT_PATH: '/event.json',
      NIBFIN_GITHUB_TOKEN: 'gh-token',
      SENTRY_API_TOKEN: 'sentry-token',
      ...env,
    },
    readFile: (path) => {
      if (path !== '/event.json') throw new Error('no such file')
      return JSON.stringify({ action: 'sentry-issue', client_payload: payload })
    },
    summary: (line) => lines.push(line),
    fetch: fake.fetch,
    sleep: async () => {},
    now: () => Date.parse('2026-11-27T10:00:00Z'),
  })
  return code.then((c) => ({ code: c, lines }))
}

describe('main', () => {
  it('files from the event file and writes one summary line', async () => {
    const fake = new Fake()
    const { code, lines } = await run(fake, { issue: sentryIssue() })
    expect(code).toBe(0)
    expect(lines).toEqual([`#500 https://github.com/${REPO}/issues/500 filed; verified`])
    expect(fake.issues[0].body.startsWith('<!-- nibfin-sentry-run: 77 -->\n')).toBe(true)
  })

  it('fails a bad payload before any request and names the field', async () => {
    for (const [payload, reason] of [
      [{ issue: { ...sentryIssue(), shortId: 'x y' } }, 'issue.shortId'],
      [{ issue: { ...sentryIssue(), userCount: '3' } }, 'issue.userCount'],
      [{ issue: sentryIssue(), extra: true }, 'unexpected key'],
      [undefined, 'client_payload: not an object'],
    ] as const) {
      const fake = new Fake()
      const { code, lines } = await run(fake, payload)
      expect(code).toBe(1)
      expect(lines).toHaveLength(1)
      expect(lines[0]).toContain(reason)
      expect(fake.requests).toHaveLength(0)
    }
  })

  it('fails without NIBFIN_GITHUB_TOKEN, a run id or an event file before any request', async () => {
    for (const env of [{ NIBFIN_GITHUB_TOKEN: '' }, { GITHUB_RUN_ID: 'abc' }, { GITHUB_EVENT_PATH: '/missing.json' }, { GITHUB_REPOSITORY: 'nope' }]) {
      const fake = new Fake()
      const { code } = await run(fake, { issue: sentryIssue() }, env)
      expect(code).toBe(1)
      expect(fake.requests).toHaveLength(0)
    }
  })

  it('files unverified without SENTRY_API_TOKEN', async () => {
    const fake = new Fake()
    const { code, lines } = await run(fake, { issue: sentryIssue() }, { SENTRY_API_TOKEN: undefined })
    expect(code).toBe(0)
    expect(lines[0]).toContain('unverified: sentry token: not set')
    expect(fake.issues[0].labels).toEqual(['request', 'pause'])
  })

  it('fails the run on a GitHub error without leaking the token', async () => {
    const fake = new Fake()
    fake.fault('GET issues', 'before', 'before', 'before', 'before')
    const { code, lines } = await run(fake, { issue: sentryIssue() })
    expect(code).toBe(1)
    expect(lines[0]).toBe(`not filed: GET repos/${REPO}/issues?state=open&labels=request&per_page=100: HTTP 502`)
    expect(lines[0]).not.toContain('gh-token')
  })

  it('fails the run on a read-back mismatch', async () => {
    const fake = new Fake()
    const original = fake.fetch
    fake.fetch = async (input, init) => {
      const res = await original(input, init)
      for (const i of fake.issues) i.assignees = []
      return res
    }
    const { code, lines } = await run(fake, { issue: sentryIssue() })
    expect(code).toBe(1)
    expect(lines[0]).toContain('read-back failed: not assigned to taco3064')
  })
})
