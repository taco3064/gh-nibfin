import { describe, expect, it } from 'vitest'

import type { Job, RepoRunners, Runner } from '../../worker/messages'
import { capacityCounts, jobTarget, runnerCards, runnerErrors, safeUrl } from './runners'

const job = (over: Partial<Job> = {}): Job => ({
  title: 'Nibfin · acme/app#12 · shape',
  repo: 'acme/app',
  issue: 12,
  ref: null,
  stage: 'shape',
  url: 'https://github.com/acme/app/actions/runs/1/job/2',
  ...over,
})

const err = { code: 'github_api_failed', message: 'HTTP 502' }

// An older message may still carry a runner name; a card never does.
const named = (r: Runner, name: string) => ({ ...r, name }) as Runner

const runners: RepoRunners[] = [
  {
    repo: 'acme/core',
    runners: [
      named({ id: 7, status: 'online', busy: true, job: job() }, 'host-a-1'),
      { id: 8, status: 'online', busy: false, job: null },
    ],
  },
  { repo: 'acme/cli', runners: [], error: err },
  {
    repo: 'acme/e2e',
    runners: [
      { id: 1, status: 'offline', busy: false, job: null },
      { id: 2, status: 'starting', busy: false, job: null },
      { id: 3, status: 'online', busy: true, job: null },
    ],
  },
]

describe('runnerCards', () => {
  it('labels runners by place and gives each its state', () => {
    const cards = runnerCards(runners)
    expect(cards.map((c) => [c.key, c.label, c.state, c.status])).toEqual([
      ['acme/core:7', 'Runner 1', 'busy', 'online'],
      ['acme/core:8', 'Runner 2', 'idle', 'online'],
      ['acme/e2e:1', 'Runner 3', 'offline', 'offline'],
      ['acme/e2e:2', 'Runner 4', 'other', 'starting'],
      ['acme/e2e:3', 'Runner 5', 'busy', 'online'],
    ])
    expect(cards[0].job).toEqual(job())
    expect(cards[4].job).toBeNull()
  })

  it('never carries a runner name', () => {
    const cards = runnerCards(runners)
    expect(Object.keys(cards[0])).toEqual(['key', 'label', 'state', 'status', 'job'])
    expect(JSON.stringify(cards)).not.toContain('host-a-1')
  })

  it('counts the states and lists the repositories whose runners failed', () => {
    expect(capacityCounts(runnerCards(runners))).toEqual({ total: 5, busy: 2, idle: 1, offline: 1, other: 1 })
    expect(runnerErrors(runners)).toEqual([{ repo: 'acme/cli', error: err }])
  })
})

describe('jobTarget', () => {
  it('links the Issue of a valid OWNER/REPO and number', () => {
    expect(jobTarget(job())).toEqual({ kind: 'issue', label: 'acme/app#12', url: 'https://github.com/acme/app/issues/12' })
  })

  it('never builds a link from an invalid repository or number', () => {
    expect(jobTarget(job({ repo: 'acme/app/../x', issue: 12 })).kind).toBe('title')
    expect(jobTarget(job({ repo: 'javascript:alert(1)//x', issue: 12 })).kind).toBe('title')
    expect(jobTarget(job({ issue: 0 })).kind).toBe('title')
    expect(jobTarget(job({ issue: 1.5 })).kind).toBe('title')
  })

  it('names the ref of a route job', () => {
    expect(jobTarget(job({ issue: null, ref: 'nibfin/issue-3', stage: 'route' }))).toEqual({ kind: 'ref', label: 'acme/app@nibfin/issue-3' })
  })

  it('keeps the title of a run title it could not read', () => {
    expect(jobTarget(job({ title: 'Some other run', repo: null, issue: null, stage: null }))).toEqual({ kind: 'title', label: 'Some other run' })
  })
})

describe('safeUrl', () => {
  it('keeps https URLs only', () => {
    expect(safeUrl('https://github.com/x')).toBe('https://github.com/x')
    expect(safeUrl('javascript:alert(1)')).toBeUndefined()
    expect(safeUrl('http://github.com/x')).toBeUndefined()
    expect(safeUrl('not a url')).toBeUndefined()
    expect(safeUrl(null)).toBeUndefined()
  })
})
