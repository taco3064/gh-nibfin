// Fictional live data for the site's Live page and Home summary (#365): the
// Worker's three repository names with made-up Issues, runs and runners.
// Every runner carries a fictional name, which the page must never show.

import type { LiveData, LiveEvent } from '../src/worker/messages'

export const liveUrl = 'wss://nibfin-live.tabacotaco.workers.dev/live'

const owner = 'taco3064'
const gh = 'https://github.com'
export const repoNames = ['nibfin-core', 'gh-nibfin', 'nibfin-e2e'] as const
const full = (name: string) => `${owner}/${name}`

type Issue = LiveData['repos'][number]['issues'] extends (infer T)[] | null ? T : never
type Blocker = NonNullable<Issue['blockedBy']>[number]

const blocker = (repo: string, number: number, title: string, state: 'OPEN' | 'CLOSED' = 'OPEN'): Blocker => ({
  repo,
  number,
  title,
  state,
  url: `${gh}/${repo}/issues/${number}`,
})

const stages = [['scope'], ['scope', 'shaped'], ['scope', 'shaped', 'delivered'], []]

function issue(repo: string, number: number, title: string, blockedBy: Blocker[] | null, over: Partial<Issue> = {}): Issue {
  const labels = [...stages[number % 4]]
  const pause = number % 11 === 0
  if (pause) labels.push('pause')
  return {
    number,
    title,
    url: `${gh}/${repo}/issues/${number}`,
    labels,
    pause,
    pending: false,
    createdAt: '2026-11-22T09:00:00Z',
    blockedBy,
    runs:
      number % 3 === 0
        ? [
            {
              repo,
              name: `Nibfin · ${repo}#${number} · shape`,
              workflow: 'Nibfin shared runner',
              status: 'completed',
              conclusion: 'success',
              createdAt: '2026-11-23T10:00:00Z',
              url: `${gh}/${repo}/actions/runs/${9000 + number}`,
            },
          ]
        : [],
    ...over,
  }
}

// coreIssues is a graph of about 60 Issues: chains, a wide layer, cross
// repository and closed blockers, an unread blocker list and runs.
function coreIssues(): Issue[] {
  const r = full('nibfin-core')
  const own = (n: number, state: 'OPEN' | 'CLOSED' = 'OPEN') => blocker(r, n, `Core task ${n}`, state)
  const out: Issue[] = [issue(r, 1, 'Fictional foundation: shared stage runner', [])]
  for (let c = 0; c < 5; c++) {
    for (let i = 0; i < 5; i++) {
      const n = 10 + c * 5 + i
      const bs: Blocker[] = i > 0 ? [own(n - 1)] : c % 2 === 0 ? [own(1)] : [blocker(full('gh-nibfin'), 300 + c, `CLI prerequisite ${300 + c}`)]
      if (i === 2 && c % 2 === 0) bs.push(own(500 + c, 'CLOSED'))
      out.push(issue(r, n, `Chain ${c + 1} step ${i + 1}: ${['design', 'schema', 'stage', 'checks', 'rollout'][i]}`, bs))
    }
  }
  for (let n = 40; n < 60; n++) out.push(issue(r, n, `Wide layer task ${n}`, [own(1)]))
  out.push(issue(r, 60, 'Blockers of this Issue could not be read', null, { blockedByError: { code: 'github_error', message: 'blocked-by of #60: HTTP 502' } }))
  for (let n = 61; n <= 66; n++) out.push(issue(r, n, `Standalone improvement ${n}`, []))
  out.push(issue(r, 67, 'Older runs may be missing for this Issue', [], { runsTruncated: true }))
  return out
}

// The fictional delivered trend: 21 to 27 Nov, today last, one day without
// a delivery.
export const trendCounts = [4, 9, 0, 7, 12, 6, 3]
const trendDays = trendCounts.map((delivered, i) => ({ date: `2026-11-${21 + i}`, delivered }))

// withoutDays is data whose trend was never read or whose last read failed.
export function withoutDays(data: LiveData): LiveData {
  const { days: _days, ...summary } = data.summary
  return { ...data, summary }
}

const runnerName = (n: number) => `fictional-host-${n}-runner`

export function liveData(created = 128, delivered = 57, second = false): LiveData {
  const core = full('nibfin-core')
  const cli = full('gh-nibfin')
  return {
    repos: [
      { repo: `github.com/${core}`, issues: coreIssues() },
      { repo: `github.com/${cli}`, issues: null, issuesError: { code: 'github_api_failed', message: `GET repos/${cli}/issues: HTTP 502` } },
      { repo: `github.com/${full('nibfin-e2e')}`, issues: [] },
    ],
    summary: { since: '2026-11-21T00:00:00+08:00', created, delivered, days: trendDays },
    runners: [
      {
        repo: core,
        // The name is the Worker's old field: the page must never show it.
        runners: [
          {
            id: 11,
            name: runnerName(1),
            status: 'online',
            busy: true,
            job: { title: `Nibfin · ${core}#14 · deliver`, repo: core, issue: 14, ref: null, stage: 'deliver', url: `${gh}/${core}/actions/runs/7001/job/1` },
          },
          {
            id: 12,
            name: runnerName(2),
            status: 'online',
            busy: true,
            job: { title: `Nibfin · ${cli}@nibfin/issue-354 · route`, repo: cli, issue: null, ref: 'nibfin/issue-354', stage: 'route', url: `${gh}/${core}/actions/runs/7002/job/2` },
          },
          { id: 13, name: runnerName(3), status: 'online', busy: true, job: null },
          second
            ? { id: 14, name: runnerName(4), status: 'online', busy: true, job: { title: `Nibfin · ${core}#41 · shape`, repo: core, issue: 41, ref: null, stage: 'shape', url: `${gh}/${core}/actions/runs/7003/job/3` } }
            : { id: 14, name: runnerName(4), status: 'online', busy: false, job: null },
          { id: 15, name: runnerName(5), status: 'offline', busy: false, job: null },
        ] satisfies object[] as unknown as LiveData['runners'][number]['runners'],
      },
      { repo: cli, runners: [], error: { code: 'github_api_failed', message: `GET repos/${cli}/actions/runners: HTTP 502` } },
      { repo: full('nibfin-e2e'), runners: [] },
    ],
  }
}

export const runnerNames = [1, 2, 3, 4, 5].map(runnerName)

export const event = (e: LiveEvent) => JSON.stringify(e)

// withoutIssues drops nibfin-core's Issues numbered in gone, and the blocker
// entries that point at them, as a snapshot does once they are closed and
// leave the graph (#454).
export function withoutIssues(data: LiveData, gone: number[]): LiveData {
  const core = full('nibfin-core')
  const left = (b: Blocker) => b.repo !== core || !gone.includes(b.number)
  return {
    ...data,
    repos: data.repos.map((r) =>
      r.repo !== `github.com/${core}` || !r.issues
        ? r
        : { ...r, issues: r.issues.filter((i) => !gone.includes(i.number)).map((i) => (i.blockedBy ? { ...i, blockedBy: i.blockedBy.filter(left) } : i)) },
    ),
  }
}
