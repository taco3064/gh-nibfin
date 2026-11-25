// Pure logic of the person view: the image state of a repository and the
// updates of its Issues.

import { applyChanges, type Change } from '@/lib/issue'
import type { ApiError, LatestImage, PersonView, RepoImage, StatusIssue } from '@/lib/status'

export type ImageTone = 'neutral' | 'success' | 'warning' | 'danger'

export type ImageKind = 'error' | 'none' | 'peer' | 'noPin' | 'latest' | 'notLatest' | 'unknown' | 'otherSet'

export interface ImageState {
  kind: ImageKind
  tone: ImageTone
  label: string
  detail?: string
}

const text = (e: ApiError) => `${e.code}: ${e.message}`

// imageState describes whether a repository's workflows use the latest
// image. Every state is text; nothing is diagnosed beyond the data.
export function imageState(image: RepoImage, latest: LatestImage): ImageState {
  if (image.error) return { kind: 'error', tone: 'danger', label: 'Image could not be read', detail: text(image.error) }
  switch (image.set) {
    case 'none':
      return { kind: 'none', tone: 'neutral', label: 'No Nibfin workflows' }
    case 'peer':
      return { kind: 'peer', tone: 'neutral', label: 'Peer set: no image pin' }
    case 'execution':
      if (image.ref === null) return { kind: 'noPin', tone: 'warning', label: 'No valid image pin' }
      if (image.latest === true) return { kind: 'latest', tone: 'success', label: 'Latest image' }
      if (image.latest === false) return { kind: 'notLatest', tone: 'warning', label: 'Not the latest image' }
      return {
        kind: 'unknown',
        tone: 'neutral',
        label: 'Latest image unknown',
        ...(latest.error ? { detail: text(latest.error) } : {}),
      }
    default:
      return { kind: 'otherSet', tone: 'neutral', label: `Unknown workflow set ${image.set ?? ''}`.trim() }
  }
}

// applyPersonChanges returns view with the confirmed changes applied to the
// Issues of every repository.
export function applyPersonChanges(view: PersonView, changes: readonly Change[]): PersonView {
  if (changes.length === 0 || !view.repos) return view
  return { ...view, repos: view.repos.map((r) => (r.issues ? { ...r, issues: applyChanges(r.issues, changes) } : r)) }
}

// keepSelection drops the selected Issue URLs that view no longer lists.
export function keepSelection(selected: ReadonlySet<string>, view: PersonView): Set<string> {
  const listed = new Set((view.repos ?? []).flatMap((r) => (r.issues ?? []).map((i) => i.url)))
  return new Set([...selected].filter((u) => listed.has(u)))
}

// replaceIssue returns view with repo's Issue of the same URL replaced by
// issue; a view that does not list it is returned unchanged.
export function replaceIssue(view: PersonView, repo: string, issue: StatusIssue): PersonView {
  let found = false
  const repos = (view.repos ?? []).map((r) => {
    if (r.repo.toLowerCase() !== repo.toLowerCase() || !r.issues?.some((i) => i.url === issue.url)) return r
    found = true
    return { ...r, issues: r.issues.map((i) => (i.url === issue.url ? issue : i)) }
  })
  return found ? { ...view, repos } : view
}
