// The sentry-issue workflow's entry point: node dashboard/src/sentry-relay/file/main.ts
// It reads the dispatch from GITHUB_EVENT_PATH (never from a workflow
// expression), files the request with NIBFIN_GITHUB_TOKEN and writes one
// step-summary line. SENTRY_API_TOKEN is optional; without it every request
// is filed unverified.

import { appendFileSync, readFileSync } from 'node:fs'

import { parsePayload } from './payload.ts'
import { fileRequest, summaryLine } from './file.ts'
import { createClient, type Fetch } from './http.ts'

export const GITHUB_API = 'https://api.github.com/'
export const SENTRY_API = 'https://de.sentry.io/api/0/'

export interface MainDeps {
  env: Record<string, string | undefined>
  readFile: (path: string) => string
  summary: (line: string) => void
  fetch: Fetch
  sleep: (ms: number) => Promise<void>
  now: () => number
}

// main returns the process exit code.
export async function main(d: MainDeps): Promise<number> {
  const env = d.env
  const stop = (reason: string) => {
    d.summary(`not filed: ${reason}`)
    return 1
  }
  const repository = env.GITHUB_REPOSITORY ?? ''
  if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository)) return stop('GITHUB_REPOSITORY is not OWNER/REPO')
  const runId = env.GITHUB_RUN_ID ?? ''
  if (!/^[0-9]{1,20}$/.test(runId)) return stop('GITHUB_RUN_ID is not a number')
  if (!env.NIBFIN_GITHUB_TOKEN) return stop('NIBFIN_GITHUB_TOKEN is not set')

  let event: unknown
  try {
    event = JSON.parse(d.readFile(env.GITHUB_EVENT_PATH ?? ''))
  } catch {
    return stop('the event file at GITHUB_EVENT_PATH could not be read')
  }
  const clientPayload = typeof event === 'object' && event !== null ? (event as { client_payload?: unknown }).client_payload : undefined
  const issue = parsePayload(clientPayload)
  if (issue instanceof Error) return stop(`bad payload: ${issue.message}`)

  const github = createClient({
    base: GITHUB_API,
    token: env.NIBFIN_GITHUB_TOKEN,
    fetch: d.fetch,
    sleep: d.sleep,
    headers: { Accept: 'application/vnd.github+json', 'X-GitHub-Api-Version': '2022-11-28', 'User-Agent': 'nibfin-sentry-issue' },
  })
  const sentry = env.SENTRY_API_TOKEN
    ? createClient({ base: SENTRY_API, token: env.SENTRY_API_TOKEN, fetch: d.fetch, sleep: d.sleep, headers: { 'User-Agent': 'nibfin-sentry-issue' } })
    : undefined

  try {
    const outcome = await fileRequest(issue, { repository, runId, github, sentry, now: d.now })
    d.summary(summaryLine(issue, outcome))
    return outcome.problems.length > 0 ? 1 : 0
  } catch (err) {
    // Errors carry the method, path and status only.
    return stop(err instanceof Error ? err.message.slice(0, 300) : 'unknown error')
  }
}

if (import.meta.main) {
  const summaryPath = process.env.GITHUB_STEP_SUMMARY
  process.exitCode = await main({
    env: process.env,
    readFile: (path) => readFileSync(path, 'utf8'),
    summary: (line) => {
      console.log(line)
      if (summaryPath) appendFileSync(summaryPath, line + '\n')
    },
    fetch: (input, init) => fetch(input, init),
    sleep: (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
    now: () => Date.now(),
  })
}
