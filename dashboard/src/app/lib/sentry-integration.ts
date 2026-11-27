// The Astro integration that adds Sentry to the public website (#481) only when
// the build sets WEBSITE_SENTRY_DSN. With an empty DSN it adds nothing, so the
// build has no Sentry code at all and no source maps, exactly as before.
import type { AstroIntegration } from 'astro'

import type { SentryConfig } from './sentry'

export interface SentryIntegrationInput {
  dsn: string | undefined
  release: string | undefined
  siteUrl: string
}

export function sentryIntegration({ dsn, release, siteUrl }: SentryIntegrationInput): AstroIntegration[] {
  const trimmed = dsn?.trim() ?? ''
  if (trimmed === '') return []
  if (!URL.canParse(trimmed) || new URL(trimmed).protocol !== 'https:') {
    throw new Error('WEBSITE_SENTRY_DSN must be an https: URL.')
  }
  if (!release || !/^(?:[0-9a-f]{40}|[0-9a-f]{64})$/.test(release)) {
    throw new Error('WEBSITE_SENTRY_DSN needs GITHUB_SHA set to the full commit SHA of the build.')
  }
  const config: SentryConfig = { dsn: trimmed, release, siteUrl: new URL(siteUrl).href }
  return [
    {
      name: 'nibfin-sentry',
      hooks: {
        'astro:config:setup': ({ injectScript, updateConfig }) => {
          injectScript('page', `import { initSentry } from '@/app/lib/sentry'\ninitSentry(${JSON.stringify(config)})`)
          // Hidden maps: written for the upload, never referenced by the bundle.
          // website.yml deletes them before publishing.
          updateConfig({ vite: { build: { sourcemap: 'hidden' } } })
        },
      },
    },
  ]
}
