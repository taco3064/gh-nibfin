import { describe, expect, it } from 'vitest'

import type { LiveData } from '@/worker/messages'
import { liveRepoSource } from './live-repo-source'

const err = { code: 'github_api_failed', message: 'HTTP 502' }
const data: LiveData = {
  repos: [
    { repo: 'github.com/acme/core', issues: [] },
    { repo: 'github.com/acme/cli', issues: null, issuesError: err },
    { repo: 'github.com/acme/e2e', issues: [] },
  ],
  summary: { since: '2026-11-21T00:00:00+08:00', created: 0, delivered: 0 },
  runners: [],
}

describe('liveRepoSource', () => {
  it('shows nothing before the first snapshot', () => {
    expect(liveRepoSource(undefined, 'github.com/acme/cli')).toEqual({})
  })

  it('opens the first repository by default', () => {
    expect(liveRepoSource(data).data).toEqual({ repo: 'github.com/acme/core', issues: [] })
  })

  it('opens the chosen repository without regard to case, with its error', () => {
    expect(liveRepoSource(data, 'GitHub.com/ACME/cli').data).toEqual({ repo: 'github.com/acme/cli', issues: null, issuesError: err })
  })

  it('falls back to the first when the choice is no longer listed', () => {
    expect(liveRepoSource(data, 'github.com/acme/gone').data?.repo).toBe('github.com/acme/core')
  })

  it('lists every repository on its host for the picker, without permission', () => {
    expect(liveRepoSource(data).source).toEqual({
      host: 'github.com',
      repos: ['core', 'cli', 'e2e'].map((n) => ({
        repo: `acme/${n}`,
        url: `https://github.com/acme/${n}`,
        visibility: 'PUBLIC',
        permission: null,
        execution: null,
      })),
    })
  })

  it('lists none and opens none without repositories', () => {
    expect(liveRepoSource({ ...data, repos: [] })).toEqual({ source: { host: 'github.com', repos: [] } })
  })
})
