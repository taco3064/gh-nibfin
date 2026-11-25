/// <reference types="vitest/config" />
import { readdirSync, rmSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig, type Plugin } from 'vite'

// The Go server embeds this folder. Its committed .gitignore keeps the folder
// in Git without a build, so it must survive every build.
const outDir = fileURLToPath(new URL('../internal/dashboard/dist', import.meta.url))

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

export default defineConfig({
  plugins: [react(), tailwindcss(), cleanDist()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  build: {
    outDir,
    emptyOutDir: false,
  },
  test: {
    include: ['src/**/*.test.{ts,tsx}'],
    environment: 'node',
  },
})
