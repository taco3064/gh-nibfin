import { ExternalLink } from 'lucide-react'

import { IssueItem } from '@/components/issue'
import { tones } from '@/components/issue/tones'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import type { Change } from '@/lib/issue'
import { imageState } from '@/lib/person'
import type { LatestImage, RepoIssues } from '@/lib/status'

import { imageIcons } from './image-icons'

export interface RepoGroupProps {
  group: RepoIssues
  githubHost: string
  latestImage: LatestImage
  selected: ReadonlySet<string>
  onSelect: (url: string, on: boolean) => void
  onChanged: (changes: Change[]) => void
}

// RepoGroup is one repository of the person view: its link, Issue count and
// image state, then its Issues as the shared Issue element.
export function RepoGroup({ group, githubHost, latestImage, selected, onSelect, onChanged }: RepoGroupProps) {
  const image = imageState(group.image, latestImage)
  const ImageIcon = imageIcons[image.kind]
  const issues = group.issues ?? []
  const access = { permission: group.permission, ...(group.permissionError ? { permissionError: group.permissionError } : {}) }
  return (
    <Card className="gap-4 border-border-subtle py-4" data-repo={group.repo}>
      <CardHeader className="flex flex-col gap-2 px-4 sm:px-5">
        <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
          <h3 className="min-w-0 text-base font-semibold break-all">
            <a
              href={`https://${githubHost}/${group.repo}`}
              target="_blank"
              rel="noreferrer"
              className="rounded-sm text-link underline-offset-4 hover:underline"
            >
              {group.repo}
              <ExternalLink aria-hidden="true" className="ms-1 inline size-3.5 align-[-0.125em]" />
              <span className="sr-only"> (opens GitHub)</span>
            </a>
          </h3>
          <span className="text-sm text-muted-foreground" data-slot="count">
            {issues.length === 1 ? '1 Issue' : `${issues.length} Issues`}
          </span>
        </div>
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm" data-image={image.kind}>
          <Badge variant="outline" className={tones[image.tone]}>
            <ImageIcon aria-hidden="true" />
            {image.label}
          </Badge>
          {image.detail && <span className="min-w-0 break-words text-muted-foreground">{image.detail}</span>}
        </div>
      </CardHeader>
      <CardContent className="px-4 sm:px-5">
        {issues.length === 0 ? (
          <p className="text-sm text-muted-foreground">No Issues listed</p>
        ) : (
          <ul className="flex flex-col gap-3">
            {issues.map((issue) => (
              <li key={issue.url}>
                <IssueItem
                  issue={issue}
                  repo={group.repo}
                  access={access}
                  selected={selected.has(issue.url)}
                  onSelectedChange={(on) => onSelect(issue.url, on)}
                  onChanged={onChanged}
                />
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}
