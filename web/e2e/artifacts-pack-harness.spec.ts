/**
 * Browser acceptance: Artifacts page pack-by-run button (platform scope).
 * Temporary harness for test-node gate.
 */
import { test, expect } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import fs from 'node:fs'

const shotDir = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '../../../.tmp-artifacts-pack-shots',
)

test.describe('Artifacts pack-by-run (browser)', () => {
  test.beforeAll(() => {
    fs.mkdirSync(shotDir, { recursive: true })
  })

  test('platform shows pack; empty disabled; click packs without changing selection', async ({
    page,
  }) => {
    await page.setViewportSize({ width: 480, height: 720 })
    const shot = (name: string) =>
      page.screenshot({ path: path.join(shotDir, name), animations: 'disabled' })

    await page.goto('/artifacts-pack-harness.html?scenario=platform')
    await expect(page.getByTestId('artifacts-pack-harness-root')).toBeVisible({ timeout: 15_000 })

    const packs = page.getByTestId('artifact-run-pack')
    await expect(packs).toHaveCount(2)
    await expect(packs.nth(0)).toBeEnabled()
    await expect(packs.nth(0)).toContainText('打包')
    await expect(packs.nth(1)).toBeDisabled()
    await page.waitForTimeout(300)
    await shot('01-platform-pack-buttons.png')

    const activeBefore = await page.getByTestId('active-id').textContent()
    await packs.nth(0).click()
    await expect(packs.nth(0)).toContainText('打包中')
    await page.waitForTimeout(200)
    await shot('02-packing-in-progress.png')
    await expect(packs.nth(0)).toContainText('打包', { timeout: 5_000 })
    const activeAfter = await page.getByTestId('active-id').textContent()
    expect(activeAfter).toBe(activeBefore)
    await shot('03-after-pack-same-selection.png')

    // collapse then pack still available
    await page.getByRole('button', { name: /给产物页加打包下载/ }).click()
    await expect(packs.nth(0)).toBeVisible()
    await packs.nth(0).click()
    await expect(packs.nth(0)).toContainText('打包中')
    await expect(packs.nth(0)).toContainText('打包', { timeout: 5_000 })
    await shot('04-collapsed-still-packable.png')
  })

  test('scope=run has no pack button', async ({ page }) => {
    await page.setViewportSize({ width: 480, height: 720 })
    await page.goto('/artifacts-pack-harness.html?scenario=run')
    await expect(page.getByTestId('artifacts-pack-harness-root')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('artifact-run-pack')).toHaveCount(0)
    await page.screenshot({
      path: path.join(shotDir, '05-run-scope-no-pack.png'),
      animations: 'disabled',
    })
  })

  test('pack failure shows toast and restores button', async ({ page }) => {
    await page.setViewportSize({ width: 480, height: 720 })
    await page.goto('/artifacts-pack-harness.html?scenario=platform&fail=1')
    await expect(page.getByTestId('artifacts-pack-harness-root')).toBeVisible({ timeout: 15_000 })
    const pack = page.getByTestId('artifact-run-pack').nth(0)
    await pack.click()
    await expect(pack).toContainText('打包中')
    await expect(pack).toContainText('打包', { timeout: 5_000 })
    await expect(page.getByTestId('toast-host')).toContainText('打包失败', { timeout: 5_000 })
    await page.screenshot({
      path: path.join(shotDir, '06-pack-failure-toast.png'),
      animations: 'disabled',
    })
  })
})
