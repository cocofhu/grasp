import { test, expect } from '@playwright/test'

test.describe('shell loading infra', () => {
  test('route switch succeeds with non-empty main', async ({ page }) => {
    await page.goto('/shell-loading.html')
    await expect(page.getByTestId('shell-main-dashboard')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('shell-main-dashboard')).toContainText('工作台内容')
    await page.getByTestId('go-runs').click()
    await expect(page.getByTestId('shell-main-runs')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('shell-main-runs')).not.toHaveText(/^$/)
    await expect(page.getByTestId('shell-main-runs')).toContainText('运行列表')
  })

  test('load failure can retry and recover', async ({ page }) => {
    await page.goto('/shell-loading.html?scene=fail-runs&start=runs')
    await expect(page.getByTestId('app-inline-error')).toBeVisible({ timeout: 10_000 })
    await page.getByTestId('app-inline-error-retry').click()
    await expect(page.getByTestId('shell-main-runs')).toContainText('已恢复')
  })
})

test('a preview frame never mounts the workspace or reads the login session', async ({ page }) => {
  await page.addInitScript(() => {
    if (window.self === window.top) return
    new MutationObserver(() => {
      if (document.querySelector('[data-testid="app-shell-workspace"], [data-testid="app-shell-full"]')) {
        document.documentElement.dataset.workspaceSeen = 'true'
      }
    }).observe(document, { childList: true, subtree: true })
  })
  await page.goto('/shell-loading.html')
  await page.evaluate(() => {
    for (const mode of ['preview-frame', 'workspace-frame']) {
      const frame = document.createElement('iframe')
      frame.id = mode
      frame.src = '/shell-loading.html?start=' + mode
      document.body.appendChild(frame)
    }
  })
  const preview = page.frameLocator('#preview-frame')
  await expect(preview.getByTestId('preview-frame-content')).toBeVisible()
  for (const frame of [preview, page.frameLocator('#workspace-frame')]) {
    const root = frame.locator('html')
    await expect(root).toHaveAttribute('data-navigation-settled', 'true')
    await expect(root).not.toHaveAttribute('data-workspace-seen', 'true')
    await expect(root).not.toHaveAttribute('data-auth-requested', 'true')
    await expect(frame.getByTestId('shell-main-dashboard')).toHaveCount(0)
  }
})
