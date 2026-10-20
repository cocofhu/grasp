import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'
import { routeApi } from './helpers/apiRoute'
import { test, expect } from '@playwright/test'

test.beforeEach(async ({ page }) => { await installWorkspaceApiDefaults(page) })
import { dismissOnboardingIfOpen, seedOnboardingDismissed } from './helpers/onboarding'

const MOCK_PROJECT = {
  id: 'proj-1',
  name: 'Demo Project',
  description: 'Project for e2e',
  variables: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

/**
 * plan g3.1 / review v2: open ?tab=externalMcp with onboarding dismissed
 * so the panel (switch / save / keys) is visible without the wizard overlay.
 */
async function gotoExternalMcpPanel(page: import('@playwright/test').Page) {
  await page.setViewportSize({ width: 1280, height: 800 })
  await seedOnboardingDismissed(page, 'proj-1')

  await routeApi(page, '**/api/workspace', async (route) => {
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
  await routeApi(page, '**/api/workflows**', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([{ id: 'wf-1', name: 'Alpha', status: 'draft', nodes: [], edges: [] }]),
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
  await routeApi(page, '**/api/agents', async (route) => {
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
  await routeApi(page, '**/api/workspace/pm-leader', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ enabled: false }),
    })
  })
  await routeApi(page, '**/api/workspace/external-mcp', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          enabled: true,
          enabledPacks: ['pm-progress', 'pm-agent-fs'],
          mcpBaseUrl: 'http://api.example.com/mcp/workspace',
        }),
      })
      return
    }
    if (route.request().method() === 'PUT') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          enabled: true,
          enabledPacks: ['pm-progress'],
          mcpBaseUrl: 'http://api.example.com/mcp/workspace',
        }),
      })
      return
    }
    await route.continue()
  })
  await routeApi(page, '**/api/workspace/external-mcp/keys', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            id: 'k1',
            name: 'cursor',
            key_prefix: 'cf_proj_••••abcd',
            created_at: '2026-01-01T00:00:00Z',
          },
        ]),
      })
      return
    }
    if (route.request().method() === 'POST') {
      await route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'k2',
          name: 'new-key',
          key: 'cf_proj_plaintext_once',
          key_prefix: 'cf_proj_••••once',
          created_at: '2026-01-02T00:00:00Z',
        }),
      })
      return
    }
    await route.continue()
  })

  await page.goto('/workspace-config.html?view=/settings/external-mcp')
  await dismissOnboardingIfOpen(page)
  await expect(page.getByTestId('onboarding-backdrop')).toHaveCount(0)
}

test.describe('工作区外部 MCP 面板（无引导遮罩）', () => {
  test('总开关、保存、创钥与密钥行可见 (g3.1/g3.2)', async ({ page }) => {
    await gotoExternalMcpPanel(page)

    await expect(page.getByTestId('project-external-mcp-panel')).toBeVisible()
    await expect(page.getByTestId('external-mcp-enabled')).toBeVisible()
    await expect(page.getByTestId('external-mcp-save')).toBeVisible()
    await expect(page.getByTestId('external-mcp-create-key')).toBeVisible()
    await expect(page.getByTestId('external-mcp-key-row')).toBeVisible()
    // URL appears in code block, hint paragraph, and JSON example — assert exact code text
    await expect(page.getByText('http://api.example.com/mcp/workspace', { exact: true })).toBeVisible()
    // g3.2 review v3: {base} must be interpolated, not shown literally
    await expect(page.getByText(/完整 URL 形如：http:\/\/api\.example\.com\/mcp\/workspace\/pm-progress/)).toBeVisible()
    await expect(page.getByText('{base}')).toHaveCount(0)

    await page.screenshot({
      path: '/tmp/external-mcp-panel.png',
      fullPage: true,
    })
  })
  test('creates a workspace MCP key and displays its plaintext only in the creation dialog', async ({ page }) => {
    await gotoExternalMcpPanel(page)
    await page.getByTestId('external-mcp-create-key').click()
    const dialog = page.getByRole('dialog')
    await dialog.getByTestId('external-mcp-key-name').fill('new-key')
    await dialog.getByRole('button', { name: '创建密钥', exact: true }).click()
    await expect(dialog).toContainText('cf_proj_plaintext_once')
    await dialog.getByRole('button', { name: '关闭', exact: true }).click()
    await expect(page.getByTestId('project-external-mcp-panel')).not.toContainText('cf_proj_plaintext_once')
  })

})
