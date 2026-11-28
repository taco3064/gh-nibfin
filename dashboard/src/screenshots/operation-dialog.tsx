// Screenshot harness of the operation dialog (#351). It is built only with
// vite build --mode screenshots into dashboard/.screenshots-dist/ and is not
// a view of the product. The spec mocks every API route.

import { StrictMode, useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'

import { OperationDialog } from '@/components/operation'
import { ThemeControl } from '@/components/theme-control'
import { Button } from '@/components/ui/button'
import { loadCommands } from '@/lib/commands'
import { applyCspNonce } from '@/lib/nonce'
import type { Command } from '@/lib/operation'
import { openSession, readProof } from '@/lib/session'
import { applyTheme } from '@/lib/theme'
import '@/styles/theme.css'

applyCspNonce()
applyTheme()

const proof = readProof(location.hash, history)

// The spec paces the apply and planning streams: window.__holdApply(n) (or
// __holdPlan(n)) lets only the first n lines through until
// window.__releaseApply() (or __releasePlan()) is called. Route mocks answer
// a whole body at once, so this is the only way to show a stream mid-way.
// A later hold lets the stream on to line n. An aborted request errors its
// paced stream as a real aborted fetch does; window.__aborts counts them.
interface Gate {
  allowed: number
  waiters: (() => void)[]
}
const gates: Record<'apply' | 'plan', Gate> = {
  apply: { allowed: Number.POSITIVE_INFINITY, waiters: [] },
  plan: { allowed: Number.POSITIVE_INFINITY, waiters: [] },
}
const w = window as unknown as {
  __holdApply: (n: number) => void
  __releaseApply: () => void
  __holdPlan: (n: number) => void
  __releasePlan: () => void
  __aborts: number
}
const wakeAll = () => {
  for (const g of Object.values(gates)) for (const f of g.waiters.splice(0)) f()
}
const hold = (g: Gate, n: number) => {
  g.allowed = n
  wakeAll()
}
w.__aborts = 0
w.__holdApply = (n) => hold(gates.apply, n)
w.__releaseApply = () => hold(gates.apply, Number.POSITIVE_INFINITY)
w.__holdPlan = (n) => hold(gates.plan, n)
w.__releasePlan = () => hold(gates.plan, Number.POSITIVE_INFINITY)

const paced: typeof fetch = async (input, init) => {
  const res = await fetch(input, init)
  const url = String(input)
  const gate = url.endsWith('/apply') ? gates.apply : url.endsWith('/plan/stream') ? gates.plan : null
  if (!gate || !res.ok || !res.body) return res
  const lines = (await res.text()).split(/(?<=\n)/)
  const enc = new TextEncoder()
  const signal = init?.signal
  let i = 0
  let aborted = false
  const body = new ReadableStream<Uint8Array>({
    start(c) {
      signal?.addEventListener('abort', () => {
        aborted = true
        w.__aborts++
        c.error(signal.reason)
        wakeAll()
      })
    },
    async pull(c) {
      while (i >= gate.allowed && !aborted) await new Promise<void>((r) => gate.waiters.push(r))
      if (aborted) return
      if (i < lines.length) c.enqueue(enc.encode(lines[i++]))
      else c.close()
    },
  })
  return new Response(body, { status: res.status, headers: res.headers })
}

function Harness() {
  const [ready, setReady] = useState<'opening' | 'open' | 'failed'>(proof ? 'opening' : 'failed')
  const [commands, setCommands] = useState<Command[]>([])
  const [open, setOpen] = useState<Command | null>(null)
  const [finished, setFinished] = useState(0)

  useEffect(() => {
    if (!proof) return
    void openSession(proof).then(async (r) => {
      if (!r.ok) return setReady('failed')
      const c = await loadCommands()
      if (!c.ok) return setReady('failed')
      setCommands(c.value)
      setReady('open')
    })
  }, [])

  return (
    <div className="flex min-h-svh flex-col">
      <header className="border-b border-border-subtle bg-card">
        <div className="mx-auto flex w-full max-w-5xl flex-wrap items-center justify-between gap-3 px-4 py-3 sm:px-6">
          <h1 className="text-lg font-semibold tracking-tight">Operation dialog</h1>
          <ThemeControl />
        </div>
      </header>
      <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-4 px-4 py-6 sm:px-6" data-session={ready}>
        <p className="text-sm text-muted-foreground" data-finished={finished}>
          Fictional data for the screenshot review.
        </p>
        <div className="flex flex-wrap gap-2">
          {commands
            .filter((c) => c.api)
            .map((c) => (
              <Button key={c.path} variant="outline" onClick={() => setOpen(c)}>
                {c.path}
              </Button>
            ))}
        </div>
        {open && <OperationDialog command={open} open onOpenChange={(o) => !o && setOpen(null)} onFinished={() => setFinished((n) => n + 1)} fetcher={paced} />}
      </main>
    </div>
  )
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Harness />
  </StrictMode>,
)
