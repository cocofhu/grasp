/**
 * Browser acceptance: ArtifactPreview page.html version chip (Artifacts page path).
 * Temporary harness for test-node gate; mirrors Approve-stage version UX.
 */
import { test, expect } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import fs from 'node:fs'

const shotDir = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '../../../.tmp-artifacts-version-shots',
)

test.describe('Artifacts page.html version switch (browser)', () => {
  test.beforeAll(() => {
    fs.mkdirSync(shotDir, { recursive: true })
  })

  test('persistent versions, historical readonly and responsive HTML toolbar', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 800 })
    const shot = (name: string) =>
      page.screenshot({ path: path.join(shotDir, name), animations: 'disabled' })

    // --- multi version ---
    await page.goto('/artifacts-page-version.html?scenario=multi')
    await expect(page.getByTestId('artifacts-version-harness-root')).toBeVisible({ timeout: 15_000 })
    const chipBtn = page.getByTestId('artifact-preview-version-chip-btn')
    await expect(chipBtn).toBeVisible()
    await expect(chipBtn).toContainText('v2')
    await expect(chipBtn).toContainText('最新')
    await expect(page.getByTestId('artifact-preview-historical-readonly')).toHaveCount(0)
    await expect(page.getByTestId('artifact-preview-identity-bar')).toHaveCount(0)
    await expect(page.frameLocator('iframe').locator('#mark-latest')).toContainText('最新稿 v2')
    await page.waitForTimeout(400)
    await shot('01-multi-latest.png')

    await chipBtn.click()
    await expect(page.getByTestId('artifact-preview-version-menu')).toBeVisible()
    await expect(page.getByTestId('artifact-preview-version-option-v1')).toHaveText('v1')
    await expect(page.getByTestId('artifact-preview-version-option-v2')).toContainText('最新')
    await page.waitForTimeout(200)
    await shot('02-version-menu.png')

    await page.getByTestId('artifact-preview-version-option-v1').click()
    await expect(page.getByTestId('artifact-preview-historical-readonly')).toBeVisible()
    await expect(page.getByTestId('artifact-preview-delete')).toHaveCount(0)
    await expect(page.frameLocator('iframe').locator('#mark-v1')).toContainText('历史稿 v1')
    await page.waitForTimeout(400)
    await shot('03-historical-readonly.png')

    await page.getByTestId('artifact-preview-version-chip-btn').click()
    await page.getByTestId('artifact-preview-version-option-v2').click()
    await expect(page.getByTestId('artifact-preview-historical-readonly')).toHaveCount(0)
    await expect(page.frameLocator('iframe').locator('#mark-latest')).toContainText('最新稿 v2')
    await page.waitForTimeout(300)
    await shot('04-back-to-latest.png')

    // --- single version: real gate actions fit 411px; 240px can wrap without overflow ---
    // The harness has a 1px outer border on each side, leaving a 411px toolbar.
    await page.setViewportSize({ width: 413, height: 800 })
    await page.goto('/artifacts-page-version.html?scenario=single')
    await expect(page.getByTestId('artifacts-version-harness-root')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('artifact-preview-version-chip')).toHaveCount(0)
    await expect(page.getByTestId('artifact-preview-identity-bar')).toHaveCount(0)
    await expect(page.getByTestId('artifact-preview-delete')).toHaveCount(0)
    await expect(page.getByTestId('html-preview-inspect-toggle')).toBeVisible()
    await page.evaluate(() => document.fonts.ready)
    const toolbar = page.getByTestId('html-preview-toolbar')
    const toolbarMetrics = () => toolbar.evaluate((el) => {
      const bounds = el.getBoundingClientRect()
      const buttons = Array.from(el.querySelectorAll('button'))
        .map((button) => button.getBoundingClientRect())
        .filter((button) => button.width > 0 && button.height > 0)
      return {
        width: bounds.width,
        scrollWidth: el.scrollWidth,
        clientWidth: el.clientWidth,
        centers: buttons.map((button) => button.top + button.height / 2),
        allInside: buttons.every((button) => button.left >= bounds.left - 0.5 && button.right <= bounds.right + 0.5),
      }
    })
    await expect.poll(async () => (await toolbarMetrics()).width).toBeCloseTo(411, 0)
    const normal = await toolbarMetrics()
    expect(normal.centers).toHaveLength(6)
    expect(Math.max(...normal.centers) - Math.min(...normal.centers)).toBeLessThanOrEqual(1)
    expect(normal.scrollWidth).toBeLessThanOrEqual(normal.clientWidth)
    expect(normal.allInside).toBe(true)
    const actions = page.getByTestId('html-preview-toolbar-end')
    await expect(actions.getByTestId('artifact-preview-copy')).toBeVisible()
    await expect(actions.getByTestId('artifact-preview-download-raw')).toBeVisible()
    await expect(actions.getByTestId('html-preview-enlarge')).toBeVisible()
    expect(await actions.locator('button').evaluateAll((buttons) => buttons.map((button) => button.dataset.testid))).toEqual([
      'artifact-preview-copy', 'artifact-preview-download-raw', 'html-preview-enlarge',
    ])
    await page.waitForTimeout(300)
    await shot('05-single-no-chip.png')

    await page.setViewportSize({ width: 242, height: 800 })
    await expect.poll(async () => (await toolbarMetrics()).width).toBeCloseTo(240, 0)
    const narrow = await toolbarMetrics()
    expect(narrow.scrollWidth).toBeLessThanOrEqual(narrow.clientWidth)
    expect(narrow.allInside).toBe(true)
    await expect(actions.getByTestId('artifact-preview-copy')).toBeVisible()
    await expect(actions.getByTestId('artifact-preview-download-raw')).toBeVisible()
    await expect(actions.getByTestId('html-preview-enlarge')).toBeVisible()
    await shot('08-single-narrow-toolbar.png')

    // --- non page.html ---
    await page.setViewportSize({ width: 1100, height: 800 })
    await page.goto('/artifacts-page-version.html?scenario=json')
    await expect(page.getByTestId('artifacts-version-harness-root')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('artifact-preview-version-chip')).toHaveCount(0)
    await page.waitForTimeout(300)
    await shot('06-json-no-chip.png')

    // --- persistent version-list failure degrades to the live artifact ---
    await page.goto('/artifacts-page-version.html?scenario=versions-error')
    await expect(page.getByTestId('artifacts-version-harness-root')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('artifact-preview-version-chip')).toHaveCount(0)
    await expect(page.frameLocator('iframe').locator('#mark-latest')).toContainText('最新稿 v2')
    await page.waitForTimeout(300)
    await shot('07-version-list-degrade.png')
  })
})
