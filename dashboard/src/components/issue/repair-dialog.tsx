import { useRef, useState, type ReactNode } from 'react'
import { Check, CircleHelp, Copy, Hand, ListChecks, Wrench } from 'lucide-react'

import { OperationDialog } from '@/components/operation'
import { ErrorText, type FieldInputs } from '@/components/operation/operation-fields'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { latestRun, repairLook, safeLink, type Access, type RepairLook } from '@/lib/issue'
import { fieldIds } from '@/lib/operation'
import type { Repair, StatusIssue } from '@/lib/status'
import { cn } from '@/lib/utils'

import { ExternalMark } from './external-mark'
import { tones } from './tones'
import { issuesKey, optionKey, type RepairTarget } from './use-repair-target'

const linkClass = 'rounded-sm text-link underline-offset-4 hover:underline'

// SafeLink is a link that opens in a new tab, only for an http(s) URL;
// any other value is shown as text.
function SafeLink({ href, children, label }: { href: string | undefined; children: ReactNode; label: string }) {
  const url = safeLink(href)
  if (!url) return <span className="break-all text-muted-foreground">{href}</span>
  return (
    <a href={url} target="_blank" rel="noreferrer noopener" className={cn(linkClass, 'break-words')}>
      {children} <ExternalMark label={label} />
    </a>
  )
}

const repairIcons: Record<RepairLook['state'], typeof Wrench> = {
  repairing: Wrench,
  choose: ListChecks,
  act: Hand,
  unknown: CircleHelp,
}

// RepairBadge is the repair state of a pending Issue (Proposal R9.52), an
// icon with its label.
export function RepairBadge({ repair }: { repair: Repair }) {
  const look = repairLook(repair)
  const Icon = repairIcons[look.state]
  return (
    <Badge variant="outline" data-repair={look.state} className={cn(tones[look.tone], 'whitespace-normal')}>
      <Icon aria-hidden="true" />
      {look.label}
    </Badge>
  )
}

function Fact({ term, children, slot }: { term: string; children: ReactNode; slot: string }) {
  return (
    <div data-slot={slot} className="flex flex-col gap-0.5 sm:grid sm:grid-cols-[8rem_1fr] sm:gap-3">
      <dt className="text-muted-foreground">{term}</dt>
      <dd className="min-w-0 break-words whitespace-pre-line">{children}</dd>
    </div>
  )
}

// RepairSummary is the head of the Repair dialog (Proposal R9.53): the
// state, the category, the reason, what Nibfin did, and links to the
// repair record, the pending comment and the latest run. All of it is the
// record's text, shown only as text.
export function RepairSummary({ issue, repair }: { issue: StatusIssue; repair: Repair }) {
  const run = latestRun(issue)
  return (
    <section aria-label="Why it stopped" data-slot="repair-summary" className="flex flex-col gap-3 rounded-md border border-border-subtle bg-muted/40 p-3 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <RepairBadge repair={repair} />
        {repair.category && (
          <span data-slot="repair-category" className="break-words text-muted-foreground">
            {repair.category}
          </span>
        )}
      </div>
      <p data-slot="repair-reason" className="font-medium break-words">
        {repair.reason}
      </p>
      <dl className="flex flex-col gap-2">
        {repair.done && (
          <Fact term="Nibfin did" slot="repair-done">
            {repair.done}
          </Fact>
        )}
        <Fact term="Links" slot="repair-links">
          <span className="flex flex-wrap gap-x-4 gap-y-1">
            {repair.record ? (
              <SafeLink href={repair.record} label="(opens in a new tab)">
                Repair record
              </SafeLink>
            ) : (
              <span className="text-muted-foreground">No repair record</span>
            )}
            {issue.pendingComment && (
              <SafeLink href={issue.pendingComment} label="(opens in a new tab)">
                Pending comment
              </SafeLink>
            )}
            {run.kind === 'run' && (
              <SafeLink href={run.run.url} label="(opens in a new tab)">
                Latest run
              </SafeLink>
            )}
            {run.kind === 'failed' && (
              <span className="text-danger">{`Runs could not be read: ${run.error.code}: ${run.error.message}`}</span>
            )}
            {run.kind === 'notRead' && <span className="text-muted-foreground">Runs not read</span>}
            {run.kind === 'none' && <span className="text-muted-foreground">No runs</span>}
          </span>
        </Fact>
      </dl>
    </section>
  )
}

// optionInputs shows the Issue as fixed text, the option as a list of the
// record's options, and no repository field: the Issue URL carries it.
function optionInputs(issue: StatusIssue, repair: Repair): FieldInputs {
  const options = repair.options ?? []
  return ({ field, value, error, disabled, onChange }) => {
    if (field.key === issuesKey) {
      return (
        <p data-field={field.key} className="text-sm break-words">
          <span className="text-muted-foreground">Issue </span>
          <span className="font-medium">{`#${issue.number} ${issue.title}`}</span>
        </p>
      )
    }
    if (field.key === optionKey) {
      const i = fieldIds(field.key)
      return (
        <fieldset data-field={field.key} className="flex min-w-0 flex-col gap-2" aria-describedby={error ? i.error : undefined}>
          <legend id={i.label} className="mb-2 text-sm font-medium">
            Choose one option
          </legend>
          <RadioGroup
            value={String(value)}
            onValueChange={(v) => onChange(v)}
            disabled={disabled}
            aria-labelledby={i.label}
            aria-invalid={error ? true : undefined}
            className="gap-2"
          >
            {options.map((o) => {
              const id = `${i.input}-${o.number}`
              return (
                <label
                  key={o.number}
                  htmlFor={id}
                  data-option={o.number}
                  className="flex cursor-pointer items-start gap-3 rounded-md border border-border p-3 text-sm has-[[data-state=checked]]:border-primary has-[[data-state=checked]]:bg-accent has-[:disabled]:cursor-not-allowed"
                >
                  <RadioGroupItem id={id} value={String(o.number)} className="mt-0.5" aria-describedby={`${id}-more`} />
                  <span className="flex min-w-0 flex-col gap-1">
                    <span className="flex flex-wrap items-center gap-x-2 gap-y-1 font-medium break-words">
                      <span className="tabular-nums">{`${o.number}.`}</span>
                      <span className="min-w-0 break-words">{o.title}</span>
                      {o.recommended && (
                        <Badge variant="outline" className={tones.success} data-slot="recommended">
                          Recommended
                        </Badge>
                      )}
                    </span>
                    <span id={`${id}-more`} className="flex flex-col gap-1 text-muted-foreground">
                      <span className="break-words whitespace-pre-line">{o.consequence}</span>
                      {o.issueChange && <span className="break-words whitespace-pre-line">{`Issue change: ${o.issueChange}`}</span>}
                    </span>
                  </span>
                </label>
              )
            })}
          </RadioGroup>
          <ErrorText id={i.error} text={error} />
        </fieldset>
      )
    }
    // The Issue URL carries the repository.
    if (field.key === 'repo') return null
    return undefined
  }
}

// CopyBlock shows a command or text as code with a Copy button; nothing
// runs from it (Proposal R9.55).
function CopyBlock({ text, label }: { text: string; label: string }) {
  const [copy, setCopy] = useState<'idle' | 'copied' | 'failed'>('idle')
  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(text)
      setCopy('copied')
    } catch {
      setCopy('failed')
    }
  }
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <pre className="max-h-64 overflow-auto rounded-md border border-border-subtle bg-muted p-3 text-xs break-words whitespace-pre-wrap">
        <code>{text}</code>
      </pre>
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="outline" size="sm" onClick={() => void onCopy()} data-action="copy" aria-label={`${copy === 'copied' ? 'Copied' : 'Copy'} ${label}`}>
          {copy === 'copied' ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
          {copy === 'copied' ? 'Copied' : 'Copy'}
        </Button>
        <span role="status" className="text-xs text-muted-foreground">
          {copy === 'failed' ? 'Copy failed: select the text' : ''}
        </span>
      </div>
    </div>
  )
}

// AdviceDialog shows a Needs your action record's advice; nothing in it
// runs or writes anything (Proposal R9.55). The person resumes afterwards.
function AdviceDialog({ issue, repair, open, onOpenChange }: { issue: StatusIssue; repair: Repair; open: boolean; onOpenChange: (o: boolean) => void }) {
  const steps = repair.steps ?? []
  const bug = repair.bugReport
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent data-slot="repair-dialog" data-repair={repair.state} className="flex max-h-[calc(100svh-2rem)] flex-col bg-card sm:max-w-2xl">
        <DialogHeader className="pr-8 text-left">
          <DialogTitle className="break-words">{`Repair #${issue.number}`}</DialogTitle>
          <DialogDescription className="break-words">{issue.title}</DialogDescription>
        </DialogHeader>
        <div className="-mx-6 flex min-h-0 flex-col gap-4 overflow-y-auto px-6 py-1 text-sm">
          <RepairSummary issue={issue} repair={repair} />
          {repair.advice && (
            <section aria-label="What to do" data-slot="repair-advice" className="flex flex-col gap-1">
              <h3 className="font-semibold">What to do</h3>
              <p className="break-words whitespace-pre-line">{repair.advice}</p>
            </section>
          )}
          {steps.length > 0 && (
            <section aria-label="Steps" data-slot="repair-steps" className="flex flex-col gap-2">
              <h3 className="font-semibold">Steps</h3>
              <ol className="flex list-decimal flex-col gap-3 pl-5">
                {steps.map((s, i) => (
                  <li key={i} className="min-w-0 pl-1">
                    <div className="flex min-w-0 flex-col gap-1.5">
                      <p className="break-words whitespace-pre-line">{s.text}</p>
                      {s.link && (
                        <SafeLink href={s.link} label="(opens in a new tab)">
                          <span className="break-all">{s.link}</span>
                        </SafeLink>
                      )}
                      {s.command && <CopyBlock text={s.command} label={`the command of step ${i + 1}`} />}
                    </div>
                  </li>
                ))}
              </ol>
            </section>
          )}
          {bug && (
            <section aria-label="Bug report" data-slot="repair-bug" className="flex flex-col gap-2">
              <h3 className="font-semibold">Report it to Nibfin</h3>
              <p className="break-words">
                File it at{' '}
                <SafeLink href={bug.url} label="(opens in a new tab)">
                  <span className="break-all">{bug.url}</span>
                </SafeLink>
              </p>
              <p className="font-medium break-words">{bug.title}</p>
              <CopyBlock text={bug.body} label="the bug report" />
            </section>
          )}
          <p data-slot="repair-resume-note" className="rounded-md border border-border-subtle px-3 py-2 text-muted-foreground">
            Nothing here runs from the Dashboard. When you have done this, use Resume on the Issue to remove pending.
          </p>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export interface RepairActionProps {
  issue: StatusIssue
  repair: Repair
  gate: Access
  target: RepairTarget
  // describedBy names the elements with the reasons the button is disabled.
  describedBy?: string
  // onChanged asks the placing view to read the status again.
  onChanged: () => void
}

// RepairAction is the Repair button of a pending Issue that waits on a
// person, and its dialog (Proposal R9.53–R9.56). Waiting for your choice
// answers the record's options with resume --option; Needs your action only
// shows the advice.
export function RepairAction({ issue, repair, gate, target, describedBy, onChanged }: RepairActionProps) {
  const [open, setOpen] = useState(false)
  const button = useRef<HTMLButtonElement>(null)
  const choose = repair.state === 'choose'
  // The dialogs have no trigger element, so closing one moves focus back to
  // the button that opened it.
  const onOpenChange = (o: boolean) => {
    setOpen(o)
    if (!o) requestAnimationFrame(() => button.current?.focus())
  }
  return (
    <>
      <Button
        ref={button}
        variant="outline"
        size="sm"
        data-action="repair"
        disabled={!gate.enabled || !target.ready}
        aria-describedby={!gate.enabled || !target.ready ? describedBy : undefined}
        onClick={() => setOpen(true)}
      >
        <Wrench aria-hidden="true" />
        Repair
      </Button>
      {choose && target.ready && target.command && target.recommended !== undefined && (
        <OperationDialog
          command={target.command}
          open={open}
          onOpenChange={onOpenChange}
          onFinished={onChanged}
          title={`Repair #${issue.number}`}
          initial={{ [issuesKey]: issue.url, [optionKey]: String(target.recommended) }}
          intro={<RepairSummary issue={issue} repair={repair} />}
          inputs={optionInputs(issue, repair)}
          confirm={false}
        />
      )}
      {!choose && <AdviceDialog issue={issue} repair={repair} open={open} onOpenChange={onOpenChange} />}
    </>
  )
}
