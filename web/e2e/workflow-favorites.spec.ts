import { test, expect, type Page } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const shotDir = path.join(__dirname, '..', '..', '.tmp-test-shots')

async function expectNoQuickWorkflows(page: Page) {
  const sidebar = page.getByTestId('app-desktop-sidebar')
  await expect(sidebar).toBeVisible({ timeout: 15_000 })
  await expect(page.getByTestId('app-sidebar-nav')).toBeVisible()
  await expect(page.getByTestId('nav-quick-workflows')).toHaveCount(0)
  await expect(page.getByTestId('nav-quick-workflow-item')).toHaveCount(0)
  await expect(sidebar.getByText('快捷工作流')).toHaveCount(0)
  const rail = page.getByTestId('sidebar-app-rail')
  await expect(rail.getByRole('link', { name: '新需求' })).toBeVisible()
  await expect(rail.getByRole('link', { name: '看板' })).toBeVisible()
  await expect(rail.getByRole('link', { name: '工作流' })).toBeVisible()
  await expect(rail.getByRole('link', { name: '设置' })).toBeVisible()
  await expect(page.getByTestId('sidebar-recent-heading')).toBeVisible()
}

test.describe('workflow favorites stay off the sidebar', () => {
  test('empty sidebar has primary nav and history, and no quick workflows', async ({ page }) => {
    await page.goto('/workflow-favorites.html?scene=empty')
    await expectNoQuickWorkflows(page)
    await expect(page.getByTestId('nav-pending-approvals')).toHaveCount(0)
    await page.screenshot({ path: path.join(shotDir, 'fav-empty.png'), fullPage: true })
  })

  test('stored favorites do not appear in the sidebar', async ({ page }) => {
    await page.goto('/workflow-favorites.html?scene=with-items')
    await expectNoQuickWorkflows(page)
    const sidebar = page.getByTestId('app-desktop-sidebar')
    await expect(sidebar).not.toContainText('夜间回归')
    await expect(sidebar).not.toContainText('热修回滚')
    await expect(sidebar).not.toContainText('发布预检')
    await page.screenshot({ path: path.join(shotDir, 'fav-stored-off-sidebar.png'), fullPage: true })
  })

  test('favoriting toasts the name and does not pin it to the sidebar', async ({ page }) => {
    await page.goto('/workflow-favorites.html?scene=empty')
    await expectNoQuickWorkflows(page)
    await page.getByTestId('demo-favorite-extra').click()
    const toast = page.getByTestId('toast-host')
    await expect(toast).toContainText('已收藏「金丝雀发布」', { timeout: 5_000 })
    await expect(toast).not.toContainText('已置顶到侧栏')
    await expect(toast).not.toContainText('置顶')
    await expect(toast).not.toContainText('侧栏')
    await expectNoQuickWorkflows(page)
    await expect(page.getByTestId('app-desktop-sidebar')).not.toContainText('金丝雀发布')
    await page.screenshot({ path: path.join(shotDir, 'fav-added-toast.png'), fullPage: true })
  })

  test('unfavorite toast does not mention the sidebar', async ({ page }) => {
    await page.goto('/workflow-favorites.html?scene=with-items')
    await expectNoQuickWorkflows(page)
    await page.getByTestId('demo-favorite-night').click()
    const toast = page.getByTestId('toast-host')
    await expect(toast).toContainText('已取消收藏「夜间回归」', { timeout: 5_000 })
    await expect(toast).not.toContainText('侧栏')
    await expect(toast).not.toContainText('置顶')
    await expectNoQuickWorkflows(page)
    await page.screenshot({ path: path.join(shotDir, 'fav-unfavorite.png'), fullPage: true })
  })

  test('ninth favorite is rejected without a shortcut or sidebar toast', async ({ page }) => {
    await page.goto('/workflow-favorites.html?scene=full')
    await expectNoQuickWorkflows(page)
    await page.getByTestId('demo-favorite-extra').click()
    const toast = page.getByTestId('toast-host')
    await expect(toast).toContainText('已达 8 条上限，请先取消其它收藏', { timeout: 5_000 })
    await expect(toast).not.toContainText('快捷')
    await expect(toast).not.toContainText('已置顶到侧栏')
    await expect(toast).not.toContainText('置顶')
    await expect(page.getByTestId('nav-quick-workflow-item')).toHaveCount(0)
    await expect(page.getByTestId('app-desktop-sidebar')).not.toContainText('金丝雀发布')
    await page.screenshot({ path: path.join(shotDir, 'fav-full-toast.png'), fullPage: true })
  })

  test('a missing favorite is not inserted into the sidebar', async ({ page }) => {
    await page.goto('/workflow-favorites.html?scene=gone')
    await expectNoQuickWorkflows(page)
    const sidebar = page.getByTestId('app-desktop-sidebar')
    await expect(sidebar).not.toContainText('wf-missing')
    await expect(sidebar).not.toContainText('夜间回归')
    await page.screenshot({ path: path.join(shotDir, 'fav-gone-strip.png'), fullPage: true })
  })
})
