/**
 * Browser acceptance: a reviewed Agent with set_preview parks in Gates Inbox as
 * 待复审, coexists with human_gate and other reviews, mounts the app preview
 * stage once ports register, and disappears after Confirm & continue.
 */
import { expect, test, type Page } from '@playwright/test'
import { mkdirSync } from 'node:fs'
import { PAGE_CAPS, PREVIEW_REVIEW_CAPS } from '../src/test/capsFixtures'

mkdirSync('/tmp/preview-review-inbox-screenshots', { recursive: true })

const NOW = '2026-07-29T12:00:00Z'
const EARLIER = '2026-07-29T11:50:00Z'

type InboxPayload = { items: unknown[]; total: number }

function gateItem() {
  return {
    type: 'gate',
    runId: 'run-gate',
    nodeId: 'gate-approve',
    iteration: 1,
    workflowId: 'wf-gate',
    workflowName: '门禁工作流',
    runTitle: '方案门禁',
    title: '方案评审门禁',
    bodyMd: '请审批方案',
    actions: [{ id: 'approve', label: '通过' }],
    requestedAt: EARLIER,
    tags: [],
  }
}

function previewItem() {
  return {
    type: 'clarify',
    kind: 'review',
    runId: 'run-preview',
    nodeId: 'implement',
    iteration: 1,
    workflowId: 'wf-ap',
    workflowName: '预览工作流',
    runTitle: '应用预览验收',
    label: '实现',
    done: false,
    requestedAt: NOW,
    updatedAt: NOW,
    tags: [],
  }
}

function reviewItem() {
  return {
    type: 'clarify',
    kind: 'review',
    runId: 'run-review',
    nodeId: 'visual',
    iteration: 1,
    workflowId: 'wf-vis',
    workflowName: '视觉工作流',
    runTitle: '视觉复审',
    label: '视觉复审',
    done: false,
    requestedAt: EARLIER,
    updatedAt: EARLIER,
    tags: [],
  }
}

async function mockInboxApis(
  page: Page,
  state: { inbox: InboxPayload; finished: boolean },
) {
  await page.route('**/api/**', async (route) => {
    // Skip Vite module URLs like /@fs/.../src/lib/api/api.ts (pathname is not /api/...)
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
        body: JSON.stringify(state.inbox),
      })
      return
    }

    if (method === 'GET' && path.match(/\/api\/runs\/[^/]+\/inbox-context/)) {
      const runId = path.split('/')[3]
      const nodeId = url.searchParams.get('nodeId') || ''
      if (runId === 'run-preview' && nodeId === 'implement') {
        await route.fulfill({
          json: {
            type: 'clarify',
            status: 'waiting_human',
            nodes: [{ id: 'implement', type: 'agent', caps: PREVIEW_REVIEW_CAPS, label: '实现', position: { x: 0, y: 0 }, config: {} }],
            artifacts: [],
            nodeExecutions: {
              implement: [{ nodeId: 'implement', iteration: 1, status: 'waiting_human', outputs: {} }],
            },
            clarify: {
              nodeId: 'implement',
              iteration: 1,
              turns: [],
              done: false,
              label: '实现',
            },
          },
        })
        return
      }
      if (runId === 'run-gate') {
        await route.fulfill({
          json: {
            type: 'gate',
            nodes: [{ id: 'gate-approve', type: 'human_gate', label: '方案评审门禁', position: { x: 0, y: 0 }, config: {} }],
            artifacts: [],
            nodeExecutions: {},
            gate: {
              runId: 'run-gate',
              nodeId: 'gate-approve',
              title: '方案评审门禁',
              bodyMd: '请审批方案',
              actions: [{ id: 'approve', label: '通过' }],
              requestedAt: EARLIER,
            },
          },
        })
        return
      }
      if (runId === 'run-review') {
        await route.fulfill({
          json: {
            type: 'clarify',
            status: 'waiting_human',
            nodes: [{ id: 'visual', type: 'agent', caps: PAGE_CAPS, label: '视觉复审', position: { x: 0, y: 0 }, config: {} }],
            artifacts: [],
            nodeExecutions: {},
            clarify: { nodeId: 'visual', iteration: 1, turns: [], done: false, label: '视觉复审' },
          },
        })
        return
      }
      await route.fulfill({ status: 404, json: { error: 'not found' } })
      return
    }

    // Matches api.reactReply → POST /api/runs/:runId/react/:nodeId/reply
    if (method === 'POST' && path.match(/\/api\/runs\/[^/]+\/react\/[^/]+\/reply\/?$/)) {
      const body = req.postDataJSON() as { force?: boolean } | null
      if (body?.force) {
        state.finished = true
        state.inbox = {
          items: state.inbox.items.filter(
            (it) => (it as { runId?: string }).runId !== 'run-preview',
          ),
          total: Math.max(0, state.inbox.total - 1),
        }
      }
      await route.fulfill({ json: { status: 'ok' } })
      return
    }

    if (method === 'GET' && path.match(/\/api\/runs\/[^/]+\/nodes\/[^/]+\/previews/)) {
      await route.fulfill({
        json: {
          ports: [{ port: 3000, label: '前端', proxyUrl: 'http://mock-app:3000/' }],
        },
      })
      return
    }

    if (method === 'GET' && path.match(/\/api\/runs\/[^/]+\/nodes\/[^/]+\/sandbox$/)) {
      await route.fulfill({ json: { id: 42, name: 'e2e-sandbox', status: 'running' } })
      return
    }

    if (method === 'GET' && (path === '/api/workflows' || path.endsWith('/api/workflows'))) {
      await route.fulfill({
        json: [{ id: 'wf-ap', name: '预览工作流', status: 'published', version: 1, nodes: [], edges: [] }],
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

async function openGates(page: Page, state: { inbox: InboxPayload; finished: boolean }) {
  await mockInboxApis(page, state)
  await page.setViewportSize({ width: 1280, height: 800 })
  await page.goto('/tag-filter-ux.html?page=gates')
  await expect(page.getByRole('heading', { name: '待办' })).toBeVisible({ timeout: 15_000 })
}

test.describe('preview review in Gates Inbox', () => {
  test('仅预览复审：列表非空、徽章「待复审」、详情挂载应用预览舞台', async ({ page }) => {
    const state = {
      inbox: { items: [previewItem()], total: 1 },
      finished: false,
    }
    await openGates(page, state)

    await expect(page.getByText('应用预览验收', { exact: false }).first()).toBeVisible()
    await expect(page.getByText('预览工作流', { exact: false })).toBeVisible()
    await expect(page.getByText('待复审', { exact: true }).first()).toBeVisible()

    // Sole item auto-selects; the registered port mounts the app preview stage.
    await expect(page.getByRole('button', { name: '取点标注' })).toBeVisible({ timeout: 15_000 })
    await expect(page.getByRole('tab', { name: '应用预览' })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('button', { name: '确认并流转' })).toBeVisible()

    await page.screenshot({ path: '/tmp/preview-review-inbox-screenshots/01-preview-only.png', fullPage: true })
  })

  test('gate + 预览复审 + 视觉复审：徽章可区分且共存', async ({ page }) => {
    const state = {
      inbox: { items: [previewItem(), gateItem(), reviewItem()], total: 3 },
      finished: false,
    }
    await openGates(page, state)

    await expect(page.getByText('人工门禁', { exact: true }).first()).toBeVisible()
    await expect(page.getByText('待复审', { exact: true })).toHaveCount(2)

    // Click preview row and ensure stage mounts without dropping other list rows.
    await page.locator('button').filter({ hasText: '应用预览验收' }).first().click()
    await expect(page.getByRole('button', { name: '取点标注' })).toBeVisible({ timeout: 15_000 })
    await expect(page.getByText('人工门禁', { exact: true }).first()).toBeVisible()
    await expect(page.getByText('待复审', { exact: true }).first()).toBeVisible()

    await page.screenshot({ path: '/tmp/preview-review-inbox-screenshots/02-coexist-badges.png', fullPage: true })
  })

  test('确认并流转后预览复审条目从待办消失', async ({ page }) => {
    const state = {
      inbox: { items: [previewItem(), gateItem()], total: 2 },
      finished: false,
    }
    await openGates(page, state)

    await page.locator('button').filter({ hasText: '应用预览验收' }).first().click()
    await expect(page.getByRole('button', { name: '确认并流转' })).toBeVisible({ timeout: 15_000 })
    await page.getByRole('button', { name: '确认并流转' }).click()

    // Item removed locally + refresh; gate remains.
    await expect(page.locator('button').filter({ hasText: '应用预览验收' })).toHaveCount(0, {
      timeout: 10_000,
    })
    await expect(page.getByText('人工门禁', { exact: true }).first()).toBeVisible()
    expect(state.finished).toBe(true)

    await page.screenshot({ path: '/tmp/preview-review-inbox-screenshots/03-after-confirm.png', fullPage: true })
  })
})
