/**
 * Temp browser acceptance for home workflow select create footer.
 * Harness: dashboard-home-chat.html
 */
import { expect, test, type Page } from '@playwright/test'
import { mkdirSync } from 'node:fs'
import { CLARIFY_CAPS } from '../src/test/capsFixtures'

const SHOT = '/tmp/home-workflow-select-create-shots'
mkdirSync(SHOT, { recursive: true })

function approveWorkflow(overrides: Record<string, unknown> = {}) {
  return {
    id: 'wf-approve',
    name: '自我迭代Ultra',
    description: '开发前澄清 + 计划',
    status: 'published',
    version: 1,
    updatedAt: '2026-08-10T12:00:00Z',
    needsRepo: false,
    showOnHome: true,
    projectId: 'proj-1',
    projectName: '综合项目组',
    nodes: [
      { id: 'in', type: 'input', label: '开始', position: { x: 0, y: 0 }, config: {} },
      { id: 'ap', type: 'agent', caps: CLARIFY_CAPS, label: '澄清', position: { x: 0, y: 0 }, config: {} },
      { id: 'out', type: 'output', label: '结束', position: { x: 0, y: 0 }, config: {} },
    ],
    edges: [
      { id: 'e1', source: 'in', target: 'ap' },
      { id: 'e2', source: 'ap', target: 'out' },
    ],
    ...overrides,
  }
}

async function mockApis(page: Page, opts: { workflows?: unknown[] } = {}) {
  const workflows = opts.workflows ?? [
    approveWorkflow(),
    approveWorkflow({ id: 'wf-b', name: 'MiniInfra迭代' }),
  ]

  await page.route('**/api/workflows**', async (route) => {
    const req = route.request()
    const url = req.url()
    if (req.method() === 'POST' && url.includes('/from-baseline')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(
          approveWorkflow({ id: 'wf-new', name: '浏览器新建', projectName: '综合项目组' }),
        ),
      })
      return
    }
    if (req.method() === 'POST' && url.includes('/runs')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ id: 'run-home', status: 'queued' }),
      })
      return
    }
    if (req.method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(workflows),
      })
      return
    }
    await route.continue()
  })

  await page.route('**/api/projects**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (route.request().method() === 'GET' && (path === '/api/projects' || path.endsWith('/api/projects'))) {
      // listProjects expects Project[], not { items, total }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([{ id: 'proj-1', name: '综合项目组' }]),
      })
      return
    }
    await route.continue()
  })

  await page.route('**/api/me**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ username: 'e2e', isAdmin: true }),
    })
  })

  await page.route('**/api/config**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({}),
    })
  })
}

test.describe('首页工作流下拉新建入口', () => {
  test('打开下拉可见新建足栏，点击后打开基线弹窗；轨卡与 + 仍在', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await mockApis(page)
    await page.goto('/dashboard-home-chat.html')
    await expect(page.getByTestId('dashboard-view')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('home-new-workflow')).toBeVisible()
    await expect(page.getByTestId('home-composer-plus')).toBeVisible()

    await page.getByTestId('home-workflow-select-trigger').click()
    await expect(page.getByTestId('home-workflow-select-panel')).toBeVisible()
    const create = page.getByTestId('home-workflow-select-create')
    await expect(create).toBeVisible()
    await expect(create).toContainText('新建工作流')
    await expect(create).not.toHaveAttribute('aria-selected', /.*/)

    await page.screenshot({
      path: `${SHOT}/01-dropdown-with-create-footer.png`,
      fullPage: false,
    })

    await create.click()
    await expect(page.getByTestId('home-workflow-select-panel')).toHaveCount(0)
    await expect(page.getByTestId('home-create-form')).toBeVisible({ timeout: 5_000 })
    await expect(page.getByTestId('home-create-workflow-name')).toBeVisible()

    await page.screenshot({
      path: `${SHOT}/02-baseline-modal-from-select.png`,
      fullPage: false,
    })

    // composer + still attachment (title present), rail card still present
    await expect(page.getByTestId('home-new-workflow')).toBeVisible()
    await expect(page.getByTestId('home-composer-plus')).toBeVisible()
  })

  test('零工作流时 trigger 可开，仅空状态+新建足栏', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await mockApis(page, { workflows: [] })
    await page.goto('/dashboard-home-chat.html')
    await expect(page.getByTestId('dashboard-view')).toBeVisible({ timeout: 15_000 })

    const trigger = page.getByTestId('home-workflow-select-trigger')
    await expect(trigger).toBeEnabled()
    await trigger.click()
    await expect(page.getByTestId('home-workflow-select-panel')).toBeVisible()
    await expect(page.getByTestId('home-workflow-select-empty')).toBeVisible()
    await expect(page.getByTestId('home-workflow-select-create')).toBeVisible()

    await page.screenshot({
      path: `${SHOT}/03-empty-workflows-create-only.png`,
      fullPage: false,
    })
  })

  test('无匹配搜索仍显示空状态与新建足栏；键盘落到新建后 Enter 打开弹窗', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await mockApis(page)
    await page.goto('/dashboard-home-chat.html')
    await expect(page.getByTestId('dashboard-view')).toBeVisible({ timeout: 15_000 })

    await page.getByTestId('home-workflow-select-trigger').click()
    const search = page.getByTestId('home-workflow-select-search')
    await search.fill('zzzz-no-match')
    await expect(page.getByTestId('home-workflow-select-empty')).toBeVisible()
    await expect(page.getByTestId('home-workflow-select-create')).toBeVisible()

    await page.screenshot({
      path: `${SHOT}/04-no-match-still-create.png`,
      fullPage: false,
    })

    await search.press('Enter')
    await expect(page.getByTestId('home-workflow-select-panel')).toHaveCount(0)
    await expect(page.getByTestId('home-create-form')).toBeVisible({ timeout: 5_000 })
  })
})
