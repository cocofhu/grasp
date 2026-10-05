/**
 * Browser acceptance for TagFilter UX on Run list + Gates inbox (mocked API).
 */
import { expect, test, type Page, type Request } from '@playwright/test'
import { mkdirSync } from 'node:fs'

mkdirSync('/tmp/tag-filter-screenshots', { recursive: true })

function stubRun(id: string, title: string, tags: string[] = []) {
  return {
    id,
    workflowId: 'wf-1',
    workflowName: 'Demo Workflow',
    title,
    status: 'waiting_human',
    trigger: 'manual',
    priority: 'normal',
    startedAt: '2026-07-25T10:00:00Z',
    createdAt: '2026-07-25T09:00:00Z',
    durationSec: 60,
    progress: 0.5,
    tags,
    nodeRuns: {},
    artifacts: [],
  }
}

const ALL_RUNS = [
  stubRun('run-1', 'deploy-web', ['prod', 'canary']),
  stubRun('run-2', 'hotfix-api', ['hotfix', 'prod']),
  stubRun('run-3', 'nightly-batch', ['nightly']),
  stubRun('run-4', 'docs-sync', []),
]

function filterByTags(runs: typeof ALL_RUNS, tagParam: string | null) {
  if (!tagParam) return runs
  const tags = tagParam
    .split(',')
    .map((t) => t.trim())
    .filter(Boolean)
  if (!tags.length) return runs
  return runs.filter((r) => tags.every((t) => r.tags.includes(t)))
}

async function mockApis(page: Page, capture: { runRequests: Request[] }) {
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

    if (method === 'GET' && (path === '/api/runs' || path.endsWith('/api/runs'))) {
      capture.runRequests.push(req)
      const filtered = filterByTags(ALL_RUNS, url.searchParams.get('tag'))
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: filtered, total: filtered.length }),
      })
      return
    }

    if (method === 'GET' && path.match(/\/api\/projects\/[^/]+\/run-tags\/?$/)) {
      await route.fulfill({ json: { tags: ['prod', 'canary', 'hotfix', 'nightly'] } })
      return
    }

    if (method === 'GET' && (path === '/api/gates' || path.endsWith('/api/gates'))) {
      const tag = url.searchParams.get('tag')
      const items = filterByTags(ALL_RUNS, tag).map((r) => ({
        runId: r.id,
        nodeId: 'hg-1',
        workflowName: r.workflowName,
        title: r.title,
        bodyMd: '请审批',
        actions: [{ id: 'approve', label: '通过' }],
        requestedAt: r.createdAt,
        projectId: 'proj-1',
        tags: r.tags,
      }))
      await route.fulfill({ json: { items, total: items.length } })
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

    if (path.includes('/api/auth') || path.endsWith('/api/me')) {
      await route.fulfill({ json: { username: 'admin', isAdmin: true } })
      return
    }

    await route.fulfill({ status: 200, json: {} })
  })
}

function hashQuery(page: Page) {
  const hash = page.url().split('#')[1] || ''
  const q = hash.includes('?') ? hash.slice(hash.indexOf('?') + 1) : ''
  return new URLSearchParams(q)
}

async function openPage(page: Page, qs: string, capture = { runRequests: [] as Request[] }) {
  await mockApis(page, capture)
  await page.goto(`/tag-filter-ux.html${qs ? `?${qs}` : ''}`)
  return capture
}

test.describe('TagFilter UX acceptance', () => {
  test('run list: discoverable trigger, stock toggle, URL sync, chips in popover', async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    const capture = await openPage(page, 'projectId=proj-1')
    await expect(page.getByRole('heading', { name: '运行' })).toBeVisible({ timeout: 15_000 })
    const trigger = page.getByTestId('tag-filter-trigger')
    await expect(trigger).toBeVisible()
    await expect(trigger).toContainText('标签')
    await expect(page.getByTestId('tag-filter-count')).toHaveCount(0)
    await page.screenshot({
      path: '/tmp/tag-filter-screenshots/01-runlist-closed.png',
      fullPage: true,
    })

    await trigger.click()
    await expect(page.getByTestId('tag-filter-input')).toBeVisible()
    await expect(page.getByTestId('tag-filter-suggestions').getByRole('button')).toHaveCount(4)
    await page.screenshot({
      path: '/tmp/tag-filter-screenshots/02-runlist-open-stock.png',
      fullPage: true,
    })

    await page.getByTestId('tag-filter-suggestions').getByRole('button', { name: /prod/ }).click()
    await expect(page.getByTestId('tag-filter-count')).toHaveText('1')
    await expect(page.getByTestId('tag-filter-selected')).toContainText('prod')
    await expect.poll(() => hashQuery(page).get('tag')).toBe('prod')

    await page.getByTestId('tag-filter-suggestions').getByRole('button', { name: /canary/ }).click()
    await expect(page.getByTestId('tag-filter-count')).toHaveText('2')
    await expect.poll(() => hashQuery(page).get('tag')).toBe('prod,canary')
    await page.screenshot({
      path: '/tmp/tag-filter-screenshots/03-runlist-multi-selected.png',
      fullPage: true,
    })

    await expect
      .poll(() => {
        const last = capture.runRequests[capture.runRequests.length - 1]
        return last ? new URL(last.url()).searchParams.get('tag') : null
      })
      .toBe('prod,canary')

    // toggle off prod
    await page.getByTestId('tag-filter-suggestions').getByRole('button', { name: /prod/ }).click()
    await expect(page.getByTestId('tag-filter-count')).toHaveText('1')
    await expect.poll(() => hashQuery(page).get('tag')).toBe('canary')
  })

  test('run list: invalid enter blocked; valid enter adds; remove chip clears URL', async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await openPage(page, 'projectId=proj-1')
    await expect(page.getByTestId('tag-filter-trigger')).toBeVisible({ timeout: 15_000 })
    await page.getByTestId('tag-filter-trigger').click()
    const input = page.getByTestId('tag-filter-input')
    await input.fill('owner:alice')
    await input.press('Enter')
    await expect(page.getByTestId('tag-filter-error')).toBeVisible()
    expect(hashQuery(page).get('tag')).toBeNull()
    await page.screenshot({
      path: '/tmp/tag-filter-screenshots/04-runlist-invalid.png',
      fullPage: true,
    })

    await input.fill('nightly')
    await input.press('Enter')
    await expect(page.getByTestId('tag-filter-count')).toHaveText('1')
    await expect.poll(() => hashQuery(page).get('tag')).toBe('nightly')
    await page.getByTestId('tag-filter-selected').getByRole('button').click()
    await expect(page.getByTestId('tag-filter-count')).toHaveCount(0)
    await expect.poll(() => hashQuery(page).get('tag')).toBeNull()
  })

  test('run list: URL restore shows count; no-project empty state', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await openPage(page, 'tag=prod,canary&projectId=proj-1')
    await expect(page.getByTestId('tag-filter-count')).toHaveText('2', { timeout: 15_000 })
    await page.getByTestId('tag-filter-trigger').click()
    await expect(page.getByTestId('tag-filter-selected')).toContainText('prod')
    await expect(page.getByTestId('tag-filter-selected')).toContainText('canary')
    await page.screenshot({
      path: '/tmp/tag-filter-screenshots/05-runlist-url-restore.png',
      fullPage: true,
    })

    // Clear persisted project so TagFilter sees empty projectId (「全部」).
    await page.evaluate(() => localStorage.clear())
    await page.goto('/tag-filter-ux.html')
    await expect(page.getByTestId('tag-filter-trigger')).toBeVisible({ timeout: 15_000 })
    await page.getByTestId('tag-filter-trigger').click()
    await expect(page.getByTestId('tag-filter-empty')).toContainText('选择项目')
    await page.screenshot({
      path: '/tmp/tag-filter-screenshots/06-runlist-need-project.png',
      fullPage: true,
    })
  })

  test('run list mobile: full-width trigger usable; chips not in top bar', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await openPage(page, 'projectId=proj-1&tag=prod')
    await expect(page.getByTestId('tag-filter-trigger')).toBeVisible({ timeout: 15_000 })
    const box = await page.getByTestId('tag-filter-trigger').boundingBox()
    expect(box).toBeTruthy()
    expect(box!.height).toBeGreaterThanOrEqual(40)
    expect(box!.width).toBeGreaterThan(280)
    await expect(page.locator('[data-testid="tag-filter-selected"]')).toHaveCount(0)
    await page.getByTestId('tag-filter-trigger').click()
    await expect(page.getByTestId('tag-filter-selected')).toContainText('prod')
    await page.screenshot({
      path: '/tmp/tag-filter-screenshots/07-runlist-mobile-open.png',
      fullPage: true,
    })
  })

  test('gates inbox: TagFilter entry + mutual exclusion with project filter', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await openPage(page, 'page=gates&projectId=proj-1')
    await expect(page.getByTestId('tag-filter-trigger')).toBeVisible({ timeout: 20_000 })
    await expect(page.getByTestId('tag-filter-trigger')).toContainText('标签')
    await page.screenshot({
      path: '/tmp/tag-filter-screenshots/08-gates-closed.png',
      fullPage: true,
    })

    await page.getByTestId('tag-filter-trigger').click()
    await expect(page.getByTestId('tag-filter-input')).toBeVisible()
    await page.getByTestId('tag-filter-suggestions').getByRole('button', { name: /hotfix/ }).click()
    await expect.poll(() => hashQuery(page).get('tag')).toBe('hotfix')
    await page.screenshot({
      path: '/tmp/tag-filter-screenshots/09-gates-selected.png',
      fullPage: true,
    })

    // opening project filter should close tag popover
    const projectTrigger = page.locator('[data-testid="project-filter-trigger"], button', {
      hasText: /Grasp|全部项目|项目/,
    }).first()
    if (await projectTrigger.count()) {
      await projectTrigger.click()
      await expect(page.getByTestId('tag-filter-input')).toHaveCount(0)
    }
  })
})
