/**
 * Browser acceptance: confirm posts to the node reply or decision path.
 * A clarify confirm stays on that run and follows the next node; a gate confirm leaves before resume returns.
 * A failed clarify confirm restores the row so it can be retried.
 */
import { expect, test, type Page } from '@playwright/test'
import { mkdirSync } from 'node:fs'
import { CLARIFY_CAPS } from '../src/test/capsFixtures'
import { acceptConfirmFlow } from './helpers/confirmFlow'

const SHOT = '/tmp/inbox-confirm-leave-shots'
mkdirSync(SHOT, { recursive: true })

const NOW = '2026-08-21T04:00:00Z'

type InboxItem = Record<string, unknown>
type State = {
  inbox: { items: InboxItem[]; total: number }
  replyDelayMs: number
  replyFail: boolean
  resumeDelayMs: number
  resumeFail: boolean
  replyResolved: boolean
  resumeResolved: boolean
  /** Count of force reactReply calls (consecutive-approve coverage). */
  forceReplyCount: number
}

function approveItem(): InboxItem {
  return {
    type: 'clarify',
    kind: 'clarify',
    runId: 'run-approve',
    nodeId: 'approve_7gl6',
    iteration: 1,
    workflowId: 'wf-ap',
    workflowName: '审批工作流',
    runTitle: '确认后应离开待办',
    label: 'Approve',
    done: false,
    requestedAt: NOW,
    updatedAt: NOW,
    tags: [],
  }
}

function approveItemNeighbor(): InboxItem {
  return {
    type: 'clarify',
    kind: 'clarify',
    runId: 'run-approve-b',
    nodeId: 'approve_7gl7',
    iteration: 1,
    workflowId: 'wf-ap',
    workflowName: '审批工作流',
    runTitle: '第二个待确认',
    label: 'Approve',
    done: false,
    requestedAt: NOW,
    updatedAt: NOW,
    tags: [],
  }
}

function gateItem(): InboxItem {
  return {
    type: 'gate',
    runId: 'run-gate',
    nodeId: 'gate-1',
    iteration: 1,
    workflowId: 'wf-gate',
    workflowName: '门禁工作流',
    runTitle: '方案门禁',
    title: '方案评审门禁',
    bodyMd: '请审批方案',
    actions: [{ id: 'approve', label: '通过' }],
    requestedAt: NOW,
    tags: [],
  }
}

function neighborItem(): InboxItem {
  return {
    type: 'gate',
    runId: 'run-neighbor',
    nodeId: 'gate-n',
    iteration: 1,
    workflowId: 'wf-n',
    workflowName: '邻居工作流',
    runTitle: '邻居条目',
    title: '邻居门禁',
    bodyMd: '保留',
    actions: [{ id: 'approve', label: '通过' }],
    requestedAt: NOW,
    tags: [],
  }
}

async function mockApis(page: Page, state: State) {
  await page.route('**/api/**', async (route) => {
    if (!new URL(route.request().url()).pathname.startsWith('/api/')) {
      await route.continue()
      return
    }
    const req = route.request()
    const path = new URL(req.url()).pathname
    const method = req.method()

    if (method === 'GET' && (path === '/api/pending-nodes' || path.endsWith('/api/pending-nodes'))) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(state.inbox),
      })
      return
    }

    if (method === 'GET' && path.match(/\/api\/runs\/[^/]+\/nodes\/[^/]+\/context/)) {
      const parts = path.split('/')
      const runId = parts[3] || ''
      let nodeId = parts[5] || ''
      try {
        nodeId = decodeURIComponent(nodeId)
      } catch {
        /* keep the raw segment */
      }
      if (runId === 'run-approve' || runId === 'run-approve-b') {
        const nid = runId === 'run-approve-b' ? 'approve_7gl7' : 'approve_7gl6'
        await route.fulfill({
          json: {
            type: 'clarify',
            status: 'waiting_human',
            nodes: [
              {
                id: nid,
                type: 'agent',
                caps: CLARIFY_CAPS,
                label: 'Approve',
                position: { x: 0, y: 0 },
                config: {},
              },
            ],
            artifacts: [
              {
                id: 'art-1',
                name: 'clarified_requirement.json',
                sizeBytes: 64,
                createdAt: NOW,
              },
              { id: 'art-2', name: 'plan.json', sizeBytes: 32, createdAt: NOW },
            ],
            nodeExecutions: {
              [nid]: [
                {
                  nodeId: nid,
                  iteration: 1,
                  status: 'waiting_human',
                  outputs: {},
                },
              ],
            },
            clarify: {
              nodeId: nid,
              iteration: 1,
              turns: [
                {
                  role: 'assistant',
                  text: '产物已就绪，请确认并流转。',
                  at: NOW,
                },
              ],
              done: false,
              label: 'Approve',
            },
          },
        })
        return
      }
      if (runId === 'run-gate' || runId === 'run-neighbor') {
        await route.fulfill({
          json: {
            type: 'gate',
            nodes: [
              {
                id: nodeId || 'gate-1',
                type: 'human_gate',
                label: '门禁',
                position: { x: 0, y: 0 },
                config: {},
              },
            ],
            artifacts: [],
            nodeExecutions: {},
            gate: {
              runId,
              nodeId: nodeId || 'gate-1',
              title: runId === 'run-neighbor' ? '邻居门禁' : '方案评审门禁',
              bodyMd: '请审批',
              actions: [{ id: 'approve', label: '通过' }],
              requestedAt: NOW,
            },
          },
        })
        return
      }
      await route.fulfill({ status: 404, json: { error: 'not found' } })
      return
    }

    if (method === 'POST' && path.match(/\/api\/runs\/[^/]+\/nodes\/[^/]+\/reply\/?$/)) {
      const parts = path.split('/')
      const runId = parts[3]
      const body = (req.postDataJSON() as { force?: boolean } | null) || {}
      if (state.replyDelayMs > 0) {
        await new Promise((r) => setTimeout(r, state.replyDelayMs))
      }
      if (state.replyFail) {
        state.replyResolved = true
        await route.fulfill({ status: 500, json: { error: 'reactReply boom' } })
        return
      }
      if (body.force) {
        state.forceReplyCount += 1
        state.inbox = {
          items: state.inbox.items.filter((it) => it.runId !== runId),
          total: Math.max(0, state.inbox.total - 1),
        }
      }
      state.replyResolved = true
      await route.fulfill({ json: { status: 'ok' } })
      return
    }

    if (method === 'POST' && path.match(/\/api\/runs\/[^/]+\/nodes\/[^/]+\/decision\/?$/)) {
      if (state.resumeDelayMs > 0) {
        await new Promise((r) => setTimeout(r, state.resumeDelayMs))
      }
      if (state.resumeFail) {
        state.resumeResolved = true
        await route.fulfill({ status: 500, json: { error: 'resume boom' } })
        return
      }
      const parts = path.split('/')
      const runId = parts[3]
      state.inbox = {
        items: state.inbox.items.filter((it) => it.runId !== runId),
        total: Math.max(0, state.inbox.total - 1),
      }
      state.resumeResolved = true
      await route.fulfill({ json: { status: 'ok' } })
      return
    }

    if (method === 'GET' && (path === '/api/workflows' || path.endsWith('/api/workflows'))) {
      await route.fulfill({
        json: [{ id: 'wf-ap', name: '审批工作流', status: 'published', version: 1, nodes: [], edges: [] }],
      })
      return
    }
    if (path.includes('/api/auth') || path.endsWith('/api/me')) {
      await route.fulfill({ json: { username: 'admin', isAdmin: true } })
      return
    }
    if (method === 'GET' && path === '/api/workspace/run-tags') {
      await route.fulfill({ json: { tags: [] } })
      return
    }
    if (method === 'GET' && path.match(/\/api\/runs\/[^/]+\/events/)) {
      await route.fulfill({ status: 200, body: '' })
      return
    }
    await route.fulfill({ status: 200, json: {} })
  })
}

async function openGates(page: Page, state: State) {
  await mockApis(page, state)
  await page.setViewportSize({ width: 1280, height: 800 })
  const first = state.inbox.items[0] as { runId?: string; nodeId?: string } | undefined
  const run = encodeURIComponent(String(first?.runId || 'run-1'))
  const node = encodeURIComponent(String(first?.nodeId || 'n1'))
  await page.goto(`/tag-filter-ux.html?page=gates&run=${run}&node=${node}`)
  await expect(page.getByTestId('gates-inbox-view')).toBeVisible({ timeout: 15_000 })
  await expect(page.getByRole('heading', { name: '待办' })).toHaveCount(0)
}

/** Confirm leave by the active pane identity, independent of chat header copy. */
function activeDetail(page: Page, runSuffix: string, nodeId: string) {
  return page.locator(`[data-testid="inbox-active-detail"][data-run-id="run-${runSuffix}"][data-node-id="${nodeId}"]`)
}

test.describe('Inbox 确认发起即离开待办', () => {
  test('Approve 确认：回复打到节点路径，完成后留在该运行并跟随下一节点', async ({ page }) => {
    const state: State = {
      inbox: { items: [approveItem(), neighborItem()], total: 2 },
      replyDelayMs: 400,
      replyFail: false,
      resumeDelayMs: 0,
      resumeFail: false,
      replyResolved: false,
      resumeResolved: false,
      forceReplyCount: 0,
    }
    await openGates(page, state)

    const opened = activeDetail(page, 'approve', 'approve_7gl6')
    await expect(opened).toBeVisible({ timeout: 15_000 })
    const confirm = page.getByTestId('clarify-confirm-flow').or(page.getByRole('button', { name: '确认并流转' }))
    await expect(confirm.first()).toBeVisible({ timeout: 15_000 })
    await page.screenshot({ path: `${SHOT}/01-before-confirm.png`, fullPage: true })

    await confirm.first().click()
    await acceptConfirmFlow(page)

    await expect.poll(() => state.replyResolved, { timeout: 8000 }).toBe(true)
    expect(state.forceReplyCount).toBe(1)
    await expect(page.getByTestId('thought-summary-state').getByText('已确认并流转')).toBeVisible({ timeout: 5000 })
    await expect(page.getByText('等待下一节点开始')).toBeVisible({ timeout: 5000 })
    await expect(opened).toBeVisible()
    await page.screenshot({ path: `${SHOT}/03-after-reply-ok.png`, fullPage: true })
  })

  test('连续两个 Approve：当前对话的回复打到自己的节点，不串到邻居', async ({ page }) => {
    const state: State = {
      inbox: { items: [approveItem(), approveItemNeighbor()], total: 2 },
      replyDelayMs: 200,
      replyFail: false,
      resumeDelayMs: 0,
      resumeFail: false,
      replyResolved: false,
      resumeResolved: false,
      forceReplyCount: 0,
    }
    const replyPaths: string[] = []
    page.on('request', (req) => {
      const url = new URL(req.url())
      if (req.method() === 'POST' && /\/nodes\/[^/]+\/reply$/.test(url.pathname)) replyPaths.push(url.pathname)
    })
    await openGates(page, state)

    const opened = activeDetail(page, 'approve', 'approve_7gl6')
    await expect(opened).toBeVisible({ timeout: 15_000 })
    const confirm = page.getByTestId('clarify-confirm-flow').or(page.getByRole('button', { name: '确认并流转' }))
    await expect(confirm.first()).toBeVisible({ timeout: 15_000 })
    await confirm.first().click()
    await acceptConfirmFlow(page)

    await expect.poll(() => state.forceReplyCount, { timeout: 8000 }).toBe(1)
    expect(replyPaths.some((p) => p.endsWith('/nodes/approve_7gl6/reply'))).toBe(true)
    expect(replyPaths.some((p) => p.includes('/nodes/approve_7gl7/'))).toBe(false)
    await expect(opened).toBeVisible()
    await expect(page.getByText('等待下一节点开始')).toBeVisible({ timeout: 5000 })
    await page.screenshot({ path: `${SHOT}/09-both-confirmed.png`, fullPage: true })
  })

  test('Approve 确认失败：条目回显且可再点确认', async ({ page }) => {
    const state: State = {
      inbox: { items: [approveItem()], total: 1 },
      replyDelayMs: 400,
      replyFail: true,
      resumeDelayMs: 0,
      resumeFail: false,
      replyResolved: false,
      resumeResolved: false,
      forceReplyCount: 0,
    }
    await openGates(page, state)

    const opened = activeDetail(page, 'approve', 'approve_7gl6')
    await expect(opened).toBeVisible({ timeout: 15_000 })
    const confirm = page.getByTestId('clarify-confirm-flow').or(page.getByRole('button', { name: '确认并流转' }))
    await expect(confirm.first()).toBeVisible({ timeout: 15_000 })
    await confirm.first().click()
    await acceptConfirmFlow(page)

    // Brief leave then restore on failure (g1.3).
    await expect.poll(() => state.replyResolved, { timeout: 5000 }).toBe(true)
    await expect(opened).toBeVisible({ timeout: 5000 })
    await expect(
      page.getByTestId('clarify-confirm-flow').or(page.getByRole('button', { name: '确认并流转' })).first(),
    ).toBeVisible({ timeout: 5000 })

    await page.screenshot({ path: `${SHOT}/04-fail-restored.png`, fullPage: true })

    // Retry path unlocked and posts to the same node.
    state.replyFail = false
    state.replyResolved = false
    state.replyDelayMs = 100
    await page.getByTestId('clarify-confirm-flow').or(page.getByRole('button', { name: '确认并流转' })).first().click()
    await acceptConfirmFlow(page)
    await expect.poll(() => state.replyResolved, { timeout: 8000 }).toBe(true)
    await expect(page.getByTestId('thought-summary-state').getByText('已确认并流转')).toBeVisible({ timeout: 5000 })
    await expect(page.getByText('等待下一节点开始')).toBeVisible({ timeout: 5000 })
    await expect(opened).toBeVisible()
    await page.screenshot({ path: `${SHOT}/05-retry-left.png`, fullPage: true })
  })

  test('Gate 通过：resume 未返回前条目已离开', async ({ page }) => {
    const state: State = {
      inbox: { items: [gateItem(), neighborItem()], total: 2 },
      replyDelayMs: 0,
      replyFail: false,
      resumeDelayMs: 2500,
      resumeFail: false,
      replyResolved: false,
      resumeResolved: false,
      forceReplyCount: 0,
    }
    await openGates(page, state)

    const gateTitle = page.getByText('方案评审门禁')
    await expect(gateTitle).toBeVisible({ timeout: 15_000 })
    // Gate positive exit is labeled 确认并流转 (not bare 通过) in GateApproval.
    const pass = page.getByTestId('review-composer-pass').or(page.getByRole('button', { name: /确认并流转|通过/ }))
    await expect(pass.first()).toBeVisible({ timeout: 15_000 })
    await page.screenshot({ path: `${SHOT}/06-gate-before.png`, fullPage: true })
    await pass.first().click()
    await acceptConfirmFlow(page)

    await expect(gateTitle).toHaveCount(0, { timeout: 1500 })
    expect(state.resumeResolved).toBe(false)
    await expect(page.getByText('邻居门禁', { exact: false }).or(page.getByText('邻居条目')).first()).toBeVisible()
    await page.screenshot({ path: `${SHOT}/07-gate-left-before-resume.png`, fullPage: true })
    await expect.poll(() => state.resumeResolved, { timeout: 5000 }).toBe(true)
  })
})
