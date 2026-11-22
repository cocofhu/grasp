import { test, expect } from '@playwright/test'

async function vncConnectCount(page: import('@playwright/test').Page): Promise<number> {
  return page.evaluate(async () => {
    const r = await fetch('/__e2e/vnc-connect-count')
    const j = (await r.json()) as { count: number }
    return j.count
  })
}

test.describe('AppPreviewPanel noVNC', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/')
    await expect(page.getByTestId('app-preview')).toBeVisible()
  })

  test('活动预览自动连接并显示已连接', async ({ page }) => {
    await expect(page.getByText('已连接')).toBeVisible({ timeout: 10_000 })
    await expect(page.locator('iframe')).toBeHidden()
    await expect(page.getByRole('button', { name: '取点标注' })).toBeVisible()
  })

  test('Pick 模式返回选择器', async ({ page }) => {
    await expect(page.getByText('已连接')).toBeVisible({ timeout: 10_000 })
    await page.getByRole('button', { name: '取点标注' }).click()
    await expect(page.locator('code', { hasText: '#demo-title' })).toBeVisible({ timeout: 5_000 })
  })

  test('刷新活动预览后仍可连接', async ({ page }) => {
    await expect(page.getByText('已连接')).toBeVisible({ timeout: 10_000 })
    const beforeCount = await vncConnectCount(page)
    await expect(page.getByRole('button', { name: 'API' })).toHaveCount(0)

    await page.reload()
    await expect(page.getByTestId('app-preview')).toBeVisible()
    await expect(page.getByText('已连接')).toBeVisible({ timeout: 10_000 })
    expect(await vncConnectCount(page)).toBeGreaterThanOrEqual(beforeCount)
    await expect(page.getByText('连接失败')).toHaveCount(0)
    await expect(page.getByText('连接中')).toHaveCount(0)
  })

  test('connecting 状态隐藏 FPS', async ({ page }) => {
    await page.goto('/?connectDelay=10000', { waitUntil: 'domcontentloaded' })
    await Promise.all([
      expect(page.getByText('连接中…')).toBeVisible(),
      expect(page.getByText('FPS')).toHaveCount(0),
    ])
    await expect(page.locator('[aria-busy="true"]').first()).toBeVisible()
  })

  test('live 时工具栏显示数值 FPS', async ({ page }) => {
    await expect(page.getByText('已连接')).toBeVisible({ timeout: 10_000 })

    const fps = page.locator('.tabular-nums')
    await expect(fps).toBeVisible()
    await expect(fps.locator('.font-medium')).toHaveText(/^[1-9]\d*$/, { timeout: 5_000 })
    await expect(fps).toContainText('FPS')

    await expect(fps).toBeVisible()
    await expect(fps.locator('.font-medium')).toHaveText(/^[1-9]\d*$/, { timeout: 5_000 })
  })
})
