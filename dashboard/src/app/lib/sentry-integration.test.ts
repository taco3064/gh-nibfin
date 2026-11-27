import { describe, expect, it, vi } from 'vitest'

import { sentryIntegration } from './sentry-integration'

const sha = '0123456789abcdef0123456789abcdef01234567'
const siteUrl = 'https://taco3064.github.io/gh-nibfin/'
const dsn = 'https://public@o1.ingest.sentry.io/2'

function setup(integration: ReturnType<typeof sentryIntegration>[number]) {
  const injectScript = vi.fn()
  const updateConfig = vi.fn()
  const hook = integration.hooks['astro:config:setup'] as (options: unknown) => void
  hook({ injectScript, updateConfig })
  return { injectScript, updateConfig }
}

describe('sentryIntegration', () => {
  it.each([undefined, '', '   '])('adds nothing for the DSN %j', (empty) => {
    expect(sentryIntegration({ dsn: empty, release: sha, siteUrl })).toEqual([])
    expect(sentryIntegration({ dsn: empty, release: undefined, siteUrl })).toEqual([])
  })

  it('injects the init script on every page and writes hidden source maps for a set DSN', () => {
    const integrations = sentryIntegration({ dsn: ` ${dsn} `, release: sha, siteUrl })
    expect(integrations).toHaveLength(1)
    const { injectScript, updateConfig } = setup(integrations[0])

    expect(injectScript).toHaveBeenCalledOnce()
    const [stage, snippet] = injectScript.mock.calls[0]
    expect(stage).toBe('page')
    expect(snippet).toContain("import { initSentry } from '@/app/lib/sentry'")
    expect(snippet).toContain(`initSentry(${JSON.stringify({ dsn, release: sha, siteUrl })})`)
    expect(updateConfig).toHaveBeenCalledWith({ vite: { build: { sourcemap: 'hidden' } } })
  })

  it.each(['not a url', 'http://public@o1.ingest.sentry.io/2'])('rejects the DSN %j', (bad) => {
    expect(() => sentryIntegration({ dsn: bad, release: sha, siteUrl })).toThrow(/WEBSITE_SENTRY_DSN/)
  })

  it.each([undefined, '', 'abc123', sha.toUpperCase()])('rejects a set DSN with the release %j', (release) => {
    expect(() => sentryIntegration({ dsn, release, siteUrl })).toThrow(/GITHUB_SHA/)
  })
})
