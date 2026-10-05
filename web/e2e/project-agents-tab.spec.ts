import { test, expect } from '@playwright/test'
import { mkdirSync } from 'node:fs'
import { dismissOnboardingIfOpen, seedOnboardingDismissed } from './helpers/onboarding'

const SHOT = '/tmp/approving-test-screenshots'
const MOCK_PROJECT = {
  id: 'proj-1',
  name: 'Demo Project',
  description: 'Project for e2e',
  variables: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

test.beforeAll(() => {
  mkdirSync(SHOT, { recursive: true })
})

async function stubProjectApis(page: import('@playwright/test').Page) {
  await page.route('**/api/**', async (route) => {
    if (!new URL(route.request().url()).pathname.startsWith('/api/')) {
      await route.continue()
      return
    }
    const url = new URL(route.request().url())
    const path = url.pathname.replace(/^\/api/, '')
    const method = route.request().method()

    if (path === '/auth/me' || path === '/health' || path === '/live') {
      await route.fulfill({
        json: { username: 'e2e', expiresAt: '2099-01-01T00:00:00Z', isAdmin: false },
      })
      return
    }
    if (path === '/projects' && method === 'GET') {
      await route.fulfill({
        json: [
          MOCK_PROJECT,
          { id: 'proj-other', name: 'Other', description: '', createdAt: '', updatedAt: '' },
        ],
      })
      return
    }
    if (path === '/projects/proj-1' && method === 'GET') {
      await route.fulfill({ json: MOCK_PROJECT })
      return
    }
    if (path.startsWith('/workflows') && method === 'GET') {
      await route.fulfill({
        json: [
          {
            id: 'wf-1',
            name: '默认工作流',
            status: 'published',
            version: 1,
            updatedAt: '2026-01-01T00:00:00Z',
            needsRepo: false,
            nodes: [],
            edges: [],
          },
        ],
      })
      return
    }
    if (path.startsWith('/runs') && method === 'GET') {
      await route.fulfill({
        json: { items: [], total: 0, page: 1, pageSize: 100, hasMore: false },
      })
      return
    }
    if (path === '/projects/proj-1/pm-leader') {
      await route.fulfill({ json: { enabled: false } })
      return
    }
    if (path === '/projects/proj-1/shared-agent-config') {
      await route.fulfill({
        json: { acpBackend: 'opencode', env: {}, mcpServers: {} },
      })
      return
    }
    if (path.includes('/token-stats') && method === 'GET') {
      await route.fulfill({
        json: {
          window: '30d',
          bucketWidth: 'day',
          timezone: 'UTC',
          empty: true,
          trend: [],
          composition: {
            inputTokens: 0,
            outputTokens: 0,
            cacheReadTokens: 0,
            cacheWriteTokens: 0,
            total: 0,
          },
          workflows: [],
        },
      })
      return
    }
    if (path === '/agents' && method === 'GET') {
      await route.fulfill({
        json: [
          { name: 'Demo研发工程师', projectId: 'proj-1', acpBackend: 'opencode' },
          { name: 'Other项目Agent', projectId: 'proj-other', acpBackend: 'cursor' },
        ],
      })
      return
    }
    if (path === '/agents/org' && method === 'GET') {
      await route.fulfill({
        json: {
          revision: 1,
          groups: [{ id: 'g1', name: 'Demo项目组', parentGroupId: null }],
          agents: {
            Demo研发工程师: { groupIds: ['g1'] },
            Other项目Agent: { groupIds: ['g1'] },
          },
        },
      })
      return
    }
    if (path.startsWith('/agents/') && method === 'GET') {
      const name = decodeURIComponent(path.replace('/agents/', ''))
      await route.fulfill({
        json: {
          name,
          projectId: name.startsWith('Other') ? 'proj-other' : 'proj-1',
          acpBackend: 'opencode',
          env: {},
          mcpServers: {},
          files: [],
        },
      })
      return
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' })
  })
}

test('项目详情智能体 Tab：嵌入 Studio、过滤他项目、隐藏创建团队', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 })
  await seedOnboardingDismissed(page, 'proj-1')
  await stubProjectApis(page)

  await page.goto('/project-detail.html?tab=agents')
  await dismissOnboardingIfOpen(page)

  await expect(page.getByTestId('project-tab-agents')).toBeVisible({ timeout: 10_000 })
  await expect(page.getByTestId('project-agents-tab')).toBeVisible()
  await expect(page).toHaveURL(/tab=agents/)

  await expect(page.getByRole('button', { name: 'Demo研发工程师' })).toBeVisible()
  await expect(page.getByText('Other项目Agent')).toHaveCount(0)

  const body = await page.locator('[data-testid="project-agents-tab"]').innerText()
  expect(body).not.toContain('创建 Agent 团队')
  await expect(page.getByTestId('agent-studio-action-row')).toHaveCount(0)
  await expect(page.getByTestId('agent-org-import')).toBeVisible()
  await expect(page.getByTestId('agent-org-create-agent')).toBeVisible()
  await expect(page.getByRole('button', { name: '新建 Agent' })).toBeVisible()
  await expect(page.getByRole('button', { name: '导入' })).toBeVisible()

  await expect(page.getByTestId('project-tab-sharedAgent')).toBeVisible()

  await page.screenshot({ path: `${SHOT}/project-agents-tab.png`, fullPage: true })

  await page.getByTestId('project-tab-sharedAgent').click()
  await expect(page).toHaveURL(/tab=sharedAgent/)
  await page.getByTestId('project-tab-agents').click()
  await expect(page.getByTestId('project-agents-tab')).toBeVisible()
  await expect(page).toHaveURL(/tab=agents/)
})
