// Building the served LiveData from the stored check results. These are
// TypeScript ports of the status rules in internal/status (toIssue,
// IssueOfTitle and the run order), which cannot run in a Worker.

import type { ApiError, Issue, Job, LiveData, RepoData, RepoRunners, Run, Runner, Summary } from './messages'
import type { BlockersEntry, IssueBase, RunsResult, State } from './state'
import { repos, since, workflows } from './state'

const runPrefix = 'nibfin · '

// issueOfTitle returns "owner/repo#n", lower case, from a run title
// "Nibfin · OWNER/REPO#N · …", or "" for any other title (the @ref route
// runs among them). It mirrors status.IssueOfTitle.
export function issueOfTitle(title: string): string {
  const t = title.toLowerCase()
  if (!t.startsWith(runPrefix)) return ''
  const rest = t.slice(runPrefix.length)
  const sep = rest.indexOf(' · ')
  if (sep < 0) return ''
  const head = rest.slice(0, sep)
  const hash = head.indexOf('#')
  if (hash < 0) return ''
  const repo = head.slice(0, hash)
  const num = head.slice(hash + 1)
  if (num === '' || /[@# ]/.test(repo)) return ''
  if (!/^[1-9][0-9]*$/.test(num)) return ''
  const slash = repo.indexOf('/')
  if (slash <= 0) return ''
  const name = repo.slice(slash + 1)
  if (name === '' || name.includes('/')) return ''
  return `${repo}#${num}`
}

// parseJobTitle reads a running job's run title: "Nibfin · OWNER/REPO#N ·
// STAGE" or "Nibfin · OWNER/REPO@REF · STAGE". Another title keeps title
// with null parts.
export function parseJobTitle(title: string, url: string): Job {
  const job: Job = { title, repo: null, issue: null, ref: null, stage: null, url }
  if (!title.toLowerCase().startsWith(runPrefix)) return job
  const rest = title.slice(runPrefix.length)
  const sep = rest.indexOf(' · ')
  if (sep < 0) return job
  const head = rest.slice(0, sep)
  const stage = rest.slice(sep + ' · '.length).trim()
  if (stage === '') return job
  const byIssue = /^([^/@#\s]+\/[^/@#\s]+)#([1-9][0-9]*)$/.exec(head)
  if (byIssue) return { ...job, repo: byIssue[1], issue: Number(byIssue[2]), stage }
  const byRef = /^([^/@#\s]+\/[^/@#\s]+)@(\S+)$/.exec(head)
  if (byRef) return { ...job, repo: byRef[1], ref: byRef[2], stage }
  return job
}

// compareRuns orders runs newest first, then by URL, as status does.
function compareRuns(a: Run, b: Run): number {
  const ta = Date.parse(a.createdAt)
  const tb = Date.parse(b.createdAt)
  if (!Number.isNaN(ta) && !Number.isNaN(tb)) {
    if (ta !== tb) return tb - ta
  } else if (a.createdAt !== b.createdAt) {
    return a.createdAt > b.createdAt ? -1 : 1
  }
  return a.url < b.url ? -1 : a.url > b.url ? 1 : 0
}

export function issueKey(repo: string, number: number): string {
  return `${repo}#${number}`.toLowerCase()
}

function notRead(what: string): ApiError {
  return { code: 'github_api_failed', message: `${what}: not read yet` }
}

interface RunsView {
  byIssue: Map<string, Run[]>
  full: RunsResult[] // pages that held a full 100 runs
  error: ApiError | null
}

function runsView(state: State): RunsView {
  const byIssue = new Map<string, Run[]>()
  const full: RunsResult[] = []
  let error: ApiError | null = null
  for (const repo of repos) {
    for (const wf of workflows) {
      const p = state.repos[repo].runs[wf]
      if (p.result === null) {
        error ??= p.lastError ?? notRead(`the ${wf} runs of ${repo}`)
        continue
      }
      if (p.result.missing) continue
      if (p.result.full) full.push(p.result)
      for (const run of p.result.runs) {
        const key = issueOfTitle(run.name)
        if (key === '') continue
        const list = byIssue.get(key)
        if (list) list.push(run)
        else byIssue.set(key, [run])
      }
    }
  }
  return { byIssue, full, error }
}

function truncated(page: RunsResult, createdAt: string): boolean {
  const created = Date.parse(createdAt)
  let oldest = Infinity
  for (const run of page.runs) {
    const t = Date.parse(run.createdAt)
    if (!Number.isNaN(t)) oldest = Math.min(oldest, t)
  }
  return Number.isNaN(created) || oldest > created
}

function toIssue(repo: string, base: IssueBase, blockers: BlockersEntry | undefined, runs: RunsView): Issue {
  const key = issueKey(repo, base.number)
  const is: Issue = {
    number: base.number,
    title: base.title,
    url: base.url,
    labels: [...base.labels],
    pause: base.labels.some((l) => l.toLowerCase() === 'pause'),
    pending: base.labels.some((l) => l.toLowerCase() === 'pending'),
    createdAt: base.createdAt,
    blockedBy: blockers ? blockers.blockedBy.map((b) => ({ ...b })) : [],
    runs: [...(runs.byIssue.get(key) ?? [])].sort(compareRuns),
  }
  if (!blockers) is.blockedByError = notRead(`the blockers of ${repo}#${base.number}`)
  else if (blockers.error) is.blockedByError = blockers.error
  if (runs.error) is.runsError = runs.error
  if (runs.full.some((page) => truncated(page, base.createdAt))) is.runsTruncated = true
  return is
}

function repoData(state: State, repo: string, runs: RunsView): RepoData {
  const p = state.repos[repo].issues
  const out: RepoData = { repo: `github.com/${repo}`, issues: [] }
  if (p.result === null) {
    out.issuesError = p.lastError ?? notRead(`the open Issues of ${repo}`)
    return out
  }
  out.issues = p.result.pages.flatMap((page) => page.issues).map((base) => toIssue(repo, base, state.blockers.byIssue[issueKey(repo, base.number)], runs))
  return out
}

function repoRunners(state: State, repo: string): RepoRunners {
  const r = state.repos[repo]
  const out: RepoRunners = { repo, runners: [] }
  const error = r.runners.lastError ?? r.inProgress.lastError ?? r.jobs.lastError
  if (r.runners.result === null) {
    out.error = error ?? notRead(`the runners of ${repo}`)
    return out
  }
  out.runners = r.runners.result.runners.map((rn): Runner => {
    let job: Job | null = null
    if (rn.busy) {
      const j = r.jobs.result.find((x) => x.runnerId === rn.id && x.status === 'in_progress')
      if (j) job = parseJobTitle(j.workflowName, j.url)
    }
    return { id: rn.id, status: rn.status, busy: rn.busy, job }
  })
  if (error) out.error = error
  return out
}

function summary(state: State): Summary {
  const { created, delivered } = state.search
  const out: Summary = { since, created: created.result?.total ?? null, delivered: delivered.result?.total ?? null }
  const error = created.lastError ?? delivered.lastError
  if (error) out.error = error
  else if (out.created === null || out.delivered === null) out.error = notRead('the Summary')
  return out
}

// publicData keeps exactly the public fields of every runner. The hub sends
// each snapshot through it, so data stored before runner names were dropped
// is never sent with them.
export function publicData(data: LiveData): LiveData {
  return {
    ...data,
    runners: data.runners.map((r) => ({ ...r, runners: r.runners.map(({ id, status, busy, job }): Runner => ({ id, status, busy, job })) })),
  }
}

export function buildLiveData(state: State): LiveData {
  const runs = runsView(state)
  return {
    repos: repos.map((repo) => repoData(state, repo, runs)),
    summary: summary(state),
    runners: repos.map((repo) => repoRunners(state, repo)),
  }
}
