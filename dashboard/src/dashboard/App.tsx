import { useState } from 'react'
import { FolderGit2, LoaderCircle, RefreshCw, Server, UserRound, type LucideIcon } from 'lucide-react'

import wordmark from '@/assets/nibfin-logo.png?no-inline'
import { PersonView } from '@/components/person/person-view'
import { RepoView } from '@/components/repo/repo-view'
import { ConnectionCard } from '@/components/shell/connection-card'
import { FreshnessIndicator } from '@/components/shell/freshness-indicator'
import { LiveBadge, LiveNotes, RefreshOnlyBadge } from '@/components/shell/live-status'
import { Note } from '@/components/shell/note'
import { RunnerView } from '@/components/runner/runner-view'
import { ThemeControl } from '@/components/theme-control'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { LoadingContext } from '@/lib/loading-context'
import type { Loading } from '@/lib/partial'
import { pickerSource } from '@/lib/repo'
import { landingView, viewLabels, views, type View } from '@/lib/shell'
import type { PersonView as PersonData } from '@/lib/status'
import { useRepoStatus, type RepoStatus } from '@/lib/use-repo-status'
import { useStatus, type Status } from '@/lib/use-status'

const viewIcons: Record<View, LucideIcon> = { person: UserRound, repo: FolderGit2, runner: Server }

export function App() {
  const status = useStatus()
  // The repository read lives here so the chosen repository survives tab
  // switches; nothing is stored.
  const repoStatus = useRepoStatus()
  // picked is the view the person chose; landing is set once, from the
  // first successful read. Neither is stored, and a refresh changes neither.
  const [picked, setPicked] = useState<View>()
  const [landing, setLanding] = useState<View>()
  // The landing view needs the host, which a partial read may still lack.
  if (status.data && !status.loading.has('host') && landing === undefined) setLanding(landingView(status.data))
  const view = picked ?? landing ?? 'person'
  const connected = status.connection.state === 'connected'
  // On the repository view with a repository open, the header's freshness
  // and Refresh follow the repository read; otherwise the person read.
  const active = view === 'repo' && repoStatus.repo !== undefined ? repoStatus : status

  return (
    <Tabs value={view} onValueChange={(v) => setPicked(v as View)} className="flex min-h-svh flex-col gap-0">
      <header className="border-b border-border-subtle bg-card">
        <div className="mx-auto flex w-full max-w-5xl flex-wrap items-center justify-between gap-x-4 gap-y-3 px-4 py-3 sm:px-6">
          <h1 className="flex items-center">
            {/* Wide screens show the wordmark, narrow ones the "b" mark. Both are
                same-origin files: ?no-inline keeps the wordmark out of a data:
                URL, which the Dashboard CSP (img-src 'self') would block. The
                artwork is drawn for a dark background; nf-brand-image gives it
                dark letters in the light theme only (theme.css). */}
            <img src={wordmark} alt="Nibfin" width={287} height={96} className="nf-brand-image hidden h-8 w-auto sm:block" />
            <img
              src="/apple-touch-icon.png"
              alt="Nibfin"
              width={180}
              height={180}
              className="nf-brand-image h-8 w-auto sm:hidden"
            />
            <span className="sr-only"> dashboard</span>
          </h1>
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
                {active === status ? <LiveBadge live={status.live} /> : <RefreshOnlyBadge />}
                <FreshnessIndicator readAt={active.readAt} />
                <Button variant="outline" size="sm" onClick={active.refresh} disabled={active.reading} data-action="refresh">
                  {active.reading ? (
                    <LoaderCircle aria-hidden="true" className="motion-safe:animate-spin" />
                  ) : (
                    <RefreshCw aria-hidden="true" />
                  )}
                  {active.reading ? 'Refreshing…' : 'Refresh'}
                </Button>
              </div>
            </div>
          )}
        </div>
      </header>
      <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-4 px-4 py-6 sm:px-6 sm:py-8">
        {connected ? <Shell status={status} repoStatus={repoStatus} onShowRunners={() => setPicked('runner')} /> : <ConnectionCard connection={status.connection} />}
      </main>
    </Tabs>
  )
}

function Shell({ status, repoStatus, onShowRunners }: { status: Status; repoStatus: RepoStatus; onShowRunners: () => void }) {
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
          {v === 'repo' ? (
            <RepoView
              status={repoStatus}
              source={pickerSource(repoStatus.data, data, repoStatus.loading, status.loading)}
              listReading={status.reading || repoStatus.reading}
            />
          ) : data ? (
            <LoadingContext value={status.loading}>
              <ViewContent
                view={v}
                data={data}
                loading={status.loading}
                onChanged={status.onChanged}
                refresh={status.refresh}
                onShowRunners={onShowRunners}
              />
            </LoadingContext>
          ) : (
            <NoData reading={status.reading} />
          )}
        </TabsContent>
      ))}
    </>
  )
}

function ViewContent({
  view,
  data,
  loading,
  onChanged,
  refresh,
  onShowRunners,
}: {
  view: Exclude<View, 'repo'>
  data: PersonData
  loading: Loading
  onChanged: Status['onChanged']
  refresh: Status['refresh']
  onShowRunners: () => void
}) {
  switch (view) {
    case 'person':
      return <PersonView data={data} onChanged={onChanged} onShowRunners={onShowRunners} />
    case 'runner':
      // The Runners view is the host's; it waits for the host read.
      if (loading.has('host')) return <NoData reading label="Reading the host…" />
      return <RunnerView data={data} onFinished={refresh} />
  }
}

function NoData({ reading, label = 'Reading status…' }: { reading: boolean; label?: string }) {
  if (!reading) return <p className="text-sm text-muted-foreground">No status data. Use Refresh to read it again.</p>
  return (
    <p role="status" data-state="reading" className="flex items-center gap-2 text-sm text-muted-foreground">
      <LoaderCircle aria-hidden="true" className="size-4 motion-safe:animate-spin" />
      {label}
    </p>
  )
}
