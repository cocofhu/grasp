import { test, expect } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const shotDir = path.join(__dirname, '..', '..', '.tmp-test-shots')

test.describe('workflow favorites quick-launch', () => {
  test('empty state hides the quick-workflows section entirely', async ({ page }) => {
    await page.goto('/workflow-favorites.html?scene=empty')
    await expect(page.getByTestId('app-sidebar-nav')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('nav-quick-workflows')).toHaveCount(0)
    await expect(page.getByText('快捷工作流')).toHaveCount(0)
    // Primary nav still present (notifications before config)
    await expect(page.getByRole('link', { name: '通知' })).toBeVisible()
    await expect(page.getByRole('link', { name: '设置' })).toBeVisible()
    await page.screenshot({ path: path.join(shotDir, 'fav-empty.png'), fullPage: true })
  })

  test('sidebar lists favorites with project subtitle and draft badge; launch opens modal', async ({
    page,
  }) => {
    await page.goto('/workflow-favorites.html?scene=with-items')
    await expect(page.getByTestId('nav-quick-workflow-item').first()).toBeVisible({ timeout: 15_000 })
    const items = page.getByTestId('nav-quick-workflow-item')
    await expect(items).toHaveCount(3)
    // Legacy favorites migrate once to newest-first as the initial manual order.
    await expect(items.nth(0)).toContainText('夜间回归')
    await expect(items.nth(0)).toContainText('checkout-service')
    await expect(items.nth(0)).toContainText('草稿')
    await expect(items.nth(1)).toContainText('热修回滚')
    await expect(items.nth(2)).toContainText('发布预检')
    await expect(items.nth(2)).toContainText('billing-api')

    const before = page.url()
    await items.nth(0).click()
    await expect(page.getByText('启动运行 · 夜间回归')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText('分支')).toBeVisible()
    // Route must not be forced to project/editor
    expect(page.url()).toBe(before)
    await page.screenshot({ path: path.join(shotDir, 'fav-launch-modal.png'), fullPage: true })
  })

  test('sidebar star unfavorites without opening launch modal', async ({ page }) => {
    await page.goto('/workflow-favorites.html?scene=with-items')
    await expect(page.getByTestId('nav-quick-workflow-item').first()).toBeVisible({ timeout: 15_000 })
    await page.getByTestId('nav-quick-workflow-unfavorite').first().click()
    await expect(page.getByText('启动运行 ·')).toHaveCount(0)
    await expect(page.getByTestId('nav-quick-workflow-item')).toHaveCount(2)
    await expect(page.getByTestId('nav-quick-workflow-item').first()).not.toContainText('夜间回归')
    await page.screenshot({ path: path.join(shotDir, 'fav-unfavorite.png'), fullPage: true })
  })

  test('desktop handle pointer drag reorders and survives reload without a toast', async ({ page }) => {
    await page.goto('/workflow-favorites.html?scene=with-items')
    const items = page.getByTestId('nav-quick-workflow-item')
    await expect(items).toHaveCount(3)
    const handles = page.getByTestId('nav-quick-workflow-drag-handle')
    await expect(handles).toHaveCount(3)
    const firstBox = await handles.nth(0).boundingBox()
    const lastBox = await handles.nth(2).boundingBox()
    if (!firstBox || !lastBox) throw new Error('quick-workflow drag handles are not visible')

    await page.mouse.move(firstBox.x + firstBox.width / 2, firstBox.y + firstBox.height / 2)
    await page.mouse.down()
    await page.mouse.move(lastBox.x + lastBox.width / 2, lastBox.y + lastBox.height + 12, { steps: 4 })
    await expect(page.getByTestId('nav-quick-workflow-placeholder')).toBeVisible()
    await page.mouse.up()

    await expect(items.nth(0)).toContainText('热修回滚')
    await expect(items.nth(2)).toContainText('夜间回归')
    await expect(page.getByTestId('toast-host')).not.toContainText('已调整')
    await page.reload()
    await expect(page.getByTestId('nav-quick-workflow-item').nth(2)).toContainText('夜间回归')
    await page.screenshot({ path: path.join(shotDir, 'fav-reorder.png'), fullPage: true })
  })

  test('no-ask fields still open confirm modal from quick item', async ({ page }) => {
    await page.goto('/workflow-favorites.html?scene=with-items')
    await expect(page.getByTestId('nav-quick-workflow-item').nth(1)).toBeVisible({ timeout: 15_000 })
    await page.getByTestId('nav-quick-workflow-item').nth(1).click() // 热修回滚 — no ask fields
    await expect(page.getByText('启动运行 · 热修回滚')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText(/未声明任何输入字段|空参数启动/)).toBeVisible()
    await page.screenshot({ path: path.join(shotDir, 'fav-launch-no-params.png'), fullPage: true })
  })

  test('full limit rejects 9th favorite with toast', async ({ page }) => {
    await page.goto('/workflow-favorites.html?scene=full')
    await expect(page.getByTestId('nav-quick-workflow-item').first()).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('nav-quick-workflow-item')).toHaveCount(8)
    await page.getByTestId('demo-favorite-extra').click()
    await expect(page.getByTestId('toast-host')).toContainText('已达 8 条上限', { timeout: 5_000 })
    await expect(page.getByTestId('nav-quick-workflow-item')).toHaveCount(8)
    await page.screenshot({ path: path.join(shotDir, 'fav-full-toast.png'), fullPage: true })
  })

  test('404 favorite is silently stripped on hydrate', async ({ page }) => {
    await page.goto('/workflow-favorites.html?scene=gone')
    await expect(page.getByTestId('nav-quick-workflow-item')).toHaveCount(1, { timeout: 15_000 })
    await expect(page.getByTestId('nav-quick-workflow-item')).toContainText('夜间回归')
    await expect(page.getByTestId('nav-quick-workflows')).not.toContainText('wf-missing')
    // No dedicated "unavailable" toast required — toast host should not warn about strip
    await page.screenshot({ path: path.join(shotDir, 'fav-gone-strip.png'), fullPage: true })
  })
})
