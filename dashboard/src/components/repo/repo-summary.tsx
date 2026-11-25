import { useEffect, useState, type ReactNode } from 'react'
import { CircleAlert, CircleCheck, CircleMinus, ExternalLink, GitPullRequest, Rocket, TriangleAlert, Wrench, type LucideIcon } from 'lucide-react'

import { ExternalMark } from '@/components/issue/issue-item'
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
import { offeredActions, runLook, type RunLook } from '@/lib/repo'
import { useReadOnly, type RepoViewData } from '@/lib/repo-source'
import type { Outcome } from '@/lib/session'
import { ownerRepo, repoHost, type LatestImage, type NibfinRun, type RepoImage } from '@/lib/status'
import { formatTime } from '@/lib/utils'

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
// host, the image its workflows pin, whether a Nibfin run succeeded after
// nibfin-issue.yml last changed, and init or upgrade through the operation
// dialog. Each section still loading shows its own loading state. A row
// shows only when the source reads it; read-only, init and upgrade are not
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
      <CardContent className="flex flex-col gap-3 px-4 sm:px-5">
        {(hasImage || nibfinRun) && (
          <dl className="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-[auto_1fr]">
            {hasImage && (
              <>
                <dt className="text-muted-foreground">Image</dt>
                <ImageState image={image} latestImage={latestImage} loading={loading} />
              </>
            )}
            {nibfinRun && (
              <>
                <dt className="text-muted-foreground">Nibfin run</dt>
                <dd className="min-w-0" data-slot="nibfin-run">
                  {loading.has('nibfinRun') ? <SectionLoading label="Reading the Nibfin run…" data-loading="nibfinRun" /> : <RunState run={nibfinRun} />}
                </dd>
              </>
            )}
          </dl>
        )}
        {rest.length > 0 && <SectionLoading label={`Still reading: ${rest.join(', ')}`} data-loading="summary" />}
      </CardContent>
    </Card>
  )
}

function ImageState({ image: pin, latestImage, loading }: { image: RepoImage; latestImage: LatestImage; loading: Loading }) {
  if (loading.has('image')) {
    return (
      <dd className="min-w-0">
        <SectionLoading label="Reading the image pin…" data-loading="image" />
      </dd>
    )
  }
  // A pin is compared with the latest image once that is read.
  const comparing = loading.has('latestImage') && pin.set === 'execution' && pin.ref !== null && pin.latest === null
  const image = imageState(pin, latestImage)
  const ImageIcon = imageIcons[image.kind]
  return (
    <dd className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1" data-image={comparing ? 'comparing' : image.kind}>
      {comparing ? (
        <SectionLoading label="Comparing with the latest image…" data-loading="latestImage" />
      ) : (
        <Badge variant="outline" className={tones[image.tone]}>
          <ImageIcon aria-hidden="true" />
          {image.label}
        </Badge>
      )}
      {pin.ref && <code className="min-w-0 font-mono text-xs break-all">{pin.ref}</code>}
      {!comparing && image.detail && <span className="min-w-0 break-words text-muted-foreground">{image.detail}</span>}
    </dd>
  )
}

function StateLine({ tone, icon: Icon, label, state, children }: { tone: Tone; icon: LucideIcon; label: string; state: string; children?: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1" data-state={state}>
      <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <Badge variant="outline" className={tones[tone]}>
          <Icon aria-hidden="true" />
          {label}
        </Badge>
      </span>
      {children}
    </div>
  )
}

const runIcons: Record<RunLook['state'], LucideIcon> = {
  error: CircleAlert,
  'not-used': CircleMinus,
  succeeded: CircleCheck,
  'not-succeeded': TriangleAlert,
}

function RunState({ run }: { run: NibfinRun }) {
  const look = runLook(run)
  const change = run.commit ? <ChangeLine run={run} /> : null
  return (
    <StateLine tone={look.tone} icon={runIcons[look.state]} label={look.label} state={look.state}>
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
    </StateLine>
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
      <span className="flex flex-wrap items-center gap-x-1 text-muted-foreground" data-slot="change">
        <GitPullRequest aria-hidden="true" className="size-3.5" />
        <span className="min-w-0 break-words">
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
