import { test, expect } from '@playwright/test'
import { mkdirSync } from 'node:fs'

const SHOT_DIR = '/tmp/approving-test-screenshots'

const MOCK_PROJECTS = [
  {
    id: 'p1',
    name: 'Grasp',
    description: 'Grasp Project',
    workflowCount: 3,
    totalTokens: 128400,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-07-25T03:42:00Z',
    variables: [],
  },
]

test.beforeAll(() => {
  mkdirSync(SHOT_DIR, { recursive: true })
})

test.describe('管理列表 Loading：项目列表', () => {
  test('骨架 → 数据（中文深色）', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    let release!: () => void
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    await page.route('**/api/projects', async (route) => {
      if (route.request().method() !== 'GET') {
        await route.continue()
        return
      }
      await gate
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_PROJECTS),
      })
    })

    await page.goto('/project-list.html')
    await expect(page.getByTestId('project-list-skeleton')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('project-list-panel')).toHaveAttribute('aria-busy', 'true')
    await page.screenshot({ path: `${SHOT_DIR}/project-list-skeleton-zh-dark.png`, fullPage: true })
    release()
    await expect(page.getByRole('button', { name: /Grasp/ })).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('project-list-skeleton')).toHaveCount(0)
    await expect(page.getByTestId('project-list-panel')).toHaveAttribute('aria-busy', 'false')
    await page.screenshot({ path: `${SHOT_DIR}/project-list-data-zh-dark.png`, fullPage: true })
  })

  test('失败 → 重试', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    let calls = 0
    await page.route('**/api/projects', async (route) => {
      if (route.request().method() !== 'GET') {
        await route.continue()
        return
      }
      calls += 1
      if (calls === 1) {
        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'down' }),
        })
        return
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_PROJECTS),
      })
    })

    await page.goto('/project-list.html')
    await expect(page.getByTestId('project-list-failed')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText('加载失败')).toBeVisible()
    await expect(page.getByTestId('project-list-empty')).toHaveCount(0)
    await page.getByTestId('project-list-retry').click()
    await expect(page.getByRole('button', { name: /Grasp/ })).toBeVisible({ timeout: 10_000 })
  })
})
