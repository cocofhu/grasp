/**
 * Browser acceptance: a clarify Agent hides the app-preview tab until set_preview
 * registers ports; a reviewed Agent with set_preview keeps the tab even when empty.
 */
import { expect, test, type Page } from '@playwright/test'
import { mkdirSync } from 'node:fs'
import { CLARIFY_CAPS, PREVIEW_REVIEW_CAPS } from '../src/test/capsFixtures'

const SHOT = '/tmp/clarify-empty-preview-shots'
mkdirSync(SHOT, { recursive: true })

const NOW = '2026-09-07T02:00:00Z'

type PortsState = { ports: Array<Record<string, unknown>> }

function clarifyItem() {
  return {
    type: 'clarify',
    kind: 'clarify',
    runId: 'run-clarify-empty',
    nodeId: 'clarify_7gl6',
    iteration: 1,
    workflowId: 'wf-ap',
    workflowName: '审批工作流',
    runTitle: '澄清空预览验收',
    label: '需求澄清',
    done: false,
    requestedAt: NOW,
    updatedAt: NOW,
    tags: [],
  }
}

function previewReviewItem() {
  return {
    type: 'clarify',
    kind: 'review',
    runId: 'run-app-preview',
    nodeId: 'implement',
    iteration: 1,
    workflowId: 'wf-prev',
    workflowName: '预览工作流',
    runTitle: '预览复审空列表仍显示 Tab',
    label: '实现',
    done: false,
    requestedAt: NOW,
    updatedAt: NOW,
    tags: [],
  }
}

async function mockApis(page: Page, opts: { item: Record<string, unknown>; ports: PortsState }) {
  await page.route('**/api/**', async (route) => {
    if (!new URL(route.request().url()).pathname.startsWith('/api/')) {
      await route.continue()
      return
    }
    const req = route.request()
    const url = new URL(req.url())
    const path = url.pathname
    const method = req.method()

    if (method === 'GET' && (path === '/api/gates' || path.endsWith('/api/gates'))) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: [opts.item], total: 1 }),
      })
      return
    }

    if (method === 'GET' && path.match(/\/api\/runs\/[^/]+\/nodes\/[^/]+\/previews/)) {
      await route.fulfill({ json: opts.ports })
      return
    }

    if (method === 'GET' && path.match(/\/api\/runs\/[^/]+\/inbox-context/)) {
      const runId = path.split('/')[3]
      const nodeId = url.searchParams.get('nodeId') || ''
      if (runId === 'run-clarify-empty') {
        await route.fulfill({
          json: {
            type: 'clarify',
            status: 'waiting_human',
            nodes: [
              {
                id: 'clarify_7gl6',
                type: 'agent', caps: CLARIFY_CAPS,
                label: '需求澄清',
                position: { x: 0, y: 0 },
                config: {},
              },
            ],
            artifacts: [
              {
                id: 'art-1',
                name: 'clarified_requirement.json',
                kind: 'json',
                sizeBytes: 64,
                createdAt: NOW,
                nodeId: 'clarify_7gl6',
              },
              {
                id: 'art-2',
                name: 'plan.json',
                kind: 'json',
                sizeBytes: 32,
                createdAt: NOW,
                nodeId: 'clarify_7gl6',
              },
            ],
            nodeExecutions: {
              clarify_7gl6: [
                { nodeId: 'clarify_7gl6', iteration: 1, status: 'waiting_human', outputs: {} },
              ],
            },
            clarify: {
              nodeId: 'clarify_7gl6',
              iteration: 1,
              turns: [{ role: 'assistant', text: '请确认需求与计划。', at: NOW }],
              done: false,
              label: '需求澄清',
            },
          },
        })
        return
      }
      if (runId === 'run-app-preview' && nodeId === 'implement') {
        await route.fulfill({
          json: {
            type: 'clarify',
            status: 'waiting_human',
            nodes: [
              {
                id: 'implement',
                type: 'agent', caps: PREVIEW_REVIEW_CAPS,
                label: '实现',
                position: { x: 0, y: 0 },
                config: {},
              },
            ],
            artifacts: [],
            nodeExecutions: {
              implement: [
                { nodeId: 'implement', iteration: 1, status: 'waiting_human', outputs: {} },
              ],
            },
            clarify: {
              nodeId: 'implement',
              iteration: 1,
              turns: [{ role: 'assistant', text: '请预览应用。', at: NOW }],
              done: false,
              label: '实现',
            },
          },
        })
        return
      }
      await route.fulfill({ status: 404, json: { error: 'not found' } })
      return
    }

    if (method === 'GET' && (path === '/api/workflows' || path.endsWith('/api/workflows'))) {
      await route.fulfill({
        json: [{ id: 'wf-ap', name: '审批工作流', status: 'published', version: 1, nodes: [], edges: [] }],
      })
      return
    }

    if (method === 'GET' && (path === '/api/projects' || path.endsWith('/api/projects'))) {
      await route.fulfill({
        json: [{ id: 'proj-1', name: 'Grasp', slug: 'grasp' }],
      })
      return
    }

    if (path.includes('/api/auth') || path.endsWith('/api/me')) {
      await route.fulfill({ json: { username: 'admin', isAdmin: true } })
      return
    }

    if (method === 'GET' && path.match(/\/api\/projects\/[^/]+\/run-tags/)) {
      await route.fulfill({ json: { tags: [] } })
      return
    }

    await route.fulfill({ status: 200, json: {} })
  })
}

async function openGates(page: Page, opts: { item: Record<string, unknown>; ports: PortsState }) {
  await mockApis(page, opts)
  await page.setViewportSize({ width: 1280, height: 800 })
  await page.goto('/tag-filter-ux.html?page=gates')
  await expect(page.getByRole('heading', { name: '待办' })).toBeVisible({ timeout: 15_000 })
}

test.describe('Clarify empty app preview', () => {
  test('无端口时不显示应用预览 Tab，也不出现空占位文案', async ({ page }) => {
    await openGates(page, { item: clarifyItem(), ports: { ports: [] } })

    await page.locator('button').filter({ hasText: '澄清空预览验收' }).first().click()
    await expect(page.getByTestId('react-artifact-tab-grid')).toBeVisible({ timeout: 15_000 })

    // Wait > one silent poll cycle (2.5s) to ensure tab does not appear / flicker.
    await page.waitForTimeout(3000)

    await expect(page.getByTestId('react-artifact-tab-novnc')).toHaveCount(0)
    await expect(page.getByText('尚未注册预览端口')).toHaveCount(0)
    await expect(page.getByText('正在刷新')).toHaveCount(0)

    await page.screenshot({ path: `${SHOT}/01-clarify-no-ports.png`, fullPage: true })
  })

  test('有端口后出现应用预览 Tab 并可打开', async ({ page }) => {
    await openGates(page, {
      item: clarifyItem(),
      ports: {
        ports: [
          {
            port: 5173,
            label: '前端',
            status: 'healthy',
            url: 'http://127.0.0.1:5173/',
          },
        ],
      },
    })

    await page.locator('button').filter({ hasText: '澄清空预览验收' }).first().click()
    await expect(page.getByTestId('react-artifact-tab-novnc')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('react-artifact-tab-novnc')).toContainText('应用预览')
    await page.getByTestId('react-artifact-tab-novnc').click()
    await expect(page.getByText('尚未注册预览端口')).toHaveCount(0)

    await page.screenshot({ path: `${SHOT}/02-clarify-with-ports.png`, fullPage: true })
  })

  test('复审 + set_preview 空端口仍保留应用预览 Tab', async ({ page }) => {
    await openGates(page, { item: previewReviewItem(), ports: { ports: [] } })

    await page.locator('button').filter({ hasText: '预览复审空列表仍显示 Tab' }).first().click()
    await expect(page.getByTestId('react-artifact-tab-novnc')).toBeVisible({ timeout: 15_000 })
    await page.waitForTimeout(3000)
    await expect(page.getByTestId('react-artifact-tab-novnc')).toBeVisible()

    await page.screenshot({ path: `${SHOT}/03-preview-review-empty-ports.png`, fullPage: true })
  })
})
