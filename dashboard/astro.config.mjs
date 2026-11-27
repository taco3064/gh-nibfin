// The public documentation site: Astro and Starlight, built from src/app/ into
// the ignored dist-site/, which .github/workflows/website.yml publishes to
// GitHub Pages. It shares src/components, src/lib, src/styles, src/assets and
// public/ with the Dashboard, which vite.config.ts builds separately.
import { fileURLToPath } from 'node:url'

import react from '@astrojs/react'
import starlight from '@astrojs/starlight'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig, envField } from 'astro/config'

import { sentryIntegration } from './src/app/lib/sentry-integration.ts'

const site = process.env.WEBSITE_SITE || 'https://taco3064.github.io'
const base = process.env.WEBSITE_BASE || '/gh-nibfin/'
// The public live data Worker the Live page and the Home summary read.
const liveUrl = process.env.WEBSITE_LIVE_URL || 'wss://nibfin-live.tabacotaco.workers.dev/live'
// The public Sentry DSN for browser errors (#481). Empty adds no Sentry code
// and no source maps; GITHUB_SHA names the release when it is set.
const sentryDsn = process.env.WEBSITE_SENTRY_DSN || ''

const origin = new URL(site)
if (!/^https?:$/.test(origin.protocol) || origin.origin !== site.replace(/\/$/, '')) {
  throw new Error('WEBSITE_SITE must be an HTTP(S) origin without a path.')
}
if (!/^\/(?:[A-Za-z0-9_-]+\/)*$/.test(base)) {
  throw new Error('WEBSITE_BASE must start and end with / and contain only path segments.')
}
if (!URL.canParse(liveUrl) || !/^wss?:$/.test(new URL(liveUrl).protocol)) {
  throw new Error('WEBSITE_LIVE_URL must be a ws: or wss: URL.')
}

export default defineConfig({
  site,
  base,
  srcDir: './src/app',
  publicDir: './public',
  outDir: './dist-site',
  output: 'static',
  trailingSlash: 'always',
  env: {
    schema: {
      WEBSITE_LIVE_URL: envField.string({ context: 'client', access: 'public', default: liveUrl }),
    },
  },
  vite: {
    plugins: [tailwindcss()],
    resolve: {
      alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
    },
  },
  integrations: [
    ...sentryIntegration({ dsn: sentryDsn, release: process.env.GITHUB_SHA, siteUrl: new URL(base, site).href }),
    react(),
    starlight({
      title: 'Nibfin',
      description: 'Nibfin takes a GitHub Issue from scope to a merged, delivered change with AI agents on your own Runners.',
      logo: { src: './src/assets/nibfin-logo.png', alt: 'Nibfin', replacesTitle: true },
      favicon: '/favicon.png',
      head: [{ tag: 'link', attrs: { rel: 'apple-touch-icon', href: `${base}apple-touch-icon.png` } }],
      locales: { root: { label: 'English', lang: 'en' } },
      social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/taco3064/gh-nibfin' }],
      customCss: ['./src/app/styles/site.css'],
      routeMiddleware: './src/app/route-data.ts',
      sidebar: [
        { label: 'Home', link: '/' },
        { label: 'Quick start', slug: 'quick-start' },
        { label: 'How it works', slug: 'how-it-works' },
        { label: 'Setup and management', slug: 'setup' },
        { label: 'CLI commands', slug: 'cli' },
        { label: 'Live', slug: 'live' },
      ],
    }),
  ],
})
