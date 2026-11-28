import { describe, expect, it } from 'vitest'

import commands from '@/app/data/commands.json'

import {
  afterPlan,
  applyAtOnce,
  elapsedSeconds,
  endedSummary,
  fieldsOf,
  initialRun,
  initialValues,
  integerText,
  requestBody,
  requiredText,
  runReducer,
  summary,
  unknownSummary,
  type Command,
  type Plan,
  type RunAction,
  type RunState,
} from './operation'

// Fixtures in the shape of GET /api/commands, with fictional values.
const add: Command = {
  path: 'runner add',
  api: 'runner-add',
  short: 'Add Runners',
  long: '',
  flags: [
    { name: 'count', shorthand: '', type: 'int', default: '2', usage: 'number of Runners', required: false, param: 'count' },
    { name: 'yes', shorthand: 'y', type: 'bool', default: 'false', usage: 'skip the confirmation', required: false },
  ],
  args: [{ name: 'provider', required: true, repeatable: false, allowed: ['alpha', 'beta'], param: 'provider' }],
}

const remove: Command = {
  path: 'runner remove',
  api: 'runner-remove',
  short: '',
  long: '',
  flags: [
    { name: 'count', shorthand: '', type: 'int', default: '0', usage: 'number (default: all)', required: false, param: 'count' },
    { name: 'delete-login', shorthand: '', type: 'bool', default: 'false', usage: 'also delete', required: false, param: 'deleteLogin' },
    { name: 'json', shorthand: '', type: 'bool', default: 'false', usage: 'print JSON', required: false, param: '' },
  ],
  args: [{ name: 'provider', required: true, repeatable: false, allowed: ['alpha', 'beta'], param: 'provider' }],
}

const mixed: Command = {
  path: 'mixed',
  api: 'mixed',
  short: '',
  long: '',
  flags: [
    { name: 'repo', shorthand: 'R', type: 'string', default: '', usage: 'repository', required: false, param: 'repo' },
    { name: 'when', shorthand: '', type: 'duration', default: '5s', usage: 'wait', required: false, param: 'when' },
    { name: 'mode', shorthand: '', type: 'string', default: 'fast', usage: 'mode', required: false, allowed: ['fast', 'slow'], param: 'mode' },
    { name: 'port', shorthand: '', type: 'int', default: '0', usage: 'port', required: false },
  ],
  args: [
    { name: 'provider', required: false, repeatable: false, allowed: ['alpha', 'beta'], param: 'provider' },
    { name: 'issue', required: true, repeatable: true, param: 'issues' },
  ],
}

describe('fieldsOf', () => {
  it('builds one field per input with a request field, args first', () => {
    const f = fieldsOf(add)
    expect(f.map((x) => [x.key, x.kind, x.required, x.initial])).toEqual([
      ['provider', 'choice', true, ''],
      ['count', 'integer', false, '2'],
    ])
    expect(f[0]).toMatchObject({ cli: '<provider>', allowed: ['alpha', 'beta'], multiple: false })
    expect(f[1]).toMatchObject({ cli: '--count', help: 'number of Runners' })
  })

  it('leaves CLI-only inputs out and parses defaults by type', () => {
    expect(fieldsOf(remove).map((x) => [x.key, x.kind, x.initial])).toEqual([
      ['provider', 'choice', ''],
      ['count', 'integer', '0'],
      ['deleteLogin', 'boolean', false],
    ])
  })

  it('maps lists, text, unknown types and choice flags', () => {
    const f = fieldsOf(mixed)
    expect(f.map((x) => [x.key, x.kind, x.required, x.initial])).toEqual([
      ['provider', 'choice', false, ''],
      ['issues', 'list', true, ''],
      ['repo', 'text', false, ''],
      ['when', 'text', false, '5s'],
      ['mode', 'choice', false, 'fast'],
    ])
    expect(f[0].help).toMatch(/default/)
    expect(f[1].cli).toBe('<issue>...')
  })

  it('accepts a schema with null lists', () => {
    expect(fieldsOf({ path: 'x', api: 'x', short: '', long: '', flags: null, args: null })).toEqual([])
  })

  it('marks a repeatable choice as multiple', () => {
    const c: Command = { ...mixed, flags: [], args: [{ name: 'p', required: true, repeatable: true, allowed: ['a', 'b'], param: 'ps' }] }
    expect(fieldsOf(c)[0]).toMatchObject({ kind: 'choice', multiple: true })
  })
})

describe('initialValues', () => {
  it('starts at the defaults, or at an initial value for a known key', () => {
    const f = fieldsOf(add)
    expect(initialValues(f)).toEqual({ provider: '', count: '2' })
    expect(initialValues(f, { count: '5', provider: 'beta', unknown: 'x' })).toEqual({ provider: 'beta', count: '5' })
  })
})

describe('requestBody', () => {
  it('builds the body with JSON types', () => {
    const f = fieldsOf(remove)
    expect(requestBody(f, { ...initialValues(f), provider: 'beta', count: ' 3 ', deleteLogin: true })).toEqual({
      ok: true,
      body: { provider: 'beta', count: 3, deleteLogin: true },
    })
  })

  it('reports a missing required field and a bad integer', () => {
    const f = fieldsOf(add)
    expect(requestBody(f, { provider: '', count: '2x' })).toEqual({
      ok: false,
      errors: { provider: requiredText, count: integerText },
    })
  })

  it('rejects a choice outside allowed', () => {
    const f = fieldsOf(add)
    expect(requestBody(f, { provider: 'gamma', count: '1' })).toEqual({
      ok: false,
      errors: { provider: 'Choose one of: alpha, beta.' },
    })
  })

  it('omits empty optional fields and splits lists', () => {
    const f = fieldsOf(mixed)
    const r = requestBody(f, { ...initialValues(f), issues: 'a/b#1, a/b#2\n https://x/y/issues/3', when: '' })
    expect(r).toEqual({ ok: true, body: { issues: ['a/b#1', 'a/b#2', 'https://x/y/issues/3'], mode: 'fast' } })
  })

  it('requires a list with at least one value', () => {
    const f = fieldsOf(mixed)
    expect(requestBody(f, { ...initialValues(f), issues: ' ,, ' })).toMatchObject({ ok: false, errors: { issues: requiredText } })
  })

  it('omits an empty optional integer so the server default applies', () => {
    const f = fieldsOf(add)
    expect(requestBody(f, { provider: 'alpha', count: '' })).toEqual({ ok: true, body: { provider: 'alpha' } })
  })

  it('sends a multiple choice as an array', () => {
    const c: Command = { ...mixed, flags: [], args: [{ name: 'p', required: true, repeatable: true, allowed: ['a', 'b'], param: 'ps' }] }
    const f = fieldsOf(c)
    expect(requestBody(f, { ps: 'a,b' })).toEqual({ ok: true, body: { ps: ['a', 'b'] } })
    expect(requestBody(f, { ps: 'a,c' })).toMatchObject({ ok: false })
  })
})

const plan: Plan = {
  operation: 'runner-add',
  steps: [
    { id: 'login', action: 'log in', target: 'alpha', current: 'absent', next: 'logged in', noChange: false, afterStart: false },
    { id: 'image', action: 'pull', target: 'image', current: 'v1', next: 'v1', noChange: true, afterStart: false },
    { id: 'slots', action: 'start', target: 'slots', next: 'running', noChange: false, afterStart: true },
  ],
  notes: ['a note'],
  confirm: true,
  readOnly: false,
}

function run(...actions: RunAction[]): RunState {
  return actions.reduce(runReducer, initialRun)
}

const planned: RunAction = { type: 'planned', id: 'P1', plan }
const planStart: RunAction = { type: 'plan', at: 1000 }

describe('runReducer', () => {
  it('goes from planning to preview with one row per step', () => {
    const s = run(planStart, planned)
    expect(s.phase).toBe('preview')
    expect(s.planId).toBe('P1')
    expect(s.rows.map((r) => [r.id, r.progress])).toEqual([
      ['login', 'waiting'],
      ['image', 'waiting'],
      ['slots', 'waiting'],
    ])
  })

  it('shows the checked items and the running check while planning', () => {
    const [login, image, slots] = plan.steps!
    let s = run(planStart)
    expect(s).toMatchObject({ phase: 'planning', planStarted: 1000, rows: [] })
    expect(s.checking).toBeUndefined()
    s = run(planStart, { type: 'checking', check: 'login' }, { type: 'checked', steps: [login] }, { type: 'checking', check: 'image' })
    expect(s.checking).toBe('image')
    expect(s.rows.map((r) => [r.id, r.progress, r.step?.current])).toEqual([['login', 'waiting', 'absent']])
    // The same id again replaces its row in place.
    s = runReducer(s, { type: 'checked', steps: [image, { ...login, current: 'expired' }] })
    expect(s.rows.map((r) => [r.id, r.step?.current])).toEqual([
      ['login', 'expired'],
      ['image', 'v1'],
    ])
    expect(s.rows[1].step?.noChange).toBe(true)
    // The final plan replaces the rows, in its order, and ends the check.
    s = runReducer(runReducer(s, { type: 'checked', steps: [slots] }), planned)
    expect(s.phase).toBe('preview')
    expect(s.rows.map((r) => [r.id, r.step?.current])).toEqual([
      ['login', 'absent'],
      ['image', 'v1'],
      ['slots', undefined],
    ])
    expect(s.checking).toBeUndefined()
    expect(s.planStarted).toBeUndefined()
  })

  it('ignores planning events outside planning', () => {
    const s = run(planned, { type: 'checking', check: 'late' }, { type: 'checked', steps: [plan.steps![0]] })
    expect(s.checking).toBeUndefined()
    expect(s.rows).toHaveLength(3)
    // A new Preview starts with no rows.
    expect(run(planStart, { type: 'checked', steps: [plan.steps![0]] }, planStart).rows).toEqual([])
  })

  it('drops the checked items when the plan fails without a plan', () => {
    const s = run(planStart, { type: 'checked', steps: [plan.steps![0]] }, { type: 'planFailed', error: { code: 'bad', message: 'm' } })
    expect(s.rows).toEqual([])
    expect(s.checking).toBeUndefined()
  })

  it('keeps a plan error in planFailed', () => {
    expect(run(planStart, { type: 'planFailed', error: { code: 'bad', message: 'm' } })).toMatchObject({
      phase: 'planFailed',
      error: { code: 'bad' },
    })
  })

  it('updates rows by id and adds items the plan did not list', () => {
    const s = run(
      planned,
      { type: 'apply' },
      { type: 'progress', event: { id: 'login', action: 'log in', target: 'alpha', phase: 'started' } },
      { type: 'progress', event: { id: 'login', action: 'log in', target: 'alpha', phase: 'done' } },
      { type: 'progress', event: { id: 'slot-1', action: 'start', target: 'slot 1', phase: 'failed', code: 'busy', message: 'try again' } },
    )
    expect(s.phase).toBe('applying')
    expect(s.rows.map((r) => [r.id, r.progress])).toEqual([
      ['login', 'done'],
      ['image', 'waiting'],
      ['slots', 'waiting'],
      ['slot-1', 'failed'],
    ])
    expect(s.rows[3]).toMatchObject({ action: 'start', target: 'slot 1', error: { code: 'busy', message: 'try again' } })
    // The after-start placeholder stays.
    expect(s.rows[2].step?.afterStart).toBe(true)
  })

  it('ignores progress outside an apply', () => {
    const s = run(planned, { type: 'progress', event: { id: 'login', action: '', target: '', phase: 'done' } })
    expect(s.rows[0].progress).toBe('waiting')
  })

  it('maps the read-back onto rows and adds checks without a row', () => {
    const s = run(planned, { type: 'apply' }, {
      type: 'applied',
      outcome: {
        ok: true,
        value: {
          items: [
            { id: 'login', action: 'log in', target: 'alpha', confirmed: true, state: 'logged in' },
            { id: 'slot-1', action: 'start', target: 'slot 1', confirmed: false, state: 'stopped', reason: 'not running' },
          ],
        },
      },
    })
    expect(s.phase).toBe('finished')
    expect(s.ending).toBe('result')
    expect(s.rows.find((r) => r.id === 'login')?.check?.confirmed).toBe(true)
    expect(s.rows.find((r) => r.id === 'image')?.check).toBeUndefined()
    expect(s.rows.at(-1)).toMatchObject({ id: 'slot-1', check: { reason: 'not running' } })
  })

  it('returns a refused apply to the preview', () => {
    const s = run(planned, { type: 'apply' }, {
      type: 'applied',
      outcome: { ok: false, status: 409, error: { code: 'operation_running', message: 'busy' } },
    })
    expect(s).toMatchObject({ phase: 'preview', planId: 'P1', refused: { status: 409, error: { code: 'operation_running' } } })
    expect(run(planned, { type: 'apply' }, { type: 'applied', outcome: { ok: false, status: 404, error: { code: 'not_found', message: '' } } }, { type: 'apply' }).refused).toBeUndefined()
  })

  it('reads back a read-only plan at once', () => {
    const s = run(planStart, {
      type: 'readBack',
      plan: { ...plan, steps: [plan.steps![1]], readOnly: true },
      result: { items: [{ id: 'image', action: 'pull', target: 'image', confirmed: true, state: 'v1' }] },
    })
    expect(s).toMatchObject({ phase: 'finished', ending: 'readOnly' })
    expect(s.planId).toBeUndefined()
    expect(s.rows[0].check?.state).toBe('v1')
  })
})

describe('summary', () => {
  const check = (id: string, confirmed: boolean) => ({ id, action: 'a', target: 't', confirmed })

  it('is null before the end', () => {
    expect(summary(run(planned))).toBeNull()
  })

  it('confirms only when every check is confirmed', () => {
    const s = run(planned, { type: 'apply' }, { type: 'applied', outcome: { ok: true, value: { items: [check('a', true), check('b', true)] } } })
    expect(summary(s)).toEqual({ kind: 'confirmed', text: 'Confirmed: all 2 items were read back.' })
  })

  it('counts the not confirmed', () => {
    const s = run(planned, { type: 'apply' }, { type: 'applied', outcome: { ok: true, value: { items: [check('a', true), check('b', false)] } } })
    expect(summary(s)).toMatchObject({ kind: 'notConfirmed', text: expect.stringContaining('1 of 2') })
  })

  it('is not confirmed when an error came with a fully confirmed result', () => {
    const s = run(planned, { type: 'apply' }, {
      type: 'applied',
      outcome: { ok: false, error: { code: 'x', message: 'm' }, result: { items: [check('a', true)] } },
    })
    expect(summary(s)?.kind).toBe('notConfirmed')
    expect(s.error?.code).toBe('x')
  })

  it('fails without a result, with no row read back', () => {
    const s = run(
      planned,
      { type: 'apply' },
      { type: 'progress', event: { id: 'login', action: '', target: '', phase: 'done' } },
      { type: 'applied', outcome: { ok: false, error: { code: 'x', message: 'm' } } },
    )
    expect(summary(s)?.kind).toBe('failed')
    expect(s.rows.every((r) => r.check === undefined)).toBe(true)
  })

  it('is unknown after a cut stream, even when every item reported done', () => {
    const s = run(
      planned,
      { type: 'apply' },
      ...plan.steps!.map((st): RunAction => ({ type: 'progress', event: { id: st.id, action: '', target: '', phase: 'done' } })),
      { type: 'applied', outcome: { ok: false, unknown: true, error: { code: 'stream_incomplete', message: 'm' } } },
    )
    expect(summary(s)).toEqual({ kind: 'unknown', text: unknownSummary })
  })

  it('is not confirmed for a read-only plan whose read-back failed', () => {
    const s = run({
      type: 'readBack',
      plan,
      result: { items: [check('a', false)] },
      error: { code: 'read_failed', message: 'm' },
    })
    expect(summary(s)?.kind).toBe('notConfirmed')
  })
})

describe('initialValues with initial', () => {
  it('replaces the defaults of known keys only', () => {
    const v = initialValues(fieldsOf(add), { provider: 'beta', unknown: 'x' })
    expect(v).toEqual({ provider: 'beta', count: '2' })
  })
  it('keeps the defaults without initial', () => {
    expect(initialValues(fieldsOf(add))).toEqual({ provider: '', count: '2' })
  })
})

describe('aborted', () => {
  const plannedAction: RunAction = {
    type: 'planned',
    id: 'P9',
    plan: { operation: 'login', steps: [{ id: 'login:alpha', action: 'log in', target: 'alpha', noChange: false, afterStart: false }], confirm: false, readOnly: false },
  }
  it('ends an applying dialog and is never confirmed', () => {
    const started: RunAction = { type: 'progress', event: { id: 'login:alpha', action: 'log in', target: 'alpha', phase: 'started' } }
    const s = [plannedAction, { type: 'apply' } as RunAction, started, { type: 'aborted', reason: 'timed out after 15 minutes' } as RunAction].reduce(
      runReducer,
      initialRun,
    )
    // The item that was in progress is no longer shown running.
    expect(s.rows.map((r) => r.progress)).toEqual(['waiting'])
    expect(s).toMatchObject({ phase: 'finished', ending: 'ended', endedReason: 'timed out after 15 minutes' })
    expect(s.rows.every((r) => r.check === undefined)).toBe(true)
    expect(summary(s)).toEqual({ kind: 'ended', text: endedSummary('timed out after 15 minutes') })
    expect(summary(s)?.text).toContain('The login was ended (timed out after 15 minutes).')
    // The stream's later unknown outcome does not replace it.
    const after = runReducer(s, { type: 'applied', outcome: { ok: true, value: { items: [{ id: 'login:alpha', action: 'log in', target: 'alpha', confirmed: true }] } } })
    expect(after).toBe(s)
  })
  it('is ignored outside an apply', () => {
    const s = runReducer(initialRun, { type: 'aborted', reason: 'x' })
    expect(s).toBe(initialRun)
  })
})

describe('afterPlan', () => {
  const body = { id: 'P1', plan }

  it('stores a planned plan and shows its preview', () => {
    expect(afterPlan({ ok: true, value: body }, false)).toEqual({ store: 'P1', action: { type: 'planned', id: 'P1', plan } })
  })

  it('reads back a read-only plan and an error that carries the plan', () => {
    expect(afterPlan({ ok: true, value: { plan, result: { items: [] } } }, false)).toEqual({
      action: { type: 'readBack', plan, result: { items: [] } },
    })
    const error = { code: 'repo_unreadable', message: 'offline' }
    expect(afterPlan({ ok: false, error, plan, result: { items: [] } }, false)).toEqual({
      action: { type: 'readBack', plan, result: { items: [] }, error },
    })
    expect(afterPlan({ ok: false, error, plan }, false)).toEqual({ action: { type: 'planFailed', error } })
  })

  it('stores nothing for an aborted plan and drops one stored before the abort arrived', () => {
    const error = { code: 'network_error', message: 'aborted' }
    expect(afterPlan({ ok: false, error }, true)).toEqual({})
    expect(afterPlan({ ok: true, value: body }, true)).toEqual({ drop: 'P1' })
    expect(afterPlan({ ok: true, value: { plan, result: {} } }, true)).toEqual({})
  })
})

describe('applyAtOnce', () => {
  const plan = (confirm: boolean): Plan => ({ operation: 'resume', steps: [], confirm, readOnly: false })
  const planned = (confirm: boolean) => afterPlan({ ok: true, value: { id: 'P1', plan: plan(confirm) } }, false)
  it('applies a stored plan that asks no confirmation only without the preview stop', () => {
    expect(applyAtOnce(false, planned(false))).toBe('P1')
    // The default stops at the preview.
    expect(applyAtOnce(true, planned(false))).toBeUndefined()
    // A plan that asks for confirmation always stops.
    expect(applyAtOnce(false, planned(true))).toBeUndefined()
  })
  it('never applies a read-only, failed or aborted plan', () => {
    expect(applyAtOnce(false, afterPlan({ ok: true, value: { plan: { ...plan(false), readOnly: true } } }, false))).toBeUndefined()
    expect(applyAtOnce(false, afterPlan({ ok: false, error: { code: 'invalid_argument', message: 'x' } }, false))).toBeUndefined()
    expect(applyAtOnce(false, afterPlan({ ok: true, value: { id: 'P1', plan: plan(false) } }, true))).toBeUndefined()
  })
})

describe('resume --option and --reply body', () => {
  it('sends the Issue as a list and the option as an integer, without the repository', () => {
    const resume = (commands as Command[]).find((c) => c.api === 'resume')!
    const f = fieldsOf(resume)
    expect(f.map((x) => x.key).sort()).toEqual(['issues', 'option', 'reply', 'repo'])
    const values = initialValues(f, { issues: 'https://github.com/o/r/issues/7', option: '2', reply: '' })
    expect(requestBody(f, values)).toEqual({ ok: true, body: { issues: ['https://github.com/o/r/issues/7'], option: 2 } })
  })

  it('sends a reply trimmed, without the option', () => {
    const resume = (commands as Command[]).find((c) => c.api === 'resume')!
    const f = fieldsOf(resume)
    expect(f.find((x) => x.key === 'reply')?.kind).toBe('text')
    const values = initialValues(f, { issues: 'https://github.com/o/r/issues/7', option: '', reply: '\n Keep it,\nthen ship.  ' })
    expect(requestBody(f, values)).toEqual({ ok: true, body: { issues: ['https://github.com/o/r/issues/7'], reply: 'Keep it,\nthen ship.' } })
  })
})

describe('elapsedSeconds', () => {
  it('counts whole seconds and never goes below zero', () => {
    expect(elapsedSeconds(1000, 1000)).toBe(0)
    expect(elapsedSeconds(1000, 3999)).toBe(2)
    expect(elapsedSeconds(5000, 1000)).toBe(0)
    expect(elapsedSeconds(undefined, 9000)).toBe(0)
  })
})
