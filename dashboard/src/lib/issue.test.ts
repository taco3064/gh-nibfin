import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  applyChanges,
  blockersView,
  bulkAccess,
  changeAccess,
  changeIssues,
  pendingView,
  runsView,
  stageOf,
} from './issue'
import { openSession, resetSession } from './session'
import type { Run, StatusIssue } from './status'

const url = 'https://github.com/o/r/issues/1'

function issue(over: Partial<StatusIssue> = {}): StatusIssue {
  return {
    number: 1,
    title: 'T',
    url,
    labels: [],
    pause: false,
    pending: false,
    createdAt: '2026-11-01T00:00:00Z',
    blockedBy: [],
    runs: [],
    ...over,
  }
}

function run(createdAt: string, name = createdAt): Run {
  return { repo: 'o/r', name, workflow: 'w', status: 'completed', conclusion: 'success', createdAt, url: `https://x/${name}` }
}

describe('stageOf', () => {
  it.each([
    [[], 'none'],
    [null, 'none'],
    [['bug'], 'none'],
    [['shaped'], 'none'],
    [['delivered', 'shaped'], 'none'],
    [['scope'], 'shape'],
    [['Scope'], 'shape'],
    [['SCOPE', 'pause', 'pending'], 'shape'],
    [['scope', 'shaped'], 'deliver'],
    [['scope', 'Shaped'], 'deliver'],
    [['scope', 'delivered'], 'done'],
    [['scope', 'shaped', 'DELIVERED'], 'done'],
  ] as const)('%j is %s', (labels, stage) => {
    expect(stageOf(labels as string[] | null)).toBe(stage)
  })
})

describe('changeAccess', () => {
  it.each(['TRIAGE', 'TRIAGE_PLUS', 'MODERATE', 'WRITE', 'MAINTAIN', 'ADMIN', 'admin', 'Triage'])('enables %s', (p) => {
    expect(changeAccess({ permission: p }, 'o/r')).toEqual({ enabled: true })
  })

  it('disables READ with the permission named', () => {
    expect(changeAccess({ permission: 'READ' }, 'o/r')).toEqual({
      enabled: false,
      reason: 'Needs triage permission or higher; this account has READ on o/r.',
    })
  })

  it('disables null', () => {
    expect(changeAccess({ permission: null }, 'o/r')).toEqual({
      enabled: false,
      reason: 'GitHub reported no permission for this account on o/r.',
    })
  })

  it('disables a failed read, whatever the value', () => {
    expect(changeAccess({ permission: 'ADMIN', permissionError: { code: 'github_error', message: 'boom' } }, 'o/r')).toEqual({
      enabled: false,
      reason: 'Permission on o/r could not be read: github_error: boom.',
    })
  })

  it('disables an unknown value', () => {
    expect(changeAccess({ permission: 'OWNER' }, 'o/r')).toEqual({ enabled: false, reason: 'Unknown permission OWNER on o/r.' })
  })
})

describe('bulkAccess', () => {
  const ok = { repo: 'o/a', access: { permission: 'WRITE' } }
  it('is enabled when every repository is', () => {
    expect(bulkAccess([ok, { repo: 'o/b', access: { permission: 'TRIAGE' } }])).toEqual({ enabled: true })
  })

  it('names each repository that is not, once', () => {
    const a = bulkAccess([
      ok,
      { repo: 'o/b', access: { permission: 'READ' } },
      { repo: 'o/b', access: { permission: 'READ' } },
      { repo: 'o/c', access: { permission: null } },
    ])
    expect(a.enabled).toBe(false)
    if (a.enabled) return
    expect(a.reason).toBe(
      'Needs triage permission or higher; this account has READ on o/b. GitHub reported no permission for this account on o/c.',
    )
    expect(a.reason).not.toContain('o/a')
  })

  it('is disabled with nothing selected', () => {
    expect(bulkAccess([]).enabled).toBe(false)
  })
})

describe('runsView', () => {
  it('sorts newest first', () => {
    const v = runsView(issue({ runs: [run('2026-11-01T00:00:00Z'), run('2026-11-03T00:00:00Z'), run('2026-11-02T00:00:00Z')] }))
    expect(v.runs.map((r) => r.createdAt)).toEqual(['2026-11-03T00:00:00Z', '2026-11-02T00:00:00Z', '2026-11-01T00:00:00Z'])
    expect(v).toMatchObject({ notes: [], empty: false })
  })

  it('is empty only when read in full with no runs', () => {
    expect(runsView(issue())).toEqual({ runs: [], notes: [], empty: true })
  })

  it('labels a failed read and keeps what was read', () => {
    const error = { code: 'github_error', message: 'x' }
    expect(runsView(issue({ runsError: error }))).toEqual({ runs: [], notes: [{ kind: 'failed', error }], empty: false })
    const v = runsView(issue({ runs: [run('2026-11-01T00:00:00Z')], runsError: error }))
    expect(v.runs).toHaveLength(1)
    expect(v.notes).toEqual([{ kind: 'failed', error }])
    expect(runsView(issue({ runs: null, runsError: error })).notes).toEqual([{ kind: 'failed', error }])
  })

  it('labels a truncated scan', () => {
    expect(runsView(issue({ runsTruncated: true }))).toEqual({ runs: [], notes: [{ kind: 'truncated' }], empty: false })
  })

  it.each([undefined, null])('labels %j runs as not read', (runs) => {
    expect(runsView(issue({ runs }))).toEqual({ runs: [], notes: [{ kind: 'notRead' }], empty: false })
  })
})

describe('blockersView', () => {
  const b = (state: string, n: number) => ({ repo: 'o/r', number: n, title: `B${n}`, state, url: `https://github.com/o/r/issues/${n}` })

  it('keeps the open blockers only', () => {
    expect(blockersView(issue({ blockedBy: [b('OPEN', 2), b('CLOSED', 3), b('open', 4)] })).open.map((x) => x.number)).toEqual([2, 4])
  })

  it('shows nothing for zero open blockers', () => {
    expect(blockersView(issue({ blockedBy: [b('CLOSED', 3)] }))).toEqual({ open: [], notRead: false })
  })

  it('labels a failed read', () => {
    const error = { code: 'github_error', message: 'x' }
    expect(blockersView(issue({ blockedBy: [], blockedByError: error }))).toEqual({ open: [], failed: error, notRead: false })
  })

  it.each([undefined, null])('labels %j as not read', (blockedBy) => {
    expect(blockersView(issue({ blockedBy }))).toEqual({ open: [], notRead: true })
  })
})

describe('pendingView', () => {
  it('links the comment', () => {
    expect(pendingView(issue({ pending: true, pendingComment: 'https://c' }))).toEqual({ kind: 'link', url: 'https://c' })
  })
  it('labels a failed read', () => {
    const error = { code: 'github_error', message: 'x' }
    expect(pendingView(issue({ pending: true, pendingCommentError: error }))).toEqual({ kind: 'failed', error })
  })
  it('says when none was found', () => {
    expect(pendingView(issue({ pending: true }))).toEqual({ kind: 'notFound' })
  })
})

describe('applyChanges', () => {
  const other = issue({ url: 'https://github.com/o/r/issues/2', number: 2, labels: ['scope'] })

  it('adds pause once and keeps other Issues', () => {
    const [a, b] = applyChanges([issue({ labels: ['scope'] }), other], [{ url: url.toUpperCase(), result: 'paused' }])
    expect(a).toMatchObject({ pause: true, labels: ['scope', 'pause'] })
    expect(b).toBe(other)
    expect(applyChanges([issue({ pause: true, labels: ['Pause'] })], [{ url, result: 'paused' }])[0].labels).toEqual(['Pause'])
  })

  it('removes pause and pending on resume', () => {
    const [a] = applyChanges([issue({ pause: true, pending: true, labels: ['scope', 'Pause', 'PENDING'] })], [{ url, result: 'resumed' }])
    expect(a).toMatchObject({ pause: false, pending: false, labels: ['scope'] })
  })
})

describe('changeIssues', () => {
  type Call = { path: string; method: string; body?: unknown }
  let calls: Call[]

  function server(apply: () => Response | Promise<Response>, plan: () => Response = () => Response.json({ id: 'P1', plan: {} })) {
    return vi.fn(async (path: string, init: RequestInit) => {
      calls.push({ path, method: init.method ?? 'GET', body: init.body ? JSON.parse(init.body as string) : undefined })
      if (path === '/api/session') return Response.json({ session: 'S' })
      if (path.endsWith('/plan')) return plan()
      if (path.endsWith('/apply')) return apply()
      return new Response(null, { status: 204 })
    }) as unknown as typeof fetch
  }

  function ndjson(...lines: unknown[]) {
    return new Response(lines.map((l) => JSON.stringify(l)).join('\n') + '\n', { headers: { 'Content-Type': 'application/x-ndjson' } })
  }

  beforeEach(async () => {
    calls = []
    await openSession('P', server(() => ndjson()))
    calls = []
  })
  afterEach(() => resetSession())

  it('plans, applies without confirmation and returns the read-back', async () => {
    const f = server(() => ndjson({ event: 'result', result: { [url]: 'paused', 'https://github.com/o/r/issues/2': 'paused' } }))
    const r = await changeIssues('pause', [url, 'https://github.com/o/r/issues/2'], f)
    expect(r).toEqual({
      state: 'done',
      changes: [
        { url, result: 'paused' },
        { url: 'https://github.com/o/r/issues/2', result: 'paused' },
      ],
    })
    expect(calls).toEqual([
      { path: '/api/commands/pause/plan', method: 'POST', body: { issues: [url, 'https://github.com/o/r/issues/2'] } },
      { path: '/api/plans/P1/apply', method: 'POST', body: { confirm: false } },
    ])
  })

  it('does not apply after a plan error', async () => {
    const f = server(
      () => ndjson(),
      () => Response.json({ error: { code: 'not_found', message: 'no issue' } }, { status: 422 }),
    )
    expect(await changeIssues('resume', [url], f)).toEqual({ state: 'failed', error: { code: 'not_found', message: 'no issue' } })
    expect(calls.map((c) => c.path)).toEqual(['/api/commands/resume/plan'])
  })

  it('returns an error line', async () => {
    const f = server(() => ndjson({ event: 'error', error: { code: 'label_missing', message: 'no pause label' } }))
    expect(await changeIssues('pause', [url], f)).toEqual({
      state: 'failed',
      error: { code: 'label_missing', message: 'no pause label' },
    })
    expect(calls.map((c) => c.method)).toEqual(['POST', 'POST'])
  })

  it('deletes the plan after a refused apply', async () => {
    const f = server(() => Response.json({ error: { code: 'operation_running', message: 'busy' } }, { status: 409 }))
    expect(await changeIssues('pause', [url], f)).toEqual({ state: 'failed', error: { code: 'operation_running', message: 'busy' } })
    expect(calls.at(-1)).toEqual({ path: '/api/plans/P1', method: 'DELETE', body: undefined })
  })

  it('deletes the plan after confirmation_required', async () => {
    const f = server(() => ndjson({ event: 'error', error: { code: 'confirmation_required', message: 'needs it' } }))
    expect((await changeIssues('pause', [url], f)).state).toBe('failed')
    expect(calls.at(-1)).toMatchObject({ path: '/api/plans/P1', method: 'DELETE' })
  })

  it('is unknown when the stream is cut off', async () => {
    const f = server(() => new Response('{"event":"res', { headers: { 'Content-Type': 'application/x-ndjson' } }))
    const r = await changeIssues('pause', [url], f)
    expect(r.state).toBe('unknown')
    expect(calls.map((c) => c.method)).toEqual(['POST', 'POST'])
  })

  it('is unknown when the apply request fails', async () => {
    const f = server(() => {
      throw new TypeError('Failed to fetch')
    })
    expect(await changeIssues('pause', [url], f)).toMatchObject({ state: 'unknown', error: { code: 'network_error' } })
  })
})
