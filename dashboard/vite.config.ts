/// <reference types="vitest/config" />
import { readdirSync, rmSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig, type Plugin } from 'vite'

// The Go server embeds this folder. Its committed .gitignore keeps the folder
// in Git without a build, so it must survive every build.
const outDir = fileURLToPath(new URL('../internal/dashboard/dist', import.meta.url))

// The screenshot harness builds here, never into outDir, so no product
// binary embeds it. The Go server reads it only with -tags dashboardscreenshots.
const screenshotsDir = fileURLToPath(new URL('./.screenshots-dist', import.meta.url))

// The server replaces this placeholder with a fresh nonce per response; see
// noncePlaceholder in internal/dashboard/assets.go.
const cspNonce = 'NIBFIN_CSP_NONCE'

// cleanDist empties outDir like Vite's emptyOutDir, but keeps .gitignore.
function cleanDist(): Plugin {
  return {
    name: 'nibfin-clean-dist',
    apply: 'build',
    buildStart() {
      for (const entry of readdirSync(outDir)) {
        if (entry !== '.gitignore') rmSync(`${outDir}/${entry}`, { recursive: true, force: true })
      }
    },
  }
}

export default defineConfig(({ mode }) => {
  const screenshots = mode === 'screenshots'
  return {
    plugins: [react(), tailwindcss(), ...(screenshots ? [] : [cleanDist()])],
    resolve: {
      alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
    },
    html: { cspNonce },
    build: screenshots
      ? {
          outDir: screenshotsDir,
          emptyOutDir: true,
          rollupOptions: {
            input: {
              index: fileURLToPath(new URL('./index.html', import.meta.url)),
              screenshots: fileURLToPath(new URL('./screenshots.html', import.meta.url)),
              operation: fileURLToPath(new URL('./operation.html', import.meta.url)),
              diagram: fileURLToPath(new URL('./diagram.html', import.meta.url)),
            },
          },
        }
      : { outDir, emptyOutDir: false },
    test: {
      include: ['src/**/*.test.{ts,tsx}'],
      environment: 'node',
    },
  }
})
