import { Fragment, type ReactNode } from 'react'

import { Badge } from '@/components/ui/badge'
import type { Command } from '@/lib/operation'

import { referenceOf, rootCommand, type ArgRow, type CommandEntry, type FlagRow } from '@/app/lib/cli-reference'

const cell = 'border-b border-border-subtle px-3 py-2 text-left align-top'
const head = `${cell} font-heading text-xs font-semibold uppercase tracking-wide text-muted-foreground`

// Table keeps a wide table scrolling inside its own box, so the page does not.
function Table({ label, columns, children }: { label: string; columns: string[]; children: ReactNode }) {
  return (
    <div className="not-content my-3 overflow-x-auto rounded-lg border border-border-subtle" role="region" aria-label={label} tabIndex={0}>
      <table className="m-0 w-full border-collapse text-sm">
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

// Facts are a name's type, default and whether it is required, in text.
function Facts({ items }: { items: string[] }) {
  return <span className="mt-1 block text-xs text-muted-foreground">{items.join(' · ')}</span>
}

function FlagTable({ path, flags }: { path: string; flags: FlagRow[] }) {
  return (
    <Table label={`Flags of ${rootCommand} ${path}`} columns={['Flag', 'Description']}>
      {flags.map((f) => (
        <tr key={f.name}>
          <td className={`${cell} w-2/5`}>
            <code className="whitespace-nowrap">
              {f.name}
              {f.value ? ` ${f.value}` : ''}
            </code>
            <Facts items={[f.type, ...(f.defaultValue ? [`default ${f.defaultValue}`] : []), ...(f.required ? ['required'] : [])]} />
          </td>
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
    <Table label={`Arguments of ${rootCommand} ${path}`} columns={['Argument', 'Values']}>
      {args.map((a) => (
        <tr key={a.name}>
          <td className={`${cell} w-2/5`}>
            <code>{a.name}</code>
            <Facts items={[a.required ? 'required' : 'optional', ...(a.repeatable ? ['one or more'] : [])]} />
          </td>
          <td className={cell}>{a.allowed.length ? <Allowed values={a.allowed} /> : 'Any value'}</td>
        </tr>
      ))}
    </Table>
  )
}

// Summary is a command section's summary card: the command's lead text from
// the CLI definition, in the same frame as the Starlight Card summaries the
// hand-written sections open with.
function Summary({ lead }: { lead: string }) {
  return (
    <article className="card" data-slot="summary">
      <p className="title">In this section</p>
      <div className="body">
        <p>{lead}</p>
      </div>
    </article>
  )
}

function Entry({ entry }: { entry: CommandEntry }) {
  return (
    <section aria-labelledby={entry.slug} data-command={entry.path} className="pt-6">
      <h2 id={entry.slug}>
        {rootCommand} {entry.path}
      </h2>
      {entry.lead ? <Summary lead={entry.lead} /> : null}
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
          <pre className="whitespace-normal" data-slot="usage">
            <code>
              {entry.usage.map((part, i) => (
                <Fragment key={i}>
                  {i > 0 ? ' ' : null}
                  <span className="whitespace-pre">{part}</span>
                </Fragment>
              ))}
            </code>
          </pre>
          {entry.more.length > 0 ? (
            <details data-slot="details">
              <summary>
                More about{' '}
                <code>
                  {rootCommand} {entry.path}
                </code>
              </summary>
              {entry.more.map((p, i) => (
                <p key={i}>{p}</p>
              ))}
            </details>
          ) : null}
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
