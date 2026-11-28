import { isValidElement, type ReactNode } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { OperationDialogProps } from '@/components/operation'
import type { FieldInputs, FieldProps } from '@/components/operation/operation-fields'
import type { Command, Field } from '@/lib/operation'
import type { Repair, RepairOption, StatusIssue } from '@/lib/status'

// OperationDialog is outside this test: the stand-in records what the
// Repair button opens it with and renders nothing.
const dialogs = vi.hoisted(() => [] as unknown[])
vi.mock('@/components/operation', () => ({
  OperationDialog: (props: unknown) => {
    dialogs.push(props)
    return null
  },
}))

// The dialog parts render their children inline, in place of the Portal that
// renders nothing on the server, so the advice body shows in the markup.
vi.mock('@/components/ui/dialog', () => {
  const part =
    (slot: string) =>
    ({ children }: { children?: ReactNode }) => <div data-part={slot}>{children}</div>
  return {
    Dialog: part('dialog'),
    DialogContent: part('content'),
    DialogDescription: part('description'),
    DialogFooter: part('footer'),
    DialogHeader: part('header'),
    DialogTitle: part('title'),
  }
})

import { RepairAction, RepairBadge, RepairSummary } from './repair-dialog'
import type { RepairTarget } from './use-repair-target'

const url = 'https://github.com/o/r/issues/7'

function issue(over: Partial<StatusIssue> = {}): StatusIssue {
  return {
    number: 7,
    title: 'Fix the thing',
    url,
    labels: ['pending'],
    pause: false,
    pending: true,
    createdAt: '2026-11-01T00:00:00Z',
    runs: [],
    ...over,
  }
}

function option(number: number, recommended = false): RepairOption {
  return { number, title: `Title ${number}`, consequence: `Consequence ${number}`, issueChange: number === 1 ? 'Adds a label' : '', recommended }
}

function repair(over: Partial<Repair> = {}): Repair {
  return { state: 'choose', reason: 'Two Shapes conflict', options: [option(1), option(2, true)], ...over }
}

const command: Command = { path: 'nibfin resume', api: 'resume', short: '', long: '', flags: null, args: null }
const ready: RepairTarget = { ready: true, command, recommended: 2 }
const enabled = { enabled: true } as const
const text = (html: string) => html.replaceAll('<!-- -->', '').replace(/<[^>]+>/g, '')

function field(key: string): Field {
  return { key, name: key, cli: `--${key}`, kind: 'text', help: '', required: true, allowed: [], multiple: false, initial: '' }
}

function props(key: string, value = ''): FieldProps {
  return { field: field(key), value, disabled: false, onChange: () => {} }
}

function lastDialog(): OperationDialogProps {
  expect(dialogs).toHaveLength(1)
  return dialogs[0] as OperationDialogProps
}

function render(node: ReactNode): string {
  return renderToStaticMarkup(<>{node}</>)
}

beforeEach(() => {
  dialogs.length = 0
})

describe('RepairBadge', () => {
  it.each([
    ['repairing', 'Repairing'],
    ['choose', 'Waiting for your choice'],
    ['act', 'Needs your action'],
    ['other', 'Unknown repair state'],
  ])('shows %s with its label', (state, label) => {
    const html = render(<RepairBadge repair={repair({ state: state as Repair['state'] })} />)
    expect(html).toContain(`data-repair="${state === 'other' ? 'unknown' : state}"`)
    expect(text(html)).toContain(label)
  })
})

describe('RepairSummary', () => {
  it('shows the category, reason, what Nibfin did and the links', () => {
    const html = render(
      <RepairSummary
        issue={issue({ pendingComment: 'https://github.com/o/r/issues/7#c1' })}
        repair={repair({ category: 'Shape conflict', done: 'Retried once', record: 'https://github.com/o/r/issues/7#c2' })}
      />,
    )
    expect(html).toContain('data-slot="repair-category"')
    expect(text(html)).toContain('Shape conflict')
    expect(text(html)).toContain('Two Shapes conflict')
    expect(html).toContain('data-slot="repair-done"')
    expect(text(html)).toContain('Retried once')
    expect(html).toContain('href="https://github.com/o/r/issues/7#c2"')
    expect(text(html)).toContain('Repair record')
    expect(html).toContain('href="https://github.com/o/r/issues/7#c1"')
    expect(text(html)).toContain('Pending comment')
  })

  it('leaves out what the record lacks', () => {
    const html = render(<RepairSummary issue={issue()} repair={repair()} />)
    expect(html).not.toContain('data-slot="repair-category"')
    expect(html).not.toContain('data-slot="repair-done"')
    expect(text(html)).toContain('No repair record')
    expect(text(html)).not.toContain('Pending comment')
  })

  it('shows an unsafe record link as text only', () => {
    const html = render(<RepairSummary issue={issue()} repair={repair({ record: 'javascript:alert(1)' })} />)
    expect(html).not.toContain('href="javascript:')
    expect(html).not.toContain('<a ')
    expect(text(html)).toContain('javascript:alert(1)')
  })

  it.each([
    [
      'a run',
      issue({
        runs: [
          {
            repo: 'o/r',
            name: 'n',
            workflow: 'w',
            status: 'completed',
            conclusion: 'success',
            createdAt: '2026-11-02T00:00:00Z',
            url: 'https://github.com/o/r/actions/runs/9',
          },
        ],
      }),
      'Latest run',
    ],
    ['a read error', issue({ runs: undefined, runsError: { code: 'http_502', message: 'bad gateway' } }), 'Runs could not be read: http_502: bad gateway'],
    ['runs not read', issue({ runs: undefined }), 'Runs not read'],
    ['no runs', issue({ runs: [] }), 'No runs'],
  ])('shows the latest run area for %s', (_, i, label) => {
    const html = render(<RepairSummary issue={i} repair={repair()} />)
    expect(text(html)).toContain(label)
    if (label === 'Latest run') expect(html).toContain('href="https://github.com/o/r/actions/runs/9"')
  })
})

describe('RepairAction for a choose record', () => {
  it('enables Repair and opens the resume command with the recommended option', () => {
    const i = issue()
    const r = repair()
    const html = render(<RepairAction issue={i} repair={r} gate={enabled} target={ready} describedBy="why" onChanged={() => {}} />)
    const button = html.match(/<button[^>]*data-action="repair"[^>]*>/)![0]
    expect(button).not.toMatch(/ disabled=""/)
    expect(button).not.toContain('aria-describedby')

    const d = lastDialog()
    expect(d.command).toBe(command)
    expect(d.title).toBe('Repair #7')
    expect(d.initial).toEqual({ issues: url, option: '2' })
    expect(d.confirm).toBe(false)
    expect(isValidElement(d.intro)).toBe(true)
    expect(render(d.intro)).toContain('data-slot="repair-summary"')
  })

  it('shows the Issue as fixed text, the options as radios, and no repository field', () => {
    render(<RepairAction issue={issue()} repair={repair()} gate={enabled} target={ready} onChanged={() => {}} />)
    const inputs = lastDialog().inputs as FieldInputs

    const issues = render(inputs(props('issues', url)))
    expect(text(issues)).toContain('#7 Fix the thing')
    expect(issues).not.toContain('<input')

    const options = render(inputs(props('option', '2')))
    expect(options).toContain('role="radiogroup"')
    const radios = [...options.matchAll(/<button[^>]*role="radio"[^>]*>/g)].map((m) => m[0])
    expect(radios).toHaveLength(2)
    expect(radios[0]).toContain('value="1"')
    expect(radios[0]).toContain('aria-checked="false"')
    expect(radios[1]).toContain('value="2"')
    expect(radios[1]).toContain('aria-checked="true"')
    expect(text(options)).toContain('1.Title 1')
    expect(text(options)).toContain('Consequence 1')
    expect(text(options)).toContain('Issue change: Adds a label')
    expect(text(options)).toContain('Consequence 2')
    expect(text(options).match(/Issue change/g)).toHaveLength(1)
    expect(options.match(/data-slot="recommended"/g)).toHaveLength(1)
    expect(options).toMatch(/data-option="2".*data-slot="recommended"/)
    expect(options).not.toMatch(/data-option="1"((?!data-option).)*data-slot="recommended"/)
    expect(options).not.toContain('<textarea')

    expect(inputs(props('repo'))).toBeNull()
    expect(inputs(props('other'))).toBeUndefined()
  })

  it.each([
    ['the gate is disabled', { enabled: false, reason: 'Needs triage' } as const, ready],
    ['the target is not ready', enabled, { ready: false, reason: 'Reading the commands…' } as RepairTarget],
  ])('disables Repair when %s', (_, gate, target) => {
    const html = render(<RepairAction issue={issue()} repair={repair()} gate={gate} target={target} describedBy="why" onChanged={() => {}} />)
    const button = html.match(/<button[^>]*data-action="repair"[^>]*>/)![0]
    expect(button).toMatch(/ disabled=""/)
    expect(button).toContain('aria-describedby="why"')
    expect(dialogs).toHaveLength(target.ready ? 1 : 0)
  })
})

describe('RepairAction for an act record', () => {
  const act = repair({
    state: 'act',
    options: undefined,
    advice: 'Rotate the token',
    steps: [
      { text: 'Open the settings', link: 'https://github.com/o/r/settings' },
      { text: 'Check the bad link', link: 'javascript:alert(1)' },
      { text: 'Run the command', command: 'gh nibfin resume 7' },
    ],
    bugReport: { url: 'https://github.com/taco3064/nibfin-core/issues/new', title: 'Bug title', body: 'Bug body' },
  })

  it('shows the advice, steps and bug report and runs nothing', () => {
    const html = render(<RepairAction issue={issue()} repair={act} gate={enabled} target={{ ready: true }} onChanged={() => {}} />)
    expect(dialogs).toHaveLength(0)
    const t = text(html)
    expect(t).toContain('Repair #7')
    expect(html).toContain('data-slot="repair-summary"')
    expect(html).toContain('data-slot="repair-advice"')
    expect(t).toContain('Rotate the token')
    expect(t).toContain('Open the settings')
    expect(t).toContain('Check the bad link')
    expect(t).toContain('Run the command')
    expect(html).toMatch(/<a href="https:\/\/github.com\/o\/r\/settings" target="_blank"/)
    expect(html).not.toContain('href="javascript:')
    expect(t).toContain('javascript:alert(1)')
    expect(html).toContain('<code>gh nibfin resume 7</code>')
    expect(html).toContain('aria-label="Copy the command of step 3"')
    expect(html).toContain('data-slot="repair-bug"')
    expect(html).toContain('href="https://github.com/taco3064/nibfin-core/issues/new"')
    expect(t).toContain('Bug title')
    expect(html).toContain('<code>Bug body</code>')
    expect(html).toContain('aria-label="Copy the bug report"')
    expect(html).toContain('data-slot="repair-resume-note"')
    expect(t).toContain('use Resume on the Issue to remove pending')
    expect(html).not.toContain('<form')
    expect(html).not.toContain('type="submit"')
    expect(html).not.toContain('<textarea')
  })

  it('leaves out the sections the record lacks', () => {
    const html = render(
      <RepairAction issue={issue()} repair={repair({ state: 'act', options: undefined })} gate={enabled} target={{ ready: true }} onChanged={() => {}} />,
    )
    expect(dialogs).toHaveLength(0)
    expect(html).not.toContain('data-slot="repair-advice"')
    expect(html).not.toContain('data-slot="repair-steps"')
    expect(html).not.toContain('data-slot="repair-bug"')
    expect(html).not.toContain('data-action="copy"')
    expect(html).toContain('data-slot="repair-resume-note"')
  })
})
