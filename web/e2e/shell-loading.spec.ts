import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

for (const theme of ['light', 'dark'] as const) {
  test(`preview boot shell applies ${theme} before the application loads and after reload`, async ({ page }) => {
    const html = readFileSync(new URL('../index.html', import.meta.url), 'utf8')
    const preference = theme === 'light' ? 'dark' : 'light'
    await page.addInitScript(value => localStorage.setItem('grasp-theme', value), preference)
    // Leave the production HTML on screen: the application cannot fix its colors.
    await page.route('**/src/main.ts', route => route.abort())
    await page.route('**/embed/runs/boot-theme/nodes/*/shared-chat', route => route.fulfill({ contentType: 'text/html', body: html }))
    await page.goto(`/embed/runs/boot-theme/nodes/solo/shared-chat#theme=${theme}`)
    const shell = page.locator('[data-boot-shell]')
    await expect(shell).toBeVisible()
    const color = theme === 'light' ? 'rgb(250, 250, 251)' : 'rgb(10, 10, 11)'
    await expect(page.locator('body')).toHaveCSS('background-color', color)
    await expect(page.locator('html')).toHaveCSS('color-scheme', theme)
    expect(await page.evaluate(() => localStorage.getItem('grasp-theme'))).toBe(preference)
    await page.evaluate(() => history.replaceState(null, '', location.pathname))
    await page.reload()
    await expect(shell).toBeVisible()
    await expect(page.locator('body')).toHaveCSS('background-color', color)
    await expect(page.locator('html')).toHaveCSS('color-scheme', theme)
    expect(await page.evaluate(() => localStorage.getItem('grasp-theme'))).toBe(preference)
    await page.goto('/embed/runs/boot-theme/nodes/other/shared-chat')
    const fallback = preference === 'light' ? 'rgb(250, 250, 251)' : 'rgb(10, 10, 11)'
    await expect(page.locator('body')).toHaveCSS('background-color', fallback)
  })
}

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
