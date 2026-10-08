import { test, expect } from '@playwright/test'
import { mkdirSync } from 'node:fs'

const SHOT_DIR = '/tmp/approving-test-screenshots'

test.beforeAll(() => {
  mkdirSync(SHOT_DIR, { recursive: true })
})

test.describe('Sandbox detail CDP/noVNC isolation', () => {
  test('zh-CN detail hides direct cdp/novnc and shows proxy + info notice', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.goto('/sandbox-list.html')
    await expect(page.getByText('e2e-sandbox')).toBeVisible({ timeout: 10_000 })
    await page.screenshot({ path: `${SHOT_DIR}/01-sandbox-list.png`, fullPage: true })

    await page.getByRole('button', { name: /详情/ }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible({ timeout: 10_000 })
    await expect(dialog.getByText('CDP / noVNC 不提供直连')).toBeVisible()
    await expect(dialog.getByTestId('sandbox-endpoints-notice')).toBeVisible()
    await expect(dialog.getByText('/sandbox-vnc/42/ws')).toBeVisible()
    await expect(dialog.getByText('/sandbox/42')).toBeVisible()
    await expect(dialog.getByText('/sandbox-bridge/42')).toBeVisible()
    await expect(dialog.getByTestId('sandbox-vnc-open-preview')).toBeVisible()
    await expect(dialog.getByText('10.8.2.14:30201')).toBeVisible()
    await expect(dialog.getByText('10.8.2.14:30202')).toBeVisible()
    await expect(dialog.getByText('10.8.2.14:30222')).toBeVisible()

    const body = await page.locator('body').innerText()
    expect(body).not.toMatch(/10\.8\.2\.14:30203/)
    expect(body).not.toMatch(/10\.8\.2\.14:30204/)
    expect(body).not.toMatch(/10\.8\.2\.14:9222/)
    expect(body).not.toMatch(/10\.8\.2\.14:6080/)
    expect(body).not.toContain('10.8.2.14:30880')

    await page.screenshot({ path: `${SHOT_DIR}/02-sandbox-detail-zh.png`, fullPage: true })
  })

  test('open preview goes to sandbox console noVNC, not preview-vnc or :6080', async ({ page }) => {
    await page.goto('/sandbox-list.html')
    await expect(page.getByText('e2e-sandbox')).toBeVisible({ timeout: 10_000 })
    await page.getByRole('button', { name: /详情/ }).click()
    await expect(page.getByTestId('sandbox-vnc-open-preview')).toBeVisible({ timeout: 10_000 })
    await page.getByTestId('sandbox-vnc-open-preview').click()
    const stub = page.getByTestId('sandbox-console-stub')
    await expect(stub).toBeVisible({ timeout: 10_000 })
    await expect(stub).toContainText('path:/sandboxes/42/console')
    await expect(stub).toContainText('tab:novnc')
    await expect(page.locator('body')).not.toContainText('preview-vnc')
    await expect(page.locator('body')).not.toContainText(':6080')
    await page.screenshot({ path: `${SHOT_DIR}/03-open-preview-console.png`, fullPage: true })
  })

  test('en notice covers proxy-only CDP/noVNC and session/ide/ssh auth', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.goto('/sandbox-list.html?lang=en')
    await expect(page.getByText('e2e-sandbox')).toBeVisible({ timeout: 10_000 })
    await page.getByRole('button', { name: /Details/i }).click()
    const notice = page.getByTestId('sandbox-endpoints-notice')
    await expect(notice).toBeVisible({ timeout: 10_000 })
    await expect(notice).toContainText(/CDP \/ noVNC are not directly reachable/i)
    await expect(notice).toContainText('/sandbox-vnc/:sandboxId/ws')
    await expect(notice).not.toContainText('/preview-vnc/')
    await expect(notice).toContainText(/Session required/i)
    await page.screenshot({ path: `${SHOT_DIR}/04-sandbox-detail-en.png`, fullPage: true })
  })
})
