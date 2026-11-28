import { describe, expect, it } from 'vitest'

import type { Arg, Command, Flag } from '@/lib/operation'
import type { Repair, RepairOption } from '@/lib/status'

import { resolveRepairTarget, type Load } from './use-repair-target'

function option(number: number, recommended = false): RepairOption {
  return { number, title: `Option ${number}`, consequence: 'c', issueChange: '', recommended }
}

function repair(over: Partial<Repair> = {}): Repair {
  return { state: 'choose', reason: 'r', options: [option(1), option(2, true)], ...over }
}

function arg(param: string): Arg {
  return { name: param, required: true, repeatable: true, param }
}

function flag(param: string): Flag {
  return { name: param, shorthand: '', type: 'int', default: '0', usage: '', required: false, param }
}

function command(over: Partial<Command> = {}): Command {
  return { path: 'nibfin resume', api: 'resume', short: '', long: '', args: [arg('issues')], flags: [flag('option')], ...over }
}

const loaded = (...commands: Command[]): Load => ({ state: 'loaded', commands })

describe('resolveRepairTarget', () => {
  it.each([
    ['disabled', repair(), false],
    ['no repair', undefined, true],
    ['repairing', repair({ state: 'repairing' }), true],
    ['act', repair({ state: 'act' }), true],
  ] as const)('is ready with no command when %s', (_, r, enabled) => {
    expect(resolveRepairTarget(r, enabled, loaded(command()))).toEqual({ ready: true })
  })

  it('is not ready when the choose record lists no options', () => {
    for (const options of [undefined, []]) {
      expect(resolveRepairTarget(repair({ options }), true, loaded(command()))).toEqual({
        ready: false,
        reason: 'The repair record lists no options.',
      })
    }
  })

  it('waits while the commands load', () => {
    expect(resolveRepairTarget(repair(), true, { state: 'loading' })).toEqual({ ready: false, reason: 'Reading the commands…' })
  })

  it('names the error when the commands failed to load', () => {
    expect(resolveRepairTarget(repair(), true, { state: 'failed', error: { code: 'http_500', message: 'boom' } })).toEqual({
      ready: false,
      reason: 'Commands could not be loaded: http_500: boom',
    })
  })

  it('is not ready without a resume command', () => {
    expect(resolveRepairTarget(repair(), true, loaded(command({ api: 'pause' }), command({ api: undefined })))).toEqual({
      ready: false,
      reason: 'The server offers no resume command.',
    })
  })

  it.each([
    ['no option', command({ flags: [] })],
    ['no issues', command({ args: null })],
    ['neither', command({ args: [], flags: null })],
  ])('is not ready when the resume command takes %s', (_, c) => {
    expect(resolveRepairTarget(repair(), true, loaded(c))).toEqual({ ready: false, reason: 'The server’s resume command takes no option.' })
  })

  it('opens the resume command with the recommended option', () => {
    const pause = command({ api: 'pause', path: 'nibfin pause' })
    const resume = command()
    expect(resolveRepairTarget(repair(), true, loaded(pause, resume))).toEqual({ ready: true, command: resume, recommended: 2 })
  })

  it('preselects the first option when none is recommended', () => {
    const resume = command()
    expect(resolveRepairTarget(repair({ options: [option(3), option(4)] }), true, loaded(resume))).toEqual({
      ready: true,
      command: resume,
      recommended: 3,
    })
  })
})
