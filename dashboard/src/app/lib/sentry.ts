// Browser error reporting for the public website (#481). Only the page script
// that sentry-integration.ts injects imports this module, and only when the
// build sets WEBSITE_SENTRY_DSN; nothing in the Dashboard imports it.
import { init, type BrowserOptions, type ErrorEvent } from '@sentry/browser'

export interface SentryConfig {
  dsn: string
  // The full commit SHA of the build, so a Sentry issue points to one build.
  release: string
  // The site's own origin and base path, for example https://taco3064.github.io/gh-nibfin/.
  siteUrl: string
}

// Schemes of browser extensions; their scripts are not the site's errors.
const extensionSchemes = new Set([
  'chrome-extension:',
  'moz-extension:',
  'safari-extension:',
  'safari-web-extension:',
  'ms-browser-extension:',
])
const localHosts = new Set(['localhost', '127.0.0.1', '[::1]'])

// isIgnoredUrl reports a URL on localhost, a file: URL or a browser extension.
// Text that is not a URL (for example "<anonymous>") is not ignored by itself.
export function isIgnoredUrl(url: string | undefined): boolean {
  if (!url || !URL.canParse(url)) return false
  const { protocol, hostname } = new URL(url)
  return protocol === 'file:' || extensionSchemes.has(protocol) || localHosts.has(hostname)
}

// beforeSend drops events whose page or any stack frame is on localhost, a
// file: URL or a browser extension, and keeps every other event unchanged.
export function beforeSend(event: ErrorEvent): ErrorEvent | null {
  const page = event.request?.url ?? globalThis.location?.href
  if (isIgnoredUrl(page)) return null
  for (const value of event.exception?.values ?? []) {
    for (const frame of value.stacktrace?.frames ?? []) {
      if (isIgnoredUrl(frame.filename)) return null
    }
  }
  return event
}

function escapeRegExp(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\/]/g, '\\$&')
}

// sentryOptions returns the init options: errors only (no tracing, replay,
// profiling or sessions), every error sampled, and no SDK-populated user data.
export function sentryOptions({ dsn, release, siteUrl }: SentryConfig): BrowserOptions {
  return {
    dsn,
    release,
    environment: 'production',
    sampleRate: 1,
    allowUrls: [new RegExp(`^${escapeRegExp(new URL(siteUrl).href)}`)],
    // SDK v11 replaces sendDefaultPii: no user.* fields, so no IP address.
    dataCollection: { userInfo: false },
    integrations: (defaults) => defaults.filter((integration) => integration.name !== 'BrowserSession'),
    beforeSend,
  }
}

export function initSentry(config: SentryConfig): void {
  init(sentryOptions(config))
}
