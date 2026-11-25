import { describe, expect, it } from 'vitest'

import { accessOf, changeAccess } from './issue'
import { complete, dropPartial, fillPersonView, fillRepoView, isComplete, loadingLabels, takePartial, type Shown } from './partial'
import { pickerSource, runLook } from './repo'
import type { NibfinRepo, PersonView, RepoView } from './status'

// Fictional data.
const entry = (repo: string): NibfinRepo => ({ repo, url: `https://x/${repo}`, visibility: 'PRIVATE', permission: 'ADMIN', execution: false })

describe('fillRepoView', () => {
  it('keeps the read sections and fills the others', () => {
    const issues = [{ number: 1, title: 'One', url: 'u1', labels: [], pause: false, pending: false, createdAt: 't' }]
    const v = fillRepoView({ repo: 'github.com/o/r', issues }, 'o/r')
    expect(v.repo).toBe('github.com/o/r')
    expect(v.issues).toEqual(issues)
    expect(v.image).toEqual({ ref: null, latest: null })
    expect(v.nibfinRun).toEqual({ usesNibfin: false, succeeded: false })
    expect(v.nibfinRepos).toBeNull()
    expect(v.host.runners.runners).toBeNull()
  })

  it('takes the requested repository when the partial names none', () => {
    expect(fillRepoView(null, 'o/r').repo).toBe('o/r')
  })
})

describe('fillPersonView', () => {
  it('keeps the read sections and fills the others', () => {
    const v = fillPersonView({ githubHost: 'github.com', user: 'me' })
    expect(v).toMatchObject({ githubHost: 'github.com', user: 'me', repos: [], truncated: false, nibfinRepos: null, latestImage: {} })
  })
})

describe('takePartial', () => {
  const full = { data: 'complete', loading: complete }

  it('shows a partial while nothing is shown', () => {
    const s = takePartial<string>({ loading: complete }, 'part', ['host', 'runs'])
    expect(s.data).toBe('part')
    expect([...s.loading]).toEqual(['host', 'runs'])
    expect(isComplete(s)).toBe(false)
  })

  it('merges a later partial over an earlier one', () => {
    const first = takePartial<string>({ loading: complete }, 'issues', ['image', 'runs'])
    const second = takePartial(first, 'issues and image', ['runs'])
    expect(second).toEqual({ data: 'issues and image', loading: new Set(['runs']) })
  })

  it('never replaces a complete read', () => {
    expect(takePartial<string>(full, 'part', ['host'])).toBe(full)
  })

  it('drops a partial after a failed read but keeps a complete one', () => {
    const part: Shown<string> = { data: 'part', loading: new Set(['host']) }
    expect(dropPartial(part)).toEqual({ loading: complete })
    expect(dropPartial(full)).toBe(full)
  })
})

describe('loadingLabels', () => {
  it('names the asked keys still loading, in reading order, once each', () => {
    const loading = new Set(['latestImage', 'host', 'permission', 'issues', 'repos'])
    expect(loadingLabels(loading, ['permission', 'host', 'latestImage'])).toEqual(['permission', 'host', 'latest image'])
    expect(loadingLabels(loading, ['issues', 'repos'])).toEqual(['Issues'])
    expect(loadingLabels(complete, ['host'])).toEqual([])
  })
})

describe('accessOf', () => {
  it('disables changes while the permission is still being read', () => {
    const a = accessOf({ permission: null }, new Set(['permission']))
    expect(a).toEqual({ permission: null, permissionLoading: true })
    expect(changeAccess(a, 'o/r')).toEqual({ enabled: false, reason: 'Reading the permission on o/r…' })
  })

  it('keeps the read permission and its error', () => {
    const e = { code: 'github_api_failed', message: 'x' }
    expect(accessOf({ permission: 'WRITE' }, complete)).toEqual({ permission: 'WRITE' })
    expect(accessOf({ permission: null, permissionError: e }, complete)).toEqual({ permission: null, permissionError: e })
  })
})

describe('pickerSource while loading', () => {
  const repo = { ...fillRepoView({ repo: 'ghe.example.test/corp/api' }, ''), nibfinRepos: [entry('corp/web')] } satisfies RepoView
  const person = { ...fillPersonView({ githubHost: 'github.com' }), nibfinRepos: [entry('acme/app')] } satisfies PersonView

  it('takes the person list while the repository list is loading', () => {
    expect(pickerSource(repo, person, new Set(['nibfinRepos']))).toEqual({ host: 'github.com', repos: [entry('acme/app')] })
  })

  it('has no list while both are loading', () => {
    const loading = new Set(['nibfinRepos'])
    expect(pickerSource(repo, person, loading, loading)).toBeUndefined()
  })
})

describe('runLook', () => {
  it('names the three states of the Nibfin run, and a failed read', () => {
    expect(runLook({ usesNibfin: false, succeeded: false })).toEqual({ state: 'not-used', tone: 'neutral', label: 'Does not use Nibfin' })
    expect(runLook({ usesNibfin: true, commit: 'abc', changedAt: 't', succeeded: true, run: 'u' })).toEqual({
      state: 'succeeded',
      tone: 'success',
      label: 'A Nibfin run succeeded after the last change',
    })
    expect(runLook({ usesNibfin: true, commit: 'abc', changedAt: 't', succeeded: false })).toEqual({
      state: 'not-succeeded',
      tone: 'warning',
      label: 'No successful Nibfin run since the last change',
    })
    expect(runLook({ usesNibfin: true, succeeded: false, error: { code: 'github_api_failed', message: 'x' } }).state).toBe('error')
  })
})
