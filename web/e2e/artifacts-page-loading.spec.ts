/**
 * Browser acceptance: ArtifactsView HardLoadLayer / RefreshStrip dual-track loading.
 */
import { test, expect } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import fs from 'node:fs'

const shotDir = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '../../../.tmp-artifacts-loading-shots',
)

test.describe('Artifacts page loading states (browser)', () => {
  test.beforeAll(() => {
    fs.mkdirSync(shotDir, { recursive: true })
  })

  test('first entry shows HardLoadLayer without empty-group flash', async ({ page }) => {
    await page.setViewportSize({ width: 1200, height: 800 })
    await page.goto('/artifacts-page-loading.html?scenario=pending&delay=4000')
    await expect(page.getByTestId('artifacts-loading-harness-root')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('hard-load-layer')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('hard-load-stage')).toContainText(/加载中|Loading/)
    await expect(page.locator('.card[aria-busy="true"]')).toBeVisible()
    await expect(page.getByText('无匹配分组')).toHaveCount(0)
    await page.screenshot({
      path: path.join(shotDir, '01-first-hard-load.png'),
      animations: 'disabled',
    })
  })

  test('settled ready shows groups without overlay', async ({ page }) => {
    await page.setViewportSize({ width: 1200, height: 800 })
    await page.goto('/artifacts-page-loading.html?scenario=ready&delay=0')
    await expect(page.getByTestId('artifacts-loading-harness-root')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('hard-load-layer')).toHaveCount(0, { timeout: 15_000 })
    await expect(page.locator('.card[aria-busy="false"]')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByRole('button', { name: 'Demo' })).toBeVisible()
    await page.screenshot({
      path: path.join(shotDir, '02-ready-groups.png'),
      animations: 'disabled',
    })
  })

  test('fail without cache keeps HardLoadLayer and retry recovers', async ({ page }) => {
    await page.setViewportSize({ width: 1200, height: 800 })
    await page.goto('/artifacts-page-loading.html?scenario=fail')
    await expect(page.getByTestId('hard-load-layer')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText('无匹配分组')).toHaveCount(0)
    await page.screenshot({
      path: path.join(shotDir, '03-fail-hard-load.png'),
      animations: 'disabled',
    })
    // stuck-after-ms is 10s
    await expect(page.getByTestId('hard-load-retry')).toBeVisible({ timeout: 15_000 })
    await page.screenshot({
      path: path.join(shotDir, '04-fail-stuck-retry.png'),
      animations: 'disabled',
    })
    await page.getByTestId('hard-load-retry').click()
    await expect(page.getByTestId('hard-load-layer')).toHaveCount(0, { timeout: 15_000 })
    await expect(page.getByRole('button', { name: 'Demo' })).toBeVisible()
    await page.screenshot({
      path: path.join(shotDir, '05-fail-retry-recovered.png'),
      animations: 'disabled',
    })
  })

  test('project switch with cache shows RefreshStrip', async ({ page }) => {
    await page.setViewportSize({ width: 1200, height: 800 })
    await page.goto('/artifacts-page-loading.html?scenario=refresh&delay=1500')
    await expect(page.getByTestId('artifacts-loading-harness-root')).toBeVisible({ timeout: 15_000 })
    // Wait for first settle
    await expect(page.getByTestId('hard-load-layer')).toHaveCount(0, { timeout: 20_000 })
    await expect(page.locator('aside span[title="Demo"]')).toBeVisible()
    // Harness auto-switches project; RefreshStrip should appear during reload
    await expect(page.getByTestId('refresh-strip')).toBeVisible({ timeout: 20_000 })
    await expect(page.locator('.card[aria-busy="true"]')).toBeVisible()
    await expect(page.getByText('无匹配分组')).toHaveCount(0)
    await page.screenshot({
      path: path.join(shotDir, '06-refresh-strip.png'),
      animations: 'disabled',
    })
  })
})
