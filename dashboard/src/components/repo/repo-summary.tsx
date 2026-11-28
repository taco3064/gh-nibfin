import { useEffect, useState, type ReactNode } from 'react'
import { CircleAlert, CircleCheck, CircleHelp, CircleMinus, ExternalLink, GitPullRequest, Rocket, TriangleAlert, Wrench, type LucideIcon } from 'lucide-react'

import { ExternalMark } from '@/components/issue/external-mark'
import { tones, type Tone } from '@/components/issue/tones'
import { OperationDialog } from '@/components/operation'
import { imageIcons } from '@/components/person/image-icons'
import { Note } from '@/components/shell/note'
import { SectionLoading } from '@/components/shell/section-loading'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { commandByApi, loadCommands } from '@/lib/commands'
import { useLoading } from '@/lib/loading-context'
import type { Command } from '@/lib/operation'
import { loadingLabels, type Loading } from '@/lib/partial'
import { imageState } from '@/lib/person'
import { offeredActions, runLook, workflowsLook, type RunLook, type WorkflowsLook } from '@/lib/repo'
import { useReadOnly, type RepoViewData } from '@/lib/repo-source'
import type { Outcome } from '@/lib/session'
import { ownerRepo, repoHost, type LatestImage, type NibfinRun, type RepoImage } from '@/lib/status'
import { cn, formatTime } from '@/lib/utils'

// The command schema is loaded once per page; a failed load is tried again
// the next time a repository opens.
let commandsLoad: Promise<Outcome<Command[]>> | null = null

function useCommands(): Outcome<Command[]> | undefined {
  const [loaded, setLoaded] = useState<Outcome<Command[]>>()
  useEffect(() => {
    let live = true
    commandsLoad ??= loadCommands()
    const p = commandsLoad
    void p.then((r) => {
      if (!r.ok && commandsLoad === p) commandsLoad = null
      if (live) setLoaded(r)
    })
    return () => {
      live = false
    }
  }, [])
  return loaded
}

const linkClass = 'rounded-sm text-link underline-offset-4 hover:underline'

export interface RepoSummaryProps {
  data: RepoViewData
  // onFinished reads the repository again after an init or upgrade.
  onFinished: () => void
}

// RepoSummary heads the repository view: the repository's link on its own
// host, three health lights (whether a Nibfin run succeeded after
// nibfin-issue.yml last changed, the image the workflows pin, and the
// Nibfin workflow set), and init or upgrade through the operation dialog.
// Each light still loading shows its own loading state. A light shows only
// when the source reads its data; read-only, init and upgrade are not
// offered.
export function RepoSummary({ data, onFinished }: RepoSummaryProps) {
  const loading = useLoading()
  const readOnly = useReadOnly()
  const { image, latestImage, nibfinRun } = data
  const hasImage = image !== undefined && latestImage !== undefined
  const name = ownerRepo(data.repo)
  const rest = loadingLabels(loading, ['permission', 'host', 'latestImage'])
  return (
    <Card className="gap-4 border-border-subtle py-4" data-slot="repo-summary">
      <CardHeader className="flex flex-wrap items-start justify-between gap-3 px-4 sm:px-5">
        <div className="flex min-w-0 flex-col gap-2">
          <h2 className="min-w-0 text-lg font-semibold tracking-tight break-all">
            <a href={`https://${repoHost(data.repo)}/${name}`} target="_blank" rel="noreferrer" className={linkClass} data-slot="repo-link">
              {name}
              <ExternalLink aria-hidden="true" className="ms-1 inline size-4 align-[-0.125em]" />
              <span className="sr-only"> (opens GitHub)</span>
            </a>
          </h2>
          <p className="text-sm text-muted-foreground">on {repoHost(data.repo)}</p>
        </div>
        {!readOnly && image && !loading.has('image') && <Actions repo={data.repo} image={image} onFinished={onFinished} />}
      </CardHeader>
      {(image || nibfinRun || rest.length > 0) && (
        <CardContent className="flex flex-col gap-3 px-4 sm:px-5">
          {(image || nibfinRun) && (
            <div className="grid gap-3 text-sm sm:grid-cols-3" data-slot="health">
              {nibfinRun && <RunCard run={nibfinRun} loading={loading} />}
              {hasImage && <ImageCard image={image} latestImage={latestImage} loading={loading} />}
              {image && <WorkflowsCard image={image} loading={loading} />}
            </div>
          )}
          {rest.length > 0 && <SectionLoading label={`Still reading: ${rest.join(', ')}`} data-loading="summary" />}
        </CardContent>
      )}
    </Card>
  )
}

// lights pairs each tone with the colour of its light. The light only
// repeats what the icon and the label beside it say.
const lights: Record<Tone, string> = {
  neutral: 'bg-muted-foreground ring-muted',
  info: 'bg-info ring-info-bg',
  success: 'bg-success ring-success-bg',
  warning: 'bg-warning ring-warning-bg',
  danger: 'bg-danger ring-danger-bg',
}

type DataAttrs = Record<`data-${string}`, string>

// HealthCard is one health light of the summary: its title beside a light
// in the state's tone, then the state and its detail line. A card still
// being read shows an empty light.
function HealthCard({ title, tone, children, ...data }: { title: string; tone?: Tone; children: ReactNode } & DataAttrs) {
  return (
    <section aria-label={title} className="flex min-w-0 flex-col gap-2 rounded-lg border border-border-subtle p-3" data-tone={tone ?? 'loading'} {...data}>
      <h3 className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
        <span aria-hidden="true" className={cn('size-3 shrink-0 rounded-full', tone ? cn('ring-4', lights[tone]) : 'border border-muted-foreground')} />
        {title}
      </h3>
      {children}
    </section>
  )
}

function StateBadge({ tone, icon: Icon, label }: { tone: Tone; icon: LucideIcon; label: string }) {
  return (
    <Badge variant="outline" className={cn('h-auto max-w-full text-left whitespace-normal', tones[tone])}>
      <Icon aria-hidden="true" />
      {label}
    </Badge>
  )
}

function RunCard({ run, loading }: { run: NibfinRun; loading: Loading }) {
  if (loading.has('nibfinRun')) {
    return (
      <HealthCard title="Nibfin runs" data-slot="nibfin-run">
        <SectionLoading label="Reading the Nibfin run…" data-loading="nibfinRun" />
      </HealthCard>
    )
  }
  const look = runLook(run)
  return (
    <HealthCard title="Nibfin runs" tone={look.tone} data-slot="nibfin-run">
      <RunState run={run} look={look} />
    </HealthCard>
  )
}

function ImageCard({ image: pin, latestImage, loading }: { image: RepoImage; latestImage: LatestImage; loading: Loading }) {
  if (loading.has('image')) {
    return (
      <HealthCard title="Image" data-slot="image">
        <SectionLoading label="Reading the image pin…" data-loading="image" />
      </HealthCard>
    )
  }
  // A pin is compared with the latest image once that is read.
  const comparing = loading.has('latestImage') && pin.set === 'execution' && pin.ref !== null && pin.latest === null
  const image = imageState(pin, latestImage)
  return (
    <HealthCard title="Image" tone={comparing ? undefined : image.tone} data-slot="image">
      <div className="flex min-w-0 flex-col items-start gap-1" data-image={comparing ? 'comparing' : image.kind}>
        {comparing ? (
          <SectionLoading label="Comparing with the latest image…" data-loading="latestImage" />
        ) : (
          <StateBadge tone={image.tone} icon={imageIcons[image.kind]} label={image.label} />
        )}
        {pin.ref && <code className="max-w-full min-w-0 font-mono text-xs break-all">{pin.ref}</code>}
        {!comparing && image.detail && <span className="min-w-0 break-words text-muted-foreground">{image.detail}</span>}
      </div>
    </HealthCard>
  )
}

const workflowIcons: Record<WorkflowsLook['state'], LucideIcon> = {
  error: CircleAlert,
  execution: CircleCheck,
  peer: CircleCheck,
  none: CircleMinus,
  unknown: CircleHelp,
}

// WorkflowsCard names the Nibfin workflow set and the files the status read
// confirmed. It links no file: the read gives no file URL.
function WorkflowsCard({ image, loading }: { image: RepoImage; loading: Loading }) {
  if (loading.has('image')) {
    return (
      <HealthCard title="Workflows" data-slot="workflows">
        <SectionLoading label="Reading the workflows…" data-loading="workflows" />
      </HealthCard>
    )
  }
  const look = workflowsLook(image)
  return (
    <HealthCard title="Workflows" tone={look.tone} data-slot="workflows" data-workflows={look.state}>
      <StateBadge tone={look.tone} icon={workflowIcons[look.state]} label={look.label} />
      {look.files.length > 0 && (
        <span className="min-w-0 break-words text-muted-foreground" data-slot="workflow-files">
          {look.files.map((f, i) => (
            <span key={f}>
              {i > 0 && ', '}
              <code className="font-mono text-xs">{f}</code>
            </span>
          ))}{' '}
          found
        </span>
      )}
      {look.state === 'error' && image.error && (
        <span className="break-words text-danger">
          {image.error.code}: {image.error.message}
        </span>
      )}
    </HealthCard>
  )
}

const runIcons: Record<RunLook['state'], LucideIcon> = {
  error: CircleAlert,
  'not-used': CircleMinus,
  succeeded: CircleCheck,
  'not-succeeded': TriangleAlert,
}

function RunState({ run, look }: { run: NibfinRun; look: RunLook }) {
  const change = run.commit ? <ChangeLine run={run} /> : null
  return (
    <div className="flex min-w-0 flex-col items-start gap-1" data-state={look.state}>
      <StateBadge tone={look.tone} icon={runIcons[look.state]} label={look.label} />
      {look.state === 'not-used' ? (
        <span className="text-muted-foreground">
          No <code className="font-mono text-xs">nibfin-issue.yml</code> on the default branch
        </span>
      ) : (
        change
      )}
      {look.state === 'error' && run.error && (
        <span className="break-words text-danger">
          {run.error.code}: {run.error.message}
        </span>
      )}
      {look.state === 'succeeded' && run.run && (
        <span>
          <a href={run.run} target="_blank" rel="noreferrer" className={linkClass}>
            Successful run{run.at && <> at <time dateTime={run.at}>{formatTime(run.at)}</time></>}
            <ExternalMark label="(opens on GitHub in a new tab)" />
          </a>
        </span>
      )}
    </div>
  )
}

// ChangeLine names the last change of nibfin-issue.yml on the default
// branch: its merged PR, or else its commit, and the time.
function ChangeLine({ run }: { run: NibfinRun }) {
  const file = <code className="font-mono text-xs">nibfin-issue.yml</code>
  const at = run.changedAt ? (
    <>
      {' '}
      <time dateTime={run.changedAt}>{formatTime(run.changedAt)}</time>
    </>
  ) : null
  if (run.pr) {
    return (
      <span className="min-w-0 break-words text-muted-foreground" data-slot="change">
        <GitPullRequest aria-hidden="true" className="me-1 inline size-3.5 align-[-0.125em]" />
        <span>
          {file} last changed in PR{' '}
          <a href={run.pr.url} target="_blank" rel="noreferrer" className={linkClass}>
            #{run.pr.number}
            <ExternalMark label="(opens on GitHub in a new tab)" />
          </a>
          , merged{at}
        </span>
      </span>
    )
  }
  return (
    <span className="min-w-0 break-words text-muted-foreground" data-slot="change">
      {file} last changed in commit <code className="font-mono text-xs">{(run.commit ?? '').slice(0, 7)}</code>
      {at}
    </span>
  )
}

const actionLook = {
  init: { label: 'Init', icon: Rocket },
  upgrade: { label: 'Upgrade', icon: Wrench },
}

function Actions({ repo, image, onFinished }: { repo: string; image: RepoImage; onFinished: () => void }) {
  const commands = useCommands()
  const [open, setOpen] = useState<'init' | 'upgrade'>()
  const offered = offeredActions(image)
  const missing = commands?.ok ? offered.filter((a) => !commandByApi(commands.value, a)) : []
  const reason = !commands
    ? undefined
    : !commands.ok
      ? `Commands could not be loaded: ${commands.error.code}: ${commands.error.message}`
      : missing.length > 0
        ? `The server offers no ${missing.join(' or ')} command.`
        : undefined
  const command = open && commands?.ok ? commandByApi(commands.value, open) : undefined
  return (
    <div className="flex min-w-0 flex-col gap-2 sm:items-end" data-slot="repo-actions">
      <div className="flex flex-wrap gap-2">
        {offered.map((a) => {
          const look = actionLook[a]
          const Icon = look.icon
          const enabled = commands?.ok === true && commandByApi(commands.value, a) !== undefined
          return (
            <Button key={a} variant={a === 'init' ? 'default' : 'outline'} size="sm" disabled={!enabled} data-action={a} onClick={() => setOpen(a)}>
              <Icon aria-hidden="true" />
              {look.label}
            </Button>
          )
        })}
      </div>
      {reason && (
        <Note tone="warning" className="max-w-full sm:max-w-xs" data-note="commands">
          {reason}
        </Note>
      )}
      {command && (
        <OperationDialog
          command={command}
          open
          onOpenChange={(o) => {
            if (!o) setOpen(undefined)
          }}
          onFinished={onFinished}
          initial={{ repo }}
        />
      )}
    </div>
  )
}
