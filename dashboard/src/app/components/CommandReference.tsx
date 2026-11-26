import type { ReactNode } from 'react'

import { Badge } from '@/components/ui/badge'
import type { Command } from '@/lib/operation'

import { referenceOf, rootCommand, type ArgRow, type CommandEntry, type FlagRow } from '../lib/cli-reference'

const cell = 'border-b border-border-subtle px-3 py-2 text-left align-top'
const head = `${cell} font-heading text-xs font-semibold uppercase tracking-wide text-muted-foreground`

// Table keeps a wide table scrolling inside its own box, so the page does not.
function Table({ label, columns, children }: { label: string; columns: string[]; children: ReactNode }) {
  return (
    <div className="not-content my-3 overflow-x-auto rounded-lg border border-border-subtle" role="region" aria-label={label} tabIndex={0}>
      <table className="m-0 w-full min-w-[32rem] border-collapse text-sm">
        <thead>
          <tr>
            {columns.map((c) => (
              <th key={c} scope="col" className={head}>
                {c}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>{children}</tbody>
      </table>
    </div>
  )
}

function Allowed({ values }: { values: string[] }) {
  if (values.length === 0) return null
  return (
    <span className="mt-1 flex flex-wrap gap-1">
      {values.map((v) => (
        <Badge key={v} variant="outline" className="font-mono">
          {v}
        </Badge>
      ))}
    </span>
  )
}

function FlagTable({ path, flags }: { path: string; flags: FlagRow[] }) {
  return (
    <Table label={`Flags of ${rootCommand} ${path}`} columns={['Flag', 'Type', 'Default', 'Description']}>
      {flags.map((f) => (
        <tr key={f.name}>
          <td className={cell}>
            <code className="whitespace-nowrap">
              {f.name}
              {f.value ? ` ${f.value}` : ''}
            </code>
            {f.required ? (
              <Badge variant="outline" className="ml-2">
                required
              </Badge>
            ) : null}
          </td>
          <td className={cell}>{f.type}</td>
          <td className={cell}>{f.defaultValue ? <code>{f.defaultValue}</code> : <span className="text-muted-foreground">none</span>}</td>
          <td className={cell}>
            {f.usage}
            <Allowed values={f.allowed} />
          </td>
        </tr>
      ))}
    </Table>
  )
}

function ArgTable({ path, args }: { path: string; args: ArgRow[] }) {
  return (
    <Table label={`Arguments of ${rootCommand} ${path}`} columns={['Argument', 'Required', 'Repeatable', 'Allowed values']}>
      {args.map((a) => (
        <tr key={a.name}>
          <td className={cell}>
            <code>{a.name}</code>
          </td>
          <td className={cell}>{a.required ? 'Yes' : 'No'}</td>
          <td className={cell}>{a.repeatable ? 'Yes, one or more' : 'No'}</td>
          <td className={cell}>{a.allowed.length ? <Allowed values={a.allowed} /> : <span className="text-muted-foreground">any</span>}</td>
        </tr>
      ))}
    </Table>
  )
}

function Entry({ entry }: { entry: CommandEntry }) {
  return (
    <section aria-labelledby={entry.slug} data-command={entry.path}>
      <h2 id={entry.slug}>
        {rootCommand} {entry.path}
      </h2>
      <p>{entry.short}.</p>
      {entry.subcommands.length > 0 ? (
        <ul>
          {entry.subcommands.map((s) => (
            <li key={s.slug}>
              <a href={`#${s.slug}`}>
                <code>
                  {rootCommand} {s.path}
                </code>
              </a>
              : {s.short}.
            </li>
          ))}
        </ul>
      ) : (
        <>
          <pre className="overflow-x-auto" data-slot="usage">
            <code>{entry.usage}</code>
          </pre>
          {entry.paragraphs.map((p, i) => (
            <p key={i}>{p}</p>
          ))}
          <h3 id={`${entry.slug}-arguments`}>Arguments</h3>
          {entry.args.length ? <ArgTable path={entry.path} args={entry.args} /> : <p>No arguments.</p>}
          <h3 id={`${entry.slug}-flags`}>Flags</h3>
          {entry.flags.length ? <FlagTable path={entry.path} flags={entry.flags} /> : <p>No flags.</p>}
        </>
      )}
    </section>
  )
}

/** The generated command index: one link per command, in the CLI's order. */
export function CommandIndex({ commands }: { commands: readonly Command[] }) {
  return (
    <ul>
      {referenceOf(commands).map((e) => (
        <li key={e.slug}>
          <a href={`#${e.slug}`}>
            <code>
              {rootCommand} {e.path}
            </code>
          </a>
          : {e.short}.
        </li>
      ))}
    </ul>
  )
}

/** The generated command reference, rendered on the server from the CLI definition. */
export function CommandReference({ commands }: { commands: readonly Command[] }) {
  return (
    <>
      {referenceOf(commands).map((e) => (
        <Entry key={e.slug} entry={e} />
      ))}
    </>
  )
}
