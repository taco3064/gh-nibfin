import { describe, expect, it } from 'vitest'

import {
  applyPersonChanges,
  filterNeedsYou,
  imageState,
  keepSelection,
  needsYou,
  needsYouCount,
  replaceIssue,
  runCounts,
  runnerCounts,
  stageCounts,
} from '@/lib/person'
import type { HostReport, PersonView, RepoIssues, Run, RunnerState, StatusIssue } from '@/lib/status'

const err = { code: 'github_error', message: 'HTTP 502' }

describe('imageState', () => {
  const latest = { ref: 'ghcr.io/x/img:v2' }
  it('labels every state with text', () => {
    expect(imageState({ ref: null, latest: null, error: err }, latest)).toEqual({
      kind: 'error',
      tone: 'danger',
      label: 'Image could not be read',
      detail: 'github_error: HTTP 502',
    })
    expect(imageState({ set: 'none', ref: null, latest: null }, latest)).toMatchObject({ tone: 'neutral', label: 'No Nibfin workflows' })
    expect(imageState({ set: 'peer', ref: null, latest: null }, latest)).toMatchObject({ tone: 'neutral', label: 'Peer set: no image pin' })
    expect(imageState({ set: 'execution', ref: null, latest: null }, latest)).toMatchObject({ tone: 'warning', label: 'No valid image pin' })
    expect(imageState({ set: 'execution', ref: 'a', latest: true }, latest)).toMatchObject({ tone: 'success', label: 'Latest image' })
    expect(imageState({ set: 'execution', ref: 'a', latest: false }, latest)).toMatchObject({ tone: 'warning', label: 'Not the latest image' })
    expect(imageState({ set: 'odd', ref: null, latest: null }, latest)).toMatchObject({ tone: 'neutral', label: 'Unknown workflow set odd' })
  })
  it('gives the latest image error as the unknown detail', () => {
    expect(imageState({ set: 'execution', ref: 'a', latest: null }, latest)).toEqual({
      kind: 'unknown',
      tone: 'neutral',
      label: 'Latest image unknown',
    })
    expect(imageState({ set: 'execution', ref: 'a', latest: null }, { error: err }).detail).toBe('github_error: HTTP 502')
  })
})

function issue(url: string, labels: string[]): StatusIssue {
  return {
    number: 1,
    title: 't',
    url,
    labels,
    pause: labels.includes('pause'),
    pending: labels.includes('pending'),
    createdAt: '2026-11-01T00:00:00Z',
  }
}

const repo = (name: string, issues: StatusIssue[] | null): RepoIssues => ({
  repo: name,
  image: { set: 'none', ref: null, latest: null },
  issues,
  permission: 'WRITE',
})

const view = (repos: RepoIssues[] | null): PersonView => ({
  host: { host: {}, memory: { totalKB: 0, availableKB: 0 }, runners: { runners: [] }, providers: [] },
  latestImage: {},
  githubHost: 'github.com',
  repos,
  truncated: false,
  nibfinRepos: [],
})

describe('applyPersonChanges', () => {
  it('applies changes across repositories', () => {
    const v = view([
      repo('a/x', [issue('https://github.com/a/x/issues/1', ['scope']), issue('https://github.com/a/x/issues/2', ['scope'])]),
      repo('a/y', [issue('https://github.com/a/y/issues/3', ['scope', 'pause', 'pending'])]),
      repo('a/z', null),
    ])
    const next = applyPersonChanges(v, [
      { url: 'https://github.com/a/x/issues/2', result: 'paused' },
      { url: 'https://github.com/a/y/issues/3', result: 'resumed' },
    ])
    expect(next.repos![0].issues!.map((i) => i.pause)).toEqual([false, true])
    expect(next.repos![1].issues![0]).toMatchObject({ pause: false, pending: false, labels: ['scope'] })
    expect(next.repos![2].issues).toBeNull()
    expect(v.repos![0].issues![1].pause).toBe(false)
  })
  it('keeps the view for no changes', () => {
    const v = view(null)
    expect(applyPersonChanges(v, [])).toBe(v)
  })
})

describe('keepSelection', () => {
  it('drops URLs the view no longer lists', () => {
    const v = view([repo('a/x', [issue('u1', [])]), repo('a/y', null)])
    expect([...keepSelection(new Set(['u1', 'u2']), v)]).toEqual(['u1'])
    expect(keepSelection(new Set(['u1']), view(null)).size).toBe(0)
  })
})

describe('replaceIssue', () => {
  it('replaces the listed Issue of the same URL in its repository', () => {
    const v = view([repo('a/x', [issue('u1', ['scope']), issue('u2', ['scope'])]), repo('a/y', [issue('u3', [])])])
    const next = replaceIssue(v, 'A/X', issue('u2', ['scope', 'pause']))
    expect(next.repos![0].issues!.map((i) => i.pause)).toEqual([false, true])
    expect(next.repos![1]).toBe(v.repos![1])
    expect(v.repos![0].issues![1].pause).toBe(false)
  })
  it('keeps the view when the Issue is not listed there', () => {
    const v = view([repo('a/x', [issue('u1', [])])])
    expect(replaceIssue(v, 'a/x', issue('u9', []))).toBe(v)
    expect(replaceIssue(v, 'a/y', issue('u1', []))).toBe(v)
    const empty = view(null)
    expect(replaceIssue(empty, 'a/x', issue('u1', []))).toBe(empty)
  })
})

const run = (conclusion: string | null): Run => ({
  repo: 'a/x',
  name: 'n',
  workflow: 'w',
  status: conclusion === null ? 'in_progress' : 'completed',
  conclusion,
  createdAt: '2026-11-02T00:00:00Z',
  url: 'https://x/run',
})

describe('stageCounts', () => {
  it('counts the listed Issues by stage from labels, other stages as other', () => {
    const v = view([
      repo('a/x', [issue('u1', ['scope']), issue('u2', ['scope', 'shaped']), issue('u3', ['scope', 'shaped', 'delivered'])]),
      repo('a/y', [issue('u4', []), issue('u5', ['scope', 'shaped', 'pause'])]),
      repo('a/z', null),
    ])
    expect(stageCounts(v)).toEqual({ total: 5, shape: 1, deliver: 2, other: 2 })
  })
  it('counts zero Issues', () => {
    expect(stageCounts(view(null))).toEqual({ total: 0, shape: 0, deliver: 0, other: 0 })
    expect(stageCounts(view([]))).toEqual({ total: 0, shape: 0, deliver: 0, other: 0 })
  })
})

describe('needsYou', () => {
  it('is pending or pause', () => {
    expect(needsYou(issue('u', ['pause']))).toBe(true)
    expect(needsYou(issue('u', ['pending']))).toBe(true)
    expect(needsYou(issue('u', ['scope']))).toBe(false)
    const v = view([repo('a/x', [issue('u1', ['pause']), issue('u2', ['scope'])]), repo('a/y', [issue('u3', ['pending', 'pause'])])])
    expect(needsYouCount(v)).toBe(2)
  })
})

describe('runCounts', () => {
  it('groups the conclusions of every listed Issue', () => {
    const v = view([
      repo('a/x', [
        { ...issue('u1', []), runs: [run('success'), run('failure'), run(null)] },
        { ...issue('u2', []), runs: [run('timed_out'), run('startup_failure'), run('cancelled'), run('success')] },
      ]),
    ])
    expect(runCounts(v)).toEqual({ total: 7, succeeded: 2, failed: 3, running: 1, other: 1, incomplete: false })
  })
  it('marks missing, failed and truncated run reads as incomplete', () => {
    const read = { ...issue('u1', []), runs: [run('success')] }
    expect(runCounts(view([repo('a/x', [read])])).incomplete).toBe(false)
    expect(runCounts(view([repo('a/x', [read, issue('u2', [])])])).incomplete).toBe(true)
    expect(runCounts(view([repo('a/x', [{ ...issue('u2', []), runs: null }])])).incomplete).toBe(true)
    expect(runCounts(view([repo('a/x', [{ ...read, runsError: err }])])).incomplete).toBe(true)
    const cut = runCounts(view([repo('a/x', [{ ...read, runsTruncated: true }])]))
    expect(cut).toMatchObject({ total: 1, succeeded: 1, incomplete: true })
  })
  it('counts no runs', () => {
    expect(runCounts(view(null))).toEqual({ total: 0, succeeded: 0, failed: 0, running: 0, other: 0, incomplete: false })
  })
})

describe('runnerCounts', () => {
  const slot = (over: Partial<RunnerState>): RunnerState => ({
    slot: 's',
    provider: 'claude',
    configured: true,
    unit: true,
    enabled: true,
    active: true,
    listed: true,
    status: 'online',
    busy: false,
    ...over,
  })
  const host = (runners: RunnerState[] | null, memory = { totalKB: 8 * 1024 * 1024, availableKB: 2 * 1024 * 1024 }): HostReport => ({
    host: {},
    memory,
    runners: { runners },
    providers: [],
  })
  it('counts online and busy slots like the runner view', () => {
    const h = host([slot({}), slot({ busy: true }), slot({ status: 'offline', busy: true }), slot({ status: undefined, listed: false })])
    expect(runnerCounts(h)).toEqual({ github: 'read', online: 2, busy: 1, usedKB: 6 * 1024 * 1024, totalKB: 8 * 1024 * 1024 })
  })
  it('says when GitHub was not read or needs admin', () => {
    const h = host([slot({ status: undefined, listed: false })])
    expect(runnerCounts({ ...h, runners: { ...h.runners, githubNeedsAdmin: true } }).github).toBe('needs-admin')
    expect(runnerCounts({ ...h, runners: { ...h.runners, githubError: err } }).github).toBe('error')
    expect(runnerCounts({ ...h, runners: { ...h.runners, githubError: err, githubNeedsAdmin: true } }).github).toBe('error')
  })
  it('gives no memory when it was not read', () => {
    expect(runnerCounts(host(null, { totalKB: 0, availableKB: 0 }))).toEqual({ github: 'read', online: 0, busy: 0, usedKB: null, totalKB: null })
    expect(runnerCounts(host([], { totalKB: 100, availableKB: 50, error: err } as HostReport['memory']))).toMatchObject({ usedKB: null, totalKB: null })
  })
})

describe('filterNeedsYou', () => {
  it('keeps the Issues that need you and drops empty repositories', () => {
    const v = view([
      repo('a/x', [issue('u1', ['pause']), issue('u2', ['scope'])]),
      repo('a/y', [issue('u3', ['scope'])]),
      repo('a/z', null),
      repo('a/w', [issue('u4', ['pending'])]),
    ])
    const f = filterNeedsYou(v)
    expect(f.repos!.map((r) => [r.repo, r.issues!.map((i) => i.url)])).toEqual([
      ['a/x', ['u1']],
      ['a/w', ['u4']],
    ])
    expect(v.repos![0].issues).toHaveLength(2)
    expect(filterNeedsYou(view(null)).repos).toEqual([])
  })
})
