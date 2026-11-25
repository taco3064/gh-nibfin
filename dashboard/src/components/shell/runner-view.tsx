import type { ReactNode } from 'react'
import { Server } from 'lucide-react'

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { hasNoRunner } from '@/lib/shell'
import type { PersonView } from '@/lib/status'

import { Note } from './note'

const code = 'rounded-sm bg-[var(--nf-code-bg)] px-1.5 py-0.5 font-mono text-[0.85em] break-words text-[var(--nf-code)]'

// RunnerView guides runner add then init when this host has no Runner yet.
// Its other content is not built yet.
export function RunnerView({ data }: { data: PersonView }) {
  if (!hasNoRunner(data.host)) {
    return (
      <Note tone="info" data-note="not-built">
        The runner view is not built yet.
      </Note>
    )
  }
  const error = data.host.runners.error
  return (
    <Card className="border-border-subtle" data-guide="no-runner">
      <CardHeader>
        <CardTitle>
          <h2 className="flex items-center gap-2 text-base">
            <Server aria-hidden="true" className="size-5 shrink-0" />
            This host has no Runner yet
          </h2>
        </CardTitle>
        <CardDescription>Nibfin runs its work on Runners on this host. Set them up in two steps.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {error && (
          <Note tone="warning">
            {error.code}: {error.message}
          </Note>
        )}
        <ol className="flex flex-col gap-4">
          <Step n={1}>
            <p>
              Run <code className={code}>gh nibfin runner add &lt;claude|codex&gt;</code> in the repository the Runners
              should serve.
            </p>
            <p className="text-sm text-muted-foreground">It also completes Runners an interrupted add left.</p>
          </Step>
          <Step n={2}>
            <p>
              Run <code className={code}>gh nibfin init</code> in each repository that should use Nibfin.
            </p>
          </Step>
        </ol>
      </CardContent>
    </Card>
  )
}

function Step({ n, children }: { n: number; children: ReactNode }) {
  return (
    <li className="flex items-start gap-3">
      <span
        aria-hidden="true"
        className="flex size-7 shrink-0 items-center justify-center rounded-full border border-border bg-muted text-sm font-semibold"
      >
        {n}
      </span>
      <div className="flex min-w-0 flex-col gap-1 pt-0.5">
        {children}
      </div>
    </li>
  )
}
