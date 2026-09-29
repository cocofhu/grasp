/**
 * Browser acceptance: bottom Artifact opens modal; stage hides app preview.
 */
import { test, expect } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import fs from 'node:fs'

const shotDir = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '../../.tmp-preview-artifact-modal-shots',
)

test.describe('Preview Artifact modal (browser)', () => {
  test.beforeAll(() => {
    fs.mkdirSync(shotDir, { recursive: true })
  })

  test('stage: tabs without app preview (g2/g3)', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 720 })
    const shot = (name: string) =>
      page.screenshot({ path: path.join(shotDir, name), animations: 'disabled' })

    await page.goto('/preview-artifact-modal-harness.html?mode=stage')
    await expect(page.getByTestId('preview-artifact-stage-harness')).toBeVisible({ timeout: 20_000 })
    await expect(page.getByTestId('react-artifact-stage')).toBeVisible()
    await expect(page.getByTestId('react-artifact-tab-grid')).toBeVisible()
    await expect(page.getByTestId('react-artifact-card-novnc')).toHaveCount(0)
    await expect(page.getByTestId('react-artifact-tab-novnc')).toHaveCount(0)
    await expect(page.getByTestId('react-artifact-card-research.json')).toBeVisible()
    await expect(page.getByTestId('react-artifact-card-plan.json')).toBeVisible()
    await page.waitForTimeout(300)
    await shot('01-stage-grid-no-app-preview.png')

    await page.getByTestId('react-artifact-card-research.json').click()
    await expect(page.getByTestId('react-artifact-tab-research.json')).toBeVisible()
    await expect(page.getByTestId('react-artifact-preview-research.json')).toBeVisible()
    await page.waitForTimeout(200)
    await shot('02-stage-tab-research.png')

    await page.getByTestId('react-artifact-tab-close-research.json').click()
    await expect(page.getByTestId('react-artifact-grid')).toBeVisible()
    await expect(page.getByTestId('react-artifact-tab-research.json')).toHaveCount(0)
    await shot('03-stage-back-to-grid.png')
  })

  test('pick bar: Artifact opens modal without replacing chat (g1/g3.2)', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 720 })
    const shot = (name: string) =>
      page.screenshot({ path: path.join(shotDir, name), animations: 'disabled' })

    await page.goto('/preview-artifact-modal-harness.html?mode=pick')
    await expect(page.getByTestId('preview-pick-artifact-harness')).toBeVisible({ timeout: 20_000 })

    const host = page.locator('grasp-preview-pick')
    await expect(host).toBeAttached()

    const pick = host.locator('[data-role="toggle"]')
    const artifact = host.locator('[data-role="artifact"]')
    const chat = host.locator('[data-role="chat"]')
    const mask = host.locator('[data-role="artifact-mask"]')
    const drawer = host.locator('[data-role="drawer"]')

    await expect(pick).toBeVisible()
    await expect(artifact).toBeVisible()
    await expect(chat).toBeVisible()
    await expect(artifact).toBeEnabled({ timeout: 5_000 })
    await expect(artifact).toHaveText(/Artifact|产物/)
    await expect(drawer).toBeVisible()
    await page.waitForTimeout(200)
    await shot('04-bar-pick-artifact-chat.png')

    await artifact.click()
    await expect(mask).toBeVisible()
    await expect(artifact).toHaveAttribute('aria-expanded', 'true')
    const frame = host.locator('[data-role="artifact-modal"] iframe')
    await expect(frame).toBeAttached()
    const src = await frame.getAttribute('src')
    expect(src || '').toContain('/embed/runs/run-1/nodes/ap1/artifacts')
    // Chat drawer stays open while modal is up.
    await expect(drawer).toBeVisible()
    await expect(chat).toHaveAttribute('aria-expanded', 'true')
    await page.waitForTimeout(200)
    await shot('05-artifact-modal-open.png')

    await host.locator('[data-role="artifact-close"]').click()
    await expect(mask).toBeHidden()
    await expect(artifact).toHaveAttribute('aria-expanded', 'false')
    await expect(drawer).toBeVisible()
    await expect(chat).toHaveAttribute('aria-expanded', 'true')

    // Pick still only toggles inspect mode.
    await pick.click()
    await expect(pick).toHaveAttribute('aria-pressed', 'true')
    await pick.click()
    await expect(pick).toHaveAttribute('aria-pressed', 'false')
    await shot('06-after-close-pick-still-works.png')
  })
})
