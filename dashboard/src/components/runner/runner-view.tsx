import { useState, type ReactNode } from 'react'
import { KeyRound, LogIn, MemoryStick, Minus, Plus, Server, type LucideIcon } from 'lucide-react'

import { tones } from '@/components/issue/tones'
import { OperationDialog } from '@/components/operation'
import { Note } from '@/components/shell/note'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { commandByApi } from '@/lib/commands'
import { providerTitle } from '@/lib/login'
import type { Command, Values } from '@/lib/operation'
import { gib, githubLabel, providerPreset, serviceLabel, slotsByProvider, type Label } from '@/lib/runner'
import { hasNoRunner } from '@/lib/shell'
import type { ApiError, HostReport, PersonView, ProviderReport, RunnerState } from '@/lib/status'
import { cn } from '@/lib/utils'

import { useCommands, type CommandsState } from './use-commands'

const code = 'rounded-sm bg-[var(--nf-code-bg)] px-1.5 py-0.5 font-mono text-[0.85em] break-words text-[var(--nf-code)]'

type Opener = (api: string, provider: string) => void

// RunnerView shows this host's runner state: the machine and its memory,
// each provider's login and Runner slots, and opens runner add, runner
// remove and login in the operation dialog. onFinished reads the status
// again after a dialog that applied something.
export function RunnerView({ data, onFinished }: { data: PersonView; onFinished?: () => void }) {
  const commands = useCommands()
  const [open, setOpen] = useState<{ command: Command; initial: Values } | null>(null)

  const opener: Opener | undefined =
    commands.state === 'loaded'
      ? (api, provider) => {
          const c = commandByApi(commands.commands, api)
          if (c) setOpen({ command: c, initial: providerPreset(c, provider) })
        }
      : undefined
  const has = (api: string) => commands.state === 'loaded' && commandByApi(commands.commands, api) !== undefined

  return (
    <>
      <CommandsNote commands={commands} />
      {hasNoRunner(data.host) ? (
        <NoRunnerGuide host={data.host} open={has('runner-add') ? opener : undefined} />
      ) : (
        <RunnerSections host={data.host} open={opener} has={has} />
      )}
      {open && (
        <OperationDialog
          key={open.command.path}
          command={open.command}
          initial={open.initial}
          open
          onOpenChange={(o) => !o && setOpen(null)}
          onFinished={onFinished}
        />
      )}
    </>
  )
}

function CommandsNote({ commands }: { commands: CommandsState }) {
  if (commands.state !== 'failed') return null
  return (
    <Note tone="danger" data-note="commands-error">
      Commands could not be read: {commands.error.code}: {commands.error.message}
    </Note>
  )
}

function ErrorText({ error, className }: { error: ApiError; className?: string }) {
  return (
    <p data-slot="error" className={cn('text-sm break-words text-danger', className)}>
      <code className="font-semibold">{error.code}</code>: {error.message}
    </p>
  )
}

function Tag({ label, icon: I, slot }: { label: Label; icon?: LucideIcon; slot: string }) {
  return (
    <Badge variant="outline" data-badge={slot} className={cn('whitespace-normal', tones[label.tone])}>
      {I && <I aria-hidden="true" />}
      {label.text}
    </Badge>
  )
}

function RunnerSections({ host, open, has }: { host: HostReport; open?: Opener; has: (api: string) => boolean }) {
  const groups = slotsByProvider(host)
  const providers: ProviderReport[] = [...(host.providers ?? [])]
  // A provider that only a slot names still gets its section.
  for (const p of groups.keys()) {
    if (!providers.some((x) => x.provider === p)) providers.push({ provider: p, loggedIn: false })
  }
  const { error, githubError } = host.runners
  return (
    <>
      <HostCard host={host} />
      {error && <Note tone="warning">Runners could not be read: {error.code}: {error.message}</Note>}
      {githubError && (
        <Note tone="warning" data-note="github-error">
          GitHub&apos;s Runner list could not be read; the local state is shown. {githubError.code}: {githubError.message}
        </Note>
      )}
      {providers.map((p) => (
        <ProviderSection
          key={p.provider}
          provider={p}
          slots={groups.get(p.provider) ?? []}
          githubError={githubError}
          open={open}
          has={has}
        />
      ))}
    </>
  )
}

function HostCard({ host }: { host: HostReport }) {
  const h = host.host
  const m = host.memory
  const repo = host.runners.repo
  const free = m && !m.error && m.totalKB > 0 ? Math.min(100, Math.round((m.availableKB / m.totalKB) * 100)) : null
  return (
    <Card className="gap-4 border-border-subtle py-4" data-card="host">
      <CardHeader className="px-4 sm:px-6">
        <CardTitle>
          <h2 className="flex items-center gap-2 text-base">
            <Server aria-hidden="true" className="size-5 shrink-0" />
            <span className="min-w-0 break-words">{h.describe || 'This host'}</span>
          </h2>
        </CardTitle>
        {(h.machine || h.vmType) && (
          <CardDescription className="break-words">
            {h.machine && (
              <>
                Machine <span className="font-medium text-foreground">{h.machine}</span>
              </>
            )}
            {h.machine && h.vmType && ' · '}
            {h.vmType && <>VM type {h.vmType}</>}
          </CardDescription>
        )}
      </CardHeader>
      <CardContent className="flex flex-col gap-4 px-4 sm:px-6">
        <dl className="grid gap-4 sm:grid-cols-2">
          <div className="flex flex-col gap-1.5">
            <dt className="text-sm text-muted-foreground">State</dt>
            <dd className="flex flex-col gap-1.5">
              {h.error ? (
                <ErrorText error={h.error} />
              ) : (
                <Tag slot="machine" label={h.running ? { text: 'Running', tone: 'success' } : { text: 'Stopped', tone: 'warning' }} />
              )}
            </dd>
          </div>
          <div className="flex flex-col gap-1.5">
            <dt className="flex items-center gap-1.5 text-sm text-muted-foreground">
              <MemoryStick aria-hidden="true" className="size-4" />
              Free memory
            </dt>
            <dd className="flex flex-col gap-1.5">
              {m?.error ? (
                <ErrorText error={m.error} />
              ) : m && m.totalKB > 0 ? (
                <>
                  <p data-slot="memory" className="text-sm">
                    <span className="font-semibold tabular-nums">{gib(m.availableKB)}</span>{' '}
                    <span className="text-muted-foreground">of {gib(m.totalKB)}</span>
                  </p>
                  {free !== null && (
                    <div
                      role="meter"
                      aria-label="Free memory"
                      aria-valuemin={0}
                      aria-valuemax={100}
                      aria-valuenow={free}
                      aria-valuetext={`${free}% free`}
                      className="h-2 w-full max-w-xs overflow-hidden rounded-full border border-border bg-muted"
                    >
                      <div className="h-full rounded-full bg-primary" style={{ width: `${free}%` }} />
                    </div>
                  )}
                </>
              ) : (
                <p className="text-sm text-muted-foreground">Not read</p>
              )}
            </dd>
          </div>
        </dl>
        {repo && (
          <p data-slot="repo" className="text-sm break-words">
            <span className="text-muted-foreground">Runners serve </span>
            <span className="font-medium">{repo}</span>
          </p>
        )}
      </CardContent>
    </Card>
  )
}

function OpenButton({
  api,
  provider,
  open,
  has,
  icon: I,
  children,
}: {
  api: string
  provider: string
  open?: Opener
  has: (api: string) => boolean
  icon: LucideIcon
  children: ReactNode
}) {
  return (
    <Button
      variant="outline"
      size="sm"
      disabled={!open || !has(api)}
      onClick={() => open?.(api, provider)}
      data-open={api}
      className="flex-1 sm:flex-none"
    >
      <I aria-hidden="true" />
      {children}
    </Button>
  )
}

function ProviderSection({
  provider,
  slots,
  githubError,
  open,
  has,
}: {
  provider: ProviderReport
  slots: RunnerState[]
  githubError?: ApiError
  open?: Opener
  has: (api: string) => boolean
}) {
  const p = provider.provider
  const titleId = `provider-${p}`
  return (
    <Card className="gap-4 border-border-subtle py-4" data-provider={p} aria-labelledby={titleId} role="region">
      <CardHeader className="flex flex-col gap-3 px-4 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between sm:px-6">
        <div className="flex min-w-0 flex-col gap-1.5">
          <CardTitle>
            <h2 id={titleId} className="text-base">
              {providerTitle(p)}
            </h2>
          </CardTitle>
          <div className="flex flex-wrap items-center gap-1.5" data-slot="login-state">
            {provider.error ? (
              <ErrorText error={provider.error} />
            ) : (
              <Tag
                slot="login"
                icon={KeyRound}
                label={provider.loggedIn ? { text: 'Logged in', tone: 'success' } : { text: 'Not logged in', tone: 'warning' }}
              />
            )}
          </div>
        </div>
        <div className="flex w-full flex-wrap gap-2 sm:w-auto">
          <OpenButton api="login" provider={p} open={open} has={has} icon={LogIn}>
            Log in…
          </OpenButton>
          <OpenButton api="runner-add" provider={p} open={open} has={has} icon={Plus}>
            Add Runners…
          </OpenButton>
          <OpenButton api="runner-remove" provider={p} open={open} has={has} icon={Minus}>
            Remove Runners…
          </OpenButton>
        </div>
      </CardHeader>
      <CardContent className="px-4 sm:px-6">
        {slots.length === 0 ? (
          <p className="text-sm text-muted-foreground">No Runners of {p} on this host.</p>
        ) : (
          <ul aria-label={`${providerTitle(p)} Runners`} className="divide-y divide-border-subtle rounded-md border border-border-subtle">
            {slots.map((s) => (
              <li
                key={s.slot}
                data-slot-id={s.slot}
                className="flex flex-col gap-2 px-3 py-2.5 sm:flex-row sm:items-center sm:justify-between"
              >
                <p className="min-w-0 break-words">
                  <span className="font-medium">{s.slot}</span>
                  {s.name && <span className="text-sm text-muted-foreground"> · {s.name}</span>}
                </p>
                <div className="flex flex-wrap gap-1.5 sm:justify-end">
                  <Tag slot="service" label={serviceLabel(s)} />
                  <Tag slot="github" label={githubLabel(s, githubError)} />
                </div>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}

// NoRunnerGuide guides runner add then init when this host has no Runner
// yet; Add Runners… opens runner add per provider.
function NoRunnerGuide({ host, open }: { host: HostReport; open?: Opener }) {
  const error = host.runners.error
  const providers = (host.providers ?? []).map((p) => p.provider)
  const list = providers.length > 0 ? providers : ['claude', 'codex']
  return (
    <Card className="border-border-subtle" data-guide="no-runner">
      <CardHeader>
        <CardTitle>
          <h2 className="flex items-center gap-2 text-base">
            <Server aria-hidden="true" className="size-5 shrink-0" />
            This host has no Runner yet
          </h2>
        </CardTitle>
        <CardDescription>Nibfin runs its work on Runners on this host. Set them up in two steps.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {error && (
          <Note tone="warning">
            {error.code}: {error.message}
          </Note>
        )}
        <ol className="flex flex-col gap-4">
          <Step n={1}>
            <p>
              Run <code className={code}>gh nibfin runner add &lt;claude|codex&gt;</code> in the repository the Runners
              should serve.
            </p>
            <p className="text-sm text-muted-foreground">It also completes Runners an interrupted add left.</p>
            {open && (
              <div className="flex flex-wrap gap-2 pt-1">
                {list.map((p) => (
                  <Button key={p} variant="outline" size="sm" onClick={() => open('runner-add', p)} data-open="runner-add" data-provider-open={p}>
                    <Plus aria-hidden="true" />
                    Add {providerTitle(p)} Runners…
                  </Button>
                ))}
              </div>
            )}
          </Step>
          <Step n={2}>
            <p>
              Run <code className={code}>gh nibfin init</code> in each repository that should use Nibfin.
            </p>
          </Step>
        </ol>
      </CardContent>
    </Card>
  )
}

function Step({ n, children }: { n: number; children: ReactNode }) {
  return (
    <li className="flex items-start gap-3">
      <span
        aria-hidden="true"
        className="flex size-7 shrink-0 items-center justify-center rounded-full border border-border bg-muted text-sm font-semibold"
      >
        {n}
      </span>
      <div className="flex min-w-0 flex-col gap-1 pt-0.5">{children}</div>
    </li>
  )
}
