import { describe, expect, it } from 'vitest'

import { Sequence, filterRepos, offeredActions, pickerSource } from './repo'
import { ownerRepo, repoHost, type NibfinRepo, type PersonView, type RepoView } from './status'

// Fictional data.
const entry = (repo: string): NibfinRepo => ({ repo, url: `https://x/${repo}`, visibility: 'PRIVATE', permission: 'ADMIN', execution: false })

const person = (over: Partial<PersonView> = {}): PersonView => ({
  host: { host: {}, runners: { runners: [] } },
  latestImage: {},
  githubHost: 'github.com',
  repos: [],
  truncated: false,
  nibfinRepos: [entry('acme/app')],
  ...over,
})

const repoView = (over: Partial<RepoView> = {}): RepoView => ({
  host: { host: {}, runners: { runners: [] } },
  latestImage: {},
  repo: 'ghe.example.test/corp/api',
  image: { set: 'none', ref: null, latest: null },
  issues: [],
  nibfinRun: { initialized: false, succeeded: false },
  permission: 'ADMIN',
  nibfinRepos: [entry('corp/web')],
  ...over,
})

describe('repoHost and ownerRepo', () => {
  it('split HOST/OWNER/REPO, also on an Enterprise host', () => {
    expect(repoHost('github.com/acme/app')).toBe('github.com')
    expect(ownerRepo('github.com/acme/app')).toBe('acme/app')
    expect(repoHost('ghe.example.test/corp/api')).toBe('ghe.example.test')
    expect(ownerRepo('ghe.example.test/corp/api')).toBe('corp/api')
    expect(repoHost('acme/app')).toBe('')
    expect(ownerRepo('acme/app')).toBe('acme/app')
  })
})

describe('pickerSource', () => {
  it('takes the repository read first, with its own host', () => {
    expect(pickerSource(repoView(), person())).toEqual({ host: 'ghe.example.test', repos: [entry('corp/web')] })
  })

  it('takes the person read without a repository read', () => {
    expect(pickerSource(undefined, person())).toEqual({ host: 'github.com', repos: [entry('acme/app')] })
  })

  it('never mixes the list and the error of one read with another read', () => {
    const e = { code: 'github_error', message: 'HTTP 502' }
    expect(pickerSource(repoView({ nibfinRepos: null, nibfinReposError: e }), person())).toEqual({
      host: 'ghe.example.test',
      repos: null,
      error: e,
    })
    expect(pickerSource(undefined, person({ nibfinRepos: null, nibfinReposError: e }))).toEqual({ host: 'github.com', repos: null, error: e })
  })

  it('is undefined before any read', () => {
    expect(pickerSource(undefined, undefined)).toBeUndefined()
  })
})

describe('Sequence', () => {
  it('keeps only the latest request current', () => {
    const s = new Sequence()
    const a = s.next()
    const b = s.next()
    expect(s.current(a)).toBe(false)
    expect(s.current(b)).toBe(true)
    s.drop()
    expect(s.current(b)).toBe(false)
  })
})

describe('filterRepos', () => {
  it('filters by OWNER/REPO in any case', () => {
    const list = [entry('acme/app'), entry('acme/Docs'), entry('corp/web')]
    expect(filterRepos(list, ' DOC').map((r) => r.repo)).toEqual(['acme/Docs'])
    expect(filterRepos(list, '')).toHaveLength(3)
  })
})

describe('offeredActions', () => {
  it('offers init without workflows, upgrade with them, both on an error', () => {
    expect(offeredActions({ set: 'none', ref: null, latest: null })).toEqual(['init'])
    expect(offeredActions({ set: 'execution', ref: 'x', latest: true })).toEqual(['upgrade'])
    expect(offeredActions({ set: 'peer', ref: null, latest: null })).toEqual(['upgrade'])
    expect(offeredActions({ ref: null, latest: null, error: { code: 'e', message: 'm' } })).toEqual(['init', 'upgrade'])
  })
})
