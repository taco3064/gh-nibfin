import { defineConfig, devices } from '@playwright/test'

// npm run screenshots builds the UI and the Issue element harness into
// .screenshots-dist/ and the site into dist-site/. This then starts the real
// Go server with the dashboardscreenshots tag, which serves that build, and
// Vite's preview of the site under its base path, both in the foreground,
// captures the pages and stops both servers. (astro preview would detach.)
const port = 4790
const sitePort = 4791
const siteBase = '/gh-nibfin/'
const siteSpec = /site\.spec\.ts$/
// docs.spec.ts writes the website's tracked Dashboard images; only npm run
// docs-screenshots runs it.
const docsSpec = /docs\.spec\.ts$/

export default defineConfig({
  testDir: './screenshots',
  outputDir: './test-results',
  reporter: 'list',
  workers: 1,
  forbidOnly: true,
  use: {
    ...devices['Desktop Chrome'],
    baseURL: `http://127.0.0.1:${port}`,
    headless: true,
    colorScheme: 'light',
  },
  projects: [
    { name: 'chromium', testIgnore: [siteSpec, docsSpec] },
    { name: 'docs', testMatch: docsSpec },
    { name: 'site', testMatch: siteSpec, use: { baseURL: `http://127.0.0.1:${sitePort}${siteBase}` } },
  ],
  webServer: [
    {
      command: `go run -tags dashboardscreenshots . dashboard --no-browser --port ${port}`,
      cwd: '..',
      url: `http://127.0.0.1:${port}/`,
      reuseExistingServer: false,
      timeout: 180_000,
      // The server prints its real session URL; keep it out of the output.
      stdout: 'ignore',
      stderr: 'pipe',
    },
    {
      command: `npx vite preview --outDir dist-site --base ${siteBase} --port ${sitePort} --strictPort --host 127.0.0.1`,
      url: `http://127.0.0.1:${sitePort}${siteBase}`,
      reuseExistingServer: false,
      timeout: 60_000,
      stdout: 'ignore',
      stderr: 'pipe',
    },
  ],
})
