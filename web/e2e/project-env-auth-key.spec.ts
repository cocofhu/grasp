import { test, expect } from '@playwright/test'
import { dismissOnboardingIfOpen, seedOnboardingDismissed } from './helpers/onboarding'

const MOCK_PROJECT = {
  id: 'proj-1',
  name: 'Demo Project',
  description: 'Project for e2e',
  variables: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

const MOCK_SHARED_AGENT = {
  projectId: 'proj-1',
  acpBackend: 'cursor',
  defaultProjectId: '',
  gitCredentialType: '',
  files: [],
  mcp: [],
  env: {
    CURSOR_API_KEY: '****',
    API_URL: 'https://example.com',
  },
  layout: { configRoot: '/root/.cursor', workspaceDir: '/root/workspace' },
}

const MOCK_WORKFLOWS: unknown[] = []

async function gotoSharedAgentEnv(page: import('@playwright/test').Page) {
  await page.setViewportSize({ width: 1280, height: 800 })
  await seedOnboardingDismissed(page, 'proj-1')
  await page.route('**/api/projects/proj-1', async (route) => {
    if (route.request().method() === 'GET' || route.request().method() === 'PUT') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_PROJECT),
      })
      return
    }
    await route.continue()
  })
  await page.route('**/api/projects/proj-1/shared-agent-config', async (route) => {
    const method = route.request().method()
    if (method === 'GET' || method === 'PUT') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_SHARED_AGENT),
      })
      return
    }
    await route.continue()
  })
  await page.route('**/api/agents', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([{ name: 'demo-agent', projectId: 'proj-1' }]),
      })
      return
    }
    await route.continue()
  })
  await page.route('**/api/workflows**', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_WORKFLOWS),
      })
      return
    }
    await route.continue()
  })
  await page.route('**/api/runs**', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: [], total: 0, page: 1, pageSize: 20, hasMore: false }),
      })
      return
    }
    await route.continue()
  })
  await page.route('**/api/projects/proj-1/pm-leader', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ enabled: false }),
    })
  })
  await page.goto('/project-detail.html')
  await dismissOnboardingIfOpen(page)
  await expect(page.getByRole('heading', { name: 'Demo Project' })).toBeVisible({ timeout: 10_000 })
  await page.getByRole('button', { name: '共享Agent配置' }).click()
  await page.getByTestId('shared-agent-subtab-env').click()
}

test.describe('项目共享 Agent env', () => {
  test('共享 env 可见 CURSOR_API_KEY 与 API_URL；无旧沙箱页签', async ({ page }) => {
    await gotoSharedAgentEnv(page)

    await expect(page.getByRole('button', { name: '沙箱环境变量' })).toHaveCount(0)
    await expect(page.getByTestId('project-shared-agent-panel')).toBeVisible()
    await expect(page.getByPlaceholder('KEY').first()).toHaveValue('CURSOR_API_KEY')
    await expect(page.getByPlaceholder('KEY').nth(1)).toHaveValue('API_URL')
    await expect(page.getByRole('button', { name: '合并规则' })).toHaveCount(0)
    await expect(page.getByTestId('project-detail-merge-rules')).toHaveCount(0)
  })

  test('可新增 KEY 行（共享 env 表单）', async ({ page }) => {
    await gotoSharedAgentEnv(page)
    await page.getByRole('button', { name: /添加/ }).first().click()
    const newKey = page.getByPlaceholder('KEY').last()
    await newKey.fill('EXTRA_KEY')
    await expect(newKey).toHaveValue('EXTRA_KEY')
  })
})
