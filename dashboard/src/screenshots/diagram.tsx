// Screenshot harness of the shared site diagram (#456, #457). It is built only
// with vite build --mode screenshots into dashboard/.screenshots-dist/ and is
// not a view of the product or of the site.

import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import { Diagram } from '@/app/components/diagram'
import { ThemeControl } from '@/components/theme-control'
import { applyCspNonce } from '@/lib/nonce'
import { applyTheme } from '@/lib/theme'
import '@/styles/theme.css'

import { framesExample, lanesExample, pipelineExample } from './diagram-fixtures'

applyCspNonce()
applyTheme()

function Harness() {
  return (
    <div className="flex min-h-svh flex-col">
      <header className="border-b border-border-subtle bg-card">
        <div className="mx-auto flex w-full max-w-5xl flex-wrap items-center justify-between gap-3 px-4 py-3 sm:px-6">
          <h1 className="text-lg font-semibold tracking-tight">Diagram</h1>
          <ThemeControl />
        </div>
      </header>
      <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-10 px-4 py-6 sm:px-6">
        <p className="text-sm text-muted-foreground">Fictional data for the screenshot review.</p>
        <Diagram
          spec={framesExample}
          title="From idea to shelf (groups as frames)"
          description="A fictional example: planning at the desk, then making in the workshop."
        />
        <Diagram spec={lanesExample} title="From idea to shelf (groups as lanes)" />
        <Diagram
          flow
          spec={pipelineExample}
          title="From board to shelf (kinds, icons, rows and flow)"
          description="A fictional example: who works at each step, wrapped into rows."
        />
      </main>
    </div>
  )
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Harness />
  </StrictMode>,
)
