import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './tests', fullyParallel: true, workers: 2, reporter: 'list',
  use: { ...devices['Desktop Chrome'], channel: process.env.PLAYWRIGHT_CHANNEL || 'chrome', baseURL: 'http://127.0.0.1:5177/admin/', viewport: { width: 1440, height: 900 }, screenshot: 'only-on-failure', trace: 'retain-on-failure' },
  webServer: { command: 'npm run dev -- --port 5177', url: 'http://127.0.0.1:5177/admin/', reuseExistingServer: false },
})
