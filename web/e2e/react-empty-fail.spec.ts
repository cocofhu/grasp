import { test, expect } from '@playwright/test'
import fs from 'node:fs'
import path from 'node:path'

const shotDir = '/tmp/react-empty-fail-shots'
fs.mkdirSync(shotDir, { recursive: true })

type Page = import('@playwright/test').Page
type Outcome = 'ok' | 'empty' | 'failed' | 'interrupted'
/** The harness's fake server (clarify-session-ux-main.ts). */
type Server = {
  start: (text: string) => void
  stream: (reply: string, thought?: string) => void
  end: (outcome: Outcome, error?: string) => void
}

function server(page: Page) {
  const call = (fn: keyof Server, ...args: string[]) =>
    page.evaluate(([f, a]) => {
      const s = (window as unknown as { __server: Record<string, (...x: string[]) => void> }).__server
      s[f as string]!(...(a as string[]))
    }, [fn, args] as const)
  return {
    start: (text: string) => call('start', text),
    stream: (reply: string) => call('stream', reply),
    end: (outcome: Outcome, error?: string) => call('end', outcome, ...(error ? [error] : [])),
  }
}

async function openHarness(page: Page) {
  await page.setViewportSize({ width: 1100, height: 900 })
  await page.goto('/clarify-session-ux.html')
  await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })
}

/** The server ends a turn with no output: outcome empty. */
async function pumpEmptyFail(page: Page, text: string) {
  const s = server(page)
  await s.start(text)
  await s.end('empty')
}

test.describe('ReAct empty-fail retry (ClarifyChat harness, server outcome)', () => {
  test('empty outcome shows fail card + retry; retry does not duplicate human', async ({ page }) => {
    await openHarness(page)
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

  test('a new send supersedes the failed turn: its retry goes away', async ({ page }) => {
    await openHarness(page)
    await pumpEmptyFail(page, '做登录')
    await expect(page.getByTestId('clarify-empty-fail-retry')).toBeEnabled()
    await page.locator('[data-testid="clarify-input"]').fill('下一条待发送')
    await page.getByTestId('clarify-send-label').click()
    await expect(page.getByTestId('clarify-review-queue')).toBeVisible()
    await expect(page.getByTestId('clarify-empty-fail')).toBeVisible()
    await expect(page.getByTestId('clarify-empty-fail-retry')).toHaveCount(0)
    await page.screenshot({ path: path.join(shotDir, '03-retry-disabled-when-queued.png'), fullPage: true })
  })

  test('running output is visible; ok has no retry; interrupt is not empty-fail', async ({ page }) => {
    await openHarness(page)
    const s = server(page)
    await s.start('成功轮')
    await s.stream('好的，开始对齐。')
    await expect(page.getByText('好的，开始对齐。')).toBeVisible()
    await expect(page.getByTestId('clarify-stream-caret')).toBeVisible()
    await s.end('ok')
    await expect(page.getByText('好的，开始对齐。')).toBeVisible()
    await expect(page.getByTestId('clarify-turn-completed')).toHaveCount(0)
    await expect(page.getByTestId('clarify-empty-fail')).toHaveCount(0)
    await expect(page.getByTestId('clarify-empty-fail-retry')).toHaveCount(0)
    await page.screenshot({ path: path.join(shotDir, '04-success-no-retry.png'), fullPage: true })

    await s.start('取消这一轮')
    // Stop: the harness server ends the running turn as interrupted.
    await page.getByTestId('clarify-review-cancel').click()
    const scroller = page.getByTestId('clarify-scroller')
    await expect(page.getByTestId('clarify-interrupted')).toBeVisible()
    await expect(page.getByTestId('clarify-interrupted')).toContainText(/已中断|interrupted/i)
    await expect(scroller).toContainText('(已中断)')
    await expect(page.getByTestId('clarify-empty-fail')).toHaveCount(0)
    await expect(page.getByTestId('clarify-empty-fail-retry')).toHaveCount(0)
    await page.screenshot({ path: path.join(shotDir, '05-interrupted-not-empty-fail.png'), fullPage: true })
  })

  test('a failed outcome shows the server error on the card', async ({ page }) => {
    await openHarness(page)
    const s = server(page)
    await s.start('做登录')
    await s.end('failed', 'timeout')
    await expect(page.getByTestId('clarify-empty-fail')).toBeVisible()
    await expect(page.getByTestId('clarify-empty-fail-title')).toHaveText('这一轮没有完成')
    await expect(page.getByTestId('clarify-empty-fail-desc')).toHaveText('timeout')
    await expect(page.getByTestId('clarify-empty-fail-retry')).toBeEnabled()
    await page.screenshot({ path: path.join(shotDir, '06-fail-banner-copy.png'), fullPage: true })
  })
})
