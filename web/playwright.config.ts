import { defineConfig, devices } from '@playwright/test'

const e2ePort = Number(process.env.E2E_PORT ?? '5174')
if (!Number.isInteger(e2ePort) || e2ePort < 1 || e2ePort > 65535) {
  throw new Error('E2E_PORT must be an integer between 1 and 65535')
}

const baseURL = `http://127.0.0.1:${e2ePort}`

const MOBILE_CRITICAL_TESTS = /(?:mobile-browser-contract|gate-mobile-fill|run-detail-mobile|run-detail-mobile-panel|agent-studio-mobile-data|board-workspace|workspace-(?:credentials|shared-env|external-mcp|audit)|desktop-sidebar-hide|shell-loading|artifacts-page-version)\.spec\.ts/

export default defineConfig({
  testDir: './e2e',
  testMatch: '*.spec.ts',
  timeout: 30_000,
  retries: 0,
  // Shared vite.e2e mock state (vncConnectCount / delay/fail flags) is process-global.
  workers: 1,
  fullyParallel: false,
  // CI uploads html report + traces on failure (see ci-web web-e2e job).
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL,
    headless: true,
    locale: 'zh-CN',
    trace: 'retain-on-failure',
  },
  // Keep the existing desktop suite on its current browser while running the
  // mobile contract against both Chromium and WebKit. The mobile project is
  // intentionally limited to the contract spec so the full desktop suite is
  // not multiplied by every device profile.
  projects: [
    {
      name: 'desktop',
      // Preserve the snapshot names from the original unnamed desktop project.
      snapshotPathTemplate: '{snapshotDir}/{testFileDir}/{testFileName}-snapshots/{arg}{-snapshotSuffix}{ext}',
      testIgnore: /mobile-browser-contract\.spec\.ts/,
    },
    {
      name: 'mobile-chromium',
      testMatch: /mobile-browser-contract\.spec\.ts/,
      use: { ...devices['Pixel 7'] },
    },
    {
      name: 'mobile-webkit',
      testMatch: /mobile-browser-contract\.spec\.ts/,
      use: { ...devices['iPhone 13'] },
    },
    {
      name: 'mobile-chromium-critical',
      testMatch: MOBILE_CRITICAL_TESTS,
      use: { ...devices['Pixel 7'] },
    },
    {
      name: 'mobile-webkit-critical',
      testMatch: MOBILE_CRITICAL_TESTS,
      use: { ...devices['iPhone 13'] },
    },
  ],
  webServer: {
    command: `npx vite --config vite.e2e.config.ts --port ${e2ePort}`,
    url: baseURL,
    reuseExistingServer: !process.env.CI,
  },
})
