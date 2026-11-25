// Fictional status data for the screenshot harness. None of it names a real
// repository, account or run.

import type { RepoPermission, Run, StatusIssue } from '@/lib/status'

export interface FixtureIssue {
  issue: StatusIssue
  repo: string
}

const gh = 'https://github.com'

function issueURL(repo: string, n: number) {
  return `${gh}/${repo}/issues/${n}`
}

function run(repo: string, id: number, name: string, status: string, conclusion: string | null, createdAt: string): Run {
  return { repo, name, workflow: 'nibfin-issue', status, conclusion, createdAt, url: `${gh}/${repo}/actions/runs/${id}` }
}

const error = (code: string, message: string) => ({ code, message })

function base(repo: string, number: number, title: string, labels: string[]): StatusIssue {
  return {
    number,
    title,
    url: issueURL(repo, number),
    labels,
    pause: labels.includes('pause'),
    pending: labels.includes('pending'),
    createdAt: '2026-11-02T09:00:00Z',
    blockedBy: [],
    runs: [],
  }
}

export const permissions: Record<string, RepoPermission> = {
  'acme/app': { permission: 'TRIAGE' },
  'acme/docs': { permission: 'READ' },
  'acme/infra': { permission: null },
  'acme/legacy': { permission: null, permissionError: error('github_error', 'repository acme/legacy: timeout after 10s') },
}

export const fixtures: FixtureIssue[] = [
  {
    repo: 'acme/app',
    issue: {
      ...base('acme/app', 101, 'Add CSV export to the reports page', ['scope', 'shaped']),
      blockedBy: [
        { repo: 'acme/app', number: 99, title: 'Reports API returns paged rows', state: 'OPEN', url: issueURL('acme/app', 99) },
        { repo: 'acme/infra', number: 12, title: 'Object storage bucket for exports', state: 'OPEN', url: issueURL('acme/infra', 12) },
        { repo: 'acme/app', number: 98, title: 'Old report layout', state: 'CLOSED', url: issueURL('acme/app', 98) },
      ],
      runs: [
        run('acme/app', 9003, 'Deliver #101 Add CSV export to the reports page', 'in_progress', null, '2026-11-24T15:20:00Z'),
        run('acme/nibfin-exec', 7002, 'Accept #101', 'completed', 'failure', '2026-11-24T11:05:00Z'),
        run('acme/app', 9001, 'Shape #101 Add CSV export to the reports page', 'completed', 'success', '2026-11-23T08:40:00Z'),
        run('acme/nibfin-exec', 7001, 'Shape review #101', 'completed', 'cancelled', '2026-11-22T17:10:00Z'),
      ],
    },
  },
  {
    repo: 'acme/app',
    issue: {
      ...base('acme/app', 102, 'Fix timezone handling in the nightly scheduler', ['scope', 'pause']),
      runs: [run('acme/app', 9010, 'Shape #102 Fix timezone handling', 'completed', 'success', '2026-11-20T10:00:00Z')],
      runsError: error('github_error', 'listing runs of acme/nibfin-exec: HTTP 502'),
    },
  },
  {
    repo: 'acme/app',
    issue: {
      ...base('acme/app', 103, 'Document the API rate limits', ['scope', 'pending']),
      pendingComment: `${issueURL('acme/app', 103)}#issuecomment-5001`,
      runsTruncated: true,
      runs: [run('acme/app', 9020, 'Shape #103 Document the API rate limits', 'completed', 'success', '2026-11-19T12:00:00Z')],
      blockedByError: error('github_error', 'blockedBy of acme/app#103: rate limited'),
    },
  },
  {
    repo: 'acme/app',
    issue: {
      ...base(
        'acme/app',
        104,
        'Replace the legacy authentication middleware with the shared session library across every service endpoint',
        ['scope', 'shaped', 'delivered', 'pending'],
      ),
      pendingCommentError: error('github_error', 'comments of acme/app#104: HTTP 500'),
    },
  },
  {
    repo: 'acme/app',
    issue: { ...base('acme/app', 105, 'Spike: evaluate a new charting library', []), runs: undefined, blockedBy: undefined },
  },
  {
    repo: 'acme/docs',
    issue: { ...base('acme/docs', 7, 'Rewrite the getting started guide', ['scope', 'pending']) },
  },
  {
    repo: 'acme/infra',
    issue: { ...base('acme/infra', 12, 'Object storage bucket for exports', ['scope', 'shaped']) },
  },
  {
    repo: 'acme/legacy',
    issue: { ...base('acme/legacy', 3, 'Archive the old billing service', ['scope']) },
  },
]
