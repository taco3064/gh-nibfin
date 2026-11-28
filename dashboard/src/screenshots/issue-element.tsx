// Screenshot harness of the shared Issue element. It is built only with
// vite build --mode screenshots into dashboard/.screenshots-dist/ and is not
// a view of the product.

import { StrictMode, useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'

import { ThemeControl } from '@/components/theme-control'
import { IssueBulkActions, IssueItem, RunLegend } from '@/components/issue'
import { applyChanges, type Change } from '@/lib/issue'
import { applyCspNonce } from '@/lib/nonce'
import { openSession, readProof } from '@/lib/session'
import { applyTheme } from '@/lib/theme'
import '@/styles/theme.css'

import { fixtures, permissions } from './fixtures'

applyCspNonce()
applyTheme()

const proof = readProof(location.hash, history)

function Harness() {
  const [ready, setReady] = useState<'opening' | 'open' | 'failed'>(proof ? 'opening' : 'failed')
  const [items, setItems] = useState(fixtures)
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const [rereads, setRereads] = useState(0)

  useEffect(() => {
    if (!proof) return
    void openSession(proof).then((r) => setReady(r.ok ? 'open' : 'failed'))
  }, [])

  const onChanged = (changes: Change[]) => {
    if (changes.length === 0) setRereads((n) => n + 1)
    setItems((prev) => {
      const next = applyChanges(
        prev.map((f) => f.issue),
        changes,
      )
      return prev.map((f, i) => ({ ...f, issue: next[i] }))
    })
  }
  const chosen = items.filter((f) => selected.has(f.issue.url)).map((f) => ({ ...f, access: permissions[f.repo] }))

  return (
    <div className="flex min-h-svh flex-col">
      <header className="border-b border-border-subtle bg-card">
        <div className="mx-auto flex w-full max-w-5xl flex-wrap items-center justify-between gap-3 px-4 py-3 sm:px-6">
          <h1 className="text-lg font-semibold tracking-tight">Issue element</h1>
          <ThemeControl />
        </div>
      </header>
      <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-4 px-4 py-6 sm:px-6" data-session={ready}>
        <p className="text-sm text-muted-foreground" data-rereads={rereads}>
          Fictional data for the screenshot review.
        </p>
        <RunLegend />
        <IssueBulkActions selected={chosen} onChanged={onChanged} />
        {/* The Issues render once the session is open, as in the
            product, whose views read data only with a session. */}
        <ul className="flex flex-col gap-3">
          {ready !== 'opening' && items.map((f) => (
            <li key={f.issue.url}>
              <IssueItem
                issue={f.issue}
                repo={f.repo}
                access={permissions[f.repo]}
                selected={selected.has(f.issue.url)}
                onSelectedChange={(on) =>
                  setSelected((prev) => {
                    const next = new Set(prev)
                    if (on) next.add(f.issue.url)
                    else next.delete(f.issue.url)
                    return next
                  })
                }
                onChanged={onChanged}
              />
            </li>
          ))}
        </ul>
      </main>
    </div>
  )
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Harness />
  </StrictMode>,
)
