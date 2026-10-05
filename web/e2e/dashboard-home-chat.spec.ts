/**
 * E2E: Home chat starts an Approve-first workflow and opens the inbox.
 * Harness: dashboard-home-chat.html (DashboardView + gates stub)
 */
import { expect, test, type Page } from '@playwright/test'
import { CLARIFY_CAPS } from '../src/test/capsFixtures'

function approveWorkflow() {
  return {
    id: 'wf-approve',
    name: '自我迭代PRO',
    description: '开发前澄清 + 计划',
    status: 'published',
    version: 1,
    updatedAt: '2026-08-10T12:00:00Z',
    needsRepo: false,
    showOnHome: true,
    nodes: [
      { id: 'in', type: 'input', label: '开始', position: { x: 0, y: 0 }, config: {} },
      { id: 'ap', type: 'agent', caps: CLARIFY_CAPS, label: '澄清', position: { x: 0, y: 0 }, config: {} },
      { id: 'out', type: 'output', label: '结束', position: { x: 0, y: 0 }, config: {} },
    ],
    edges: [
      { id: 'e1', source: 'in', target: 'ap' },
      { id: 'e2', source: 'ap', target: 'out' },
    ],
  }
}

async function mockHomeApis(page: Page) {
  await page.route('**/api/workflows**', async (route) => {
    const req = route.request()
    const url = req.url()
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
        body: JSON.stringify([approveWorkflow()]),
      })
      return
    }
    await route.continue()
  })

  await page.route('**/api/runs/**', async (route) => {
    const req = route.request()
    const url = req.url()
    if (req.method() === 'POST' && url.includes('/react/') && url.includes('/reply')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'ok' }),
      })
      return
    }
    if (req.method() === 'GET' && url.includes('/runs/run-home')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'run-home',
          status: 'waiting_human',
          nodes: approveWorkflow().nodes,
          edges: approveWorkflow().edges,
          nodeRuns: { ap: { nodeId: 'ap', status: 'waiting_human' } },
          artifacts: [],
        }),
      })
      return
    }
    await route.continue()
  })

  await page.route('**/api/projects**', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: [], total: 0 }),
      })
      return
    }
    await route.continue()
  })
}

test.describe('首页 Chat 启动 Approve 工作流', () => {
  test('无项目记忆：仍跨项目加载工作流并可发送', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await mockHomeApis(page)
    await page.goto('/dashboard-home-chat.html')
    await expect(page.getByTestId('dashboard-view')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('home-no-project')).toHaveCount(0)
    await expect(page.getByTestId('home-workflow-card-wf-approve')).toContainText('自我迭代PRO')
    await page.getByTestId('home-composer-input').fill('不先选项目也能开跑')
    await page.getByTestId('home-composer-send').click()
    await expect(page.getByTestId('gates-inbox-page')).toBeVisible({ timeout: 10_000 })
  })

  test('发送第一句话：StartRun + ReactReply 后进入待审批并带上原文', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await mockHomeApis(page)
    await page.goto('/dashboard-home-chat.html?memory=1&projectId=proj-1')
    await expect(page.getByTestId('dashboard-view')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('home-workflow-card-wf-approve')).toContainText('自我迭代PRO')

    await page.getByTestId('home-composer-input').fill('把登录做清楚')
    await page.getByTestId('home-composer-send').click()

    await expect(page.getByTestId('gates-inbox-page')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('gates-query-run')).toHaveText('run-home')
    await expect(page.getByTestId('gates-query-node')).toHaveText('ap')
    await expect(page.getByTestId('gates-handoff-text')).toHaveText('把登录做清楚')
    await expect(page.getByTestId('gates-item-status')).toHaveText('waiting_human')
    await expect(page.getByTestId('run-detail-page')).toHaveCount(0)
  })
})
