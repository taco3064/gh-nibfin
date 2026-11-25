// Status data types: the parts of `gh nibfin status --json` the Dashboard
// reads. They mirror schema/status.schema.json ($defs status.Issue,
// status.Blocker, status.Run and op.Error); follow that file when it changes.

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
