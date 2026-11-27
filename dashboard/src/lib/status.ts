// Status data types: the parts of `gh nibfin status --json` the Dashboard
// reads. They mirror schema/status.schema.json ($defs status.PersonView,
// status.RepoView, status.RepoIssues, status.RepoImage, status.LatestImage,
// status.Issue, status.Blocker, status.Run, status.NibfinRepo,
// status.NibfinRun, status.PullRequest, op.HostReport, op.HostInfo,
// op.MemoryReport, op.ProviderReport, op.RunnersReport, op.RunnerState and
// op.Error); follow that file when it changes.

import type { ApiError } from '@/lib/session'

export type { ApiError }

// Blocker is an Issue GitHub lists as blocking another (status.Blocker).
export interface Blocker {
  // repo is OWNER/REPO.
  repo: string
  number: number
  title: string
  // state is GitHub's OPEN or CLOSED.
  state: string
  url: string
}

// Run is one Nibfin Actions run of an Issue (status.Run).
export interface Run {
  // repo is the OWNER/REPO the run belongs to.
  repo: string
  name: string
  workflow: string
  status: string
  // conclusion is null while the run has not finished.
  conclusion: string | null
  createdAt: string
  url: string
}

// StatusIssue is one Issue of the status data (status.Issue). runs and
// blockedBy are optional here: a value missing from the data, with no error,
// was not read, which is never the same as none.
export interface StatusIssue {
  number: number
  title: string
  url: string
  labels: string[] | null
  pause: boolean
  pending: boolean
  pendingComment?: string
  pendingCommentError?: ApiError
  createdAt: string
  blockedBy?: Blocker[] | null
  blockedByError?: ApiError
  runs?: Run[] | null
  runsError?: ApiError
  runsTruncated?: boolean
}

// RepoPermission is the current account's permission on one repository:
// repos[].permission in the person view, the top-level permission in the
// repository view. null means GitHub returned none.
export interface RepoPermission {
  permission: string | null
  permissionError?: ApiError
  // permissionLoading is the page's own: the permission is still being
  // read in a partial read. It is never in the status data.
  permissionLoading?: boolean
}

// LatestImage is the newest Nibfin image release (status.LatestImage).
export interface LatestImage {
  ref?: string
  error?: ApiError
}

// RepoImage is the image a repository's Nibfin workflows pin
// (status.RepoImage). set is execution, peer or none; it is omitted with an
// error. latest is null when it could not be compared.
export interface RepoImage {
  set?: string
  ref: string | null
  latest: boolean | null
  error?: ApiError
}

// RepoIssues is one repository of the person view (status.RepoIssues).
export interface RepoIssues extends RepoPermission {
  // repo is OWNER/REPO.
  repo: string
  image: RepoImage
  issues: StatusIssue[] | null
}

// RunnerState is one Runner slot on this host (op.RunnerState).
export interface RunnerState {
  slot: string
  provider: string
  name?: string
  repo?: string
  configured: boolean
  unit: boolean
  enabled: boolean
  active: boolean
  listed: boolean
  status?: string
  busy: boolean
}

// RunnersReport is the Runner slots of this host (op.RunnersReport).
export interface RunnersReport {
  // repo is the OWNER/REPO the Runners serve, when one is read.
  repo?: string
  runners: RunnerState[] | null
  error?: ApiError
  githubError?: ApiError
  // githubNeedsAdmin is set when GitHub's Runner list was not read
  // because the gh login is not admin of repo.
  githubNeedsAdmin?: boolean
}

// HostInfo is the runner host (op.HostInfo). machine and vmType are empty
// on the local Linux host.
export interface HostInfo {
  describe?: string
  machine?: string
  vmType?: string
  running?: boolean
  error?: ApiError
}

// MemoryReport is the runner host's memory from /proc/meminfo, in kB
// (op.MemoryReport).
export interface MemoryReport {
  totalKB: number
  availableKB: number
  error?: ApiError
}

// ProviderReport is one provider's login on the host (op.ProviderReport).
export interface ProviderReport {
  provider: string
  loggedIn: boolean
  error?: ApiError
}

// HostReport is the parts of op.HostReport the Dashboard reads.
export interface HostReport {
  host: HostInfo
  memory: MemoryReport
  runners: RunnersReport
  providers: ProviderReport[] | null
}

// NibfinRepo is one of the account's repositories whose default branch has
// nibfin-issue.yml (status.NibfinRepo).
export interface NibfinRepo {
  // repo is OWNER/REPO, without the host; the host is the read's.
  repo: string
  url: string
  visibility: string
  permission: string | null
  // execution is true for the repository this host's Runners serve.
  execution: boolean | null
}

// PullRequest is a pull request reference (status.PullRequest).
export interface PullRequest {
  number: number
  url: string
}

// NibfinRun says whether a Nibfin run succeeded after nibfin-issue.yml last
// changed on the default branch (status.NibfinRun).
export interface NibfinRun {
  // usesNibfin is true when the default branch has nibfin-issue.yml,
  // generated or maintained by hand.
  usesNibfin: boolean
  // commit is the newest commit that changed the file on the default
  // branch, pr its merged PR when there is one, and changedAt the PR's merge
  // time, or else the commit's committer time.
  commit?: string
  pr?: PullRequest
  changedAt?: string
  succeeded: boolean
  // run is the URL of the successful run, at its creation time.
  run?: string
  at?: string
  error?: ApiError
}

// RepoView is the result of the status read of one repository
// (status.RepoView).
export interface RepoView extends RepoPermission {
  host: HostReport
  latestImage: LatestImage
  // repo is HOST/OWNER/REPO.
  repo: string
  image: RepoImage
  issues: StatusIssue[] | null
  issuesError?: ApiError
  nibfinRun: NibfinRun
  // nibfinRepos are on the host of repo.
  nibfinRepos: NibfinRepo[] | null
  nibfinReposError?: ApiError
}

// repoHost returns the host of a HOST/OWNER/REPO value, or '' without one.
export function repoHost(repo: string): string {
  const parts = repo.split('/')
  return parts.length >= 3 ? parts[0] : ''
}

// ownerRepo returns the OWNER/REPO of a HOST/OWNER/REPO value.
export function ownerRepo(repo: string): string {
  const parts = repo.split('/')
  return parts.length >= 3 ? parts.slice(1).join('/') : repo
}

// PersonView is the result of the status read without a repository
// (status.PersonView).
export interface PersonView {
  host: HostReport
  latestImage: LatestImage
  githubHost: string
  user?: string
  userError?: ApiError
  repos: RepoIssues[] | null
  truncated: boolean
  issuesError?: ApiError
  // nibfinRepos are on githubHost.
  nibfinRepos: NibfinRepo[] | null
  nibfinReposError?: ApiError
}

// CurrentJob is the Job a Runner runs now (status.CurrentJob). repo, issue
// and issueUrl are set only for a valid OWNER/REPO and number; stage only
// when it came from a run title.
export interface CurrentJob {
  repo?: string
  issue?: number
  issueUrl?: string
  stage?: string
  url?: string
  startedAt?: string
  source: 'github' | 'container'
}

// Period is one job's busy time on a Runner, clipped to the window; end is
// absent while the job runs (status.Period).
export interface Period {
  start: string
  end?: string
  failed: boolean
  repo?: string
  issue?: number
  issueUrl?: string
  stage?: string
  url?: string
}

// SlotActivity is one Runner slot's current Job and busy periods
// (status.SlotActivity).
export interface SlotActivity {
  slot: string
  provider: string
  name?: string
  current?: CurrentJob
  periods: Period[]
}

// ContainerJob is a running Job container matched to no slot
// (status.ContainerJob).
export interface ContainerJob {
  repo?: string
  issue?: number
  issueUrl?: string
  startedAt?: string
}

// Activity is the Runner view's activity read, GET /api/runner-activity
// (status.Activity). It is not part of status --json.
export interface Activity {
  from: string
  to: string
  repo?: string
  runners: SlotActivity[]
  slotsError?: ApiError
  jobsError?: ApiError
  containersError?: ApiError
  unattributed: ContainerJob[]
  // history is true on a full read's answer; the current part
  // (?part=current) has only the runs in progress.
  history: boolean
  // coversFrom is set when the window held more runs than GitHub lists:
  // the periods reach back only to this time.
  coversFrom?: string
}
