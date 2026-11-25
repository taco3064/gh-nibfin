import { describe, expect, it } from 'vitest'

import {
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

describe('runReducer', () => {
  it('goes from planning to preview with one row per step', () => {
    const s = run({ type: 'plan' }, planned)
    expect(s.phase).toBe('preview')
    expect(s.planId).toBe('P1')
    expect(s.rows.map((r) => [r.id, r.progress])).toEqual([
      ['login', 'waiting'],
      ['image', 'waiting'],
      ['slots', 'waiting'],
    ])
  })

  it('keeps a plan error in planFailed', () => {
    expect(run({ type: 'plan' }, { type: 'planFailed', error: { code: 'bad', message: 'm' } })).toMatchObject({
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
    const s = run({ type: 'plan' }, {
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
