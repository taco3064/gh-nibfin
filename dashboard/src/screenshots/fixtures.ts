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
    issue: {
      ...base('acme/app', 107, 'Cache the search index between deploys', ['scope', 'shaped', 'pending']),
      pendingComment: `${issueURL('acme/app', 107)}#issuecomment-5101`,
      runs: [run('acme/app', 9030, 'Deliver #107 Cache the search index', 'completed', 'failure', '2026-11-25T08:00:00Z')],
      repair: {
        state: 'repairing',
        reason: 'CI failed on a flaky integration test; the repair stage reruns CI.',
        category: 'CI failure',
        done: 'Read the failed job log.',
        record: `${issueURL('acme/app', 107)}#issuecomment-5102`,
      },
    },
  },
  {
    repo: 'acme/app',
    issue: {
      ...base('acme/app', 108, 'Move uploaded attachments to object storage', ['scope', 'shaped', 'pending']),
      pendingComment: `${issueURL('acme/app', 108)}#issuecomment-5201`,
      runs: [
        run('acme/nibfin-exec', 7108, 'Deliver #108 Move uploaded attachments', 'completed', 'failure', '2026-11-26T10:00:00Z'),
        run('acme/app', 9040, 'Shape #108 Move uploaded attachments', 'completed', 'success', '2026-11-25T09:00:00Z'),
      ],
      repair: {
        state: 'choose',
        reason: 'The Shape and the Issue disagree on whether existing attachments move as well; pick which one holds.',
        category: 'Requirement conflict',
        done: 'Compared the Shape with the current Issue text and the product rules; both readings are possible, so the repair stage stopped for a choice instead of guessing.',
        record: `${issueURL('acme/app', 108)}#issuecomment-5202`,
        options: [
          {
            number: 1,
            title: 'Move new uploads only',
            consequence: 'Deliver goes on as shaped; existing attachments stay where they are until a later Issue.',
            issueChange: 'none',
            recommended: false,
          },
          {
            number: 2,
            title: 'Move new uploads and migrate every existing attachment in the same change',
            consequence: 'The Shape runs again with a migration step, and Deliver starts over from the new Shape; this takes longer but leaves no attachment behind.',
            issueChange: 'adds a clause: existing attachments are migrated to object storage',
            recommended: true,
          },
          {
            number: 3,
            title: 'Stop and split the Issue',
            consequence: 'Nibfin closes nothing; a person splits the migration into its own Issue.',
            issueChange: 'removes the migration from this Issue',
            recommended: false,
          },
        ],
      },
    },
  },
  {
    repo: 'acme/app',
    issue: {
      ...base('acme/app', 109, 'Publish the nightly report to the team channel', ['scope', 'shaped', 'pending']),
      pendingComment: `${issueURL('acme/app', 109)}#issuecomment-5301`,
      runs: undefined,
      repair: {
        state: 'act',
        reason: 'The Runner has no provider login, so no agent could start.',
        category: 'Environment',
        done: 'Retried the Job twice; both stopped before the agent started.',
        advice: 'Log in to the provider on the runner host, then resume the Issue.',
        record: `${issueURL('acme/app', 109)}#issuecomment-5302`,
        steps: [
          { text: 'Open the failed Job to confirm the login error.', link: `${gh}/acme/app/actions/runs/9050` },
          { text: 'Log in on the runner host.', command: 'gh nibfin login claude' },
          { text: 'This link is not http(s) and shows as text.', link: 'javascript:alert(1)' },
        ],
        bugReport: {
          url: `${gh}/acme/nibfin/issues/new`,
          title: 'Runner started a Job without a provider login',
          body: 'The Job nibfin-job ran on a Runner whose provider login was missing.\nExpected: the Runner is not offered Jobs until it is logged in.',
        },
      },
    },
  },
  {
    repo: 'acme/app',
    issue: { ...base('acme/app', 105, 'Spike: evaluate a new charting library', []), runs: undefined, blockedBy: undefined },
  },
  {
    repo: 'acme/app',
    issue: { ...base('acme/app', 106, 'Customers ask for exports in more formats', ['request']), runs: [] },
  },
  {
    repo: 'acme/docs',
    issue: { ...base('acme/docs', 7, 'Rewrite the getting started guide', ['scope', 'pending']) },
  },
  {
    repo: 'acme/docs',
    // No repair record covers the stop; the account may not answer it.
    issue: {
      ...base('acme/docs', 8, 'Translate the FAQ', ['scope', 'shaped', 'pending']),
      pendingComment: `${issueURL('acme/docs', 8)}#issuecomment-5401`,
      repair: { state: 'act', reason: 'No repair record covers this stop yet; see the pending comment' },
    },
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
