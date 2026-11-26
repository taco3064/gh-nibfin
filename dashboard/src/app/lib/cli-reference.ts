// The CLI commands page's data: every command of the committed reference
// (src/app/data/commands.json, the cobra tree's schema that
// internal/cmd's TestCommandReferenceFile writes and keeps current), turned
// into usage lines, anchors and table rows. It names no command: the CLI
// definition is the only source.

import type { Arg, Command, Flag } from '@/lib/operation'

export interface FlagRow {
  /** The flag as typed, such as "-R, --repo". */
  name: string
  /** The value placeholder, such as "N" or "OWNER/REPO"; empty for a bool. */
  value: string
  type: string
  /** The default, or empty when it is the type's zero value. */
  defaultValue: string
  usage: string
  required: boolean
  allowed: string[]
}

export interface ArgRow {
  name: string
  required: boolean
  repeatable: boolean
  allowed: string[]
}

export interface CommandEntry {
  path: string
  slug: string
  short: string
  /** The long description split into paragraphs on blank lines. */
  paragraphs: string[]
  usage: string
  flags: FlagRow[]
  args: ArgRow[]
  /** The commands below this one, for a command that groups others. */
  subcommands: { path: string; slug: string; short: string }[]
}

export const rootCommand = 'gh nibfin'

// The zero values cobra prints for an unset default.
const zeroDefault: Record<string, string> = { bool: 'false', int: '0', string: '' }

export function slugOf(path: string): string {
  return path.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '')
}

// placeholder is the value a flag takes in a usage line: its allowed values,
// N for a number, or the upper-case word its usage names, such as
// "[HOST/]OWNER/REPO".
function placeholder(f: Flag): string {
  if (f.type === 'bool') return ''
  if (f.allowed?.length) return f.allowed.join('|')
  if (f.type === 'int' || f.type === 'uint' || f.type === 'float64') return 'N'
  const shown = /(?:\[[A-Z]+\/\])?[A-Z][A-Z/]*[A-Z]/.exec(f.usage)
  return shown ? shown[0] : f.name.toUpperCase()
}

function argText(a: Arg): string {
  const inner = a.allowed?.length ? a.allowed.join('|') : a.name
  const repeat = a.repeatable ? '...' : ''
  return a.required ? `<${inner}>${repeat}` : `[${inner}]${repeat}`
}

function flagText(f: Flag): string {
  const value = placeholder(f)
  const text = value ? `--${f.name} ${value}` : `--${f.name}`
  return f.required ? text : `[${text}]`
}

/** usageOf is a command's usage line: positionals first, then its flags. */
export function usageOf(c: Command): string {
  const parts = [rootCommand, c.path]
  for (const a of c.args ?? []) parts.push(argText(a))
  for (const f of c.flags ?? []) parts.push(flagText(f))
  return parts.join(' ')
}

export function flagRow(f: Flag): FlagRow {
  const value = placeholder(f)
  return {
    name: f.shorthand ? `-${f.shorthand}, --${f.name}` : `--${f.name}`,
    value,
    type: f.type,
    defaultValue: f.default === (zeroDefault[f.type] ?? '') ? '' : f.default,
    usage: f.usage,
    required: f.required,
    allowed: f.allowed ?? [],
  }
}

function paragraphsOf(text: string): string[] {
  return text
    .split(/\n\s*\n/)
    .map((p) => p.replace(/\s*\n\s*/g, ' ').trim())
    .filter((p) => p !== '')
}

/** referenceOf turns the committed reference into the page's entries, in the CLI's order. */
export function referenceOf(commands: readonly Command[]): CommandEntry[] {
  return commands.map((c) => ({
    path: c.path,
    slug: slugOf(c.path),
    short: c.short,
    paragraphs: paragraphsOf(c.long),
    usage: usageOf(c),
    flags: (c.flags ?? []).map(flagRow),
    args: (c.args ?? []).map((a) => ({ name: a.name, required: a.required, repeatable: a.repeatable, allowed: a.allowed ?? [] })),
    subcommands: commands
      .filter((s) => s.path.startsWith(`${c.path} `))
      .map((s) => ({ path: s.path, slug: slugOf(s.path), short: s.short })),
  }))
}
