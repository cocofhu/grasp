/**
 * Browser acceptance for Run detail load errors: not-found vs network failure.
 */
import { expect, test, type Page } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const shotDir = path.join(__dirname, '..', 'test-results', 'run-load-error')

async function openRunDetail(page: Page, id: string, mode: 'not_found' | 'network') {
  await page.setViewportSize({ width: 1280, height: 900 })
  await page.route('**/api/**', async (route) => {
    // Skip Vite module URLs like /@fs/.../src/lib/api/api.ts (pathname is not /api/...)
    if (!new URL(route.request().url()).pathname.startsWith('/api/')) {
      await route.continue()
      return
    }
    if (mode === 'network') {
      await route.abort('failed')
      return
    }
    const url = new URL(route.request().url())
    if (url.pathname.startsWith('/api/runs/')) {
      await route.fulfill({ status: 404, json: { error: 'not found' } })
      return
    }
    await route.fulfill({ status: 404, json: { error: 'not mocked' } })
  })
  await page.goto(`/run-load-error.html?id=${encodeURIComponent(id)}`)
  await expect(page.getByTestId('run-detail-root')).toBeVisible({ timeout: 15_000 })
  await expect(page.getByTestId('run-load-error')).toBeVisible({ timeout: 15_000 })
}

test.describe('Run detail load errors (browser)', () => {
  test('Run #trigger not-found: copy + grey retry unavailable', async ({ page }) => {
    await openRunDetail(page, 'trigger', 'not_found')

    await expect(page.getByTestId('run-load-error-chip')).toHaveText('不存在')
    await expect(page.getByTestId('run-load-error')).toContainText('找不到该 Run')
    await expect(page.getByTestId('run-load-error')).toContainText('这通常不是网络问题')
    await expect(page.getByTestId('run-load-error')).not.toContainText('请检查网络或确认 Run 是否存在')
    const unavailable = page.getByTestId('run-retry-unavailable')
    await expect(unavailable).toBeVisible()
    await expect(unavailable).toBeDisabled()
    await expect(unavailable).toHaveText('重试不可用')
    await expect(page.getByTestId('run-retry')).toHaveCount(0)

    await page.screenshot({ path: path.join(shotDir, '01-run-trigger-not-found.png'), fullPage: false })
  })

  test('network/server error keeps usable retry', async ({ page }) => {
    await openRunDetail(page, 'run-a1b2c3d4', 'network')

    await expect(page.getByTestId('run-load-error-chip')).toHaveText('加载失败')
    await expect(page.getByTestId('run-load-error')).toContainText('暂时无法加载运行详情')
    await expect(page.getByTestId('run-load-error')).toContainText('网络或服务异常')
    const retry = page.getByTestId('run-retry')
    await expect(retry).toBeVisible()
    await expect(retry).toBeEnabled()
    await expect(page.getByTestId('run-retry-unavailable')).toHaveCount(0)

    await page.screenshot({ path: path.join(shotDir, '02-run-network-error.png'), fullPage: false })
  })
})
