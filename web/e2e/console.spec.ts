import { test, expect } from '@playwright/test'

async function vncConnectCount(page: import('@playwright/test').Page): Promise<number> {
  return page.evaluate(async () => {
    const r = await fetch('/__e2e/vnc-connect-count')
    const j = (await r.json()) as { count: number }
    return j.count
  })
}

const TAB_ORDER = ['终端', 'IDE', 'Agent', '浏览器', '日志'] as const

test.describe('SandboxConsole Tab / noVNC', () => {
  test('顶栏顺序 terminal|ide|acp|novnc|log 且仅一个 Agent', async ({ page }) => {
    await page.goto('/console.html')
    const tabs = page.locator('.ml-4.flex.gap-1 button')
    await expect(tabs).toHaveCount(5)
    for (let i = 0; i < TAB_ORDER.length; i++) {
      await expect(tabs.nth(i)).toContainText(TAB_ORDER[i])
    }
    await expect(page.getByRole('button', { name: /^Agent$/ })).toHaveCount(1)
    await expect(page.getByRole('button', { name: /ACP 原生|ACP Native/ })).toHaveCount(0)
  })

  test('?tab=acp 进入 ACP bridge', async ({ page }) => {
    await page.goto('/console.html?tab=acp')
    await expect(page.getByRole('button', { name: /^Agent$/ })).toHaveClass(/bg-accent-dim/)
    await expect(page.locator('iframe[title="ACP bridge"]')).toBeVisible()
  })

  test('sandboxId 精简工具栏无 Pick', async ({ page }) => {
    await page.goto('/console.html?tab=novnc')
    await expect(page.getByPlaceholder('about:blank')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByRole('button', { name: '打开' })).toBeVisible()
    await expect(page.getByRole('button', { name: '取点标注' })).toHaveCount(0)
    await expect(page.getByText('FPS')).toHaveCount(0)
  })

  test('切 Tab 保持 noVNC 连接计数', async ({ page }) => {
    await page.goto('/console.html?tab=novnc')
    await expect(page.getByPlaceholder('about:blank')).toBeVisible({ timeout: 10_000 })
    // Wait until connecting overlay is gone (ready / live).
    await expect(page.getByText('正在检测浏览器组件…')).toHaveCount(0, { timeout: 10_000 })
    const before = await vncConnectCount(page)
    expect(before).toBeGreaterThan(0)

    await page.getByRole('button', { name: '终端' }).click()
    await page.getByRole('button', { name: /^浏览器$/ }).click()
    await expect(page.getByPlaceholder('about:blank')).toBeVisible()
    const after = await vncConnectCount(page)
    expect(after).toBe(before)
  })

  test('连接失败展示 empty-state', async ({ page }) => {
    await page.goto('/console.html?tab=novnc&vncFail=1')
    await expect(page.getByText('未启动浏览器组件')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText(/未启用 VNC 预览|浏览器桌面启动失败/)).toBeVisible()
    // Auto-reconnect hides the button until retries finish. Either state is the empty state.
    const reconnecting = page.getByText(/正在重新连接/)
    const reconnect = page.getByRole('button', { name: '重新连接' })
    await expect(reconnecting.or(reconnect)).toBeVisible()
  })

  test('首次进入连接中加载态', async ({ page }) => {
    await page.goto('/console.html?tab=novnc&connectDelay=10000', { waitUntil: 'domcontentloaded' })
    await expect(page.getByText('正在检测浏览器组件…')).toBeVisible({ timeout: 5_000 })
  })
})
