import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'
import { routeApi } from './helpers/apiRoute'
import { test, expect } from '@playwright/test'

test.beforeEach(async ({ page }) => { await installWorkspaceApiDefaults(page) })
import { dismissOnboardingIfOpen, seedOnboardingDismissed } from './helpers/onboarding'

const MOCK_WORKSPACE = {
  name: 'Demo Workspace',
  description: 'Workspace for e2e',
  variables: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

const MOCK_SHARED_AGENT = {
  acpBackend: 'cursor',
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
  await seedOnboardingDismissed(page)
  await routeApi(page, '**/api/workspace', async (route) => {
    if (route.request().method() === 'GET' || route.request().method() === 'PUT') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_WORKSPACE),
      })
      return
    }
    await route.continue()
  })
  await routeApi(page, '**/api/workspace/shared-agent-config', async (route) => {
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
  await routeApi(page, '**/api/agents', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([{ name: 'demo-agent' }]),
      })
      return
    }
    await route.continue()
  })
  await routeApi(page, '**/api/workflows**', async (route) => {
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
  await routeApi(page, '**/api/runs**', async (route) => {
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
  await routeApi(page, '**/api/workspace/pm-leader', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ enabled: false }),
    })
  })
  // Agent 默认配置 is the pinned first item of the Agent list in Agent Studio.
  await page.goto(`/workspace-config.html?view=${encodeURIComponent('/agents')}`)
  await dismissOnboardingIfOpen(page)
  await page.getByTestId('resource-list-pinned').getByRole('button', { name: 'Agent 默认配置' }).click()
  await expect(page.getByTestId('studio-defaults-pane')).toBeVisible()
  await page.getByTestId('shared-agent-subtab-env').click()
}

test.describe('工作区共享 Agent env', () => {
  test('共享 env 可见 CURSOR_API_KEY 与 API_URL；无旧沙箱页签', async ({ page }) => {
    await gotoSharedAgentEnv(page)

    await expect(page.getByRole('button', { name: '沙箱环境变量' })).toHaveCount(0)
    await expect(page.getByTestId('workspace-shared-agent-panel')).toBeVisible()
    await expect(page.getByPlaceholder('KEY').first()).toHaveValue('CURSOR_API_KEY')
    await expect(page.getByPlaceholder('KEY').nth(1)).toHaveValue('API_URL')
    await expect(page.getByRole('button', { name: '合并规则' })).toHaveCount(0)
  })

  test('可新增 KEY 行（共享 env 表单）', async ({ page }) => {
    await gotoSharedAgentEnv(page)
    await page.getByRole('button', { name: /添加/ }).first().click()
    const newKey = page.getByPlaceholder('KEY').last()
    await newKey.fill('EXTRA_KEY')
    await expect(newKey).toHaveValue('EXTRA_KEY')
  })
})
