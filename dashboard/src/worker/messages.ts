// The public live data the Worker serves, and the events it sends. Each
// event is one WebSocket text message holding the event's JSON, the same
// JSON the local Dashboard stream (GET /api/live) sends as a data: frame.
// This file holds types only, so the page can import type it.

// ApiError is the schema's op.Error.
export interface ApiError {
  code: string
  message: string
}

// LiveEvent is one message. The Worker sends no issue or podman events:
// a change sends the whole snapshot.
export type LiveEvent =
  | { event: 'snapshot'; at: string; result: LiveData }
  | { event: 'checked'; at: string }
  | { event: 'error'; source: 'github'; error: ApiError }

export interface LiveData {
  repos: RepoData[] // the three repositories, in a fixed order
  summary: Summary
  runners: RepoRunners[]
}

// RepoData holds the status.RepoView keys about a repository's Issues.
// repo is HOST/OWNER/REPO, as status.RepoView.repo.
export interface RepoData {
  repo: string
  issues: Issue[] | null
  issuesError?: ApiError
}

// Summary counts Issues since the showcase start, summed over the three
// repositories: created since then, and closed since then with delivered
// whenever they were created. A count is null before its first read.
// days is the delivered count of each of the last 7 Asia/Taipei days,
// oldest first and today last, by the same delivered rule; it is absent
// until that trend is read and whenever its last read failed.
export interface Summary {
  since: '2026-11-21T00:00:00+08:00'
  created: number | null
  delivered: number | null
  days?: TrendDay[]
  error?: ApiError
}

// TrendDay is one Asia/Taipei day; date is YYYY-MM-DD.
export interface TrendDay {
  date: string
  delivered: number
}

// RepoRunners lists one repository's self-hosted runners. repo is
// OWNER/REPO.
export interface RepoRunners {
  repo: string
  runners: Runner[]
  error?: ApiError
}

// Runner carries GitHub's status and busy as they are, and for a busy
// runner its running Nibfin job, or null when none matched. It carries no
// runner name: names include host names, and the runner list is not
// otherwise public.
export interface Runner {
  id: number
  status: string
  busy: boolean
  job: Job | null
}

// Job is a running job, read from its run title "Nibfin · OWNER/REPO#N ·
// STAGE" or "Nibfin · OWNER/REPO@REF · STAGE". A title of another form
// keeps title with null parts.
export interface Job {
  title: string
  repo: string | null
  issue: number | null
  ref: string | null
  stage: string | null
  url: string
}

// Issue, Blocker and Run are the schema's status.Issue, status.Blocker and
// status.Run. pendingComment is never read.
export interface Issue {
  number: number
  title: string
  url: string
  labels: string[] | null
  pause: boolean
  pending: boolean
  createdAt: string
  blockedBy: Blocker[] | null
  blockedByError?: ApiError
  runs: Run[] | null
  runsError?: ApiError
  runsTruncated?: boolean
}

export interface Blocker {
  repo: string
  number: number
  title: string
  state: string
  url: string
}

export interface Run {
  repo: string
  name: string
  workflow: string
  status: string
  conclusion: string | null
  createdAt: string
  url: string
}
