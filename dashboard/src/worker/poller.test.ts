import { describe, expect, it } from 'vitest'

import { issueOfTitle, parseJobTitle } from './build'
import { createGitHub } from './github'
import { check, minInterval, missingWorkflowInterval } from './poller'
import { initialState, repos, type State } from './state'
import { FakeGitHub, inProgressPath, issueJSON, issuesPath, jobsPath, runJSON, runnersPath, runsPath, searchPathOf, token, quick } from './testing'

const core = 'taco3064/nibfin-core'
const cli = 'taco3064/gh-nibfin'
const t0 = Date.parse('2026-11-25T00:00:00Z')
// The 7 Taipei days that end on 25 Nov, the Taipei date of t0, without a delivery.
const noDays = ['19', '20', '21', '22', '23', '24', '25'].map((d) => ({ date: `2026-11-${d}`, delivered: 0 }))

async function run(fake: FakeGitHub, state: State, now: number, budget?: number) {
  fake.reset()
  return check(state, createGitHub({ token, fetch: fake.fetch, budget, ...quick }), now)
}

async function first(fake: FakeGitHub) {
  return run(fake, initialState(), t0)
}

function issuesOf(state: State, repo: string) {
  return state.data?.repos.find((r) => r.repo === `github.com/${repo}`)?.issues ?? []
}

describe('issueOfTitle and parseJobTitle', () => {
  it('read run titles as status does', () => {
    expect(issueOfTitle('Nibfin · Taco3064/GH-Nibfin#12 · deliver')).toBe('taco3064/gh-nibfin#12')
    expect(issueOfTitle('Nibfin · taco3064/gh-nibfin@nibfin/issue-354 · route')).toBe('')
    expect(issueOfTitle('Nibfin · taco3064/gh-nibfin#012 · shape')).toBe('')
    expect(issueOfTitle('CI')).toBe('')
  })

  it('read a job title into repo, Issue or ref, and stage', () => {
    expect(parseJobTitle('Nibfin · taco3064/gh-nibfin#355 · review', 'u')).toEqual({ title: 'Nibfin · taco3064/gh-nibfin#355 · review', repo: 'taco3064/gh-nibfin', issue: 355, ref: null, stage: 'review', url: 'u' })
    expect(parseJobTitle('Nibfin · taco3064/gh-nibfin@nibfin/issue-354 · route', 'u')).toMatchObject({ repo: 'taco3064/gh-nibfin', issue: null, ref: 'nibfin/issue-354', stage: 'route' })
    expect(parseJobTitle('CI', 'u')).toEqual({ title: 'CI', repo: null, issue: null, ref: null, stage: null, url: 'u' })
  })
})

describe('check', () => {
  it('reads everything on the first check', async () => {
    const fake = new FakeGitHub()
    fake.set(issuesPath(cli), [issueJSON(1, { labels: [{ name: 'Pause' }, { name: 'scope' }] }), issueJSON(2, { pull_request: {} })])
    const { state, changed, errors } = await first(fake)
    expect(errors).toEqual([])
    expect(changed).toBe(true)
    expect(state.seq).toBe(1)
    expect(state.at).toBe(t0)
    expect(state.okAt).toBe(t0)
    const gets = fake.gets()
    for (const repo of repos) {
      expect(gets).toContain(issuesPath(repo))
      expect(gets).toContain(runsPath(repo, 'nibfin-issue.yml'))
      expect(gets).toContain(runsPath(repo, 'nibfin-shared-runner.yml'))
      expect(gets).toContain(runnersPath(repo))
    }
    expect(fake.graphqlCalls()).toHaveLength(1)
    const is = issuesOf(state, cli)
    expect(is).toHaveLength(1)
    expect(is[0]).toMatchObject({ number: 1, labels: ['Pause', 'scope'], pause: true, pending: false, blockedBy: [], runs: [] })
    expect(state.data?.summary).toEqual({ since: '2026-11-21T00:00:00+08:00', created: 50, delivered: 42, days: noDays })
  })

  it('makes no follow-up and reports no change when every probe answers 304', async () => {
    const fake = new FakeGitHub()
    fake.set(issuesPath(cli), [issueJSON(1)])
    const one = await first(fake)
    const two = await run(fake, one.state, t0 + minInterval)
    expect(two.changed).toBe(false)
    expect(two.errors).toEqual([])
    expect(fake.graphqlCalls()).toHaveLength(0)
    const etagged = fake.requests.filter((r) => r.headers['If-None-Match'])
    // Every conditional read sent its ETag; only the three searches, whose
    // answers carried none, send none.
    expect(fake.requests.length - etagged.length).toBe(3)
    expect(two.state.seq).toBe(1)
    expect(two.state.okAt).toBe(t0 + minInterval)
  })

  it('reads the Issues and the blockers again after an open-Issues 200', async () => {
    const fake = new FakeGitHub()
    fake.set(issuesPath(cli), [issueJSON(1)])
    const one = await first(fake)
    fake.set(issuesPath(cli), [issueJSON(1), issueJSON(3)])
    fake.blockers['taco3064/gh-nibfin#3'] = [{ repo: core, number: 9, title: 'Core', state: 'OPEN', url: 'https://github.com/taco3064/nibfin-core/issues/9' }]
    const two = await run(fake, one.state, t0 + minInterval)
    expect(two.changed).toBe(true)
    expect(fake.graphqlCalls()).toHaveLength(1)
    expect(issuesOf(two.state, cli).map((i) => i.number)).toEqual([1, 3])
    expect(issuesOf(two.state, cli)[1].blockedBy).toEqual([{ repo: core, number: 9, title: 'Core', state: 'OPEN', url: 'https://github.com/taco3064/nibfin-core/issues/9' }])
  })

  it('counts a page count change as a change', async () => {
    const fake = new FakeGitHub()
    fake.set(issuesPath(cli), Array.from({ length: 100 }, (_, i) => issueJSON(i + 1)), 'W/"p1"')
    fake.set(issuesPath(cli, 2), [issueJSON(101)], 'W/"p2"')
    const one = await first(fake)
    expect(issuesOf(one.state, cli)).toHaveLength(101)
    expect(one.state.repos[cli].issues.etags).toEqual(['W/"p1"', 'W/"p2"'])
    // The first page now holds 99 Issues under the same ETag: a fake
    // inconsistency that only the page count can show.
    fake.routes.set(issuesPath(cli), { status: 200, body: Array.from({ length: 99 }, (_, i) => issueJSON(i + 1)), etag: 'W/"p1b"' })
    const two = await run(fake, one.state, t0 + minInterval)
    expect(two.changed).toBe(true)
    expect(two.state.repos[cli].issues.result?.pages).toHaveLength(1)
    expect(fake.gets()).not.toContain(issuesPath(cli, 2))
  })

  it('attaches runs by title and marks a full page that ends after the Issue was created', async () => {
    const fake = new FakeGitHub()
    fake.set(issuesPath(cli), [issueJSON(5, { created_at: '2026-11-22T00:00:00Z' }), issueJSON(6, { created_at: '2026-11-24T12:00:00Z' })])
    fake.set(runsPath(core, 'nibfin-shared-runner.yml'), {
      workflow_runs: [
        runJSON('Nibfin · Taco3064/GH-Nibfin#5 · shape', '2026-11-23T00:00:00Z'),
        runJSON('Nibfin · taco3064/gh-nibfin#5 · deliver', '2026-11-24T00:00:00Z'),
        runJSON('Nibfin · taco3064/gh-nibfin@nibfin/issue-5 · route', '2026-11-24T01:00:00Z'),
        runJSON('Nibfin · taco3064/nibfin-core#5 · deliver', '2026-11-24T02:00:00Z'),
        runJSON('CI', '2026-11-24T03:00:00Z'),
        ...Array.from({ length: 95 }, (_, i) => runJSON(`Nibfin · taco3064/nibfin-core#${100 + i} · shape`, '2026-11-23T06:00:00Z')),
      ],
    })
    const { state } = await first(fake)
    const [five, six] = issuesOf(state, cli)
    expect(five.runs?.map((r) => r.name)).toEqual(['Nibfin · taco3064/gh-nibfin#5 · deliver', 'Nibfin · Taco3064/GH-Nibfin#5 · shape'])
    expect(five.runs?.[0]).toEqual({ repo: core, name: 'Nibfin · taco3064/gh-nibfin#5 · deliver', workflow: 'Nibfin shared runner', status: 'completed', conclusion: 'success', createdAt: '2026-11-24T00:00:00Z', url: expect.any(String) })
    // The page's oldest run (11-23) is newer than #5 (11-22): older runs
    // may exist. #6 was created after it, so its runs are complete.
    expect(five.runsTruncated).toBe(true)
    expect(six.runsTruncated).toBeUndefined()
    expect(six.runs).toEqual([])
  })

  it('treats a missing workflow as no runs and no error, probed again after ten minutes', async () => {
    const fake = new FakeGitHub()
    fake.routes.delete(runsPath(cli, 'nibfin-shared-runner.yml'))
    fake.set(issuesPath(cli), [issueJSON(1)])
    const one = await first(fake)
    expect(one.errors).toEqual([])
    expect(issuesOf(one.state, cli)[0].runsError).toBeUndefined()
    expect(one.state.repos[cli].runs['nibfin-shared-runner.yml'].due).toBe(t0 + missingWorkflowInterval)
    await run(fake, one.state, t0 + minInterval)
    expect(fake.gets()).not.toContain(runsPath(cli, 'nibfin-shared-runner.yml'))
  })

  it('maps a busy runner to its running job by the run title', async () => {
    const fake = new FakeGitHub()
    fake.set(runnersPath(core), {
      runners: [
        { id: 7, name: 'nibfin-1', status: 'online', busy: true },
        { id: 8, name: 'nibfin-2', status: 'online', busy: true },
        { id: 9, name: 'nibfin-3', status: 'offline', busy: false },
        { id: 10, name: 'nibfin-4', status: 'online', busy: true },
      ],
    })
    fake.set(inProgressPath(core), { workflow_runs: [{ id: 501 }, { id: 502 }] })
    fake.set(jobsPath(core, 501), { jobs: [{ runner_id: 7, status: 'in_progress', name: 'job / stage', workflow_name: 'Nibfin · taco3064/gh-nibfin#355 · review', html_url: 'https://github.com/j/1' }] }, null)
    fake.set(jobsPath(core, 502), {
      jobs: [
        { runner_id: 8, status: 'in_progress', name: 'job / route', workflow_name: 'Nibfin · taco3064/gh-nibfin@nibfin/issue-354 · route', html_url: 'https://github.com/j/2' },
        { runner_id: 10, status: 'completed', name: 'job / stage', workflow_name: 'Nibfin · taco3064/gh-nibfin#1 · shape', html_url: 'https://github.com/j/3' },
      ],
    }, null)
    const { state, errors } = await first(fake)
    expect(errors).toEqual([])
    const rs = state.data?.runners.find((r) => r.repo === core)?.runners ?? []
    expect(rs[0]).toEqual({ id: 7, status: 'online', busy: true, job: { title: 'Nibfin · taco3064/gh-nibfin#355 · review', repo: cli, issue: 355, ref: null, stage: 'review', url: 'https://github.com/j/1' } })
    expect(rs[1].job).toMatchObject({ repo: cli, issue: null, ref: 'nibfin/issue-354', stage: 'route' })
    expect(rs[2]).toEqual({ id: 9, status: 'offline', busy: false, job: null })
    // Runner names include host names: no built runner carries one.
    for (const r of rs) expect(Object.keys(r)).toEqual(['id', 'status', 'busy', 'job'])
    expect(rs[3].job).toBeNull()
    // No runner in the other repositories: no in-progress probe there.
    expect(fake.gets()).not.toContain(inProgressPath(cli))
  })

  it('reads the jobs again after an in-progress 200 without a runners change', async () => {
    const fake = new FakeGitHub()
    fake.set(runnersPath(core), { runners: [{ id: 7, name: 'nibfin-1', status: 'online', busy: true }] })
    fake.set(inProgressPath(core), { workflow_runs: [{ id: 501 }] })
    fake.set(jobsPath(core, 501), { jobs: [{ runner_id: 7, status: 'in_progress', workflow_name: 'Nibfin · taco3064/gh-nibfin#1 · shape', html_url: 'u1' }] }, null)
    const one = await first(fake)
    fake.set(inProgressPath(core), { workflow_runs: [{ id: 502 }] })
    fake.set(jobsPath(core, 502), { jobs: [{ runner_id: 7, status: 'in_progress', workflow_name: 'Nibfin · taco3064/gh-nibfin#2 · deliver', html_url: 'u2' }] }, null)
    const two = await run(fake, one.state, t0 + minInterval)
    expect(fake.gets()).toContain(jobsPath(core, 502))
    expect(two.changed).toBe(true)
    expect(two.state.data?.runners[0].runners[0].job).toMatchObject({ issue: 2, stage: 'deliver' })
  })

  it('delays only the probe that sent x-poll-interval, and a failed probe until Retry-After', async () => {
    const fake = new FakeGitHub()
    fake.set(runnersPath(cli), { runners: [] }, 'W/"r"', { 'x-poll-interval': '120' })
    fake.fail(issuesPath(core), 403, 'secondary rate limit', { 'retry-after': '90' })
    const one = await first(fake)
    expect(one.errors.map((e) => e.message)).toEqual([`GET ${issuesPath(core)}: HTTP 403: secondary rate limit`])
    expect(one.state.repos[cli].runners.due).toBe(t0 + 120_000)
    expect(one.state.repos[core].issues.due).toBe(t0 + 90_000)
    await run(fake, one.state, t0 + minInterval)
    expect(fake.gets()).not.toContain(runnersPath(cli))
    expect(fake.gets()).not.toContain(issuesPath(core))
    expect(fake.gets()).toContain(runnersPath(core))
    await run(fake, one.state, t0 + 120_000)
    expect(fake.gets()).toContain(runnersPath(cli))
    expect(fake.gets()).toContain(issuesPath(core))
  })

  it('keeps the previous result and ETag on a failure and retries on the next check', async () => {
    const fake = new FakeGitHub()
    fake.set(issuesPath(cli), [issueJSON(1)], 'W/"good"')
    const one = await first(fake)
    fake.fail(issuesPath(cli), 502, 'Server Error')
    const two = await run(fake, one.state, t0 + minInterval)
    expect(two.changed).toBe(false)
    expect(two.errors).toEqual([{ code: 'github_api_failed', message: `GET ${issuesPath(cli)}: HTTP 502: Server Error` }])
    expect(two.state.repos[cli].issues.etags).toEqual(['W/"good"'])
    expect(issuesOf(two.state, cli).map((i) => i.number)).toEqual([1])
    expect(two.state.okAt).toBe(t0)
    fake.set(issuesPath(cli), [issueJSON(1)], 'W/"good"')
    const three = await run(fake, two.state, t0 + 2 * minInterval)
    expect(fake.gets()).toContain(issuesPath(cli))
    expect(three.errors).toEqual([])
    expect(three.state.okAt).toBe(t0 + 2 * minInterval)
  })

  it('keeps old ETags past the request budget and reads them on the next check', async () => {
    const fake = new FakeGitHub()
    const one = await first(fake)
    for (const repo of repos) fake.set(issuesPath(repo), [issueJSON(4)])
    fake.set(runnersPath(cli), { runners: [{ id: 1, name: 'r', status: 'online', busy: false }] })
    const two = await run(fake, one.state, t0 + minInterval, 5)
    expect(fake.requests).toHaveLength(5)
    expect(two.errors.some((e) => e.message.includes('request budget of this check reached'))).toBe(true)
    expect(two.state.repos[cli].runners.etags).toEqual(one.state.repos[cli].runners.etags)
    // The issues read in budget wait for their blockers, so they are not
    // committed either.
    expect(two.state.repos[core].issues.etags).toEqual(one.state.repos[core].issues.etags)
    const three = await run(fake, two.state, t0 + 2 * minInterval)
    expect(three.errors).toEqual([])
    expect(three.state.data?.runners[1].runners).toHaveLength(1)
    for (const repo of repos) expect(issuesOf(three.state, repo).map((i) => i.number)).toEqual([4])
  })

  it('sets blockedByError from a GraphQL error path and retries it', async () => {
    const fake = new FakeGitHub()
    fake.set(issuesPath(cli), [issueJSON(1), issueJSON(2)])
    fake.graphqlErrors = [{ message: 'Something went wrong', path: ['r0', 'i1', 'blockedBy'] }]
    const one = await first(fake)
    const [a, b] = issuesOf(one.state, cli)
    expect(a.blockedByError).toBeUndefined()
    expect(b.blockedByError).toEqual({ code: 'github_api_failed', message: `read the blockers of ${cli}#2: Something went wrong` })
    fake.graphqlErrors = []
    const two = await run(fake, one.state, t0 + minInterval)
    expect(fake.graphqlCalls()).toHaveLength(1)
    expect(issuesOf(two.state, cli)[1].blockedByError).toBeUndefined()
    expect(two.changed).toBe(true)
  })

  it('keeps the old blockers and Issues when the blocker query fails', async () => {
    const fake = new FakeGitHub()
    fake.set(issuesPath(cli), [issueJSON(1)])
    const one = await first(fake)
    fake.set(issuesPath(cli), [issueJSON(1), issueJSON(2)])
    fake.graphqlStatus = 502
    const two = await run(fake, one.state, t0 + minInterval)
    expect(two.errors[0].message).toContain('POST graphql: HTTP 502')
    expect(issuesOf(two.state, cli).map((i) => i.number)).toEqual([1])
    fake.graphqlStatus = 200
    const three = await run(fake, two.state, t0 + 2 * minInterval)
    expect(issuesOf(three.state, cli).map((i) => i.number)).toEqual([1, 2])
    expect(three.errors).toEqual([])
  })

  it('retries a failed jobs read after its probes answer 304 again and clears the error', async () => {
    const fake = new FakeGitHub()
    const idle = { runners: [{ id: 7, name: 'nibfin-1', status: 'online', busy: false }] }
    fake.set(runnersPath(core), idle, 'W/"idle"')
    fake.set(inProgressPath(core), { workflow_runs: [] }, 'W/"none"')
    const one = await first(fake)
    expect(one.errors).toEqual([])
    // A runner goes busy and its jobs read fails.
    fake.set(runnersPath(core), { runners: [{ id: 7, name: 'nibfin-1', status: 'online', busy: true }] }, 'W/"busy"')
    fake.set(inProgressPath(core), { workflow_runs: [{ id: 501 }] }, 'W/"one"')
    fake.fail(jobsPath(core, 501), 502, 'Bad Gateway')
    const two = await run(fake, one.state, t0 + minInterval)
    expect(two.errors.map((e) => e.message).join()).toContain('HTTP 502')
    expect(two.state.okAt).toBe(t0)
    // The job finished: both probes are back to their old contents (304).
    fake.set(runnersPath(core), idle, 'W/"idle"')
    fake.set(inProgressPath(core), { workflow_runs: [] }, 'W/"none"')
    const three = await run(fake, two.state, t0 + 2 * minInterval)
    const probes = fake.requests.filter((r) => r.path === runnersPath(core) || r.path === inProgressPath(core))
    expect(probes.map((r) => r.headers['If-None-Match'])).toEqual(['W/"idle"', 'W/"none"'])
    expect(three.errors).toEqual([])
    expect(three.state.repos[core].jobs.lastError).toBeNull()
    expect(three.state.okAt).toBe(t0 + 2 * minInterval)
    expect(three.state.data?.runners.find((r) => r.repo === core)?.runners[0]).toMatchObject({ busy: false, job: null })
  })

  it('retries a failed blocker query after the lists answer 304 again and clears the error', async () => {
    const fake = new FakeGitHub()
    fake.set(issuesPath(cli), [issueJSON(1)], 'W/"l1"')
    const one = await first(fake)
    fake.set(issuesPath(cli), [issueJSON(1), issueJSON(2)], 'W/"l2"')
    fake.graphqlStatus = 502
    const two = await run(fake, one.state, t0 + minInterval)
    expect(two.errors[0].message).toContain('POST graphql: HTTP 502')
    expect(two.state.okAt).toBe(t0)
    // Issue 2 was closed again: the list is back to its old ETag (304).
    fake.set(issuesPath(cli), [issueJSON(1)], 'W/"l1"')
    fake.graphqlStatus = 200
    fake.blockers['taco3064/gh-nibfin#1'] = [{ repo: core, number: 9, title: 'Core', state: 'CLOSED', url: 'https://github.com/taco3064/nibfin-core/issues/9' }]
    const three = await run(fake, two.state, t0 + 2 * minInterval)
    expect(fake.requests.find((r) => r.path === issuesPath(cli))?.headers['If-None-Match']).toBe('W/"l1"')
    expect(fake.graphqlCalls()).toHaveLength(1)
    expect(three.errors).toEqual([])
    expect(three.state.blockers.lastError).toBeNull()
    expect(three.state.okAt).toBe(t0 + 2 * minInterval)
    expect(issuesOf(three.state, cli)[0].blockedBy).toHaveLength(1)
  })

  it('sets issuesError while the open Issues were never read', async () => {
    const fake = new FakeGitHub()
    fake.fail(issuesPath(cli), 404, 'Not Found')
    const { state } = await first(fake)
    const repo = state.data?.repos.find((r) => r.repo === `github.com/${cli}`)
    expect(repo).toEqual({ repo: `github.com/${cli}`, issues: [], issuesError: { code: 'github_api_failed', message: `GET ${issuesPath(cli)}: HTTP 404: Not Found` } })
  })

  it('makes no request and reports auth_unavailable without a token', async () => {
    const fake = new FakeGitHub()
    const res = await check(initialState(), createGitHub({ token: '', fetch: fake.fetch, ...quick }), t0)
    expect(fake.requests).toHaveLength(0)
    expect(res.errors).toEqual([{ code: 'auth_unavailable', message: 'the Worker has no GITHUB_TOKEN secret' }])
    expect(res.changed).toBe(false)
  })

  describe('Summary', () => {
    const created = 'repo:taco3064/nibfin-core repo:taco3064/gh-nibfin repo:taco3064/nibfin-e2e is:issue created:>=2026-11-21T00:00:00+08:00'
    const delivered = 'repo:taco3064/nibfin-core repo:taco3064/gh-nibfin repo:taco3064/nibfin-e2e is:issue is:closed label:delivered closed:>=2026-11-21T00:00:00+08:00'

    it('sends exactly the two queries, the delivered one without created:', async () => {
      const fake = new FakeGitHub()
      await first(fake)
      const qs = fake
        .gets()
        .filter((p) => p.startsWith('search/issues?per_page=1&'))
        .map((p) => new URLSearchParams(p.slice('search/issues?'.length)).get('q'))
      expect(qs).toEqual([created, delivered])
      expect(qs[1]).not.toContain('created:')
    })

    it('takes each count from its answer total_count', async () => {
      const fake = new FakeGitHub()
      fake.set(searchPathOf(created), { total_count: 7, items: [] }, null)
      fake.set(searchPathOf(delivered), { total_count: 3, items: [] }, null)
      const { state } = await first(fake)
      expect(state.data?.summary).toEqual({ since: '2026-11-21T00:00:00+08:00', created: 7, delivered: 3, days: noDays })
    })

    it('keeps the previous counts on a failure, and is null before the first success', async () => {
      const fake = new FakeGitHub()
      fake.fail(searchPathOf(delivered), 403, 'search rate limit')
      const one = await first(fake)
      expect(one.state.data?.summary).toMatchObject({ created: 50, delivered: null, error: { code: 'github_api_failed' } })
      fake.set(searchPathOf(delivered), { total_count: 42 }, null)
      const two = await run(fake, one.state, t0 + minInterval)
      expect(two.state.data?.summary).toEqual({ since: '2026-11-21T00:00:00+08:00', created: 50, delivered: 42, days: noDays })
      fake.fail(searchPathOf(created), 500, 'boom')
      const three = await run(fake, two.state, t0 + 2 * minInterval)
      expect(three.state.data?.summary).toMatchObject({ created: 50, delivered: 42, error: { message: expect.stringContaining('HTTP 500') } })
    })

    it('is null with an error before any success', async () => {
      const fake = new FakeGitHub()
      fake.fail(searchPathOf(created), 403, 'x')
      fake.fail(searchPathOf(delivered), 403, 'x')
      const { state } = await first(fake)
      expect(state.data?.summary).toMatchObject({ created: null, delivered: null, error: { code: 'github_api_failed' } })
    })
  })
})

describe('delivered trend', () => {
  // closed builds a trend item; n makes its URL unique.
  const closed = (n: number, at: string) => ({ html_url: `https://github.com/taco3064/gh-nibfin/issues/${n}`, closed_at: at })
  const trend = (state: State) => state.data?.summary.days
  const trendQ = 'repo:taco3064/nibfin-core repo:taco3064/gh-nibfin repo:taco3064/nibfin-e2e is:issue is:closed label:delivered closed:>=2026-11-19T00:00:00+08:00'

  it('reads the Issues closed since Taipei midnight 6 days ago and buckets them by Taipei day', async () => {
    const fake = new FakeGitHub()
    fake.trend.items = [
      closed(1, '2026-11-18T16:00:00Z'), // 19 Nov 00:00 in Taipei
      closed(2, '2026-11-20T03:00:00Z'),
      closed(3, '2026-11-20T15:59:59Z'), // still 20 Nov in Taipei
      closed(4, '2026-11-20T16:00:00Z'), // 21 Nov in Taipei
      closed(5, '2026-11-24T16:30:00Z'), // 25 Nov in Taipei
    ]
    const { state, errors } = await first(fake)
    expect(errors).toEqual([])
    expect(fake.trendQueries()).toEqual([trendQ])
    const path = fake.gets().find((p) => p.startsWith('search/issues?per_page=100&page=1'))
    expect(path).toContain('sort=created&order=asc')
    expect(trend(state)).toEqual([
      { date: '2026-11-19', delivered: 1 },
      { date: '2026-11-20', delivered: 2 },
      { date: '2026-11-21', delivered: 1 },
      { date: '2026-11-22', delivered: 0 },
      { date: '2026-11-23', delivered: 0 },
      { date: '2026-11-24', delivered: 0 },
      { date: '2026-11-25', delivered: 1 },
    ])
    // The stored result and the built data hold close times and counts only.
    expect(Object.keys(state.search.trend.result ?? {})).toEqual(['start', 'closedAt'])
    expect(JSON.stringify(state.data?.summary)).not.toContain('issues/')
  })

  it('puts 2026-11-26T16:30:00Z on 27 Nov', async () => {
    const fake = new FakeGitHub()
    fake.trend.items = [closed(1, '2026-11-26T16:30:00Z')]
    const now = Date.parse('2026-11-27T01:00:00Z')
    const { state } = await run(fake, initialState(), now)
    const days = trend(state) ?? []
    expect(days.map((d) => d.date)).toEqual(['2026-11-21', '2026-11-22', '2026-11-23', '2026-11-24', '2026-11-25', '2026-11-26', '2026-11-27'])
    expect(days.map((d) => d.delivered)).toEqual([0, 0, 0, 0, 0, 0, 1])
  })

  it('reads more than 100 Issues over two pages', async () => {
    const fake = new FakeGitHub()
    fake.trend.items = Array.from({ length: 130 }, (_, i) => closed(i + 1, i < 100 ? '2026-11-23T02:00:00Z' : '2026-11-24T02:00:00Z'))
    const { state, errors } = await first(fake)
    expect(errors).toEqual([])
    expect(fake.trendQueries()).toHaveLength(2)
    expect(fake.gets().some((p) => p.includes('per_page=100&page=2'))).toBe(true)
    expect(trend(state)?.map((d) => d.delivered)).toEqual([0, 0, 0, 0, 100, 30, 0])
  })

  it('fails without sending a page past the cap when more than 3 pages are needed', async () => {
    const fake = new FakeGitHub()
    fake.trend.items = Array.from({ length: 100 }, (_, i) => closed(i + 1, '2026-11-23T02:00:00Z'))
    fake.trend.total = 301
    const { state, errors } = await first(fake)
    expect(fake.trendQueries()).toHaveLength(1)
    expect(errors.map((e) => e.message)).toEqual(['read the delivered trend: 301 Issues need more than 3 pages'])
    expect(trend(state)).toBeUndefined()
    expect(state.data?.summary).toMatchObject({ created: 50, delivered: 42, error: { code: 'github_api_failed', message: 'read the delivered trend: 301 Issues need more than 3 pages' } })
  })

  it('fails on incomplete results and on a count that differs from total_count', async () => {
    const fake = new FakeGitHub()
    fake.trend.items = [closed(1, '2026-11-23T02:00:00Z')]
    fake.trend.incomplete = true
    const one = await first(fake)
    expect(one.errors.map((e) => e.message)).toEqual(['read the delivered trend: GitHub reported incomplete results'])
    expect(trend(one.state)).toBeUndefined()

    fake.trend = { items: [closed(1, '2026-11-23T02:00:00Z'), closed(1, '2026-11-23T02:00:00Z')] }
    const two = await first(fake)
    expect(two.errors.map((e) => e.message)).toEqual(['read the delivered trend: read 1 Issues, but total_count is 2'])
    expect(trend(two.state)).toBeUndefined()
  })

  it('keeps the stored result on a failed page, leaves days out and reads again next check', async () => {
    const fake = new FakeGitHub()
    fake.trend.items = [closed(1, '2026-11-23T02:00:00Z')]
    const one = await first(fake)
    expect(trend(one.state)?.[4]).toEqual({ date: '2026-11-23', delivered: 1 })
    fake.trend.items = Array.from({ length: 150 }, (_, i) => closed(i + 1, '2026-11-24T02:00:00Z'))
    fake.trend.failPage = { page: 2, status: 502 }
    const two = await run(fake, one.state, t0 + minInterval)
    expect(two.errors).toHaveLength(1)
    expect(two.errors[0].message).toContain('HTTP 502')
    expect(two.state.search.trend.result).toEqual(one.state.search.trend.result)
    expect(two.state.data?.summary.days).toBeUndefined()
    expect(two.state.data?.summary).toMatchObject({ created: 50, delivered: 42, error: { message: expect.stringContaining('HTTP 502') } })
    fake.trend.failPage = undefined
    const three = await run(fake, two.state, t0 + 2 * minInterval)
    expect(three.errors).toEqual([])
    expect(trend(three.state)?.[5]).toEqual({ date: '2026-11-24', delivered: 150 })
    expect(three.state.data?.summary.error).toBeUndefined()
  })

  it('names the count error before the trend error', async () => {
    const fake = new FakeGitHub()
    fake.fail(searchPathOf('repo:taco3064/nibfin-core repo:taco3064/gh-nibfin repo:taco3064/nibfin-e2e is:issue created:>=2026-11-21T00:00:00+08:00'), 403, 'rate limit')
    fake.trend.incomplete = true
    const { state } = await first(fake)
    expect(state.data?.summary.error?.message).toContain('HTTP 403: rate limit')
  })

  it('sends the ETag of a stored single-page answer and keeps it on 304', async () => {
    const fake = new FakeGitHub()
    fake.trend = { items: [closed(1, '2026-11-23T02:00:00Z')], etag: 'W/"t1"' }
    const one = await first(fake)
    const two = await run(fake, one.state, t0 + minInterval)
    const req = fake.requests.find((r) => r.path.startsWith('search/issues?per_page=100'))
    expect(req?.headers['If-None-Match']).toBe('W/"t1"')
    expect(two.changed).toBe(false)
    expect(two.errors).toEqual([])
    expect(trend(two.state)?.[4].delivered).toBe(1)
  })

  it('reads the new 7 days at the first check after Taipei midnight, before the probe is due', async () => {
    const fake = new FakeGitHub()
    fake.trend = { items: [closed(1, '2026-11-23T02:00:00Z')], etag: 'W/"t1"' }
    const late = Date.parse('2026-11-25T15:59:50Z') // 23:59:50 on 25 Nov in Taipei
    const one = await run(fake, initialState(), late)
    expect(trend(one.state)?.at(-1)?.date).toBe('2026-11-25')
    // 10 s before the probe would be due again; the same day reads nothing.
    await run(fake, one.state, late + 5_000)
    expect(fake.trendQueries()).toEqual([])
    const two = await run(fake, one.state, late + 20_000)
    const req = fake.requests.find((r) => r.path.startsWith('search/issues?per_page=100'))
    expect(req?.headers['If-None-Match']).toBeUndefined()
    expect(fake.trendQueries()).toEqual([trendQ.replace('2026-11-19', '2026-11-20')])
    expect(trend(two.state)?.map((d) => d.date)).toEqual(['2026-11-20', '2026-11-21', '2026-11-22', '2026-11-23', '2026-11-24', '2026-11-25', '2026-11-26'])
    expect(trend(two.state)?.map((d) => d.delivered)).toEqual([0, 0, 0, 1, 0, 0, 0])
  })

  it('reads the trend last: a spent budget holds back only the trend, read on the next check', async () => {
    const fake = new FakeGitHub()
    fake.set(issuesPath(cli), [issueJSON(1)])
    fake.trend.items = [closed(1, '2026-11-23T02:00:00Z')]
    // 3 × (issues, 2 workflows, runners), 2 searches and 1 blocker query.
    const one = await run(fake, initialState(), t0, 15)
    expect(fake.requests).toHaveLength(15)
    expect(fake.trendQueries()).toEqual([])
    expect(one.errors).toHaveLength(1)
    expect(one.errors[0].message).toContain('request budget of this check reached')
    expect(one.state.data?.summary).toMatchObject({ created: 50, delivered: 42, error: { message: expect.stringContaining('request budget of this check reached') } })
    expect(one.state.data?.summary.days).toBeUndefined()
    expect(issuesOf(one.state, cli).map((i) => i.number)).toEqual([1])
    expect(one.state.blockers.lastError).toBeNull()
    const two = await run(fake, one.state, t0 + minInterval)
    expect(fake.trendQueries()).toEqual([trendQ])
    expect(two.errors).toEqual([])
    expect(trend(two.state)?.[4].delivered).toBe(1)
  })
})
