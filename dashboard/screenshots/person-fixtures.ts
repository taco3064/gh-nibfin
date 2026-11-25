// Fictional person-view status bodies for the shell screenshots. None of it
// names a real repository, account or run. They follow status.PersonView in
// schema/status.schema.json.

const gh = 'https://github.com'
const error = (code: string, message: string) => ({ code, message })
export const issueURL = (repo: string, n: number) => `${gh}/${repo}/issues/${n}`

function issue(repo: string, number: number, title: string, labels: string[], over: Record<string, unknown> = {}) {
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
    ...over,
  }
}

function slot(n: number, over: Record<string, unknown> = {}) {
  return {
    slot: `claude-${n}`,
    provider: 'claude',
    name: `nibfin-claude-${n}`,
    repo: 'acme/app',
    configured: true,
    unit: true,
    enabled: true,
    active: true,
    listed: true,
    status: 'online',
    busy: false,
    ...over,
  }
}

const hostOK = { running: true }

export const hosts = {
  complete: { host: hostOK, runners: { repo: 'acme/app', runners: [slot(1), slot(2, { configured: false, unit: false })] } },
  incomplete: { host: hostOK, runners: { runners: [slot(1, { unit: false, enabled: false, active: false, listed: false })] } },
  podmanMissing: {
    host: { running: false, error: error('podman_missing', 'podman is not installed; install it from https://podman.io') },
    runners: { runners: null, error: error('podman_missing', 'podman is not installed; install it from https://podman.io') },
  },
  machineStopped: {
    host: { running: false },
    runners: { runners: [], error: error('machine_stopped', 'the podman machine podman-machine-default is stopped') },
  },
}

const image = {
  latest: { set: 'execution', ref: 'ghcr.io/acme/nibfin:v1.4.0', latest: true },
  notLatest: { set: 'execution', ref: 'ghcr.io/acme/nibfin:v1.2.0', latest: false },
  unknown: { set: 'execution', ref: 'ghcr.io/acme/nibfin:v1.4.0', latest: null },
  noPin: { set: 'execution', ref: null, latest: null },
  peer: { set: 'peer', ref: null, latest: null },
  none: { set: 'none', ref: null, latest: null },
  error: { ref: null, latest: null, error: error('github_error', 'reading .github/workflows of acme/legacy: HTTP 502') },
}

export const repos = [
  {
    repo: 'acme/app',
    image: image.latest,
    permission: 'TRIAGE',
    issues: [
      issue('acme/app', 101, 'Add CSV export to the reports page', ['scope', 'shaped'], {
        runs: [
          {
            repo: 'acme/app',
            name: 'Deliver #101 Add CSV export to the reports page',
            workflow: 'nibfin-issue',
            status: 'in_progress',
            conclusion: null,
            createdAt: '2026-11-24T15:20:00Z',
            url: `${gh}/acme/app/actions/runs/9003`,
          },
        ],
      }),
      issue('acme/app', 102, 'Fix timezone handling in the nightly scheduler', ['scope', 'pause']),
      issue(
        'acme/app',
        104,
        'Replace the legacy authentication middleware with the shared session library across every service endpoint',
        ['scope', 'shaped', 'delivered', 'pending'],
        { pendingComment: `${issueURL('acme/app', 104)}#issuecomment-5001` },
      ),
    ],
  },
  { repo: 'acme/docs', image: image.notLatest, permission: 'READ', issues: [issue('acme/docs', 7, 'Rewrite the getting started guide', ['scope'])] },
  {
    repo: 'acme/infra',
    image: image.unknown,
    permission: 'WRITE',
    issues: [issue('acme/infra', 12, 'Object storage bucket for exports', ['scope', 'shaped', 'pause'])],
  },
  {
    repo: 'acme/legacy',
    image: image.error,
    permission: null,
    permissionError: error('github_error', 'repository acme/legacy: timeout after 10s'),
    issues: [issue('acme/legacy', 3, 'Archive the old billing service', ['scope'])],
  },
  { repo: 'acme/mobile', image: image.noPin, permission: 'MAINTAIN', issues: [issue('acme/mobile', 41, 'Offline mode for the field app', ['scope'])] },
  { repo: 'acme/site', image: image.peer, permission: 'WRITE', issues: [issue('acme/site', 5, 'New pricing page', ['scope', 'shaped'])] },
  { repo: 'acme/tools', image: image.none, permission: 'ADMIN', issues: [issue('acme/tools', 9, 'Lint rule for imports', [])] },
]

export function personView(over: Record<string, unknown> = {}) {
  return {
    host: hosts.complete,
    latestImage: { error: error('github_error', 'latest release of acme/nibfin: rate limited') },
    githubHost: 'github.com',
    user: 'octo-dev',
    repos,
    truncated: false,
    nibfinRepos: [],
    ...over,
  }
}
