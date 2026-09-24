import { test, expect, type Page } from '@playwright/test'

const MOCK_DRAFT = {
  id: 'rd-1',
  projectId: 'proj-1',
  title: '支付失败重试',
  bodyMarkdown: '## 要点\n\n正文内容',
  status: 'open',
  kind: 'requirement',
  startAt: '2026-08-01',
  dueAt: '2026-08-15',
  progress: 30,
  parentId: null,
  createdAt: '2026-08-08T10:12:00Z',
  updatedAt: '2026-08-10T14:22:00Z',
}

async function mockDraftApi(page: Page, onSchedulePatch?: () => void) {
  await page.route('**/api/projects/proj-1/requirement-drafts**', async (route) => {
    const req = route.request()
    const url = new URL(req.url())
    if (req.method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: [MOCK_DRAFT] }),
      })
      return
    }
    if (req.method() === 'PATCH' && url.pathname.endsWith('/schedule')) {
      onSchedulePatch?.()
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_DRAFT),
      })
      return
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(MOCK_DRAFT),
    })
  })
}

async function openEditView(page: Page, width: number, height: number) {
  await page.setViewportSize({ width, height })
  await page.goto('/requirement-drafts-schedule.html')
  await page.getByTestId('requirement-drafts-view-edit').click()
  await page.getByTestId('requirement-drafts-item-rd-1').click()
  await expect(page.getByTestId('requirement-drafts-schedule-toggle')).toBeVisible()
}

test('schedule block is expanded by default (f1/f2)', async ({ page }) => {
  await mockDraftApi(page)
  await openEditView(page, 1280, 1000)

  const toggle = page.getByTestId('requirement-drafts-schedule-toggle')
  await expect(toggle).toHaveAttribute('aria-expanded', 'true')
  await expect(toggle.locator('.rd-schedule-arrow')).toHaveClass(/is-open/)
  await expect(page.getByTestId('requirement-drafts-schedule-kind')).toBeVisible()
  await expect(page.getByTestId('requirement-drafts-schedule-start')).toBeVisible()
  await expect(page.getByTestId('requirement-drafts-schedule-due')).toBeVisible()
  await expect(page.getByTestId('requirement-drafts-schedule-progress')).toBeVisible()
})

test('clicking the title row collapses then re-expands, releasing editor space (f1/f2/f3)', async ({
  page,
}) => {
  await mockDraftApi(page)
  await openEditView(page, 1280, 1000)

  const toggle = page.getByTestId('requirement-drafts-schedule-toggle')
  const sourcePane = page.getByTestId('requirement-drafts-source-pane')

  const expandedBox = await sourcePane.boundingBox()
  expect(expandedBox).not.toBeNull()

  await toggle.click()
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  await expect(toggle.locator('.rd-schedule-arrow')).not.toHaveClass(/is-open/)
  await expect(page.getByTestId('requirement-drafts-schedule-kind')).toBeHidden()
  await expect(page.getByTestId('requirement-drafts-schedule-start')).toBeHidden()
  await expect(page.getByTestId('requirement-drafts-schedule-due')).toBeHidden()
  await expect(page.getByTestId('requirement-drafts-schedule-progress')).toBeHidden()
  await expect(page.getByTestId('requirement-drafts-schedule-block')).toBeVisible()

  const collapsedBox = await sourcePane.boundingBox()
  expect(collapsedBox).not.toBeNull()
  expect(collapsedBox!.height).toBeGreaterThan(expandedBox!.height)

  await toggle.click()
  await expect(toggle).toHaveAttribute('aria-expanded', 'true')
  await expect(page.getByTestId('requirement-drafts-schedule-kind')).toBeVisible()
  const restoredBox = await sourcePane.boundingBox()
  expect(restoredBox).not.toBeNull()
  expect(Math.abs(restoredBox!.height - expandedBox!.height)).toBeLessThan(2)
})

test('collapsing does not trigger a schedule PATCH nor clear unsaved edits (f4)', async ({
  page,
}) => {
  let schedulePatches = 0
  await mockDraftApi(page, () => {
    schedulePatches++
  })
  await openEditView(page, 1280, 1000)

  await page.getByTestId('requirement-drafts-title').fill('未保存标题')
  await expect(page.getByTestId('requirement-drafts-dirty-chip')).toBeVisible()

  await page.getByTestId('requirement-drafts-schedule-toggle').click()
  await page.waitForTimeout(300)

  expect(schedulePatches).toBe(0)
  await expect(page.getByTestId('requirement-drafts-dirty-chip')).toBeVisible()
  await expect(page.getByTestId('requirement-drafts-title')).toHaveValue('未保存标题')
})

test('schedule block collapses on a narrow viewport (n2)', async ({ page }) => {
  await mockDraftApi(page)
  await openEditView(page, 390, 780)

  const toggle = page.getByTestId('requirement-drafts-schedule-toggle')
  await toggle.click()
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  await expect(page.getByTestId('requirement-drafts-schedule-kind')).toBeHidden()
})
