import { test, expect } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const shotDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../.tmp-clarify-ux-shots')

async function sendClarify(page: import('@playwright/test').Page, text: string) {
  const box = page.locator('[data-testid="clarify-input"]')
  await box.fill(text)
  await page.getByTestId('clarify-send-label').click()
}

test.describe('ClarifyChat 非 reviewMode 澄清会话', () => {
  test('排队 → 流式 → Cancel 保留队列 → 刷新续传', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })
    await page.goto('/clarify-session-ux.html')
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })

    await sendClarify(page, '澄清意见甲（队列首条）')
    await sendClarify(page, '澄清意见乙（仍在队列）')
    const queue = page.getByTestId('clarify-review-queue')
    await expect(queue).toBeVisible()
    await expect(queue).toContainText('待发送队列')
    await expect(queue).toContainText('澄清意见甲')
    await expect(queue).toContainText('澄清意见乙')
    await expect(page.getByTestId('clarify-review-cancel')).toBeVisible()
    await expect(page.getByTestId('clarify-send-label')).toHaveCount(0)
    await expect(page.getByTestId('clarify-review-queue')).toBeVisible()
    await page.screenshot({ path: path.join(shotDir, '01-clarify-queue.png'), fullPage: true })

    await page.getByTestId('sim-turn-stream').click()
    await expect(page.getByText('流式产出正文（非整轮一次性）。')).toBeVisible()
    // After real pump order, live human must be 甲 (not blind-shifted 乙).
    const scrollerAfterBegin = page.getByTestId('clarify-scroller')
    await expect(scrollerAfterBegin).toContainText('澄清意见甲（队列首条）')
    const beginText = await scrollerAfterBegin.innerText()
    expect(beginText.match(/澄清意见甲（队列首条）/g)?.length ?? 0).toBe(1)
    expect(beginText).not.toContain('澄清意见乙（仍在队列）')
    await expect(queue).toContainText('澄清意见乙')
    await expect(queue).not.toContainText('澄清意见甲')
    // Host busy gate: react mid-turn must skip full refresh.
    await expect(page.getByTestId('host-skip-refresh')).toContainText('hostSkip=1')
    await page.screenshot({ path: path.join(shotDir, '02-clarify-stream.png'), fullPage: true })

    // Demo Cancel: keep queue (乙 remains), mark interrupted.
    await page.getByTestId('clarify-review-cancel').click()
    await expect(page.getByTestId('clarify-interrupted')).toBeVisible()
    await expect(page.getByTestId('clarify-review-queue')).toContainText('澄清意见乙')
    await page.screenshot({ path: path.join(shotDir, '03-clarify-cancel-keep.png'), fullPage: true })

    // Refresh resume: busy + activeItem + continued stream.
    await page.getByTestId('sim-refresh-resume').click()
    await expect(page.getByText('刷新后续上的流式正文。')).toBeVisible()
    await expect(page.getByTestId('clarify-review-queue')).toContainText('澄清意见乙')
    await page.screenshot({ path: path.join(shotDir, '04-clarify-refresh-resume.png'), fullPage: true })
  })

  test('真实泵帧序：queue_state→turn_begin 双消息不踩踏', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })
    await page.goto('/clarify-session-ux.html')
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })

    await sendClarify(page, '澄清意见甲（队列首条）')
    await sendClarify(page, '澄清意见乙（仍在队列）')
    await page.getByTestId('sim-pump-order-dual').click()

    const scroller = page.getByTestId('clarify-scroller')
    await expect(scroller).toContainText('澄清意见甲（队列首条）')
    const text = await scroller.innerText()
    expect(text.match(/澄清意见甲（队列首条）/g)?.length ?? 0).toBe(1)
    expect(text).not.toContain('澄清意见乙（仍在队列）')
    const queue = page.getByTestId('clarify-review-queue')
    await expect(queue).toContainText('澄清意见乙（仍在队列）')
    await expect(queue).not.toContainText('澄清意见甲')
    await page.screenshot({ path: path.join(shotDir, '06-clarify-pump-order.png'), fullPage: true })
  })

  test('transcript 追上 + live streaming 不双显 human', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })
    await page.goto('/clarify-session-ux.html')
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })

    await page.getByTestId('sim-transcript-catchup').click()
    await expect(page.getByText('流式产出正文（非整轮一次性）。')).toBeVisible()
    const scroller = page.getByTestId('clarify-scroller')
    const text = await scroller.innerText()
    const matches = text.match(/澄清意见甲（队列首条）/g) || []
    expect(matches.length).toBe(1)
    await expect(page.getByTestId('host-skip-refresh')).toContainText(/hostSkip=[1-9]/)
    await page.screenshot({ path: path.join(shotDir, '05-clarify-no-dup-human.png'), fullPage: true })
  })

  test('硬刷新 ACP 竞态：延迟 seed 恢复 thought/message 并续流', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })
    await page.goto('/clarify-session-ux.html')
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })

    await page.getByTestId('sim-hard-refresh-acp-race').click()
    await expect(page.getByTestId('clarify-thought')).toContainText('竞态恢复的思考', {
      timeout: 5_000,
    })
    await expect(page.getByText('竞态恢复的正文续流。继续追加。')).toBeVisible()
    await expect(page.getByTestId('clarify-busy-status')).toContainText('输出中')
    await expect(page.getByTestId('clarify-stream-caret')).toBeVisible()
    await page.screenshot({ path: path.join(shotDir, '07-clarify-hard-refresh-acp-race.png'), fullPage: true })
  })

  test('四阶段完成态：输出中三件套消失 + 已完成脚注', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })
    await page.goto('/clarify-session-ux.html')
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })

    await page.getByTestId('sim-four-phase-complete').click()
    await expect(page.getByText('四阶段正文输出。')).toBeVisible()
    await expect(page.getByTestId('clarify-busy-status')).toHaveCount(0)
    await expect(page.getByTestId('clarify-stream-caret')).toHaveCount(0)
    await expect(page.getByTestId('clarify-turn-completed')).toContainText('已完成')
    // Thought default-collapsed after message / done.
    await expect(page.getByTestId('clarify-thought')).not.toHaveAttribute('open')
    await page.screenshot({ path: path.join(shotDir, '08-clarify-four-phase-done.png'), fullPage: true })
  })
})
