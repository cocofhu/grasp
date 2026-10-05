/**
 * Acceptance E2E for DeleteRun UI on RunListView (mocked API).
 * Harness: run-list.html → RunListView at /runs.
 */
import { expect, test, type Page } from '@playwright/test'

type RunStatus = 'queued' | 'running' | 'waiting_human' | 'completed' | 'failed' | 'cancelled'

function buildRun(id: string, status: RunStatus, name: string) {
  return {
    id,
    workflowId: 'wf-list-delete',
    workflowName: name,
    workflowVersion: 1,
    title: name,
    status,
    trigger: 'manual',
    startedAt: '2026-07-24T12:00:00Z',
    createdAt: '2026-07-24T11:59:00Z',
    durationSec: 42,
    progress: status === 'completed' || status === 'failed' || status === 'cancelled' ? 1 : 0.4,
    priority: 'normal',
    currentNodeLabel: status === 'running' || status === 'waiting_human' ? '执行中' : undefined,
  }
}

async function openRunList(
  page: Page,
  opts?: {
    width?: number
    height?: number
    runs?: ReturnType<typeof buildRun>[]
    onDelete?: (id: string) => void
    deleteFailStatus?: number
  },
) {
  const width = opts?.width ?? 1280
  const height = opts?.height ?? 900
  await page.setViewportSize({ width, height })

  let runs =
    opts?.runs ??
    [
      buildRun('run-completed', 'completed', '已结束工作流'),
      buildRun('run-failed', 'failed', '失败工作流'),
      buildRun('run-running', 'running', '运行中工作流'),
      buildRun('run-cancelled', 'cancelled', '已取消工作流'),
    ]

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

    if (method === 'DELETE' && path.startsWith('/api/runs/')) {
      const id = path.slice('/api/runs/'.length)
      if (opts?.deleteFailStatus) {
        await route.fulfill({
          status: opts.deleteFailStatus,
          json: { error: opts.deleteFailStatus === 409 ? 'cannot delete run' : 'not found' },
        })
        return
      }
      opts?.onDelete?.(id)
      runs = runs.filter((r) => r.id !== id)
      await route.fulfill({ json: { status: 'deleted' } })
      return
    }

    if (path === '/api/runs' || (path.startsWith('/api/runs') && method === 'GET' && !path.slice('/api/runs'.length).includes('/'))) {
      await route.fulfill({ json: { items: runs, total: runs.length } })
      return
    }
    if (path === '/api/workflows') {
      await route.fulfill({ json: [] })
      return
    }
    if (path === '/api/projects') {
      await route.fulfill({ json: [] })
      return
    }
    await route.fulfill({ status: 404, json: { error: 'not mocked' } })
  })

  await page.goto('/run-list.html')
  await expect(page.getByRole('heading', { name: '运行记录' })).toBeVisible({ timeout: 15_000 })
  await expect(page.getByText('按项目、工作流与状态筛选')).toBeVisible()
}

test.describe('RunListView delete run acceptance', () => {
  test('completed: delete confirm mentions not WorkflowDef, success stays on list', async ({ page }) => {
    let deletedId = ''
    await openRunList(page, {
      onDelete: (id) => {
        deletedId = id
      },
    })

    const completedRow = page.locator('tr', { hasText: '已结束工作流' })
    await expect(completedRow.getByTestId('delete-run-btn')).toBeVisible()
    await expect(completedRow.getByTestId('cancel-run-btn')).toHaveCount(0)

    await completedRow.getByTestId('delete-run-btn').click()
    await expect(page.getByText('确认删除该次运行？')).toBeVisible()
    await expect(page.getByText(/不会删除工作流定义/)).toBeVisible()
    await expect(page.getByText(/删除成功后列表将刷新/)).toBeVisible()

    // Dismiss without deleting (modal footer ghost cancel, not list cancel-run-btn)
    await page
      .locator('.fixed.inset-0')
      .filter({ hasText: '确认删除该次运行？' })
      .getByRole('button', { name: '取消' })
      .click()
    await expect(page.getByText('确认删除该次运行？')).toHaveCount(0)
    await expect(page.getByText('已结束工作流', { exact: true })).toBeVisible()

    await completedRow.getByTestId('delete-run-btn').click()
    await page.getByTestId('confirm-delete-run-btn').click()
    await expect.poll(() => deletedId).toBe('run-completed')
    await expect(page.getByText('已结束工作流', { exact: true })).toHaveCount(0)
    await expect(page.getByRole('heading', { name: '运行' })).toBeVisible()
    await expect(page.getByText(/已删除运行/)).toBeVisible()
    await expect(page.getByTestId('run-detail-page')).toHaveCount(0)
  })

  test('running: only cancel; cancelled: delete enabled like completed', async ({ page }) => {
    await openRunList(page)

    const runningRow = page.locator('tr', { hasText: '运行中工作流' })
    await expect(runningRow.getByTestId('cancel-run-btn')).toBeVisible()
    await expect(runningRow.getByTestId('delete-run-btn')).toHaveCount(0)

    const cancelledRow = page.locator('tr', { hasText: '已取消工作流' })
    await expect(cancelledRow.getByTestId('delete-run-btn')).toBeVisible()
    await expect(cancelledRow.getByTestId('cancel-run-btn')).toHaveCount(0)
    await expect(cancelledRow.getByTestId('run-ops-placeholder')).toHaveCount(0)
  })

  test('cancelled: confirm delete removes row from list', async ({ page }) => {
    let deletedId = ''
    await openRunList(page, {
      onDelete: (id) => {
        deletedId = id
      },
    })

    const cancelledRow = page.locator('tr', { hasText: '已取消工作流' })
    await cancelledRow.getByTestId('delete-run-btn').click()
    await expect(page.getByText('确认删除该次运行？')).toBeVisible()
    await page.getByTestId('confirm-delete-run-btn').click()
    await expect.poll(() => deletedId).toBe('run-cancelled')
    await expect(page.getByText('已取消工作流', { exact: true })).toHaveCount(0)
    await expect(page.getByRole('heading', { name: '运行' })).toBeVisible()
    await expect(page.getByText(/已删除运行/)).toBeVisible()
  })

  test('delete failure keeps row and shows error', async ({ page }) => {
    await openRunList(page, { deleteFailStatus: 409 })
    const failedRow = page.locator('tr', { hasText: '失败工作流' })
    await failedRow.getByTestId('delete-run-btn').click()
    await page.getByTestId('confirm-delete-run-btn').click()
    await expect(page.getByText('当前状态不可删除')).toBeVisible()
    await expect(page.getByText('失败工作流', { exact: true })).toBeVisible()
  })

  test('mobile cards: delete / cancel / cancelled parity', async ({ page }) => {
    await openRunList(page, { width: 390, height: 844 })
    await expect(page.locator('table')).toHaveCount(0)

    // Cards use RouterLink custom → role=link (not role=button / bare <a>)
    const completedCard = page.locator('[role="link"]', { hasText: '已结束工作流' })
    await expect(completedCard.getByTestId('delete-run-btn')).toBeVisible()
    await expect(completedCard.getByTestId('cancel-run-btn')).toHaveCount(0)

    const runningCard = page.locator('[role="link"]', { hasText: '运行中工作流' })
    await expect(runningCard.getByTestId('cancel-run-btn')).toBeVisible()
    await expect(runningCard.getByTestId('delete-run-btn')).toHaveCount(0)

    const cancelledCard = page.locator('[role="link"]', { hasText: '已取消工作流' })
    await expect(cancelledCard.getByTestId('delete-run-btn')).toBeVisible()
    await expect(cancelledCard.getByTestId('run-ops-placeholder')).toHaveCount(0)

    await completedCard.getByTestId('delete-run-btn').click()
    await expect(page.getByText('确认删除该次运行？')).toBeVisible()
    await expect(page.getByTestId('run-detail-page')).toHaveCount(0)
    // Ops stop must not navigate away from the list
    await expect(page.getByRole('heading', { name: '运行' })).toBeVisible()

    // Cancel on running card: confirm only, no jump to /runs/:id
    await page
      .locator('.fixed.inset-0')
      .filter({ hasText: '确认删除该次运行？' })
      .getByRole('button', { name: '取消' })
      .click()
    await runningCard.getByTestId('cancel-run-btn').click()
    await expect(page.getByText('取消运行？')).toBeVisible()
    await expect(page.getByTestId('run-detail-page')).toHaveCount(0)
    await expect(page).toHaveURL(/run-list\.html/)
    await expect(page.getByRole('heading', { name: '运行' })).toBeVisible()
  })
})
