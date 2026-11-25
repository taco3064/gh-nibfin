import { useState } from 'react'
import { FolderGit2, LoaderCircle, RefreshCw, Server, UserRound, type LucideIcon } from 'lucide-react'

import { PersonView } from '@/components/person/person-view'
import { ConnectionCard } from '@/components/shell/connection-card'
import { FreshnessIndicator } from '@/components/shell/freshness-indicator'
import { LiveBadge, LiveNotes } from '@/components/shell/live-status'
import { Note } from '@/components/shell/note'
import { RepoView } from '@/components/shell/repo-view'
import { RunnerView } from '@/components/shell/runner-view'
import { ThemeControl } from '@/components/theme-control'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { landingView, viewLabels, views, type View } from '@/lib/shell'
import type { PersonView as PersonData } from '@/lib/status'
import { useStatus, type Status } from '@/lib/use-status'

const viewIcons: Record<View, LucideIcon> = { person: UserRound, repo: FolderGit2, runner: Server }

export function App() {
  const status = useStatus()
  // picked is the view the person chose; landing is set once, from the
  // first successful read. Neither is stored, and a refresh changes neither.
  const [picked, setPicked] = useState<View>()
  const [landing, setLanding] = useState<View>()
  if (status.data && landing === undefined) setLanding(landingView(status.data))
  const view = picked ?? landing ?? 'person'
  const connected = status.connection.state === 'connected'

  return (
    <Tabs value={view} onValueChange={(v) => setPicked(v as View)} className="flex min-h-svh flex-col gap-0">
      <header className="border-b border-border-subtle bg-card">
        <div className="mx-auto flex w-full max-w-5xl flex-wrap items-center justify-between gap-x-4 gap-y-3 px-4 py-3 sm:px-6">
          <h1 className="text-lg font-semibold tracking-tight">gh nibfin dashboard</h1>
          <ThemeControl />
          {connected && (
            <div className="flex w-full flex-wrap items-center justify-between gap-x-4 gap-y-3">
              <TabsList aria-label="Views" className="w-full sm:w-fit">
                {views.map((v) => {
                  const Icon = viewIcons[v]
                  return (
                    <TabsTrigger key={v} value={v} className="px-3">
                      <Icon aria-hidden="true" />
                      {viewLabels[v]}
                    </TabsTrigger>
                  )
                })}
              </TabsList>
              <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
                <LiveBadge live={status.live} />
                <FreshnessIndicator readAt={status.readAt} />
                <Button variant="outline" size="sm" onClick={status.refresh} disabled={status.reading} data-action="refresh">
                  {status.reading ? (
                    <LoaderCircle aria-hidden="true" className="motion-safe:animate-spin" />
                  ) : (
                    <RefreshCw aria-hidden="true" />
                  )}
                  {status.reading ? 'Refreshing…' : 'Refresh'}
                </Button>
              </div>
            </div>
          )}
        </div>
      </header>
      <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-4 px-4 py-6 sm:px-6 sm:py-8">
        {connected ? <Shell status={status} /> : <ConnectionCard connection={status.connection} />}
      </main>
    </Tabs>
  )
}

function Shell({ status }: { status: Status }) {
  const { data, readError } = status
  return (
    <>
      <LiveNotes
        live={status.live}
        podman={status.podman}
        checkError={status.checkError}
        liveReadError={status.liveReadError}
        data={data}
      />
      {readError && (
        <Note tone="danger" data-note="read-error">
          {data ? 'Refresh failed' : 'Status could not be read'}: {readError.code}: {readError.message}
        </Note>
      )}
      {views.map((v) => (
        <TabsContent key={v} value={v} className="flex flex-col gap-4" data-content={v}>
          {data ? <ViewContent view={v} data={data} onChanged={status.onChanged} /> : <NoData reading={status.reading} />}
        </TabsContent>
      ))}
    </>
  )
}

function ViewContent({ view, data, onChanged }: { view: View; data: PersonData; onChanged: Status['onChanged'] }) {
  switch (view) {
    case 'person':
      return <PersonView data={data} onChanged={onChanged} />
    case 'repo':
      return <RepoView />
    case 'runner':
      return <RunnerView data={data} />
  }
}

function NoData({ reading }: { reading: boolean }) {
  if (!reading) return <p className="text-sm text-muted-foreground">No status data. Use Refresh to read it again.</p>
  return (
    <p role="status" data-state="reading" className="flex items-center gap-2 text-sm text-muted-foreground">
      <LoaderCircle aria-hidden="true" className="size-4 motion-safe:animate-spin" />
      Reading status…
    </p>
  )
}
