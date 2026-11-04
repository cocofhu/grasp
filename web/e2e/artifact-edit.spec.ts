/**
 * Run artifact editing: stage quick edit and run-detail panel save flows,
 * plus light / dark screenshots of the form editor.
 */
import { test, expect, type Page } from '@playwright/test'
import path from 'node:path'
import fs from 'node:fs'
import { fileURLToPath } from 'node:url'

const shotDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../.tmp-artifact-edit-shots')

async function removeG2AndSave(page: Page) {
  const goals = page.locator('[data-list-key="goals"] > [data-testid="artifact-editor-item"]')
  await expect(goals).toHaveCount(2)
  await goals.nth(1).hover()
  await goals.nth(1).getByTestId('artifact-editor-item-remove').first().click()
  await expect(goals).toHaveCount(1)
  await expect(page.getByTestId('artifact-editor-undo-bar')).toContainText('实机验收')
  await expect(page.getByTestId('artifact-edit-summary')).toContainText('已修改')
  await page.keyboard.press('Control+s')
  await expect(page.getByTestId('artifact-edit-saved-note')).toHaveText('已保存为 v4')
}

test.describe('run artifact editing', () => {
  test.beforeAll(() => fs.mkdirSync(shotDir, { recursive: true }))

  test('stage: quick edit plan.json, save with Ctrl+S, previous version keeps the old body', async ({ page }) => {
    await page.setViewportSize({ width: 1200, height: 820 })
    await page.goto('/artifact-edit.html?surface=stage')
    const card = page.getByTestId('react-artifact-card-plan.json')
    await expect(card).toBeVisible({ timeout: 15_000 })
    await card.hover()
    await page.getByTestId('react-artifact-card-edit-plan.json').click()
    await expect(page.getByTestId('react-artifact-tab-plan.json')).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByTestId('artifact-editor-form')).toBeVisible()
    await page.screenshot({ path: path.join(shotDir, 'stage-light-form.png'), animations: 'disabled' })

    const goals = page.locator('[data-list-key="goals"] > [data-testid="artifact-editor-item"]')
    await goals.nth(1).hover()
    await goals.nth(1).getByTestId('artifact-editor-item-remove').first().click()
    await expect(page.getByTestId('react-artifact-tab-dirty-plan.json')).toBeVisible()
    await page.keyboard.press('Control+s')
    await expect(page.getByTestId('artifact-edit-saved-note')).toHaveText('已保存为 v4')
    await expect(page.getByTestId('react-artifact-tab-dirty-plan.json')).toHaveCount(0)

    await page.getByTestId('react-artifact-tab-grid').click()
    const chip = page.getByTestId('react-artifact-version-chip-btn-plan.json')
    await expect(chip).toContainText('v4')
    await expect(chip).toContainText('最新')
    await chip.click()
    await page.getByTestId('react-artifact-version-option-v3').click()
    await expect(page.getByTestId('react-artifact-tab-plan.json')).toHaveAttribute('aria-selected', 'true')
    await page.getByTestId('artifact-preview-mode-raw').click()
    await expect(page.getByTestId('artifact-preview-raw-json')).toContainText('实机验收')
    await expect(page.getByTestId('artifact-preview-edit')).toHaveCount(0)
  })

  test('run detail panel: edit and save plan.json', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 800 })
    await page.goto('/artifact-edit.html?surface=detail')
    await expect(page.getByTestId('artifact-edit-harness')).toBeVisible({ timeout: 15_000 })
    await page.getByTestId('artifact-preview-edit').click()
    await expect(page.getByTestId('artifact-editor-form')).toBeVisible()
    await removeG2AndSave(page)
  })

  test('run detail drawer width: list collapses and the form stacks to one column', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.goto('/artifact-edit.html?surface=detail&width=480')
    await expect(page.getByTestId('artifact-panel-list')).toBeVisible({ timeout: 15_000 })
    await page.getByTestId('artifact-preview-edit').click()
    await expect(page.getByTestId('artifact-editor-form')).toBeVisible()
    await expect(page.getByTestId('artifact-panel-list')).toBeHidden()

    const row = page.locator('.ed-row[data-field-path="title"]').first()
    const lb = await row.locator('.ed-row__label').boundingBox()
    const cb = await row.locator('.ed-row__control').boundingBox()
    expect(lb && cb && cb.y >= lb.y + lb.height - 1).toBe(true)
    expect(cb!.width).toBeGreaterThan(380)
    const sub = page.locator('.ed-row[data-field-path="goals[0].subgoals[0].title"] .ed-row__control').first()
    expect((await sub.boundingBox())!.width).toBeGreaterThan(300)
    await page.screenshot({ path: path.join(shotDir, 'detail-drawer-form.png'), animations: 'disabled' })

    await removeG2AndSave(page)
    await expect(page.getByTestId('artifact-panel-list')).toBeVisible()
  })

  test('dark theme and narrow layout render the editor', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 800 })
    await page.goto('/artifact-edit.html?surface=detail&theme=dark')
    await page.getByTestId('artifact-preview-edit').click()
    await expect(page.getByTestId('artifact-editor-form')).toBeVisible()
    await page.screenshot({ path: path.join(shotDir, 'detail-dark-form.png'), animations: 'disabled' })

    await page.setViewportSize({ width: 520, height: 800 })
    const row = page.locator('.ed-row[data-field-path="title"]').first()
    const label = row.locator('.ed-row__label')
    const control = row.locator('.ed-row__control')
    const lb = await label.boundingBox()
    const cb = await control.boundingBox()
    expect(lb && cb && cb.y >= lb.y + lb.height - 1).toBe(true)
    await page.screenshot({ path: path.join(shotDir, 'detail-dark-narrow.png'), animations: 'disabled' })
  })
})
