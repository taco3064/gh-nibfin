// Status data types: the parts of `gh nibfin status --json` the Dashboard
// reads. They mirror schema/status.schema.json ($defs status.PersonView,
// status.RepoIssues, status.RepoImage, status.LatestImage, status.Issue,
// status.Blocker, status.Run, op.HostReport, op.HostInfo,
// op.MemoryReport, op.ProviderReport, op.RunnersReport, op.RunnerState and op.Error);
// follow that file when it changes.

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
}
