import {defineConfig} from '@playwright/test';
export default defineConfig({
  testDir: '.', testMatch: '*.spec.mjs', workers: 1, fullyParallel: false,
  timeout: 30000, expect: {timeout: 5000}, reporter: 'line',
  outputDir: process.env.NIBFIN_TEST_OUTPUT,
  use: {browserName: 'chromium', headless: true, actionTimeout: 5000}
});
