import { test, expect } from '@playwright/test'

test.use({ baseURL: 'http://127.0.0.1:5174' })

test('artifact cold loading shows skeletons without an empty-state flash', async ({ page }) => {
  await page.goto('/artifacts-page-loading.html?scenario=pending&delay=2000')
  await expect(page.getByTestId('artifacts-surface')).toHaveAttribute('aria-busy', 'true')
  await expect(page.getByTestId('artifacts-list-skeleton')).toBeVisible()
  await expect(page.getByTestId('artifacts-empty')).toHaveCount(0)
  await expect(page.getByTestId('artifacts-surface')).toHaveAttribute('aria-busy', 'false')
  await expect(page.getByTestId('artifacts-list-pane').getByText('notes.md', { exact: true })).toBeVisible()
})

test('artifact source loading failure has a direct retry', async ({ page }) => {
  await page.goto('/artifacts-page-loading.html?scenario=fail')
  await expect(page.getByTestId('artifacts-load-failed')).toBeVisible()
  await expect(page.getByTestId('artifacts-empty')).toHaveCount(0)
  await page.getByTestId('artifacts-retry').click()
  await expect(page.getByTestId('artifacts-list-pane').getByText('notes.md', { exact: true })).toBeVisible()
  await expect(page.getByTestId('artifacts-load-failed')).toHaveCount(0)
})

test('source switch clears unrelated cached files while loading', async ({ page }) => {
  await page.goto('/artifacts-page-loading.html?scenario=refresh&delay=2500')
  const file = page.getByTestId('artifacts-list-pane').getByText('notes.md', { exact: true })
  await expect(file).toBeVisible()
  await page.getByTestId('artifacts-tree-toggle').click()
  await page.locator('[data-resource-key="workflow:wf-2"]').click()
  await expect(page.getByTestId('artifacts-list-skeleton')).toBeVisible()
  await expect(file).toHaveCount(0)
  await expect(page.getByTestId('artifacts-surface')).toHaveAttribute('aria-busy', 'true')
  await expect(page.getByTestId('artifacts-surface')).toHaveAttribute('aria-busy', 'false')
  await expect(page.getByTestId('artifacts-breadcrumb')).toContainText('Review')
})
