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

function codexSlot(n: number, over: Record<string, unknown> = {}) {
  return slot(n, { slot: `codex-${n}`, provider: 'codex', name: `nibfin-codex-${n}`, ...over })
}

const machine = { describe: 'podman machine nibfin-dev', machine: 'nibfin-dev', vmType: 'applehv', running: true }
const memory = { totalKB: 16_303_136, availableKB: 5_452_596 }
const loggedIn = [
  { provider: 'claude', loggedIn: true },
  { provider: 'codex', loggedIn: false },
]
const hostOK = { describe: 'local podman', running: true }
const stoppedErr = error('machine_stopped', 'podman machine nibfin-dev is stopped; status does not start it')
const podmanErr = error('podman_missing', 'podman is not installed; install it from https://podman.io')

export const hosts = {
  complete: {
    host: hostOK,
    memory,
    runners: { repo: 'acme/app', runners: [slot(1), slot(2, { configured: false, unit: false })] },
    providers: loggedIn,
  },
  // mixed is every slot state: active and inactive, online busy and idle,
  // offline, not listed and incomplete.
  mixed: {
    host: machine,
    memory,
    runners: {
      repo: 'acme/app',
      runners: [
        slot(1, { busy: true }),
        slot(2),
        slot(3, { active: false, status: 'offline' }),
        codexSlot(1, { listed: false, status: undefined }),
        codexSlot(2, { configured: false, unit: false, enabled: false, active: false, listed: false, status: undefined }),
      ],
    },
    providers: loggedIn,
  },
  githubError: {
    host: machine,
    memory,
    runners: {
      repo: 'acme/app',
      runners: [slot(1), slot(2, { active: false }), codexSlot(1)],
      githubError: error('github_error', 'listing the runners of acme/app: HTTP 502'),
    },
    providers: [
      { provider: 'claude', loggedIn: true },
      { provider: 'codex', loggedIn: true },
    ],
  },
  incomplete: {
    host: hostOK,
    memory,
    runners: { runners: [slot(1, { unit: false, enabled: false, active: false, listed: false })] },
    providers: loggedIn,
  },
  podmanMissing: {
    host: { running: false, error: podmanErr },
    memory: { totalKB: 0, availableKB: 0, error: podmanErr },
    runners: { runners: null, error: podmanErr },
    providers: [
      { provider: 'claude', loggedIn: false, error: podmanErr },
      { provider: 'codex', loggedIn: false, error: podmanErr },
    ],
  },
  machineStopped: {
    host: { ...machine, running: false },
    memory: { totalKB: 0, availableKB: 0, error: stoppedErr },
    runners: { runners: [], error: error('machine_stopped', 'the podman machine nibfin-dev is stopped') },
    providers: [
      { provider: 'claude', loggedIn: false, error: stoppedErr },
      { provider: 'codex', loggedIn: false, error: stoppedErr },
    ],
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

// nibfinRepos are the account's repositories on github.com that use Nibfin.
export const nibfinRepos = [
  { repo: 'acme/app', url: `${gh}/acme/app`, visibility: 'PRIVATE', permission: 'ADMIN', execution: true },
  { repo: 'acme/docs', url: `${gh}/acme/docs`, visibility: 'PUBLIC', permission: 'WRITE', execution: false },
  { repo: 'acme/infra', url: `${gh}/acme/infra`, visibility: 'INTERNAL', permission: 'MAINTAIN', execution: false },
  { repo: 'acme/platform', url: `${gh}/acme/platform`, visibility: 'PRIVATE', permission: 'WRITE', execution: false },
  { repo: 'acme/site', url: `${gh}/acme/site`, visibility: 'PUBLIC', permission: 'READ', execution: false },
]

export function personView(over: Record<string, unknown> = {}) {
  return {
    host: hosts.complete,
    latestImage: { error: error('github_error', 'latest release of acme/nibfin: rate limited') },
    githubHost: 'github.com',
    user: 'octo-dev',
    repos,
    truncated: false,
    nibfinRepos,
    ...over,
  }
}
