/**
 * Acceptance E2E for CancelRun UI on RunDetailView (mocked API).
 * Harness page loads /runs/run-responsive-e2e via run-detail-real-main.ts.
 */
import { expect, test, type Page } from '@playwright/test'

const RUN_ID = 'run-responsive-e2e'
const WF_ID = 'wf-cancel-e2e'

const nodes = [
  { id: 'input', type: 'input', label: '输入', position: { x: 0, y: 0 }, config: {} },
  { id: 'agent', type: 'agent', label: '执行', position: { x: 180, y: 0 }, config: {} },
]

function buildRun(status: string) {
  return {
    id: RUN_ID,
    workflowId: WF_ID,
    workflowName: '取消验收工作流',
    workflowVersion: 1,
    status,
    trigger: 'manual',
    startedAt: '2026-07-24T12:00:00Z',
    durationSec: 42,
    progress: status === 'cancelled' ? 0.4 : status === 'completed' || status === 'failed' ? 1 : 0.4,
    branch: 'feature/web-cancel-run',
    git: { pushed: false, branch: 'feature/web-cancel-run' },
    nodeRuns: {
      input: {
        nodeId: 'input',
        status: 'completed',
        startedAt: '2026-07-24T12:00:00Z',
        durationSec: 1,
        varsSnapshot: {},
        outputs: {},
      },
      agent: {
        nodeId: 'agent',
        status:
          status === 'running'
            ? 'running'
            : status === 'waiting_human'
              ? 'waiting_human'
              : status === 'queued'
                ? 'pending'
                : status === 'cancelled'
                  ? 'cancelled'
                  : 'completed',
        startedAt: '2026-07-24T12:00:01Z',
        durationSec: 40,
        varsSnapshot: {},
        outputs: {},
      },
    },
    nodeExecutions: {},
    artifacts: [],
    trace: [],
    vars: [],
    nodes,
    edges: [{ id: 'e1', source: 'input', target: 'agent' }],
  }
}

async function openRunDetail(
  page: Page,
  status: string,
  opts?: { onCancel?: () => void; afterCancelStatus?: string; slowCancel?: boolean },
) {
  await page.setViewportSize({ width: 1280, height: 900 })
  let currentStatus = status

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

    if (method === 'POST' && path === `/api/runs/${RUN_ID}/cancel`) {
      opts?.onCancel?.()
      currentStatus = opts?.afterCancelStatus || 'cancelled'
      if (opts?.slowCancel) await new Promise((r) => setTimeout(r, 600))
      await route.fulfill({ json: { status: 'cancelled' } })
      return
    }

    if (path.includes('/events')) {
      await route.fulfill({ json: { events: [], nextCursor: '', hasMore: false, live: false } })
      return
    }
    if (path.includes('/sandbox-log')) {
      await route.fulfill({ json: { content: '', live: false, found: false } })
      return
    }
    if (path === `/api/runs/${RUN_ID}`) {
      await route.fulfill({ json: buildRun(currentStatus) })
      return
    }
    if (path === '/api/runs' || path.startsWith('/api/runs')) {
      await route.fulfill({ json: { items: [buildRun(currentStatus)], total: 1 } })
      return
    }
    await route.fulfill({ status: 404, json: { error: 'not mocked' } })
  })

  await page.goto('/run-detail-real.html')
  await expect(page.getByTestId('run-detail-root')).toBeVisible({ timeout: 15_000 })
}

test.describe('CancelRun UI acceptance', () => {
  test('queued: cancel visible, confirm copy distinguishes stop from delete, success stays on detail', async ({
    page,
  }) => {
    let cancelCalled = false
    await openRunDetail(page, 'queued', {
      onCancel: () => {
        cancelCalled = true
      },
    })
    const btn = page.getByTestId('cancel-run-btn')
    await expect(btn).toBeVisible()
    await expect(btn).toBeEnabled()
    await page.screenshot({ path: '/tmp/cancel-run-queued-detail.png', fullPage: true })

    await btn.click()
    await expect(page.getByText('取消运行？')).toBeVisible()
    await expect(page.getByText(/停止并变为/)).toBeVisible()
    await expect(page.getByText(/不可恢复为继续执行/)).toBeVisible()
    await expect(page.getByText(/全部恢复点会被清理，运行历史保留/)).toBeVisible()
    await expect(page.getByText(/永久删除/)).toHaveCount(0)
    await page.screenshot({ path: '/tmp/cancel-run-queued-confirm.png', fullPage: true })

    await page.getByRole('button', { name: '取消', exact: true }).click()
    await expect(page.getByText('取消运行？')).toHaveCount(0)
    await expect(cancelCalled).toBe(false)

    await btn.click()
    await page.getByTestId('confirm-cancel-run-btn').click()
    await expect.poll(() => cancelCalled).toBe(true)
    await expect(page.getByTestId('run-detail-root')).toBeVisible()
    await expect(page.getByTestId('cancel-run-btn')).toHaveCount(0)
    await page.screenshot({ path: '/tmp/cancel-run-queued-after.png', fullPage: true })
  })

  test('confirm cancel shows 取消中… pending copy', async ({ page }) => {
    let cancelCalled = false
    await openRunDetail(page, 'running', {
      onCancel: () => {
        cancelCalled = true
      },
      slowCancel: true,
    })
    await page.getByTestId('cancel-run-btn').click()
    const confirm = page.getByTestId('confirm-cancel-run-btn')
    await confirm.click()
    await expect(confirm).toHaveText(/取消中/)
    await expect.poll(() => cancelCalled).toBe(true)
    await expect(page.getByTestId('run-detail-root')).toBeVisible()
  })

  test('running: cancel confirm posts and refreshes to cancelled', async ({ page }) => {
    let cancelCalled = false
    await openRunDetail(page, 'running', {
      onCancel: () => {
        cancelCalled = true
      },
    })
    await page.getByTestId('cancel-run-btn').click()
    await page.getByTestId('confirm-cancel-run-btn').click()
    await expect.poll(() => cancelCalled).toBe(true)
    await expect(page.getByTestId('run-detail-root')).toBeVisible()
    await expect(page.getByTestId('cancel-run-btn')).toHaveCount(0)
  })

  test('waiting_human: cancel entry is available', async ({ page }) => {
    await openRunDetail(page, 'waiting_human')
    await expect(page.getByTestId('cancel-run-btn')).toBeVisible()
  })

  test('completed: cancel entry is hidden', async ({ page }) => {
    await openRunDetail(page, 'completed')
    await expect(page.getByTestId('cancel-run-btn')).toHaveCount(0)
  })
})
