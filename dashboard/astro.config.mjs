// The public documentation site: Astro and Starlight, built from src/app/ into
// the ignored dist-site/, which .github/workflows/website.yml publishes to
// GitHub Pages. It shares src/components, src/lib, src/styles, src/assets and
// public/ with the Dashboard, which vite.config.ts builds separately.
import { fileURLToPath } from 'node:url'

import react from '@astrojs/react'
import starlight from '@astrojs/starlight'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'astro/config'

const site = process.env.WEBSITE_SITE || 'https://taco3064.github.io'
const base = process.env.WEBSITE_BASE || '/gh-nibfin/'

const origin = new URL(site)
if (!/^https?:$/.test(origin.protocol) || origin.origin !== site.replace(/\/$/, '')) {
  throw new Error('WEBSITE_SITE must be an HTTP(S) origin without a path.')
}
if (!/^\/(?:[A-Za-z0-9_-]+\/)*$/.test(base)) {
  throw new Error('WEBSITE_BASE must start and end with / and contain only path segments.')
}

export default defineConfig({
  site,
  base,
  srcDir: './src/app',
  publicDir: './public',
  outDir: './dist-site',
  output: 'static',
  trailingSlash: 'always',
  vite: {
    plugins: [tailwindcss()],
    resolve: {
      alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
    },
  },
  integrations: [
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
