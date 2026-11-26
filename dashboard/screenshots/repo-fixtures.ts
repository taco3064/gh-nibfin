// Fictional repository-view status bodies for the repository view
// screenshots (#353). None of it names a real repository, account or run.
// They follow status.RepoView in schema/status.schema.json.

import { hosts, nibfinRepos } from './person-fixtures'

const gh = 'https://github.com'
const ghe = 'https://ghe.example.test'
const error = (code: string, message: string) => ({ code, message })

type Blocker = { repo: string; number: number; title: string; state: 'OPEN' | 'CLOSED'; url: string }

const blocker = (repo: string, number: number, title: string, state: 'OPEN' | 'CLOSED' = 'OPEN', base = gh): Blocker => ({
  repo,
  number,
  title,
  state,
  url: `${base}/${repo}/issues/${number}`,
})

const stageLabels = [['scope'], ['scope', 'shaped'], ['scope', 'shaped', 'delivered'], []]

function issue(repo: string, number: number, title: string, blockedBy: Blocker[] | undefined, over: Record<string, unknown> = {}, base = gh) {
  const labels = [...stageLabels[number % 4]]
  const pause = number % 11 === 0
  const pending = number % 13 === 0
  if (pause) labels.push('pause')
  if (pending) labels.push('pending')
  return {
    number,
    title,
    url: `${base}/${repo}/issues/${number}`,
    labels,
    pause,
    pending,
    ...(pending ? { pendingComment: `${base}/${repo}/issues/${number}#issuecomment-${7000 + number}` } : {}),
    createdAt: '2026-11-02T09:00:00Z',
    ...(blockedBy !== undefined ? { blockedBy } : {}),
    runs: [],
    ...over,
  }
}

const image = {
  latest: { set: 'execution', ref: 'ghcr.io/acme/nibfin:v1.4.0', latest: true },
  notLatest: { set: 'execution', ref: 'ghcr.io/acme/nibfin:v1.2.0', latest: false },
  peer: { set: 'peer', ref: null, latest: null },
  none: { set: 'none', ref: null, latest: null },
  error: { ref: null, latest: null, error: error('github_error', 'reading .github/workflows of acme/infra: HTTP 502') },
}

const runs = {
  succeeded: {
    usesNibfin: true,
    commit: '4f2a9c1d8e7b6a5f4e3d2c1b0a9f8e7d6c5b4a39',
    pr: { number: 42, url: `${gh}/acme/app/pull/42` },
    changedAt: '2026-10-01T12:00:00Z',
    succeeded: true,
    run: `${gh}/acme/app/actions/runs/8801`,
    at: '2026-10-02T08:30:00Z',
  },
  notSucceeded: {
    usesNibfin: true,
    commit: '9b8a7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b',
    pr: { number: 3, url: `${gh}/acme/docs/pull/3` },
    changedAt: '2026-11-20T16:45:00Z',
    succeeded: false,
  },
  commitOnly: { usesNibfin: true, commit: 'c0ffee1234567890abcdef1234567890abcdef12', changedAt: '2026-09-12T10:00:00Z', succeeded: false },
  none: { usesNibfin: false, succeeded: false },
  error: {
    usesNibfin: true,
    succeeded: false,
    error: error('github_api_failed', 'list the commits of .github/workflows/nibfin-issue.yml of acme/infra: HTTP 502'),
  },
}

function repoView(repo: string, over: Record<string, unknown>) {
  return {
    host: hosts.complete,
    latestImage: { ref: 'ghcr.io/acme/nibfin:v1.4.0' },
    repo,
    image: image.latest,
    issues: [],
    nibfinRun: runs.succeeded,
    permission: 'WRITE',
    nibfinRepos,
    ...over,
  }
}

// longRepo is an OWNER/REPO without a break opportunity on a phone.
const longRepo = 'acme-platform-infrastructure/shared-terraform-modules-and-runbooks'

// platformIssues is a large generated graph: eight chains six deep, a wide
// layer on one foundation Issue, cross-repository and closed blockers, Issues
// blocked by several chain ends, two cycles, an unread blocker list and
// Issues without dependencies. A few titles and one blocker's OWNER/REPO are
// long unbroken tokens, such as file paths.
function platformIssues() {
  const r = 'acme/platform'
  const own = (n: number, state: 'OPEN' | 'CLOSED' = 'OPEN') => blocker(r, n, `Platform task ${n}`, state)
  const out: ReturnType<typeof issue>[] = []
  out.push(issue(r, 1, 'Platform foundation: shared service skeleton', []))
  out.push(issue(r, 2, 'Second foundation: configuration loader', [own(400, 'CLOSED')]))
  for (let n = 3; n <= 9; n++) out.push(issue(r, n, `Housekeeping task ${n}`, []))
  for (let c = 0; c < 8; c++) {
    for (let i = 0; i < 6; i++) {
      const n = 10 + c * 6 + i
      const blockers: Blocker[] = []
      if (i > 0) blockers.push(own(n - 1))
      else if (c % 2 === 0) blockers.push(own(1))
      else if (c === 3) blockers.push(blocker(longRepo, 203, 'Provision infrastructure/terraform/modules/object-storage-bucket/main.tf'))
      else blockers.push(blocker('acme/infra', 200 + c, `Infra prerequisite ${200 + c}`))
      if (i === 2 && c % 3 === 0) blockers.push(own(500 + c, 'CLOSED'))
      out.push(issue(r, n, `Chain ${c + 1} step ${i + 1}: ${['design', 'schema', 'service', 'API', 'client', 'rollout'][i]}`, blockers))
    }
  }
  for (let n = 60; n < 100; n++) {
    const blockers = [own(1)]
    if (n % 5 === 0) blockers.push(own(2))
    out.push(issue(r, n, n === 60 ? 'Wide layer task 60: templates/workflows/nibfin-shared-runner.yml.tmpl' : `Wide layer task ${n}`, blockers))
  }
  for (let n = 100; n < 120; n++) {
    const a = 15 + ((n - 100) % 8) * 6
    const b = 15 + ((n - 97) % 8) * 6
    out.push(issue(r, n, `Integration ${n} across chains`, [own(a), own(b)]))
  }
  out.push(issue(r, 120, 'Cycle A: billing waits on invoices', [own(121)]))
  out.push(issue(r, 121, 'Cycle A: invoices wait on billing', [own(120)]))
  out.push(issue(r, 122, 'Cycle B: first', [own(123)]))
  out.push(issue(r, 123, 'Cycle B: second', [own(124)]))
  out.push(issue(r, 124, 'Cycle B: third', [own(122)]))
  out.push(issue(r, 125, 'Blockers of this Issue could not be read', undefined, { blockedByError: error('github_error', 'blocked-by of #125: HTTP 502') }))
  out.push(issue(r, 126, 'Standalone: split dashboard/src/components/operation/operation-items.tsx', []))
  for (let n = 127; n <= 150; n++) out.push(issue(r, n, `Standalone improvement ${n}`, []))
  return out
}

export const platform = platformIssues()

// edgeCount is the number of distinct dependent → blocker pairs.
export function edgeCount(issues: { number: number; blockedBy?: Blocker[] }[], repo: string): number {
  const pairs = new Set<string>()
  for (const i of issues) for (const b of i.blockedBy ?? []) pairs.add(`${repo}#${i.number}>${b.repo}#${b.number}`.toLowerCase())
  return pairs.size
}

// outsideCount is the number of distinct blockers that are not listed.
export function outsideCount(issues: { number: number; blockedBy?: Blocker[] }[], repo: string): number {
  const listed = new Set(issues.map((i) => `${repo}#${i.number}`.toLowerCase()))
  const out = new Set<string>()
  for (const i of issues) for (const b of i.blockedBy ?? []) out.add(`${b.repo}#${b.number}`.toLowerCase())
  return [...out].filter((k) => !listed.has(k)).length
}

const appIssues = [
  issue('acme/app', 101, 'Add CSV export to the reports page', [
    blocker('acme/app', 99, 'Reports API returns paged rows'),
    blocker('acme/app', 98, 'Old report layout', 'CLOSED'),
  ]),
  issue('acme/app', 99, 'Reports API returns paged rows', [blocker('acme/infra', 12, 'Object storage bucket for exports')]),
  issue('acme/app', 102, 'Fix timezone handling in the nightly scheduler', []),
]

export const corpRepos = [
  { repo: 'corp/api', url: `${ghe}/corp/api`, visibility: 'INTERNAL', permission: 'ADMIN', execution: true },
  { repo: 'corp/web', url: `${ghe}/corp/web`, visibility: 'PRIVATE', permission: 'WRITE', execution: false },
]

// repoStatus answers the repository reads by the repo the page sends.
export const repoStatus: Record<string, { result: unknown }> = {
  'github.com/acme/platform': { result: repoView('github.com/acme/platform', { issues: platform }) },
  'github.com/acme/app': { result: repoView('github.com/acme/app', { issues: appIssues, permission: 'ADMIN' }) },
  'github.com/acme/docs': {
    result: repoView('github.com/acme/docs', {
      image: image.notLatest,
      nibfinRun: runs.notSucceeded,
      permission: 'READ',
      issues: [issue('acme/docs', 7, 'Rewrite the getting started guide', [])],
      nibfinRepos: null,
      nibfinReposError: error('github_error', 'listing the repositories of the account: HTTP 503'),
    }),
  },
  'github.com/acme/infra': {
    result: repoView('github.com/acme/infra', {
      image: image.error,
      nibfinRun: runs.error,
      permission: null,
      permissionError: error('github_error', 'repository acme/infra: timeout after 10s'),
      issues: null,
      issuesError: error('github_error', 'listing the Issues of acme/infra: HTTP 502'),
    }),
  },
  'github.com/acme/site': { result: repoView('github.com/acme/site', { image: image.peer, nibfinRun: runs.commitOnly, permission: 'READ', issues: [] }) },
  'acme/new': { result: repoView('github.com/acme/new', { image: image.none, nibfinRun: runs.none, permission: 'ADMIN', issues: [] }) },
  'ghe.example.test/corp/api': {
    result: repoView('ghe.example.test/corp/api', {
      nibfinRepos: corpRepos,
      issues: [
        issue('corp/api', 5, 'Rate limit the public endpoints', [blocker('corp/web', 2, 'Shared gateway config', 'OPEN', ghe)], {}, ghe),
      ],
    }),
  },
  'ghe.example.test/corp/web': { result: repoView('ghe.example.test/corp/web', { nibfinRepos: corpRepos, issues: [] }) },
}
