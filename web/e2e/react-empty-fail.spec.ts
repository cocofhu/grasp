import { test, expect } from '@playwright/test'
import fs from 'node:fs'
import path from 'node:path'

const shotDir = '/tmp/react-empty-fail-shots'
fs.mkdirSync(shotDir, { recursive: true })

type ChatExpose = {
  applyReviewFrame: (f: Record<string, unknown>) => void
  applyQueueState: (w: number, items: unknown[], busy?: boolean) => void
  cancelReview: () => void
}

async function pumpEmptyFail(page: import('@playwright/test').Page, text: string) {
  await page.evaluate((human) => {
    const c = (window as unknown as { __clarifyChat: ChatExpose }).__clarifyChat
    c.applyReviewFrame({ event: 'turn_begin', nodeId: 'clarify', item: { text: human } })
    c.applyReviewFrame({ event: 'turn_done', nodeId: 'clarify' })
    c.applyQueueState(0, [], false)
  }, text)
}

test.describe('ReAct empty-fail retry (ClarifyChat harness)', () => {
  test('empty idle shows fail card + retry; retry does not duplicate human', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })
    await page.goto('/clarify-session-ux.html')
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })

    await pumpEmptyFail(page, 'title 有问题')
    const scroller = page.getByTestId('clarify-scroller')
    await expect(scroller).toContainText('title 有问题')
    await expect(page.getByTestId('clarify-empty-fail')).toBeVisible()
    await expect(page.getByTestId('clarify-empty-fail')).toContainText('本轮没有输出')
    await expect(page.getByTestId('clarify-empty-fail-retry')).toBeVisible()
    await expect(page.getByTestId('clarify-empty-fail-retry')).toBeEnabled()
    await expect(page.getByTestId('clarify-turn-completed')).toHaveCount(0)
    expect((await scroller.innerText()).match(/title 有问题/g)?.length ?? 0).toBe(1)
    await page.screenshot({ path: path.join(shotDir, '01-empty-fail-card.png'), fullPage: true })

    const box = page.locator('[data-testid="clarify-input"]')
    await box.fill('草稿应保留')
    await page.getByTestId('clarify-empty-fail-retry').click()
    await expect(page.getByTestId('clarify-empty-fail-retry')).toHaveCount(0)
    await expect(page.getByTestId('clarify-busy-placeholder')).toBeVisible()
    expect((await scroller.innerText()).match(/title 有问题/g)?.length ?? 0).toBe(1)
    await expect(box).toHaveValue('草稿应保留')
    await page.screenshot({ path: path.join(shotDir, '02-retry-streaming.png'), fullPage: true })
  })

  test('queued pending send disables retry; success body has no retry', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })
    await page.goto('/clarify-session-ux.html')
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })

    await pumpEmptyFail(page, '做登录')
    await expect(page.getByTestId('clarify-empty-fail-retry')).toBeEnabled()
    await page.locator('[data-testid="clarify-input"]').fill('下一条待发送')
    await page.getByTestId('clarify-send-label').click()
    await expect(page.getByTestId('clarify-review-queue')).toBeVisible()
    await expect(page.getByTestId('clarify-empty-fail-retry')).toBeDisabled()
    await page.screenshot({ path: path.join(shotDir, '03-retry-disabled-when-queued.png'), fullPage: true })
  })

  test('success completed footnote has no retry; interrupt is not empty-fail', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })
    await page.goto('/clarify-session-ux.html')
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })

    await page.evaluate(() => {
      const c = (window as unknown as { __clarifyChat: ChatExpose }).__clarifyChat
      c.applyReviewFrame({ event: 'turn_begin', nodeId: 'clarify', item: { text: '成功轮' } })
    })
    await page.evaluate(() => {
      const c = (window as unknown as { __clarifyChat: { applyAcpEvents: (e: { kind: string; text: string }[]) => void } }).__clarifyChat
      c.applyAcpEvents([{ kind: 'message', text: '好的，开始对齐。' }])
    })
    await page.evaluate(() => {
      const c = (window as unknown as { __clarifyChat: ChatExpose }).__clarifyChat
      c.applyReviewFrame({ event: 'turn_done', nodeId: 'clarify' })
      c.applyQueueState(0, [], false)
    })
    await expect(page.getByText('好的，开始对齐。')).toBeVisible()
    await expect(page.getByTestId('clarify-turn-completed')).toHaveCount(0)
    await expect(page.getByTestId('clarify-empty-fail')).toHaveCount(0)
    await expect(page.getByTestId('clarify-empty-fail-retry')).toHaveCount(0)
    await page.screenshot({ path: path.join(shotDir, '04-success-no-retry.png'), fullPage: true })

    await page.evaluate(() => {
      const c = (window as unknown as { __clarifyChat: ChatExpose }).__clarifyChat
      c.applyReviewFrame({ event: 'turn_begin', nodeId: 'clarify', item: { text: '取消这一轮' } })
      c.cancelReview()
    })
    const scroller = page.getByTestId('clarify-scroller')
    await expect(page.getByTestId('clarify-interrupted')).toBeVisible()
    await expect(page.getByTestId('clarify-interrupted')).toContainText(/已中断|interrupted/i)
    await expect(scroller).toContainText('(已中断)')
    await expect(page.getByTestId('clarify-empty-fail')).toHaveCount(0)
    await expect(page.getByTestId('clarify-empty-fail-retry')).toHaveCount(0)
    await page.screenshot({ path: path.join(shotDir, '05-interrupted-not-empty-fail.png'), fullPage: true })
  })

  test('failure banner copy is preferred on the card', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })
    await page.goto('/clarify-session-ux.html')
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })
    await page.evaluate(() => {
      const c = (window as unknown as { __clarifyChat: ChatExpose }).__clarifyChat
      c.applyReviewFrame({ event: 'turn_begin', nodeId: 'clarify', item: { text: '做登录' } })
      c.applyReviewFrame({ event: 'turn_done', nodeId: 'clarify' })
      c.applyQueueState(0, [], false)
    })
    await page.evaluate(() => {
      const c = (window as unknown as { __clarifyChat: { applyAcpEvents?: (e: { kind: string; text: string }[]) => void } }).__clarifyChat
      // After idle empty, inject a failure banner by beginning a new empty then setting via frame is hard;
      // use live overwrite: turn_begin already done. Fallback: re-pump with message event then idle.
      c.applyReviewFrame({ event: 'turn_begin', nodeId: 'clarify', item: { text: '做登录' } })
    })
    await page.evaluate(() => {
      const c = (window as unknown as { __clarifyChat: { applyAcpEvents: (e: { kind: string; text: string }[]) => void } }).__clarifyChat
      c.applyAcpEvents([{ kind: 'message', text: '(澄清回复失败:timeout)' }])
    })
    await page.evaluate(() => {
      const c = (window as unknown as { __clarifyChat: ChatExpose }).__clarifyChat
      c.applyReviewFrame({ event: 'turn_done', nodeId: 'clarify' })
      c.applyQueueState(0, [], false)
    })
    await expect(page.getByTestId('clarify-empty-fail')).toBeVisible()
    await expect(page.getByTestId('clarify-empty-fail-desc')).toContainText('澄清回复失败')
    await expect(page.getByTestId('clarify-empty-fail-retry')).toBeEnabled()
    await page.screenshot({ path: path.join(shotDir, '06-fail-banner-copy.png'), fullPage: true })
  })
})
