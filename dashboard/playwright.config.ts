import { defineConfig, devices } from '@playwright/test'

// npm run screenshots builds the UI and the Issue element harness into
// .screenshots-dist/, then this starts the real Go server with the
// dashboardscreenshots tag, which serves that build, captures the pages and
// stops the server.
const port = 4790

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
  projects: [{ name: 'chromium' }],
  webServer: {
    command: `go run -tags dashboardscreenshots . dashboard --no-browser --port ${port}`,
    cwd: '..',
    url: `http://127.0.0.1:${port}/`,
    reuseExistingServer: false,
    timeout: 180_000,
    // The server prints its real session URL; keep it out of the output.
    stdout: 'ignore',
    stderr: 'pipe',
  },
})
