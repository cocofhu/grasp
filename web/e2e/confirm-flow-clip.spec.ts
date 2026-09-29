import { test, expect } from '@playwright/test'
import { mkdir } from 'node:fs/promises'

const SHOT_DIR = 'test-results/screenshots-confirm-clip'

async function assertButtonFullyInsideHost(page: import('@playwright/test').Page) {
  const host = page.getByTestId('clip-host')
  const btn = page.getByTestId('clarify-confirm-flow')
  await expect(host).toBeVisible()
  await expect(btn).toBeVisible()
  await expect(btn).toContainText('确认并流转')

  const geometry = await page.evaluate(() => {
    const hostEl = document.querySelector('[data-testid="clip-host"]') as HTMLElement | null
    const btnEl = document.querySelector('[data-testid="clarify-confirm-flow"]') as HTMLElement | null
    if (!hostEl || !btnEl) return null
    const hr = hostEl.getBoundingClientRect()
    const br = btnEl.getBoundingClientRect()
    const style = getComputedStyle(btnEl)
    return {
      host: { top: hr.top, bottom: hr.bottom, height: hr.height },
      btn: { top: br.top, bottom: br.bottom, height: br.height, width: br.width },
      btnCssHeight: parseFloat(style.height),
      fullyInside:
        br.top >= hr.top - 0.5 &&
        br.bottom <= hr.bottom + 0.5 &&
        br.height >= 35 /* h-9 ≈ 36px */,
    }
  })

  expect(geometry).toBeTruthy()
  expect(geometry!.btnCssHeight).toBeGreaterThanOrEqual(35)
  expect(geometry!.btn.height).toBeGreaterThanOrEqual(35)
  expect(geometry!.fullyInside).toBe(true)
  // Not truncated to ~14 CSS px as in the bug screenshot.
  expect(geometry!.btn.height).toBeGreaterThan(20)
}

test.describe('确认并流转底栏裁切回归', () => {
  test.beforeAll(async () => {
    await mkdir(SHOT_DIR, { recursive: true })
  })

  test('截图同款：pageControl=offline + 定高宿主，按钮完整在卡片内', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/confirm-flow-clip.html?h=320')
    await expect(page.getByTestId('page-control-status')).toContainText('未连接')
    await expect(page.getByTestId('clarify-hot-actions')).toBeVisible()
    await assertButtonFullyInsideHost(page)
    await page.screenshot({ path: `${SHOT_DIR}/01-offline-status-confirm-full.png` })
  })

  test('确认失败条出现时按钮与错误条都在卡片内', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/confirm-flow-clip.html?h=360&error=1')
    await expect(page.getByTestId('clarify-confirm-error')).toBeVisible()
    await assertButtonFullyInsideHost(page)

    const errInside = await page.evaluate(() => {
      const hostEl = document.querySelector('[data-testid="clip-host"]') as HTMLElement
      const errEl = document.querySelector('[data-testid="clarify-confirm-error"]') as HTMLElement
      const hr = hostEl.getBoundingClientRect()
      const er = errEl.getBoundingClientRect()
      return er.bottom <= hr.bottom + 0.5 && er.top >= hr.top - 0.5 && er.height > 10
    })
    expect(errInside).toBe(true)
    await page.screenshot({ path: `${SHOT_DIR}/02-confirm-error-bar-full.png` })
  })

  test('输入区变高时消息滚动，按钮仍完整', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/confirm-flow-clip.html?h=380&tall=1')
    await assertButtonFullyInsideHost(page)
    const scroller = page.getByTestId('clarify-scroller')
    await expect(scroller).toBeVisible()
    // Scroller should be the flex grow / scroll region.
    const overflowY = await scroller.evaluate((el) => getComputedStyle(el).overflowY)
    expect(overflowY).toMatch(/auto|scroll/)
    await page.screenshot({ path: `${SHOT_DIR}/03-tall-input-confirm-full.png` })
  })

  test('冷会话确认按钮仍完整可见', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/confirm-flow-clip.html?h=280&cold=1')
    await expect(page.getByTestId('clarify-cold-actions')).toBeVisible()
    await expect(page.getByTestId('clarify-hot-actions')).toHaveCount(0)
    await assertButtonFullyInsideHost(page)
    await page.screenshot({ path: `${SHOT_DIR}/04-cold-session-confirm-full.png` })
  })
})
