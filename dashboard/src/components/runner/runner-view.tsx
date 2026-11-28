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
import { elapsed, jobLabel, providerPreset, slotsByProvider, type Label } from '@/lib/runner'
import { hasNoRunner } from '@/lib/shell'
import type { Activity, ApiError, HostReport, PersonView, ProviderReport, RunnerState } from '@/lib/status'
import { cn } from '@/lib/utils'

import { BusyTimeline } from './busy-timeline'
import { MemoryGauge } from './memory-gauge'
import { RunnerTile } from './runner-tiles'
import { useActivity, useNow, type ActivityView } from './use-activity'
import { useCommands, type CommandsState } from './use-commands'

const code = 'rounded-sm bg-[var(--nf-code-bg)] px-1.5 py-0.5 font-mono text-[0.85em] break-words text-[var(--nf-code)]'

type Opener = (api: string, provider: string) => void

// RunnerView shows this host's runner state: the machine and its memory
// gauge, each provider's login and Runner tiles with what each Runner is
// doing, and each Runner's busy periods of the last 24 hours. It opens
// runner add, runner remove and login in the operation dialog. onFinished
// reads the status again after a dialog that applied something; each newer
// status read reads the activity again.
export function RunnerView({ data, onFinished }: { data: PersonView; onFinished?: () => void }) {
  const commands = useCommands()
  const activity = useActivity(data)
  const [open, setOpen] = useState<{ command: Command; initial: Values } | null>(null)

  const opener: Opener | undefined =
    commands.state === 'loaded'
      ? (api, provider) => {
          const c = commandByApi(commands.commands, api)
          if (c) setOpen({ command: c, initial: providerPreset(c, provider) })
        }
      : undefined
  const has = (api: string) => commands.state === 'loaded' && commandByApi(commands.commands, api) !== undefined
  // Without a complete Runner the guide leads; the host and the providers
  // still show below it.
  const none = hasNoRunner(data.host)

  return (
    <>
      <CommandsNote commands={commands} />
      {none && <NoRunnerGuide host={data.host} open={has('runner-add') ? opener : undefined} />}
      <RunnerSections host={data.host} open={opener} has={has} runnersError={!none} activity={activity} />
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

// RunnerSections is the host card and one section per provider. runnersError
// is false when the guide already shows the runners error.
function RunnerSections({
  host,
  open,
  has,
  runnersError,
  activity,
}: {
  host: HostReport
  open?: Opener
  has: (api: string) => boolean
  runnersError: boolean
  activity: ActivityView
}) {
  const now = useNow()
  const groups = slotsByProvider(host)
  const providers: ProviderReport[] = [...(host.providers ?? [])]
  // A provider that only a slot names still gets its section.
  for (const p of groups.keys()) {
    if (!providers.some((x) => x.provider === p)) providers.push({ provider: p, loggedIn: false })
  }
  const { error, githubError, githubNeedsAdmin, repo } = host.runners
  const act = activity.tiles
  const slots = host.runners.runners ?? []
  return (
    <>
      <HostCard host={host} activity={act} now={now} />
      {runnersError && error && (
        <Note tone="warning">
          Runners could not be read: {error.code}: {error.message}
        </Note>
      )}
      {githubError && (
        <Note tone="warning" data-note="github-error">
          GitHub&apos;s Runner list could not be read; the local state is shown. {githubError.code}: {githubError.message}
        </Note>
      )}
      {!githubError && githubNeedsAdmin && (
        <Note tone="info" data-note="local-state">
          Online and offline come from this host&apos;s Runner services: GitHub&apos;s Runner list needs admin of {repo ?? 'the repository'}.
        </Note>
      )}
      {providers.map((p) => (
        <ProviderSection
          key={p.provider}
          provider={p}
          slots={groups.get(p.provider) ?? []}
          githubError={githubError}
          needsAdmin={!!githubNeedsAdmin}
          activity={act}
          now={now}
          open={open}
          has={has}
        />
      ))}
      {slots.length > 0 && <BusyTimeline slots={slots} activity={activity.timeline} />}
    </>
  )
}

// HostJobs lists the running Job containers no Runner was matched to, and
// says when the containers could not be read.
function HostJobs({ activity, now }: { activity?: Activity; now: number }) {
  if (!activity) return null
  const { unattributed, containersError, jobsError } = activity
  return (
    <>
      {unattributed.map((j, i) => {
        const took = elapsed(j.startedAt, now)
        return (
          <p key={i} data-slot="host-job" className="text-sm break-words">
            <span className="text-muted-foreground">Job running on this host: </span>
            {j.issueUrl ? (
              <a href={j.issueUrl} target="_blank" rel="noreferrer" className="font-medium text-link underline-offset-2 hover:underline">
                {jobLabel(j)}
              </a>
            ) : (
              <span className="font-medium">{jobLabel(j)}</span>
            )}
            {took && <span className="text-muted-foreground"> for {took}</span>}
          </p>
        )
      })}
      {containersError && containersError.message !== jobsError?.message && (
        <p data-slot="containers-error" className="text-sm break-words text-muted-foreground">
          Job containers could not be read: {containersError.code}: {containersError.message}
        </p>
      )}
    </>
  )
}

function HostCard({ host, activity, now }: { host: HostReport; activity?: Activity; now: number }) {
  const h = host.host
  const m = host.memory
  const repo = host.runners.repo
  return (
    <Card className="gap-4 border-border-subtle py-4" data-card="host">
      <CardHeader className="px-4 sm:px-6">
        <CardTitle>
          <h2 className="flex items-center gap-2 text-base">
            <Server aria-hidden="true" className="size-5 shrink-0" />
            <span className="min-w-0 wrap-anywhere">{h.describe || 'This host'}</span>
          </h2>
        </CardTitle>
        {(h.machine || h.vmType) && (
          <CardDescription className="wrap-anywhere">
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
                <MemoryGauge memory={m} />
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
        <HostJobs activity={activity} now={now} />
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
    <Button variant="outline" size="sm" disabled={!open || !has(api)} onClick={() => open?.(api, provider)} data-open={api} className="flex-1 sm:flex-none">
      <I aria-hidden="true" />
      {children}
    </Button>
  )
}

function ProviderSection({
  provider,
  slots,
  githubError,
  needsAdmin,
  activity,
  now,
  open,
  has,
}: {
  provider: ProviderReport
  slots: RunnerState[]
  githubError?: ApiError
  needsAdmin: boolean
  activity?: Activity
  now: number
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
          <ul aria-label={`${providerTitle(p)} Runners`} className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {slots.map((s) => (
              <RunnerTile
                key={s.slot}
                slot={s}
                current={activity?.runners.find((r) => r.slot === s.slot)?.current}
                githubError={githubError}
                needsAdmin={needsAdmin}
                activityRead={!!activity && !activity.jobsError}
                now={now}
              />
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
              Run <code className={code}>gh nibfin runner add &lt;claude|codex&gt;</code> in the repository the Runners should serve.
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
      <span aria-hidden="true" className="flex size-7 shrink-0 items-center justify-center rounded-full border border-border bg-muted text-sm font-semibold">
        {n}
      </span>
      <div className="flex min-w-0 flex-col gap-1 pt-0.5">{children}</div>
    </li>
  )
}
