import { test, expect } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import fs from 'node:fs'

const shotDir = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '../../../.tmp-friendly-name-shots',
)

test.describe('Reserved artifact friendly names (browser)', () => {
  test.beforeAll(() => {
    fs.mkdirSync(shotDir, { recursive: true })
  })

  test('preview header shows 需求澄清 plus technical filename', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 800 })
    await page.goto('/friendly-name-harness.html?scenario=preview')
    await expect(page.getByTestId('friendly-name-harness-root')).toBeVisible({ timeout: 15_000 })
    const header = page.getByTestId('artifact-preview-file-row')
    await expect(header).toContainText('需求澄清')
    await expect(header).toContainText('clarified_requirement.json')
    await expect(header).toContainText('json')
    await page.waitForTimeout(400)
    await page.screenshot({
      path: path.join(shotDir, '01-preview-clarified.png'),
      animations: 'disabled',
    })
  })

  test('non-reserved json keeps technical filename only', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 800 })
    await page.goto('/friendly-name-harness.html?scenario=notes')
    await expect(page.getByTestId('friendly-name-harness-root')).toBeVisible({ timeout: 15_000 })
    const header = page.getByTestId('artifact-preview-file-row')
    await expect(header).toContainText('notes.json')
    await expect(header).not.toContainText('需求澄清')
    await page.waitForTimeout(300)
    await page.screenshot({
      path: path.join(shotDir, '02-preview-notes.png'),
      animations: 'disabled',
    })
  })

  test('artifact list friendly name and dual search', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 800 })
    await page.goto('/friendly-name-harness.html?scenario=list')
    await expect(page.getByTestId('friendly-name-harness-root')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTitle('clarified_requirement.json')).toBeVisible()
    await expect(page.getByTitle('clarified_requirement.json')).toContainText('需求澄清')
    await expect(page.getByTitle('notes.json')).toBeVisible()
    const search = page.locator('input[type="search"], input').first()
    await search.fill('需求澄清')
    await expect(page.getByTitle('clarified_requirement.json')).toBeVisible()
    await expect(page.getByTitle('notes.json')).toBeHidden()
    await page.waitForTimeout(300)
    await page.screenshot({
      path: path.join(shotDir, '03-list-search-friendly.png'),
      animations: 'disabled',
    })
    await search.fill('clarified_requirement')
    await expect(page.getByTitle('clarified_requirement.json')).toBeVisible()
    await expect(page.getByTitle('notes.json')).toBeHidden()
    await page.waitForTimeout(300)
    await page.screenshot({
      path: path.join(shotDir, '04-list-search-technical.png'),
      animations: 'disabled',
    })
  })
})
