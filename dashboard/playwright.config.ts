import { defineConfig, devices } from '@playwright/test'

// npm run screenshots builds the UI, then this starts the real Go server,
// which embeds the fresh build, captures the page and stops the server.
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
    command: `go run . dashboard --no-browser --port ${port}`,
    cwd: '..',
    url: `http://127.0.0.1:${port}/`,
    reuseExistingServer: false,
    timeout: 180_000,
    // The server prints its real session URL; keep it out of the output.
    stdout: 'ignore',
    stderr: 'pipe',
  },
})
