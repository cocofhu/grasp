import { test, expect } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const shotDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../.tmp-review-ux-shots')

async function sendOpinion(page: import('@playwright/test').Page, text: string) {
  const box = page.locator('textarea').first()
  await box.fill(text)
  await page.locator('button[class*="bg-accent"]').last().click()
}

test.describe('ClarifyChat reviewMode 双面三项', () => {
  test('视觉：排队 → 流式 → Cancel', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })
    await page.goto('/review-session-ux.html')
    await expect(page.getByTestId('review-ux-root')).toBeVisible({ timeout: 15_000 })
    await page.getByTestId('surface-visual').click()

    await sendOpinion(page, '视觉意见甲（队列首条）')
    await sendOpinion(page, '视觉意见乙（仍在队列）')
    const queue = page.getByTestId('clarify-review-queue')
    await expect(queue).toBeVisible()
    await expect(queue).toContainText('待发送队列')
    await expect(queue).toContainText('视觉意见甲')
    await expect(queue).toContainText('视觉意见乙')
    await expect(page.getByTestId('clarify-confirm-flow')).toBeDisabled()
    await expect(page.getByTestId('clarify-review-queue')).toBeVisible()
    await page.screenshot({ path: path.join(shotDir, '01-visual-queue.png'), fullPage: true })

    await page.getByTestId('sim-turn-stream').click()
    await expect(page.getByText('流式产出正文（非整轮一次性）。')).toBeVisible()
    await expect(queue).toContainText('视觉意见乙')
    await page.screenshot({ path: path.join(shotDir, '02-visual-stream.png'), fullPage: true })

    await page.getByTestId('clarify-review-cancel').click()
    await expect(page.getByTestId('clarify-review-queue')).toHaveCount(0)
    await expect(page.getByTestId('clarify-interrupted')).toBeVisible()
    await expect(page.getByTestId('clarify-confirm-flow')).toBeEnabled()
    await page.screenshot({ path: path.join(shotDir, '03-visual-cancel.png'), fullPage: true })
  })

  test('调研：排队 → 流式 → Cancel', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })
    await page.goto('/review-session-ux.html')
    await expect(page.getByTestId('review-ux-root')).toBeVisible({ timeout: 15_000 })
    await page.getByTestId('surface-research').click()
    // remount after surface switch
    await page.waitForTimeout(200)

    await sendOpinion(page, '调研意见甲（队列首条）')
    await sendOpinion(page, '调研意见乙（仍在队列）')
    const queue = page.getByTestId('clarify-review-queue')
    await expect(queue).toBeVisible()
    await expect(queue).toContainText('调研意见甲')
    await expect(queue).toContainText('调研意见乙')
    await page.screenshot({ path: path.join(shotDir, '04-research-queue.png'), fullPage: true })

    await page.getByTestId('sim-turn-stream').click()
    await expect(page.getByText('流式产出正文（非整轮一次性）。')).toBeVisible()
    await page.screenshot({ path: path.join(shotDir, '05-research-stream.png'), fullPage: true })

    await page.getByTestId('clarify-review-cancel').click()
    await expect(page.getByTestId('clarify-review-queue')).toHaveCount(0)
    await expect(page.getByTestId('clarify-interrupted')).toBeVisible()
    await page.screenshot({ path: path.join(shotDir, '06-research-cancel.png'), fullPage: true })
  })
})
