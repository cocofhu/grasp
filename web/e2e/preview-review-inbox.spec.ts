/**
 * Browser acceptance: a reviewed Agent with set_preview parks in Gates Inbox as
 * 待复审, coexists with human_gate and other reviews, mounts the app preview
 * stage once ports register, and disappears after Confirm & continue.
 */
import { expect, test, type Page } from '@playwright/test'
import { mkdirSync } from 'node:fs'
import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'
import { acceptConfirmFlow } from './helpers/confirmFlow'
import { PAGE_CAPS, PREVIEW_REVIEW_CAPS } from '../src/test/capsFixtures'

mkdirSync('/tmp/preview-review-inbox-screenshots', { recursive: true })

const NOW = '2026-07-29T12:00:00Z'
const EARLIER = '2026-07-29T11:50:00Z'

type InboxPayload = { items: unknown[]; total: number }

function unfinishedRun(item: Record<string, unknown>) {
  return {
    id: item.runId,
    workflowId: item.workflowId,
    workflowName: item.workflowName,
    title: item.runTitle,
    status: 'waiting_human',
    startedAt: NOW,
    lastActivityAt: NOW,
    currentNode: { id: item.nodeId, name: item.label || item.title, iteration: item.iteration ?? 1 },
  }
}

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
    const path = new URL(req.url()).pathname
    const method = req.method()

    if (method === 'GET' && (path === '/api/runs' || path === '/api/runs/')) {
      const status = new URL(req.url()).searchParams.get('status') ?? ''
      const unfinished = ['queued', 'running', 'waiting_human'].some((s) => status.split(',').includes(s))
      const items = unfinished ? state.inbox.items.map((it) => unfinishedRun(it as Record<string, unknown>)) : []
      await route.fulfill({ json: { items, total: items.length, page: 1, pageSize: 50, hasMore: false } })
      return
    }

    if (method === 'GET' && (path === '/api/pending-nodes' || path.endsWith('/api/pending-nodes'))) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(state.inbox),
      })
      return
    }

    if (method === 'GET' && path.match(/\/api\/runs\/[^/]+\/nodes\/[^/]+\/context/)) {
      const { runId, nodeId } = pathRunNode(path)
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

    // Matches api.reactReply → POST /api/runs/:runId/nodes/:nodeId/reply
    if (method === 'POST' && path.match(/\/api\/runs\/[^/]+\/nodes\/[^/]+\/reply\/?$/)) {
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

    if (method === 'GET' && path === '/api/workspace') {
      await route.fulfill({
        json: { name: 'Grasp', description: '', variables: [] },
      })
      return
    }

    if (path.includes('/api/auth') || path.endsWith('/api/me')) {
      await route.fulfill({ json: { username: 'admin', is_admin: true, expires_at: '2099-01-01T00:00:00Z' } })
      return
    }

    if (method === 'GET' && path === '/api/workspace/run-tags') {
      await route.fulfill({ json: { tags: [] } })
      return
    }

    await route.fallback()
  })
}

/** `/api/runs/:runId/nodes/:nodeId/...` — node id is a path segment. */
function pathRunNode(pathname: string): { runId: string; nodeId: string } {
  const parts = pathname.split('/')
  let nodeId = parts[5] || ''
  try {
    nodeId = decodeURIComponent(nodeId)
  } catch {
    /* keep the raw segment */
  }
  return { runId: parts[3] || '', nodeId }
}

async function openGates(page: Page, state: { inbox: InboxPayload; finished: boolean }) {
  await installWorkspaceApiDefaults(page)
  await mockInboxApis(page, state)
  await page.setViewportSize({ width: 1280, height: 800 })
  const first = (state.inbox.items[0] || {}) as { runId?: string; nodeId?: string }
  const runId = encodeURIComponent(String(first.runId || ''))
  const nodeId = encodeURIComponent(String(first.nodeId || ''))
  await page.goto(`/workspace-config.html?view=${encodeURIComponent(`/runs/${runId}/nodes/${nodeId}`)}`)
  await expect(page.getByTestId('nav-pending-row').first()).toBeVisible({ timeout: 15_000 })
}

const pendingRow = (page: Page, title: string) => page.getByTestId('nav-pending-row').filter({ hasText: title })

test.describe('preview review in Gates Inbox', () => {
  test('仅预览复审：列表非空、徽章「待复审」、详情挂载应用预览舞台', async ({ page }) => {
    const state = {
      inbox: { items: [previewItem()], total: 1 },
      finished: false,
    }
    await openGates(page, state)

    const preview = pendingRow(page, '应用预览验收')
    await expect(preview).toBeVisible()
    await expect(preview).toHaveAttribute('title', '应用预览验收 · 实现')
    await expect(preview.getByTestId('nav-pending-type-icon')).toHaveClass(/nav-row-type-icon--hold/)
    await preview.click()

    // The conversation opens first; artifacts remain mounted behind their pane.
    await expect(page.getByRole('button', { name: '确认并流转' })).toBeVisible()
    await expect(page.getByTestId('conversation-artifact-panel')).toBeHidden()
    await page.getByTestId('conversation-resource-toggle').click()
    await expect(page.getByTestId('conversation-artifact-panel')).toBeVisible()
    await page.getByTestId('react-artifact-card-app').click()
    await expect(page.getByRole('tab', { name: '应用预览' })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('button', { name: '取点标注' })).toBeVisible({ timeout: 15_000 })

    await page.screenshot({ path: '/tmp/preview-review-inbox-screenshots/01-preview-only.png', fullPage: true })
  })

  test('gate + 预览复审 + 视觉复审：徽章可区分且共存', async ({ page }) => {
    const state = {
      inbox: { items: [previewItem(), gateItem(), reviewItem()], total: 3 },
      finished: false,
    }
    await openGates(page, state)

    const gate = pendingRow(page, '方案门禁')
    const preview = pendingRow(page, '应用预览验收')
    const review = pendingRow(page, '视觉复审')
    await expect(page.getByTestId('nav-pending-row')).toHaveCount(3)
    await expect(gate).toHaveAttribute('title', '方案门禁 · 方案评审门禁')
    await expect(gate.getByTestId('nav-pending-type-icon')).toHaveClass(/nav-row-type-icon--hold/)
    for (const row of [preview, review]) {
      await expect(row).toBeVisible()
      await expect(row.getByTestId('nav-pending-type-icon')).toHaveClass(/nav-row-type-icon--hold/)
    }

    // Open the selected review's artifacts without dropping other list rows.
    await preview.click()
    await expect(page.getByTestId('conversation-artifact-panel')).toBeHidden()
    await page.getByTestId('conversation-resource-toggle').click()
    await expect(page.getByTestId('conversation-artifact-panel')).toBeVisible()
    await page.getByTestId('react-artifact-card-app').click()
    await expect(page.getByRole('button', { name: '取点标注' })).toBeVisible({ timeout: 15_000 })
    await expect(gate).toBeVisible()
    await expect(review).toBeVisible()
    await expect(page.getByTestId('nav-pending-row')).toHaveCount(3)

    await page.screenshot({ path: '/tmp/preview-review-inbox-screenshots/02-coexist-badges.png', fullPage: true })
  })

  test('确认并流转后预览复审条目从待办消失', async ({ page }) => {
    const state = {
      inbox: { items: [previewItem(), gateItem()], total: 2 },
      finished: false,
    }
    await openGates(page, state)

    await pendingRow(page, '应用预览验收').click()
    await expect(page.getByRole('button', { name: '确认并流转' })).toBeVisible({ timeout: 15_000 })
    await page.getByRole('button', { name: '确认并流转' }).click()
    await acceptConfirmFlow(page)

    // Item removed locally + refresh; gate remains.
    await expect(pendingRow(page, '应用预览验收')).toHaveCount(0, {
      timeout: 10_000,
    })
    await expect(pendingRow(page, '方案门禁')).toBeVisible()
    await expect(pendingRow(page, '方案门禁').getByTestId('nav-pending-type-icon')).toHaveClass(/nav-row-type-icon--hold/)
    expect(state.finished).toBe(true)

    await page.screenshot({ path: '/tmp/preview-review-inbox-screenshots/03-after-confirm.png', fullPage: true })
  })
})
