import { useEffect, useState, type ReactNode } from 'react'
import { CircleAlert, CircleCheck, CircleMinus, ExternalLink, GitPullRequest, Rocket, TriangleAlert, Wrench, type LucideIcon } from 'lucide-react'

import { ExternalMark } from '@/components/issue/issue-item'
import { tones, type Tone } from '@/components/issue/tones'
import { OperationDialog } from '@/components/operation'
import { imageIcons } from '@/components/person/image-icons'
import { Note } from '@/components/shell/note'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { commandByApi, loadCommands } from '@/lib/commands'
import type { Command } from '@/lib/operation'
import { imageState } from '@/lib/person'
import { offeredActions } from '@/lib/repo'
import type { Outcome } from '@/lib/session'
import { ownerRepo, repoHost, type NibfinRun, type RepoView } from '@/lib/status'
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
  data: RepoView
  // onFinished reads the repository again after an init or upgrade.
  onFinished: () => void
}

// RepoSummary heads the repository view: the repository's link on its own
// host, the image its workflows pin, whether a Nibfin run succeeded after
// init, and init or upgrade through the operation dialog.
export function RepoSummary({ data, onFinished }: RepoSummaryProps) {
  const image = imageState(data.image, data.latestImage)
  const ImageIcon = imageIcons[image.kind]
  const name = ownerRepo(data.repo)
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
        <Actions data={data} onFinished={onFinished} />
      </CardHeader>
      <CardContent className="px-4 sm:px-5">
        <dl className="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-[auto_1fr]">
          <dt className="text-muted-foreground">Image</dt>
          <dd className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1" data-image={image.kind}>
            <Badge variant="outline" className={tones[image.tone]}>
              <ImageIcon aria-hidden="true" />
              {image.label}
            </Badge>
            {data.image.ref && <code className="min-w-0 font-mono text-xs break-all">{data.image.ref}</code>}
            {image.detail && <span className="min-w-0 break-words text-muted-foreground">{image.detail}</span>}
          </dd>
          <dt className="text-muted-foreground">Nibfin run</dt>
          <dd className="min-w-0" data-slot="nibfin-run">
            <RunState run={data.nibfinRun} />
          </dd>
        </dl>
      </CardContent>
    </Card>
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

function RunState({ run }: { run: NibfinRun }) {
  const init = run.initialized ? <InitLine run={run} /> : null
  if (run.error) {
    return (
      <StateLine tone="danger" icon={CircleAlert} label="Could not be read" state="error">
        {init}
        <span className="break-words text-danger">
          {run.error.code}: {run.error.message}
        </span>
      </StateLine>
    )
  }
  if (!run.initialized) {
    return (
      <StateLine tone="neutral" icon={CircleMinus} label="Not initialized" state="not-initialized">
        <span className="text-muted-foreground">
          No generated <code className="font-mono text-xs">nibfin-issue.yml</code> on the default branch
        </span>
      </StateLine>
    )
  }
  if (run.succeeded) {
    return (
      <StateLine tone="success" icon={CircleCheck} label="A Nibfin run succeeded after init" state="succeeded">
        {init}
        {run.run && (
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
  return (
    <StateLine tone="warning" icon={TriangleAlert} label="No successful Nibfin run since init" state="not-succeeded">
      {init}
    </StateLine>
  )
}

function InitLine({ run }: { run: NibfinRun }) {
  const at = run.mergedAt ? (
    <>
      {' '}
      <time dateTime={run.mergedAt}>{formatTime(run.mergedAt)}</time>
    </>
  ) : null
  if (run.initPR) {
    return (
      <span className="flex flex-wrap items-center gap-x-1 text-muted-foreground" data-slot="init">
        <GitPullRequest aria-hidden="true" className="size-3.5" />
        <span>
          Init PR{' '}
          <a href={run.initPR.url} target="_blank" rel="noreferrer" className={linkClass}>
            #{run.initPR.number}
            <ExternalMark label="(opens on GitHub in a new tab)" />
          </a>{' '}
          merged{at}
        </span>
      </span>
    )
  }
  return (
    <span className="text-muted-foreground" data-slot="init">
      Init commit <code className="font-mono text-xs">{(run.initCommit ?? '').slice(0, 7)}</code>
      {at}
    </span>
  )
}

const actionLook = {
  init: { label: 'Init', icon: Rocket },
  upgrade: { label: 'Upgrade', icon: Wrench },
}

function Actions({ data, onFinished }: RepoSummaryProps) {
  const commands = useCommands()
  const [open, setOpen] = useState<'init' | 'upgrade'>()
  const offered = offeredActions(data.image)
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
          initial={{ repo: data.repo }}
        />
      )}
    </div>
  )
}
