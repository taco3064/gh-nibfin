import { describe, expect, it } from 'vitest'

import type { Command } from '@/lib/operation'

import commands from '../data/commands.json'
import { flagRow, referenceOf, slugOf, usageOf } from './cli-reference'

const command = (c: Partial<Command>): Command => ({ path: 'x', short: '', long: '', flags: [], args: [], ...c })
const flag = { shorthand: '', default: '', usage: '', required: false }

describe('usageOf', () => {
  it('writes required, optional, repeatable and allowed-value arguments', () => {
    expect(usageOf(command({ path: 'a', args: [{ name: 'provider', required: true, repeatable: false, allowed: ['claude', 'codex'] }] }))).toBe(
      'gh nibfin a <claude|codex>',
    )
    expect(usageOf(command({ path: 'b', args: [{ name: 'provider', required: false, repeatable: false, allowed: ['claude', 'codex'] }] }))).toBe(
      'gh nibfin b [claude|codex]',
    )
    expect(usageOf(command({ path: 'c', args: [{ name: 'issue', required: true, repeatable: true }] }))).toBe('gh nibfin c <issue>...')
    expect(usageOf(command({ path: 'd', args: [{ name: 'issue', required: false, repeatable: true, allowed: null }] }))).toBe('gh nibfin d [issue]...')
  })

  it('writes int, bool, string and required flags after the arguments', () => {
    const c = command({
      path: 'e',
      args: [{ name: 'issue', required: true, repeatable: false }],
      flags: [
        { ...flag, name: 'count', type: 'int', default: '2' },
        { ...flag, name: 'yes', type: 'bool', default: 'false' },
        { ...flag, name: 'repo', shorthand: 'R', type: 'string', usage: 'target repository as [HOST/]OWNER/REPO (default: here)' },
        { ...flag, name: 'mode', type: 'string', required: true, allowed: ['a', 'b'] },
      ],
    })
    expect(usageOf(c)).toBe('gh nibfin e <issue> [--count N] [--yes] [--repo [HOST/]OWNER/REPO] --mode a|b')
  })
})

describe('flagRow', () => {
  it('names the shorthand and omits zero defaults', () => {
    expect(flagRow({ ...flag, name: 'repo', shorthand: 'R', type: 'string', usage: 'as OWNER/REPO' })).toMatchObject({
      name: '-R, --repo',
      value: 'OWNER/REPO',
      defaultValue: '',
    })
    expect(flagRow({ ...flag, name: 'yes', type: 'bool', default: 'false' })).toMatchObject({ value: '', defaultValue: '' })
    expect(flagRow({ ...flag, name: 'port', type: 'int', default: '0' }).defaultValue).toBe('')
    expect(flagRow({ ...flag, name: 'count', type: 'int', default: '2' }).defaultValue).toBe('2')
  })
})

describe('referenceOf', () => {
  it('gives nested paths unique slugs and lists a group command’s subcommands', () => {
    const entries = referenceOf([command({ path: 'runner' }), command({ path: 'runner add' }), command({ path: 'runner remove' }), command({ path: 'run' })])
    expect(entries.map((e) => e.slug)).toEqual(['runner', 'runner-add', 'runner-remove', 'run'])
    expect(entries[0].subcommands.map((s) => s.path)).toEqual(['runner add', 'runner remove'])
    expect(entries[3].subcommands).toEqual([])
    expect(slugOf(' Runner  Add ')).toBe('runner-add')
  })

  it('joins wrapped lines into paragraphs split on blank lines', () => {
    expect(referenceOf([command({ long: 'One\nline.\n\nTwo.\n' })])[0].paragraphs).toEqual(['One line.', 'Two.'])
  })

  it('covers every command of the committed reference', () => {
    const entries = referenceOf(commands)
    expect(entries.length).toBeGreaterThan(0)
    expect(new Set(entries.map((e) => e.slug)).size).toBe(entries.length)
    for (const e of entries) expect(e.usage).toMatch(new RegExp(`^gh nibfin ${e.path}\\b`))
    const add = entries.find((e) => e.path === 'runner add')
    expect(add?.usage).toContain('<claude|codex>')
    expect(add?.flags.map((f) => f.name)).toContain('--count')
  })
})
