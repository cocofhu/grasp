import { expect, test, type Page } from '@playwright/test'

/** Wait for /auth/me to paint; prefs must hydrate on auth settle (no focus/poll required). */
async function settleAuth(page: Page) {
  await expect(page.getByText('e2e', { exact: true })).toBeVisible({ timeout: 15_000 })
}

test.describe('shell notification center (IA separation)', () => {
  test('dropdown titled 通知; empty has no clickable runs escape', async ({ page }) => {
    await page.goto('/notifications-center.html?scene=empty')
    await expect(page.getByTestId('shell-main-dashboard')).toBeVisible({ timeout: 15_000 })
    await settleAuth(page)

    const bell = page.getByTestId('run-notifications-bell')
    await expect(bell).toHaveAttribute('aria-label', '通知')
    await expect(page.getByTestId('run-notifications-badge')).toHaveCount(0)

    await bell.click()
    const panel = page.getByTestId('run-notifications-panel')
    await expect(panel).toBeVisible()
    await expect(panel.getByRole('heading', { name: '通知' })).toBeVisible()
    await expect(panel).not.toContainText('运行通知')

    const empty = page.getByTestId('run-notifications-empty')
    await expect(empty).toContainText('暂无通知')
    await expect(empty).toContainText('执行完成或失败后才会出现')
    await expect(empty).toContainText('运行')
    await expect(empty.locator('a')).toHaveCount(0)
    await expect(empty.locator('button')).toHaveCount(0)
  })

  test('history-only enable: badge not inventory; list items are read with before-baseline label', async ({
    page,
  }) => {
    await page.goto('/notifications-center.html?scene=history-only&start=notifications')
    await expect(page.getByTestId('notifications-page')).toBeVisible({ timeout: 15_000 })
    await settleAuth(page)
    await expect(page.getByTestId('run-notifications-badge')).toHaveCount(0)
    await expect(page.getByTestId('nav-notifications-badge')).toHaveCount(0)
    const items = page.getByTestId('notifications-item')
    await expect(items).toHaveCount(2)
    for (const el of await items.all()) {
      await expect(el).toHaveAttribute('data-unread', 'false')
      await expect(el).toHaveAttribute('data-before-baseline', 'true')
    }
    await expect(page.getByTestId('notifications-before-baseline').first()).toContainText(
      '基线前·不计未读',
    )
  })

  test('cleans noisy titles; view-all and sidebar land on /notifications; failed goes to detail', async ({
    page,
  }) => {
    await page.goto('/notifications-center.html?scene=with-items')
    await expect(page.getByTestId('shell-main-dashboard')).toBeVisible({ timeout: 15_000 })
    await settleAuth(page)

    const badge = page.getByTestId('run-notifications-badge')
    await expect(badge).toBeVisible()
    await expect(badge).toHaveText('3')
    // Workspace chrome: unread stays on the bell, not a primary 通知 item (plan g2.1).
    await expect(page.getByTestId('nav-workspace-chrome')).toBeVisible()
    await expect(page.getByTestId('nav-notifications-badge')).toHaveCount(0)
    await expect(page.getByRole('link', { name: '运行' })).toBeVisible()
    await expect(page.getByTestId('nav-workspace-chrome').getByRole('link', { name: '通知' })).toHaveCount(0)

    await page.getByTestId('run-notifications-bell').click()
    const panel = page.getByTestId('run-notifications-panel')
    await expect(panel).toBeVisible()
    await expect(panel).toContainText('自我迭代 · 已完成')
    await expect(panel).not.toContainText('运行中 4')

    const items = panel.getByTestId('run-notifications-item')
    await expect(items).toHaveCount(3)

    const viewAll = page.getByTestId('run-notifications-view-all')
    await expect(viewAll).toHaveText('查看全部通知')
    await viewAll.click()
    await expect(page.getByTestId('notifications-page')).toBeVisible()
    await expect(page.getByRole('heading', { name: '通知' })).toBeVisible()
    await expect(page.getByTestId('shell-main-runs')).toHaveCount(0)
    // Entering the page must NOT auto mark-all-read.
    await expect(page.getByTestId('nav-notifications-badge')).toHaveText('3')
    await expect(page.getByTestId('run-notifications-badge')).toHaveText('3')

    // /notifications uses settings chrome: 通知 is highlighted; 运行/待办 are not in this sidebar.
    await expect(page.getByTestId('nav-settings-chrome')).toBeVisible()
    await expect(page.getByRole('link', { name: '通知' })).toBeVisible()
    await expect(page.getByTestId('nav-back-home')).toBeVisible()
    await expect(page.getByRole('link', { name: '待办' })).toHaveCount(0)
    await expect(page.getByRole('link', { name: '运行' })).toHaveCount(0)

    await page.getByTestId('notifications-filter-unread').click()
    await expect(page.getByTestId('notifications-item')).toHaveCount(3)

    await page.locator('[data-testid="notifications-item"][data-status="failed"]').click()
    await expect(page.getByTestId('shell-main-run-detail')).toContainText('run-new-fail')
  })

  test('dropdown caps at 5; more-hint; dual-entry mark-all clears whole pool', async ({ page }) => {
    await page.goto('/notifications-center.html?scene=capped')
    await expect(page.getByTestId('shell-main-dashboard')).toBeVisible({ timeout: 15_000 })
    await settleAuth(page)

    await expect(page.getByTestId('run-notifications-badge')).toHaveText('7')
    await expect(page.getByTestId('nav-workspace-chrome')).toBeVisible()
    await expect(page.getByTestId('nav-notifications-badge')).toHaveCount(0)

    await page.getByTestId('run-notifications-bell').click()
    const panel = page.getByTestId('run-notifications-panel')
    await expect(panel.getByTestId('run-notifications-item')).toHaveCount(5)
    await expect(page.getByTestId('run-notifications-more')).toContainText('还有 2 条未展示')

    await page.getByTestId('run-notifications-mark-all').click()
    await expect(page.getByTestId('run-notifications-badge')).toHaveCount(0)
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('run-notifications-panel')).toHaveCount(0)
    await page.getByRole('link', { name: '设置' }).click()
    await expect(page.getByTestId('nav-settings-chrome')).toBeVisible()
    await page.getByRole('link', { name: '通知' }).click()
    await expect(page.getByTestId('notifications-page')).toBeVisible()
    await expect(page.getByTestId('nav-notifications-badge')).toHaveCount(0)
  })

  test('completed click opens output without marking read; mark-as-read then clears badge', async ({
    page,
  }) => {
    await page.goto('/notifications-center.html?scene=with-items&start=notifications')
    await expect(page.getByTestId('notifications-page')).toBeVisible({ timeout: 15_000 })
    await settleAuth(page)
    await expect(page.getByTestId('run-notifications-badge')).toHaveText('3')
    await page
      .locator('[data-testid="notifications-item"][data-status="completed"]')
      .first()
      .click()
    // run-new-ok has outputCards → result-cards path (not the artifact deck)
    await expect(page.getByTestId('run-output-result-cards')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('run-output-focus-bar')).toContainText('聚焦输出节点')
    await expect(page.getByTestId('output-result-cards')).toBeVisible()
    await expect(page.getByTestId('run-output-result-cards')).toContainText('视觉 Demo')
    await expect(page.getByTestId('run-output-result-cards')).not.toContainText('node_complete.json')
    await expect(page.getByTestId('run-output-list')).toHaveCount(0)
    // plan g3.1: removed outputHint must not reappear
    await expect(page.getByRole('dialog')).not.toContainText(
      '展示聚焦输出节点的最终结果卡；与 Run 详情输出 Tab 一致，不是全量产物列表。',
    )
    // Opening alone must NOT drop unread (3 stays 3)
    await expect(page.getByTestId('run-notifications-badge')).toHaveText('3')
    await page.getByTestId('run-output-mark-read').click()
    await expect(page.getByTestId('run-output-mark-read')).toHaveCount(0)
    // badge should drop by 1 (3 → 2) only after mark-as-read
    await expect(page.getByTestId('run-notifications-badge')).toHaveText('2')
  })

  test('completed without outputCards shows empty dual exits, not full artifact list', async ({
    page,
  }) => {
    await page.goto('/notifications-center.html?scene=with-items&start=notifications')
    await expect(page.getByTestId('notifications-page')).toBeVisible({ timeout: 15_000 })
    await settleAuth(page)
    // Second completed item is run-clean (empty cards)
    await page
      .locator('[data-testid="notifications-item"][data-status="completed"]')
      .nth(1)
      .click()
    const empty = page.getByTestId('run-output-empty')
    await expect(empty).toBeVisible({ timeout: 10_000 })
    await expect(empty).toContainText('暂无最终结果可预览')
    await expect(empty).toContainText('不会回退成全量产物列表')
    await expect(page.getByTestId('run-output-empty-open-run')).toBeVisible()
    await expect(page.getByTestId('run-output-empty-open-artifacts')).toBeVisible()
    await expect(empty).not.toContainText('node_complete.json')
    await expect(empty).not.toContainText('plan.json')
    await expect(page.getByTestId('run-output-list')).toHaveCount(0)
    // plan g3.1: removed outputHint must not reappear on empty path
    await expect(page.getByRole('dialog')).not.toContainText(
      '展示聚焦输出节点的最终结果卡；与 Run 详情输出 Tab 一致，不是全量产物列表。',
    )
  })

  test('auth settle: badge matches unread immediately after /auth/me (no focus needed)', async ({
    page,
  }) => {
    await page.goto('/notifications-center.html?scene=with-items')
    await expect(page.getByTestId('shell-main-dashboard')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByText('e2e', { exact: true })).toBeVisible({ timeout: 15_000 })
    // Must not under-count until focus/15s poll — auth watch rehydrates prefs sync.
    await expect(page.getByTestId('run-notifications-badge')).toHaveText('3', { timeout: 5_000 })
  })

  test('mark-all-read persists across hard reload (no unread resurgence)', async ({ page }) => {
    await page.goto('/notifications-center.html?scene=with-items&start=notifications')
    await expect(page.getByTestId('notifications-page')).toBeVisible({ timeout: 15_000 })
    await settleAuth(page)
    await expect(page.getByTestId('run-notifications-badge')).toHaveText('3')

    await page.getByTestId('notifications-mark-all').click()
    await expect(page.getByTestId('run-notifications-badge')).toHaveCount(0)
    await expect(page.getByTestId('nav-notifications-badge')).toHaveCount(0)
    await expect(page.getByTestId('notifications-unread-count')).toContainText('0')

    await page.reload()
    await expect(page.getByTestId('notifications-page')).toBeVisible({ timeout: 15_000 })
    await settleAuth(page)
    await expect(page.getByTestId('run-notifications-badge')).toHaveCount(0)
    await expect(page.getByTestId('nav-notifications-badge')).toHaveCount(0)
    await expect(page.getByTestId('notifications-unread-count')).toContainText('0')
    await page.getByTestId('notifications-filter-unread').click()
    await expect(page.getByTestId('notifications-item')).toHaveCount(0)
  })

  test('independent page paginates at 20; filter and topbar entry reset to page 1 (g4.3)', async ({
    page,
  }) => {
    await page.goto('/notifications-center.html?scene=paged&start=notifications')
    await expect(page.getByTestId('notifications-page')).toBeVisible({ timeout: 15_000 })
    await settleAuth(page)

    await expect(page.getByTestId('notifications-item')).toHaveCount(20)
    await expect(page.getByTestId('notifications-page-range')).toHaveCount(0)
    const pager = page.getByTestId('notifications-pagination')
    await expect(pager).toBeVisible()
    await expect(page.getByTestId('notifications-pager-summary')).toHaveText('共 25 条 · 每页 20')

    await pager.getByRole('button', { name: '下一页' }).click()
    await expect(page.getByTestId('notifications-item')).toHaveCount(5)
    await expect(page.getByTestId('notifications-page-range')).toHaveCount(0)
    await expect(page).not.toHaveURL(/[?&]page=/)

    await page.getByTestId('notifications-filter-read').click()
    await expect(page.getByTestId('notifications-pagination')).toHaveCount(0)
    await expect(page.getByTestId('notifications-page-range')).toHaveCount(0)

    await page.getByTestId('notifications-filter-all').click()
    await expect(page.getByTestId('notifications-item')).toHaveCount(20)
    await expect(page.getByTestId('notifications-page-range')).toHaveCount(0)

    await pager.getByRole('button', { name: '下一页' }).click()
    await expect(page.getByTestId('notifications-page-range')).toHaveCount(0)
    await page.getByTestId('run-notifications-bell').click()
    await expect(page.getByTestId('run-notifications-item')).toHaveCount(5)
    await page.getByTestId('run-notifications-view-all').click()
    await expect(page.getByTestId('notifications-page-range')).toHaveCount(0)
    await expect(page.getByTestId('notifications-item')).toHaveCount(20)

    await pager.getByRole('button', { name: '下一页' }).click()
    await page.getByTestId('run-notifications-bell').click()
    await page.getByTestId('run-notifications-item').first().click()
    await expect(page.getByTestId('notifications-page')).toBeVisible()
    await expect(page.getByTestId('notifications-page-range')).toHaveCount(0)
    await expect(page.getByTestId('notifications-item')).toHaveCount(20)
  })
})
