import { useId, useState, type FormEvent } from 'react'
import { FolderGit2, FolderOpen, LoaderCircle, Search, Server } from 'lucide-react'

import { tones } from '@/components/issue/tones'
import { Note } from '@/components/shell/note'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { filterRepos, type PickerSource } from '@/lib/repo'
import { cn } from '@/lib/utils'

export interface RepoPickerProps {
  // source is the list and the host it was read on; undefined until a read
  // that carries it finishes.
  source?: PickerSource
  // listReading is true while the read that will supply source runs.
  listReading: boolean
  // current is the HOST/OWNER/REPO (or the value sent) of the open
  // repository, if any.
  current?: string
  onOpen: (repo: string) => void
}

// RepoPicker chooses the repository of the view: a searchable list of the
// account's repositories that use Nibfin on one host, plus a manual entry
// for any repository. Once one is open it collapses to its name.
export function RepoPicker({ source, listReading, current, onOpen }: RepoPickerProps) {
  const [expanded, setExpanded] = useState(false)
  if (current !== undefined && !expanded) {
    return (
      <div data-slot="picker" data-state="collapsed" className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
        <p className="flex min-w-0 items-center gap-2 text-sm">
          <FolderOpen aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
          <span className="text-muted-foreground">Repository</span>
          <span className="min-w-0 font-medium break-all">{current}</span>
        </p>
        <Button variant="outline" size="sm" onClick={() => setExpanded(true)} data-action="change-repo">
          Change
        </Button>
      </div>
    )
  }
  const pick = (repo: string) => {
    setExpanded(false)
    onOpen(repo)
  }
  return (
    <Card className="gap-4 border-border-subtle py-4" data-slot="picker" data-state="open">
      <CardHeader className="flex flex-wrap items-start justify-between gap-2 px-4 sm:px-5">
        <h2 className="text-base font-semibold break-words">
          {source ? (
            <>
              Repositories using Nibfin on <span data-slot="picker-host">{source.host}</span>
            </>
          ) : (
            'Repositories using Nibfin'
          )}
        </h2>
        {current !== undefined && (
          <Button variant="ghost" size="sm" onClick={() => setExpanded(false)}>
            Cancel
          </Button>
        )}
      </CardHeader>
      <CardContent className="flex flex-col gap-5 px-4 sm:px-5">
        <RepoList source={source} listReading={listReading} current={current} onPick={pick} />
        <ManualEntry onOpen={pick} />
      </CardContent>
    </Card>
  )
}

function RepoList({
  source,
  listReading,
  current,
  onPick,
}: {
  source?: PickerSource
  listReading: boolean
  current?: string
  onPick: (repo: string) => void
}) {
  const id = useId()
  const [query, setQuery] = useState('')
  if (!source) {
    if (listReading) {
      return (
        <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground" data-state="list-reading">
          <LoaderCircle aria-hidden="true" className="size-4 motion-safe:animate-spin" />
          Reading repositories…
        </p>
      )
    }
    return <p className="text-sm text-muted-foreground">The repository list is not read. Open a repository by name below.</p>
  }
  if (source.error) {
    return (
      <Note tone="danger" data-note="repos-error">
        Repository list could not be read: {source.error.code}: {source.error.message}
      </Note>
    )
  }
  const repos = source.repos ?? []
  if (repos.length === 0) {
    return (
      <p className="text-sm text-muted-foreground" data-empty="repos">
        No repositories of this account on {source.host} use Nibfin yet.
      </p>
    )
  }
  const shown = filterRepos(repos, query)
  const open = current?.toLowerCase()
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={`${id}-search`} className="sr-only">
        Search repositories
      </Label>
      <div className="relative">
        <Search aria-hidden="true" className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          id={`${id}-search`}
          type="search"
          placeholder="Search repositories"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="pl-9"
          aria-controls={`${id}-list`}
        />
      </div>
      <p className="text-xs text-muted-foreground" aria-live="polite">
        {shown.length === repos.length ? `${repos.length} repositories` : `${shown.length} of ${repos.length} repositories`}
      </p>
      <ul id={`${id}-list`} className="flex max-h-72 flex-col gap-1 overflow-y-auto rounded-md border border-border-subtle p-1" data-slot="repo-list">
        {shown.length === 0 && <li className="px-3 py-2 text-sm text-muted-foreground">No repository matches “{query.trim()}”.</li>}
        {shown.map((r) => {
          const target = `${source.host}/${r.repo}`
          const pressed = open === target.toLowerCase()
          return (
            <li key={r.repo}>
              <Button
                variant="ghost"
                aria-pressed={pressed}
                data-repo-entry={r.repo}
                onClick={() => onPick(target)}
                className={cn(
                  'h-auto w-full flex-wrap justify-start gap-x-2 gap-y-1 px-3 py-2 text-left whitespace-normal',
                  pressed && 'bg-accent text-accent-foreground',
                )}
              >
                <FolderGit2 aria-hidden="true" className="text-muted-foreground" />
                <span className="min-w-32 flex-1 font-medium break-all">{r.repo}</span>
                <span className="flex flex-wrap items-center gap-1.5 text-xs font-normal">
                  <Badge variant="outline" className={tones.neutral}>
                    {r.visibility.toLowerCase()}
                  </Badge>
                  <span className="text-muted-foreground">{r.permission ? r.permission.toLowerCase() : 'no permission reported'}</span>
                  {r.execution === true && (
                    <Badge variant="outline" className={tones.info}>
                      <Server aria-hidden="true" />
                      Runs here
                    </Badge>
                  )}
                </span>
              </Button>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

function ManualEntry({ onOpen }: { onOpen: (repo: string) => void }) {
  const id = useId()
  const [value, setValue] = useState('')
  const [error, setError] = useState<string>()
  const submit = (e: FormEvent) => {
    e.preventDefault()
    const v = value.trim()
    if (v === '') {
      setError('Enter a repository.')
      return
    }
    setError(undefined)
    onOpen(v)
  }
  return (
    <form onSubmit={submit} className="flex flex-col gap-2" data-slot="manual-entry" noValidate>
      <Label htmlFor={`${id}-repo`}>Open another repository</Label>
      <p id={`${id}-help`} className="text-sm text-muted-foreground">
        OWNER/REPO, or HOST/OWNER/REPO for another host. Any repository, also one that does not use Nibfin yet; the view then offers init.
      </p>
      <div className="flex flex-wrap gap-2">
        <Input
          id={`${id}-repo`}
          placeholder="OWNER/REPO"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? `${id}-help ${id}-error` : `${id}-help`}
          autoComplete="off"
          spellCheck={false}
          className="min-w-0 flex-1 basis-56"
        />
        <Button type="submit" data-action="open-repo">
          Open
        </Button>
      </div>
      {error && (
        <p id={`${id}-error`} className="text-sm text-danger">
          {error}
        </p>
      )}
    </form>
  )
}
