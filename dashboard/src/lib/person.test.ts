import { describe, expect, it } from 'vitest'

import { applyPersonChanges, imageState, keepSelection } from '@/lib/person'
import type { PersonView, RepoIssues, StatusIssue } from '@/lib/status'

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
  host: { host: {}, runners: { runners: [] } },
  latestImage: {},
  githubHost: 'github.com',
  repos,
  truncated: false,
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
