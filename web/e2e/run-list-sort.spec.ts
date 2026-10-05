/**
 * Acceptance E2E for RunListView desktop sort (mocked API + real component).
 * Covers: sortable headers, first-click desc, toggle, URL sync, illegal fallback,
 * page reset, no restore-default control, mobile has no sort controls.
 */
import { expect, test, type Page, type Request } from '@playwright/test'

type Prio = 'high' | 'normal' | 'low'

function stubRun(partial: {
  id: string
  status?: string
  title?: string
  priority?: Prio
  startedAt?: string
  createdAt?: string
}) {
  return {
    id: partial.id,
    workflowId: 'wf-1',
    workflowName: 'Demo Workflow',
    title: partial.title || partial.id,
    status: partial.status || 'completed',
    trigger: 'manual',
    priority: partial.priority || 'normal',
    startedAt: partial.startedAt || '2026-07-25T10:00:00Z',
    createdAt: partial.createdAt || '2026-07-25T09:00:00Z',
    durationSec: 60,
    progress: 1,
    nodeRuns: {},
    artifacts: [],
  }
}

/** 22 rows so pagination appears (PAGE_SIZE=20). */
function buildAllRuns() {
  const runs = []
  for (let i = 1; i <= 22; i++) {
    const prio: Prio = i <= 7 ? 'high' : i <= 14 ? 'normal' : 'low'
    runs.push(
      stubRun({
        id: `run-${String(i).padStart(3, '0')}`,
        title: `运行 #${i}`,
        priority: prio,
        status: i === 2 ? 'queued' : 'completed',
        startedAt: `2026-07-25T${String(10 + (i % 8)).padStart(2, '0')}:00:00Z`,
        createdAt: `2026-07-25T${String(9 + (i % 8)).padStart(2, '0')}:30:00Z`,
      }),
    )
  }
  return runs
}

const ALL_RUNS = buildAllRuns()

function sortRuns(items: typeof ALL_RUNS, sort: string | null, order: string | null) {
  const list = items.slice()
  const dir = order === 'asc' ? 1 : -1
  list.sort((a, b) => {
    let av: number | string = 0
    let bv: number | string = 0
    if (sort === 'priority') {
      const rank = { high: 3, normal: 2, low: 1 } as const
      av = rank[a.priority]
      bv = rank[b.priority]
    } else {
      // hybrid-ish for mock: queued uses createdAt, else startedAt
      av = a.status === 'queued' ? a.createdAt : a.startedAt
      bv = b.status === 'queued' ? b.createdAt : b.startedAt
    }
    if (av === bv) return a.id < b.id ? 1 : -1
    return av < bv ? -dir : dir
  })
  return list
}

async function mockApis(page: Page, capture: { requests: Request[] }) {
  await page.route('**/api/**', async (route) => {
    // Skip Vite module URLs like /@fs/.../src/lib/api/api.ts (pathname is not /api/...)
    if (!new URL(route.request().url()).pathname.startsWith('/api/')) {
      await route.continue()
      return
    }
    const req = route.request()
    const url = new URL(req.url())
    const path = url.pathname
    if (req.method() === 'GET' && (path === '/api/runs' || path.endsWith('/api/runs'))) {
      capture.requests.push(req)
      const sort = url.searchParams.get('sort')
      const order = url.searchParams.get('order')
      const pageNum = Number(url.searchParams.get('page') || '1')
      const pageSize = Number(url.searchParams.get('pageSize') || '20')
      const sorted = sortRuns(
        ALL_RUNS,
        sort === 'priority' || sort === 'started_at' ? sort : null,
        order === 'asc' || order === 'desc' ? order : null,
      )
      const start = (pageNum - 1) * pageSize
      const items = sorted.slice(start, start + pageSize)
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items, total: sorted.length }),
      })
      return
    }
    if (path.endsWith('/api/workflows') || path === '/api/workflows') {
      await route.fulfill({
        json: [{ id: 'wf-1', name: 'Demo Workflow', status: 'published', version: 1, nodes: [], edges: [] }],
      })
      return
    }
    if (path.endsWith('/api/projects') || path === '/api/projects') {
      await route.fulfill({
        json: [{ id: 'proj-1', name: 'Grasp', slug: 'grasp' }],
      })
      return
    }
    await route.fulfill({ status: 404, json: { error: 'not mocked' } })
  })
}

async function openRunList(page: Page, qs = '', capture = { requests: [] as Request[] }) {
  await page.setViewportSize({ width: 1280, height: 900 })
  await mockApis(page, capture)
  await page.goto(`/run-list-sort.html${qs ? `?${qs}` : ''}`)
  await expect(page.getByRole('heading', { name: '运行' })).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('table')).toBeVisible({ timeout: 10_000 })
  return capture
}

function hashQuery(page: Page) {
  const hash = page.url().split('#')[1] || ''
  const q = hash.includes('?') ? hash.slice(hash.indexOf('?') + 1) : ''
  return new URLSearchParams(q)
}

test.describe('RunListView sort acceptance', () => {
  test('default: headers inactive, no restore control; first click priority=desc + URL', async ({
    page,
  }) => {
    const capture = await openRunList(page)
    const startedTh = page.locator('th.sortable', { hasText: '开始时间' })
    const priorityTh = page.locator('th.sortable', { hasText: '优先级' })
    await expect(startedTh).toHaveAttribute('aria-sort', 'none')
    await expect(priorityTh).toHaveAttribute('aria-sort', 'none')
    await expect(startedTh).not.toHaveClass(/active/)
    await expect(priorityTh).not.toHaveClass(/active/)
    await expect(page.getByRole('button', { name: /恢复默认/ })).toHaveCount(0)
    await expect(page.locator('th.sortable')).toHaveCount(2)

    await page.screenshot({ path: '/tmp/run-list-sort-default.png', fullPage: true })

    await priorityTh.click()
    await expect(priorityTh).toHaveAttribute('aria-sort', 'descending')
    await expect(priorityTh).toHaveClass(/active/)
    await expect(priorityTh).toHaveClass(/desc/)
    await expect(startedTh).toHaveAttribute('aria-sort', 'none')

    await expect.poll(() => hashQuery(page).get('sort')).toBe('priority')
    await expect.poll(() => hashQuery(page).get('order')).toBe('desc')

    await expect
      .poll(() => {
        const last = capture.requests.filter((r) => r.url().includes('/api/runs')).at(-1)
        if (!last) return ''
        const u = new URL(last.url())
        return `${u.searchParams.get('sort')}|${u.searchParams.get('order')}`
      })
      .toBe('priority|desc')

    await page.screenshot({ path: '/tmp/run-list-sort-priority-desc.png', fullPage: true })
  })

  test('same column toggles asc; switching column activates new key', async ({ page }) => {
    await openRunList(page)
    const startedTh = page.locator('th.sortable', { hasText: '开始时间' })
    const priorityTh = page.locator('th.sortable', { hasText: '优先级' })

    await priorityTh.click()
    await expect(priorityTh).toHaveAttribute('aria-sort', 'descending')
    await priorityTh.click()
    await expect(priorityTh).toHaveAttribute('aria-sort', 'ascending')
    await expect.poll(() => hashQuery(page).get('order')).toBe('asc')
    await page.screenshot({ path: '/tmp/run-list-sort-priority-asc.png', fullPage: true })

    // Third click must NOT clear — stays sorted (asc→desc)
    await priorityTh.click()
    await expect(priorityTh).toHaveAttribute('aria-sort', 'descending')
    await expect.poll(() => hashQuery(page).get('sort')).toBe('priority')

    await startedTh.click()
    await expect(startedTh).toHaveAttribute('aria-sort', 'descending')
    await expect(priorityTh).toHaveAttribute('aria-sort', 'none')
    await expect.poll(() => hashQuery(page).get('sort')).toBe('started_at')
    await expect.poll(() => hashQuery(page).get('order')).toBe('desc')
    await page.screenshot({ path: '/tmp/run-list-sort-started-desc.png', fullPage: true })
  })

  test('illegal sort query stripped; headers inactive; list still loads', async ({ page }) => {
    await openRunList(page, 'sort=duration&order=up')
    const startedTh = page.locator('th.sortable', { hasText: '开始时间' })
    const priorityTh = page.locator('th.sortable', { hasText: '优先级' })
    await expect(startedTh).toHaveAttribute('aria-sort', 'none')
    await expect(priorityTh).toHaveAttribute('aria-sort', 'none')
    await expect.poll(() => hashQuery(page).get('sort')).toBeNull()
    await expect.poll(() => hashQuery(page).get('order')).toBeNull()
    await expect(page.locator('tbody tr').first()).toBeVisible()
    await page.screenshot({ path: '/tmp/run-list-sort-illegal-fallback.png', fullPage: true })
  })

  test('deep-link legal sort restores header + API params', async ({ page }) => {
    const capture = { requests: [] as Request[] }
    await openRunList(page, 'sort=started_at&order=asc', capture)
    const startedTh = page.locator('th.sortable', { hasText: '开始时间' })
    await expect(startedTh).toHaveAttribute('aria-sort', 'ascending')
    await expect(startedTh).toHaveClass(/active/)
    await expect
      .poll(() => {
        const hit = capture.requests.find((r) => {
          const u = new URL(r.url())
          return u.searchParams.get('sort') === 'started_at' && u.searchParams.get('order') === 'asc'
        })
        return !!hit
      })
      .toBe(true)
    await page.screenshot({ path: '/tmp/run-list-sort-deeplink-asc.png', fullPage: true })
  })

  test('changing sort from page 2 resets to page 1', async ({ page }) => {
    await openRunList(page)
    await expect(page.getByText(/共 22/)).toBeVisible({ timeout: 10_000 })
    await page.getByRole('button', { name: '2', exact: true }).click()
    await expect(page.getByText(/第 2\/2/)).toBeVisible()
    await page.locator('th.sortable', { hasText: '优先级' }).click()
    await expect(page.getByText(/第 1\/2/)).toBeVisible()
    await expect.poll(() => hashQuery(page).get('sort')).toBe('priority')
    await page.screenshot({ path: '/tmp/run-list-sort-page-reset.png', fullPage: true })
  })

  test('mobile: no sort controls; desktop-only sortable headers', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    const capture = { requests: [] as Request[] }
    await mockApis(page, capture)
    await page.goto('/run-list-sort.html?sort=priority&order=desc')
    await expect(page.getByRole('heading', { name: '运行' })).toBeVisible({ timeout: 15_000 })
    await expect(page.locator('table')).toHaveCount(0)
    await expect(page.locator('th.sortable')).toHaveCount(0)
    await expect(page.locator('.sort-icon')).toHaveCount(0)
    // URL-driven sort still sent on list fetch
    await expect
      .poll(() => {
        const hit = capture.requests.find((r) => {
          const u = new URL(r.url())
          return u.searchParams.get('sort') === 'priority' && u.searchParams.get('order') === 'desc'
        })
        return !!hit
      })
      .toBe(true)
    await page.screenshot({ path: '/tmp/run-list-sort-mobile.png', fullPage: true })
  })
})
