// Pure logic of the repository view: which read supplies the picker list,
// and the sequencing that drops answers for a repository no longer chosen.

import { repoHost, type ApiError, type NibfinRepo, type PersonView, type RepoView } from '@/lib/status'

// PickerSource is the list of the account's Nibfin repositories and the
// host it was read on. Both always come from the same read.
export interface PickerSource {
  host: string
  repos: NibfinRepo[] | null
  error?: ApiError
}

// pickerSource takes the list of the repository read when it has data, else
// the person read's, else none. It never mixes a list with another read's
// host, so a listed entry opens on the host it was listed on.
export function pickerSource(repoData: RepoView | undefined, personData: PersonView | undefined): PickerSource | undefined {
  if (repoData) {
    return {
      host: repoHost(repoData.repo),
      repos: repoData.nibfinRepos,
      ...(repoData.nibfinReposError ? { error: repoData.nibfinReposError } : {}),
    }
  }
  if (personData) {
    return {
      host: personData.githubHost,
      repos: personData.nibfinRepos,
      ...(personData.nibfinReposError ? { error: personData.nibfinReposError } : {}),
    }
  }
  return undefined
}

// Sequence numbers requests; only the answer of the latest one is current.
export class Sequence {
  private last = 0
  next(): number {
    return ++this.last
  }
  current(n: number): boolean {
    return n === this.last
  }
  // drop makes every sent request stale, for example on unmount.
  drop(): void {
    this.last++
  }
}

// filterRepos keeps the entries whose OWNER/REPO contains query, any case.
export function filterRepos(repos: readonly NibfinRepo[], query: string): NibfinRepo[] {
  const q = query.trim().toLowerCase()
  return q === '' ? [...repos] : repos.filter((r) => r.repo.toLowerCase().includes(q))
}

// offeredActions names the commands the summary offers: init without Nibfin
// workflows, upgrade with them, both when the image could not be read. The
// server's plan decides what the command does.
export function offeredActions(image: RepoView['image']): ('init' | 'upgrade')[] {
  if (image.error) return ['init', 'upgrade']
  return image.set === 'none' ? ['init'] : ['upgrade']
}
