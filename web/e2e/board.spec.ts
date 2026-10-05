import { test, expect } from '@playwright/test'
import { CLARIFY_CAPS } from '../src/test/capsFixtures'

function stubRun(partial: {
  id: string
  status: string
  title?: string
  workflowName?: string
  startedAt?: string
  progress?: number
  currentNodeLabel?: string
  durationSec?: number
}) {
  return {
    id: partial.id,
    workflowId: 'wf-1',
    workflowName: partial.workflowName || 'Demo Workflow',
    title: partial.title,
    status: partial.status,
    trigger: 'manual',
    startedAt: partial.startedAt || '2026-07-18T12:00:00Z',
    durationSec: partial.durationSec ?? 120,
    progress: partial.progress ?? 40,
    currentNodeLabel: partial.currentNodeLabel || '实现',
    nodeRuns: {},
    artifacts: [],
  }
}

const MOCK_RUNS = {
  running: [stubRun({ id: 'run-running-1', status: 'running', title: '看板需求-运行中' })],
  waiting_human: [stubRun({ id: 'run-waiting-1', status: 'waiting_human', title: '看板需求-等待人工', progress: 70 })],
  completed: [stubRun({ id: 'run-done-1', status: 'completed', title: '看板需求-已完成', progress: 100, durationSec: 600 })],
  failed: [stubRun({ id: 'run-fail-1', status: 'failed', title: '看板需求-失败', progress: 55 })],
  queued: [],
  cancelled: [],
  'running,waiting_human': [
    stubRun({ id: 'run-waiting-1', status: 'waiting_human', title: '看板需求-等待人工', startedAt: '2026-07-18T14:00:00Z' }),
    stubRun({ id: 'run-running-1', status: 'running', title: '看板需求-运行中', startedAt: '2026-07-18T10:00:00Z' }),
  ],
}

async function mockBoardApis(page: import('@playwright/test').Page) {
  await page.route('**/api/stats/dashboard', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          running: 1,
          waitingHuman: 1,
          failed: 1,
          completed: 1,
        }),
      })
      return
    }
    await route.continue()
  })

  await page.route('**/api/projects/*/token-stats**', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
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
        }),
      })
      return
    }
    await route.continue()
  })

  await page.route('**/api/workflows**', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            id: 'wf-approve',
            name: '自我迭代PRO',
            description: '开发前澄清 + 计划',
            status: 'published',
            version: 1,
            updatedAt: '2026-07-18T00:00:00Z',
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
          },
          {
            id: 'wf-react',
            name: '实现流',
            description: 'skip',
            status: 'published',
            version: 1,
            updatedAt: '2026-07-18T00:00:00Z',
            needsRepo: false,
            nodes: [
              { id: 'in', type: 'input', label: '开始', position: { x: 0, y: 0 }, config: {} },
              { id: 'r', type: 'agent', caps: CLARIFY_CAPS, label: '实现', position: { x: 0, y: 0 }, config: {} },
            ],
            edges: [{ id: 'e1', source: 'in', target: 'r' }],
          },
        ]),
      })
      return
    }
    await route.continue()
  })

  await page.route('**/api/runs**', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.continue()
      return
    }
    const url = new URL(route.request().url())
    // Fail-safe: unfiltered global list must not be used by project board.
    if (!url.searchParams.get('projectId')) {
      await route.fulfill({
        status: 400,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'projectId required in e2e mock' }),
      })
      return
    }
    const status = url.searchParams.get('status') || ''
    const items = (MOCK_RUNS as Record<string, unknown[]>)[status] || []
    const pageSize = Number(url.searchParams.get('pageSize') || items.length || 20)
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        items,
        total: items.length,
        page: 1,
        pageSize,
        hasMore: false,
      }),
    })
  })
}

async function gotoBoardHarness(
  page: import('@playwright/test').Page,
  opts: {
    width: number
    height?: number
    start?: 'dashboard' | 'board' | 'project-board'
    memory?: '0' | '1'
    projectId?: string
  } = { width: 1280 },
) {
  await page.setViewportSize({ width: opts.width, height: opts.height ?? 800 })
  await mockBoardApis(page)
  const qs = new URLSearchParams()
  if (opts.start) qs.set('start', opts.start)
  if (opts.memory) qs.set('memory', opts.memory)
  if (opts.projectId) qs.set('projectId', opts.projectId)
  const q = qs.toString()
  await page.goto(`/board.html${q ? `?${q}` : ''}`)
}

test.describe('需求进度看板（项目级）', () => {
  test('Dashboard 有项目记忆：首页 Composer + Approve 工作流卡片', async ({ page }) => {
    await gotoBoardHarness(page, { width: 1280, start: 'dashboard', memory: '1', projectId: 'proj-1' })
    await expect(page.getByTestId('dashboard-view')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('home-title')).toBeVisible()
    await expect(page.getByTestId('home-composer')).toBeVisible()
    await expect(page.getByTestId('home-workflow-card-wf-approve')).toContainText('自我迭代PRO')
    await expect(page.getByTestId('home-no-project')).toHaveCount(0)
    await expect(page.getByTestId('run-board-column')).toHaveCount(0)
  })

  test('Dashboard 无项目记忆：跨项目展示工作流，无先选项目门槛', async ({ page }) => {
    await gotoBoardHarness(page, { width: 1280, start: 'dashboard', memory: '0' })
    await expect(page.getByTestId('dashboard-view')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('home-no-project')).toHaveCount(0)
    await expect(page.getByTestId('home-composer')).toBeVisible()
    await expect(page.getByTestId('home-workflow-card-wf-approve')).toContainText('自我迭代PRO')
    await expect(page.getByTestId('run-board-column')).toHaveCount(0)
  })

  test('/board 有记忆重定向到项目看板', async ({ page }) => {
    await gotoBoardHarness(page, { width: 1280, start: 'board', memory: '1', projectId: 'proj-1' })
    await expect(page.getByTestId('board-view')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('projects-page')).toHaveCount(0)
    await expect(page.getByTestId('run-board-column')).toHaveCount(3)
  })

  test('/board 无记忆重定向到项目列表', async ({ page }) => {
    await gotoBoardHarness(page, { width: 1280, start: 'board', memory: '0' })
    await expect(page.getByTestId('projects-page')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('board-view')).toHaveCount(0)
  })

  test('项目看板三主列、侧滑预览关闭后不离页', async ({ page }) => {
    await gotoBoardHarness(page, { width: 1280, start: 'project-board', memory: '1', projectId: 'proj-1' })
    await expect(page.getByTestId('board-view')).toBeVisible({ timeout: 10_000 })
    const cols = page.getByTestId('run-board-column')
    await expect(cols).toHaveCount(3)
    await expect(cols.nth(0).locator('span').filter({ hasText: /^运行中$/ })).toBeVisible()
    await expect(cols.nth(1).locator('span').filter({ hasText: /^等待人工$/ })).toBeVisible()
    await expect(cols.nth(2).locator('span').filter({ hasText: /^已完成$/ })).toBeVisible()
    await expect(page.getByTestId('board-extra-columns')).toHaveCount(0)

    await page.getByText('看板需求-运行中').click()
    await expect(page.getByText('运行摘要')).toBeVisible({ timeout: 5_000 })
    await expect(page.getByRole('button', { name: '进入运行详情' })).toBeVisible()
    await page.getByRole('button', { name: '继续浏览' }).click()
    await expect(page.getByText('运行摘要')).toHaveCount(0)
    await expect(page.getByTestId('board-view')).toBeVisible()
    await expect(page.getByTestId('run-detail-page')).toHaveCount(0)
  })

  test('额外列筛选失败状态，查看更多跳转运行列表', async ({ page }) => {
    await gotoBoardHarness(page, { width: 1280, start: 'project-board', memory: '1', projectId: 'proj-1' })
    await expect(page.getByTestId('board-view')).toBeVisible({ timeout: 10_000 })

    await page.getByTestId('board-filter-failed').click()
    await expect(page.getByTestId('board-extra-columns')).toBeVisible()
    await expect(page.getByText('看板需求-失败')).toBeVisible()
    await expect(page.getByText('看板需求-已完成')).toBeVisible()

    await page.getByTestId('board-view-more-completed').click()
    await expect(page.getByTestId('runs-page')).toBeVisible({ timeout: 5_000 })
  })

  test('窄屏主列纵向堆叠仍可读', async ({ page }) => {
    await gotoBoardHarness(page, {
      width: 390,
      height: 844,
      start: 'project-board',
      memory: '1',
      projectId: 'proj-1',
    })
    await expect(page.getByTestId('board-view')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText('看板需求-运行中')).toBeVisible()
    const cols = page.getByTestId('run-board-column')
    await expect(cols).toHaveCount(3)
    const first = await cols.nth(0).boundingBox()
    const second = await cols.nth(1).boundingBox()
    expect(first && second).toBeTruthy()
    expect(second!.y).toBeGreaterThan(first!.y)
  })

  test('已完成列可独立滚动且分页策略不变', async ({ page }) => {
    const pageSizeByStatus: Record<string, number> = {}
    await page.setViewportSize({ width: 1280, height: 800 })

    await page.route('**/api/stats/dashboard', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ running: 20, waitingHuman: 1, failed: 0, completed: 24 }),
        })
        return
      }
      await route.continue()
    })

    await page.route('**/api/projects/*/token-stats**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
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
        }),
      })
    })

    await page.route('**/api/runs**', async (route) => {
      if (route.request().method() !== 'GET') {
        await route.continue()
        return
      }
      const url = new URL(route.request().url())
      if (!url.searchParams.get('projectId')) {
        await route.fulfill({
          status: 400,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'projectId required in e2e mock' }),
        })
        return
      }
      const status = url.searchParams.get('status') || ''
      const pageSize = Number(url.searchParams.get('pageSize') || 20)
      pageSizeByStatus[status] = pageSize

      let items: ReturnType<typeof stubRun>[] = []
      if (status === 'completed') {
        items = Array.from({ length: Math.min(20, pageSize) }, (_, i) =>
          stubRun({
            id: `run-done-${i}`,
            status: 'completed',
            title: `看板已完成-${i}`,
            progress: 100,
            durationSec: 600,
          }),
        )
      } else if (status === 'running') {
        items = Array.from({ length: Math.min(20, pageSize) }, (_, i) =>
          stubRun({
            id: `run-running-${i}`,
            status: 'running',
            title: `看板运行中-${i}`,
            progress: 40,
          }),
        )
      } else if (status === 'waiting_human') {
        items = [
          stubRun({
            id: 'run-waiting-1',
            status: 'waiting_human',
            title: '看板需求-等待人工',
            progress: 70,
          }),
        ]
      }

      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          items,
          total: status === 'completed' ? 24 : items.length,
          page: 1,
          pageSize,
          hasMore: status === 'completed',
        }),
      })
    })

    await page.goto('/board.html?start=project-board&memory=1&projectId=proj-1')
    await expect(page.getByTestId('board-view')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('run-board-column-body')).toHaveCount(3)

    expect(pageSizeByStatus.completed).toBe(20)
    expect(pageSizeByStatus.running).toBe(100)

    const completedCol = page.getByTestId('run-board-column').nth(2)
    const completedBody = completedCol.getByTestId('run-board-column-body')
    await expect(completedBody).toBeVisible()

    const overflow = await completedBody.evaluate((el) => ({
      scrollHeight: el.scrollHeight,
      clientHeight: el.clientHeight,
      overflowY: getComputedStyle(el).overflowY,
      maxHeight: getComputedStyle(el).maxHeight,
    }))
    expect(overflow.overflowY).toBe('auto')
    expect(overflow.scrollHeight).toBeGreaterThan(overflow.clientHeight)
    expect(overflow.maxHeight).toMatch(/px|vh/)

    const runningBody = page.getByTestId('run-board-column').nth(0).getByTestId('run-board-column-body')
    await completedBody.evaluate((el) => {
      el.scrollTop = 120
    })
    await runningBody.evaluate((el) => {
      el.scrollTop = 40
    })
    const tops = await page.evaluate(() => {
      const bodies = Array.from(document.querySelectorAll('[data-testid="run-board-column-body"]'))
      return bodies.map((el) => (el as HTMLElement).scrollTop)
    })
    expect(tops[0]).toBe(40)
    expect(tops[2]).toBe(120)

    const viewMore = page.getByTestId('board-view-more-completed')
    await expect(viewMore).toBeVisible()
    await viewMore.click()
    await expect(page.getByTestId('runs-page')).toBeVisible({ timeout: 5_000 })
  })

  test('三主列列头打开状态列表模态：加载更多、关闭三件套、点行进详情、空态', async ({ page }) => {
    test.setTimeout(60_000)
    const listCalls: { status: string; page: string; pageSize: string }[] = []

    await page.setViewportSize({ width: 1280, height: 800 })

    await page.route('**/api/stats/dashboard', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ running: 5, waitingHuman: 0, failed: 0, completed: 45 }),
        })
        return
      }
      await route.continue()
    })

    await page.route('**/api/projects/*/token-stats**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
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
        }),
      })
    })

    await page.route('**/api/runs**', async (route) => {
      if (route.request().method() !== 'GET') {
        await route.continue()
        return
      }
      const url = new URL(route.request().url())
      if (!url.searchParams.get('projectId')) {
        await route.fulfill({
          status: 400,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'projectId required in e2e mock' }),
        })
        return
      }
      const status = url.searchParams.get('status') || ''
      const pageNum = Number(url.searchParams.get('page') || 1)
      const pageSize = Number(url.searchParams.get('pageSize') || 20)
      listCalls.push({ status, page: String(pageNum), pageSize: String(pageSize) })

      // Modal lists always use pageSize=20; running/waiting columns use 100.
      const isModalList =
        pageSize === 20 && (status === 'running' || status === 'waiting_human' || (status === 'completed' && pageNum >= 1))

      // Prefer modal-shaped payloads when pageSize=20 (column completed also uses 20 — shared OK).
      if (status === 'completed' && pageSize === 20) {
        const start = (pageNum - 1) * pageSize
        const items = Array.from({ length: Math.min(20, Math.max(0, 45 - start)) }, (_, i) =>
          stubRun({
            id: `run-modal-done-${start + i}`,
            status: 'completed',
            title: `模态已完成-${start + i}`,
            progress: 100,
            durationSec: 90,
          }),
        )
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            items,
            total: 45,
            page: pageNum,
            pageSize,
            hasMore: start + items.length < 45,
          }),
        })
        return
      }

      if (status === 'waiting_human' && pageSize === 20) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ items: [], total: 0, page: 1, pageSize, hasMore: false }),
        })
        return
      }

      if (status === 'running' && pageSize === 20) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            items: [
              stubRun({ id: 'run-modal-running-1', status: 'running', title: '模态运行中-1', progress: 40 }),
            ],
            total: 1,
            page: 1,
            pageSize,
            hasMore: false,
          }),
        })
        return
      }

      void isModalList

      // Column cache loads for the board itself (running/waiting pageSize=100).
      let items: ReturnType<typeof stubRun>[] = []
      let total = 0
      if (status === 'running') {
        items = Array.from({ length: 5 }, (_, i) =>
          stubRun({ id: `run-running-${i}`, status: 'running', title: `看板运行中-${i}` }),
        )
        total = 5
      } else if (status === 'waiting_human') {
        items = []
        total = 0
      }

      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          items,
          total,
          page: 1,
          pageSize,
          hasMore: false,
        }),
      })
    })

    await page.goto('/board.html?start=project-board&memory=1&projectId=proj-1')
    await expect(page.getByTestId('board-view')).toBeVisible({ timeout: 10_000 })

    const headers = page.getByTestId('run-board-column-header')
    await expect(headers).toHaveCount(3)

    // --- completed: open + load more ---
    await headers.nth(2).click()
    await expect(page.getByTestId('board-status-list-body')).toBeVisible({ timeout: 5_000 })
    await expect(page.getByRole('dialog')).toBeVisible()
    await expect(page.getByTestId('board-status-list-subtitle')).toContainText('已完成')
    await expect(page.getByTestId('board-status-list-row-run-modal-done-0')).toBeVisible()
    await expect(page.getByTestId('board-status-list-range')).toContainText('已展示 20 / 45')
    await page.getByTestId('board-status-list-load-more').click()
    await expect(page.getByTestId('board-status-list-row-run-modal-done-20')).toBeVisible()
    await expect(page.getByTestId('board-status-list-range')).toContainText('已展示 40 / 45')

    // Esc closes and stays on board
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('board-status-list-body')).toHaveCount(0)
    await expect(page.getByTestId('board-view')).toBeVisible()
    await expect(page.getByTestId('run-detail-page')).toHaveCount(0)

    // --- running: open then backdrop close (click overlay corner, not dialog center) ---
    await headers.nth(0).click()
    await expect(page.getByTestId('board-status-list-row-run-modal-running-1')).toBeVisible()
    await page.locator('.fixed.inset-0.z-50 > .absolute.inset-0').click({ position: { x: 8, y: 8 } })
    await expect(page.getByTestId('board-status-list-body')).toHaveCount(0)
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expect(page.getByTestId('board-view')).toBeVisible()
    await expect(page.getByTestId('run-detail-page')).toHaveCount(0)

    // --- waiting_human empty state + explicit close button ---
    await headers.nth(1).click()
    await expect(page.getByTestId('board-status-list-empty')).toBeVisible()
    await expect(page.getByTestId('board-status-list-empty')).toContainText('该状态暂无运行')
    // AppModal chrome close (first button in the teleported shell)
    await page.locator('.fixed.inset-0.z-50 button').first().click()
    await expect(page.getByTestId('board-status-list-body')).toHaveCount(0)

    // --- row click → detail (no drawer) ---
    await headers.nth(2).click()
    await expect(page.getByTestId('board-status-list-row-run-modal-done-0')).toBeVisible()
    await page.getByTestId('board-status-list-row-run-modal-done-0').click()
    await expect(page.getByTestId('run-detail-page')).toBeVisible({ timeout: 5_000 })
    await expect(page.getByText('运行摘要')).toHaveCount(0)

    // Modal list used pageSize=20 with page param (independent of column cache).
    expect(listCalls.some((c) => c.status === 'completed' && c.pageSize === '20' && c.page === '1')).toBe(true)
    expect(listCalls.some((c) => c.status === 'completed' && c.pageSize === '20' && c.page === '2')).toBe(true)
  })

  test('列头模态与侧滑互斥；Dashboard 列头不可激活', async ({ page }) => {
    await gotoBoardHarness(page, { width: 1280, start: 'project-board', memory: '1', projectId: 'proj-1' })
    await expect(page.getByTestId('board-view')).toBeVisible({ timeout: 10_000 })

    // Open card drawer first
    await page.getByText('看板需求-运行中').click()
    await expect(page.getByText('运行摘要')).toBeVisible({ timeout: 5_000 })

    // Drawer overlay blocks board chrome hit-testing; click the header element
    // directly so openStatusList runs (closes drawer, opens list modal).
    await page.getByTestId('run-board-column-header').nth(0).evaluate((el: HTMLElement) => el.click())
    await expect(page.getByText('运行摘要')).toHaveCount(0)
    await expect(page.getByTestId('board-status-list-body')).toBeVisible({ timeout: 5_000 })

    // Close modal, open drawer again — modal must stay closed
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('board-status-list-body')).toHaveCount(0)
    await page.getByText('看板需求-运行中').click()
    await expect(page.getByText('运行摘要')).toBeVisible()
    await expect(page.getByTestId('board-status-list-body')).toHaveCount(0)

    // Dismiss drawer, then confirm headers still work
    await page.getByRole('button', { name: '继续浏览' }).click()
    await expect(page.getByText('运行摘要')).toHaveCount(0)

    // Home has no mini-board columns
    await gotoBoardHarness(page, { width: 1280, start: 'dashboard', memory: '1', projectId: 'proj-1' })
    await expect(page.getByTestId('dashboard-view')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('home-composer')).toBeVisible()
    await expect(page.getByTestId('run-board-column')).toHaveCount(0)
  })
})
