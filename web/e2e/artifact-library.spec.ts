import { test, expect } from '@playwright/test'

test.use({ baseURL: 'http://127.0.0.1:5174' })

for (const theme of ['light', 'dark']) for (const width of [1440, 1024, 768, 390]) {
  test(`artifact browser adapts to its canvas (${theme}, ${width})`, async ({ page }) => {
    const errors: string[] = []
    page.on('pageerror', error => errors.push(error.message))
    await page.setViewportSize({ width, height: 900 })
    await page.goto(`/workspace.html?view=/artifacts&theme=${theme}`)
    const rows = page.getByTestId('artifact-list-row')
    await expect(rows).toHaveCount(24)
    await expect(page.getByTestId('artifacts-tree-pane')).toHaveCount(0)
    if (width === 1440) {
      await expect(page.getByTestId('artifact-preview-identity-bar')).toBeVisible()
      expect((await page.getByTestId('artifacts-preview-pane').boundingBox())!.width).toBeGreaterThan(600)
    } else {
      await expect(page.getByTestId('artifacts-preview-pane')).toHaveCount(0)
      await rows.first().click()
      await expect(page.getByTestId('artifact-preview-identity-bar')).toContainText('release-notes.md')
      await expect(page.getByTestId('artifacts-list-pane')).toHaveCount(0)
      const button = page.getByTestId('artifact-preview-download-raw')
      await expect(button).toBeVisible()
      expect(await button.evaluate(el => { const b = el.getBoundingClientRect(); return el.contains(document.elementFromPoint(b.x + b.width / 2, b.y + b.height / 2)) })).toBe(true)
      await page.getByTestId('artifacts-mobile-back').click()
      await expect(rows).toHaveCount(24)
    }
    await page.getByTestId('artifacts-tree-toggle').click()
    const dialog = page.getByRole('dialog', { name: '筛选来源' })
    await expect(dialog.getByRole('searchbox')).toBeFocused()
    await dialog.getByRole('searchbox').fill('代码')
    await expect(dialog.locator('[data-resource-key]')).toHaveCount(1)
    await dialog.locator('[data-resource-key="workflow:wf-2"]').click()
    await expect(page.getByTestId('artifacts-breadcrumb')).toContainText('代码审查')
    await expect(rows).toHaveCount(10)
    await expect(page.getByTestId('artifacts-tree-toggle')).toBeFocused()
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
    expect(errors).toEqual([])
  })
}

test('source chooser supports Escape, empty search and session sources', async ({ page }) => {
  await page.goto('/workspace.html?view=/artifacts')
  const trigger = page.getByTestId('artifacts-tree-toggle')
  await trigger.click()
  const dialog = page.getByRole('dialog', { name: '筛选来源' })
  await dialog.getByRole('searchbox').fill('missing-source')
  await expect(dialog).toContainText('没有匹配的来源')
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(trigger).toBeFocused()
  await trigger.click()
  await dialog.locator('[data-resource-key="session"]').click()
  await expect(page.getByTestId('artifact-list-row')).toHaveCount(2)
  await expect(page.getByTestId('artifacts-breadcrumb')).toContainText('Agent 会话产物')
  await page.getByTestId('artifacts-crumb-root').click()
  await expect(page.getByTestId('artifact-list-row')).toHaveCount(24)
})

test('search restores files and deletion refreshes counts and preview', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/workspace.html?view=/artifacts')
  const search = page.getByRole('searchbox', { name: /搜索产物/ })
  await search.fill('does-not-exist')
  await expect(page.getByTestId('artifact-list-row')).toHaveCount(0)
  await expect(page.getByTestId('artifacts-list-pane')).toContainText('无匹配产物')
  await search.fill('')
  await expect(page.getByTestId('artifact-list-row')).toHaveCount(24)
  await page.getByTestId('artifact-preview-delete').click()
  await page.getByRole('dialog').getByRole('button', { name: '确认删除', exact: true }).click()
  await expect(page.getByTestId('artifacts-total')).toHaveText('23 个文件')
  await expect(page.getByTestId('artifact-list-row')).toHaveCount(23)
  await expect(page.getByTestId('artifact-preview-identity-bar')).toContainText('评审结论')
  await page.getByTestId('artifact-preview-mode-raw').click()
  await expect(page.getByTestId('artifact-preview-raw-json')).toContainText('审批交互已通过回归测试。')
})

for (const state of ['empty', 'fail']) test(`artifact ${state} state is distinct`, async ({ page }) => {
  await page.goto(`/workspace.html?view=/artifacts&artifacts=${state}`)
  if (state === 'empty') {
    await expect(page.getByTestId('artifacts-empty')).toBeVisible()
    await expect(page.getByTestId('artifact-preview')).toHaveCount(0)
  } else {
    await expect(page.getByTestId('artifacts-load-failed')).toBeVisible()
    await page.getByTestId('artifacts-retry').click()
    await expect(page.getByTestId('artifact-list-row')).toHaveCount(24)
  }
})
