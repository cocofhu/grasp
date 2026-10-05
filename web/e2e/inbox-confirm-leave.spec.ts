/**
 * Browser acceptance for plan g1.2 / g1.3 / f1–f4:
 * Confirm initiation must leave Inbox pending immediately (before reactReply/resume returns);
 * failure restores the row; composer must not linger after leave.
 */
import { expect, test, type Page } from '@playwright/test'
import { mkdirSync } from 'node:fs'
import { CLARIFY_CAPS } from '../src/test/capsFixtures'

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

    if (method === 'POST' && path.match(/\/api\/runs\/[^/]+\/react\/[^/]+\/reply\/?$/)) {
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

    if (method === 'POST' && path.match(/\/api\/runs\/[^/]+\/gates\/[^/]+\/resume\/?$/)) {
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
    if (method === 'GET' && (path === '/api/projects' || path.endsWith('/api/projects'))) {
      await route.fulfill({ json: [{ id: 'proj-1', name: 'Grasp', slug: 'grasp' }] })
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
  await page.goto('/tag-filter-ux.html?page=gates')
  await expect(page.getByRole('heading', { name: '待办' })).toBeVisible({ timeout: 15_000 })
}

function approveRow(page: Page) {
  return page.locator('button').filter({ hasText: '确认后应离开待办' })
}

test.describe('Inbox 确认发起即离开待办', () => {
  test('Approve 确认：reactReply 未返回前条目已离开，操作区消失', async ({ page }) => {
    const state: State = {
      inbox: { items: [approveItem(), neighborItem()], total: 2 },
      replyDelayMs: 2500,
      replyFail: false,
      resumeDelayMs: 0,
      resumeFail: false,
      replyResolved: false,
      resumeResolved: false,
      forceReplyCount: 0,
    }
    await openGates(page, state)

    await approveRow(page).first().click()
    const confirm = page.getByTestId('clarify-confirm-flow').or(page.getByRole('button', { name: '确认并流转' }))
    await expect(confirm.first()).toBeVisible({ timeout: 15_000 })
    await page.screenshot({ path: `${SHOT}/01-before-confirm.png`, fullPage: true })

    await confirm.first().click()

    // Must leave before delayed reactReply resolves (plan g1.2 / ~3s SLA).
    await expect(approveRow(page)).toHaveCount(0, { timeout: 1500 })
    expect(state.replyResolved).toBe(false)
    // Approve composer gone; neighbor gate may still show its own 确认并流转 — that is OK.
    await expect(page.getByTestId('clarify-confirm-flow')).toHaveCount(0)
    await expect(page.getByText('确认后应离开待办')).toHaveCount(0)
    await expect(page.getByText('邻居门禁', { exact: false }).or(page.getByText('邻居条目')).first()).toBeVisible()

    await page.screenshot({ path: `${SHOT}/02-left-before-reply.png`, fullPage: true })

    await expect.poll(() => state.replyResolved, { timeout: 5000 }).toBe(true)
    await expect(approveRow(page)).toHaveCount(0)
    await page.screenshot({ path: `${SHOT}/03-after-reply-ok.png`, fullPage: true })
  })

  test('plan g2.2: 连续两个 Approve，首条 reply 未返回即可确认邻居', async ({ page }) => {
    const state: State = {
      inbox: { items: [approveItem(), approveItemNeighbor()], total: 2 },
      replyDelayMs: 2500,
      replyFail: false,
      resumeDelayMs: 0,
      resumeFail: false,
      replyResolved: false,
      resumeResolved: false,
      forceReplyCount: 0,
    }
    await openGates(page, state)

    await approveRow(page).first().click()
    const confirm = page.getByTestId('clarify-confirm-flow').or(page.getByRole('button', { name: '确认并流转' }))
    await expect(confirm.first()).toBeVisible({ timeout: 15_000 })
    await confirm.first().click()

    // First left; second auto-selected while first reply still pending.
    await expect(approveRow(page)).toHaveCount(0, { timeout: 1500 })
    expect(state.forceReplyCount).toBe(0)
    await expect(page.getByText('第二个待确认')).toBeVisible({ timeout: 5000 })

    const confirmB = page.getByTestId('clarify-confirm-flow').or(page.getByRole('button', { name: '确认并流转' }))
    await expect(confirmB.first()).toBeVisible({ timeout: 10_000 })
    await expect(confirmB.first()).toBeEnabled()
    await page.screenshot({ path: `${SHOT}/08-neighbor-ready-while-first-pending.png`, fullPage: true })
    await confirmB.first().click()

    // Second must also leave before either delayed reply resolves (no global lock swallow).
    await expect(page.getByText('第二个待确认')).toHaveCount(0, { timeout: 1500 })
    expect(state.forceReplyCount).toBeLessThan(2)

    await expect.poll(() => state.forceReplyCount, { timeout: 8000 }).toBe(2)
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

    await approveRow(page).first().click()
    const confirm = page.getByTestId('clarify-confirm-flow').or(page.getByRole('button', { name: '确认并流转' }))
    await expect(confirm.first()).toBeVisible({ timeout: 15_000 })
    await confirm.first().click()

    // Brief leave then restore on failure (g1.3).
    await expect.poll(() => state.replyResolved, { timeout: 5000 }).toBe(true)
    await expect(approveRow(page)).toHaveCount(1, { timeout: 5000 })
    await expect(
      page.getByTestId('clarify-confirm-flow').or(page.getByRole('button', { name: '确认并流转' })).first(),
    ).toBeVisible({ timeout: 5000 })

    await page.screenshot({ path: `${SHOT}/04-fail-restored.png`, fullPage: true })

    // Retry path unlocked.
    state.replyFail = false
    state.replyResolved = false
    state.replyDelayMs = 100
    await page.getByTestId('clarify-confirm-flow').or(page.getByRole('button', { name: '确认并流转' })).first().click()
    await expect(approveRow(page)).toHaveCount(0, { timeout: 5000 })
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

    const gateRow = page.locator('button').filter({ hasText: '方案评审门禁' }).or(page.locator('button').filter({ hasText: '方案门禁' }))
    await gateRow.first().click()
    // Gate positive exit is labeled 确认并流转 (not bare 通过) in GateApproval.
    const pass = page.getByTestId('review-composer-pass').or(page.getByRole('button', { name: /确认并流转|通过/ }))
    await expect(pass.first()).toBeVisible({ timeout: 15_000 })
    await page.screenshot({ path: `${SHOT}/06-gate-before.png`, fullPage: true })
    await pass.first().click()

    await expect(gateRow).toHaveCount(0, { timeout: 1500 })
    expect(state.resumeResolved).toBe(false)
    await expect(page.getByText('邻居门禁', { exact: false }).or(page.getByText('邻居条目')).first()).toBeVisible()
    await page.screenshot({ path: `${SHOT}/07-gate-left-before-resume.png`, fullPage: true })
    await expect.poll(() => state.resumeResolved, { timeout: 5000 }).toBe(true)
  })
})
