import type { BrowserOptions, ErrorEvent } from '@sentry/browser'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const init = vi.hoisted(() => vi.fn())
vi.mock('@sentry/browser', () => ({ init }))

import { beforeSend, initSentry, sentryOptions } from './sentry'

const config = {
  dsn: 'https://public@o1.ingest.sentry.io/2',
  release: 'a'.repeat(40),
  siteUrl: 'https://taco3064.github.io/gh-nibfin/',
}

function event(page: string | undefined, ...frames: string[]): ErrorEvent {
  return {
    type: undefined,
    request: page === undefined ? undefined : { url: page },
    exception: { values: [{ type: 'Error', value: 'boom', stacktrace: { frames: frames.map((filename) => ({ filename })) } }] },
  }
}

describe('sentryOptions', () => {
  it('reports production errors for one release without tracing, replay, profiling or user data', () => {
    const options = sentryOptions(config)
    expect(options).toMatchObject({
      dsn: config.dsn,
      release: config.release,
      environment: 'production',
      sampleRate: 1,
      dataCollection: { userInfo: false },
      beforeSend,
    })
    expect(Object.keys(options).sort()).toEqual(
      ['allowUrls', 'beforeSend', 'dataCollection', 'dsn', 'environment', 'integrations', 'release', 'sampleRate'].sort(),
    )
    expect(options).not.toHaveProperty('tracesSampleRate')
    expect(options).not.toHaveProperty('tracesSampler')
    expect(options).not.toHaveProperty('replaysSessionSampleRate')
    expect(options).not.toHaveProperty('replaysOnErrorSampleRate')
    expect(options).not.toHaveProperty('profilesSampleRate')
  })

  it('allows only scripts under the site origin and base path', () => {
    const [pattern] = sentryOptions(config).allowUrls as RegExp[]
    expect(sentryOptions(config).allowUrls).toHaveLength(1)
    expect(pattern.test('https://taco3064.github.io/gh-nibfin/_astro/page.js')).toBe(true)
    expect(pattern.test('https://taco3064.github.io/other/_astro/page.js')).toBe(false)
    expect(pattern.test('https://taco3064.github.io/gh-nibfin-x/page.js')).toBe(false)
    expect(pattern.test('https://evil.example/https://taco3064.github.io/gh-nibfin/a.js')).toBe(false)
    expect(pattern.test('https://taco3064xgithub.io/gh-nibfin/a.js')).toBe(false)
  })

  it('drops the session integration and keeps the global error handlers', () => {
    type Defaults = Extract<BrowserOptions['integrations'], (defaults: never) => unknown>
    const integrations = sentryOptions(config).integrations as Defaults
    const names = integrations([{ name: 'GlobalHandlers' }, { name: 'BrowserSession' }, { name: 'EventFilters' }] as Parameters<Defaults>[0]).map((i) => i.name)
    expect(names).toEqual(['GlobalHandlers', 'EventFilters'])
  })
})

describe('beforeSend', () => {
  it('keeps an event from the site', () => {
    const site = event('https://taco3064.github.io/gh-nibfin/live/', 'https://taco3064.github.io/gh-nibfin/_astro/a.js', '<anonymous>')
    expect(beforeSend(site)).toBe(site)
  })

  it.each([
    ['localhost page', event('http://localhost:4321/gh-nibfin/')],
    ['127.0.0.1 page', event('http://127.0.0.1:4321/')],
    ['[::1] page', event('http://[::1]:4321/')],
    ['file page', event('file:///tmp/index.html')],
    ['localhost frame', event('https://taco3064.github.io/gh-nibfin/', 'http://localhost:4321/_astro/a.js')],
    ['chrome extension frame', event('https://taco3064.github.io/gh-nibfin/', 'chrome-extension://abc/content.js')],
    ['firefox extension frame', event('https://taco3064.github.io/gh-nibfin/', 'moz-extension://abc/content.js')],
    ['safari extension frame', event('https://taco3064.github.io/gh-nibfin/', 'safari-web-extension://abc/c.js')],
    ['old safari extension frame', event('https://taco3064.github.io/gh-nibfin/', 'safari-extension://abc/c.js')],
    ['edge extension frame', event('https://taco3064.github.io/gh-nibfin/', 'ms-browser-extension://abc/c.js')],
  ])('drops a %s', (_name, dropped) => {
    expect(beforeSend(dropped)).toBeNull()
  })

  it('falls back to the page location when the event has no request URL', () => {
    vi.stubGlobal('location', { href: 'http://localhost:4321/' })
    try {
      expect(beforeSend(event(undefined))).toBeNull()
    } finally {
      vi.unstubAllGlobals()
    }
  })
})

describe('initSentry', () => {
  beforeEach(() => init.mockClear())

  it('initializes the browser SDK with the site options', () => {
    initSentry(config)
    expect(init).toHaveBeenCalledOnce()
    const options = init.mock.calls[0][0]
    expect(options).toMatchObject({ dsn: config.dsn, release: config.release, environment: 'production', sampleRate: 1 })
    expect(options.dataCollection).toEqual({ userInfo: false })
    expect(options.beforeSend).toBe(beforeSend)
  })
})
