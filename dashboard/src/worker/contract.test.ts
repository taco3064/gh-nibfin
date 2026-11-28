// The per-repository data is shaped by the status --json schema: the
// status.RepoView keys about Issues, validated against the committed file.

import Ajv2020 from 'ajv/dist/2020'
import { describe, expect, it } from 'vitest'

import schemaFile from '../../../schema/status.schema.json'
import { createGitHub } from './github'
import type { RepoData } from './messages'
import { check } from './poller'
import { initialState } from './state'
import { FakeGitHub, issueJSON, issuesPath, runJSON, runsPath, token, quick } from './testing'

type Schema = { $defs: Record<string, { properties: Record<string, unknown> }> }
const defs = (schemaFile as unknown as Schema).$defs
const view = defs['status.RepoView'].properties

const ajv = new Ajv2020({ strict: false })
const validate = ajv.compile({
  $defs: defs,
  type: 'object',
  properties: { repo: view.repo, issues: view.issues, issuesError: view.issuesError },
  required: ['repo', 'issues'],
  additionalProperties: false,
})

const cli = 'taco3064/gh-nibfin'
const t0 = Date.parse('2026-11-25T00:00:00Z')

async function built(fake: FakeGitHub): Promise<RepoData[]> {
  const { state } = await check(initialState(), createGitHub({ token, fetch: fake.fetch, ...quick }), t0)
  return state.data?.repos ?? []
}

function expectValid(v: unknown): void {
  const ok = validate(v)
  expect(validate.errors ?? []).toEqual([])
  expect(ok).toBe(true)
}

describe('repo data against schema/status.schema.json', () => {
  it('validates Issues with blockers, runs, runsTruncated and blockedByError', async () => {
    const fake = new FakeGitHub()
    fake.set(issuesPath(cli), [
      issueJSON(1, { labels: [{ name: 'pending' }] }),
      issueJSON(2, { created_at: '2026-11-01T00:00:00Z' }),
      issueJSON(3, { labels: [{ name: 'request' }] }),
    ])
    fake.blockers['taco3064/gh-nibfin#1'] = [
      { repo: 'taco3064/nibfin-core', number: 4, title: 'Open', state: 'OPEN', url: 'https://github.com/taco3064/nibfin-core/issues/4' },
      { repo: cli, number: 3, title: 'Closed', state: 'CLOSED', url: 'https://github.com/taco3064/gh-nibfin/issues/3' },
    ]
    fake.graphqlErrors = [{ message: 'boom', path: ['r0', 'i2'] }]
    fake.set(runsPath(cli, 'nibfin-issue.yml'), {
      workflow_runs: Array.from({ length: 100 }, (_, i) =>
        runJSON(`Nibfin · ${cli}#${(i % 2) + 1} · shape`, `2026-11-2${i % 3}T00:00:00Z`, { conclusion: i % 5 ? 'success' : null }),
      ),
    })
    const repos = await built(fake)
    const repo = repos.find((r) => r.repo === `github.com/${cli}`)
    const [one, two, three] = repo?.issues ?? []
    expect(one.blockedBy).toHaveLength(2)
    expect(one.pending).toBe(true)
    expect(two.runsTruncated).toBe(true)
    expect(three.blockedByError).toBeDefined()
    expect(three.labels).toEqual(['request'])
    expect(one.runs?.length).toBeGreaterThan(0)
    for (const r of repos) expectValid(r)
  })

  it('validates a failed Issue read', async () => {
    const fake = new FakeGitHub()
    fake.fail(issuesPath(cli), 403, 'Resource not accessible')
    fake.fail(runsPath(cli, 'nibfin-issue.yml'), 500, 'x')
    const repos = await built(fake)
    const repo = repos.find((r) => r.repo === `github.com/${cli}`)
    expect(repo?.issuesError).toBeDefined()
    expect(repo?.issues).toEqual([])
    for (const r of repos) expectValid(r)
  })

  it('rejects an extra key and an Issue without runs', async () => {
    const fake = new FakeGitHub()
    fake.set(issuesPath(cli), [issueJSON(1)])
    const repo = (await built(fake)).find((r) => r.repo === `github.com/${cli}`) as RepoData
    expectValid(repo)
    expect(validate({ ...repo, extra: true })).toBe(false)
    const { runs: _runs, ...noRuns } = repo.issues?.[0] ?? {}
    expect(validate({ ...repo, issues: [noRuns] })).toBe(false)
  })
})

// The Summary is Worker-only: status --json has none. Its schema is the
// Summary type of messages.ts, written out here.
const apiError = {
  type: 'object',
  properties: { code: { type: 'string' }, message: { type: 'string' } },
  required: ['code', 'message'],
  additionalProperties: false,
}
const validateSummary = ajv.compile({
  type: 'object',
  properties: {
    since: { const: '2026-11-21T00:00:00+08:00' },
    created: { type: ['integer', 'null'], minimum: 0 },
    delivered: { type: ['integer', 'null'], minimum: 0 },
    days: {
      type: 'array',
      minItems: 7,
      maxItems: 7,
      items: {
        type: 'object',
        properties: { date: { type: 'string', pattern: '^\\d{4}-\\d{2}-\\d{2}$' }, delivered: { type: 'integer', minimum: 0 } },
        required: ['date', 'delivered'],
        additionalProperties: false,
      },
    },
    error: apiError,
  },
  required: ['since', 'created', 'delivered'],
  additionalProperties: false,
})

describe('Summary against the Summary type of messages.ts', () => {
  async function summaryOf(fake: FakeGitHub) {
    const { state } = await check(initialState(), createGitHub({ token, fetch: fake.fetch, ...quick }), t0)
    return state.data?.summary
  }

  it('validates a Summary with days', async () => {
    const fake = new FakeGitHub()
    fake.trend.items = [{ html_url: 'https://github.com/x/y/issues/1', closed_at: '2026-11-24T01:00:00Z' }]
    const s = await summaryOf(fake)
    expect(s?.days).toHaveLength(7)
    expect(validateSummary(s)).toBe(true)
    expect(validateSummary({ ...s, days: s?.days?.slice(1) })).toBe(false)
    expect(validateSummary({ ...s, days: s?.days?.map((d) => ({ ...d, title: 'x' })) })).toBe(false)
  })

  it('validates a Summary without days after a failed trend read', async () => {
    const fake = new FakeGitHub()
    fake.trend.failPage = { page: 1, status: 500 }
    const s = await summaryOf(fake)
    expect(s?.days).toBeUndefined()
    expect(s?.error).toBeDefined()
    expect(validateSummary(s)).toBe(true)
  })
})
