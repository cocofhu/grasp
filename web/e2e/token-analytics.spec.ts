import { test, expect } from '@playwright/test'
import { routeApi } from './helpers/apiRoute'

const MOCK_STATS = {
  window: '30d',
  bucketWidth: 'day',
  timezone: 'UTC',
  empty: false,
  kpi: {
    total: 9000,
    deltaPct: 10,
    inputTokens: 5000,
    outputTokens: 3000,
    cacheReadTokens: 800,
    cacheWriteTokens: 200,
    workflowTotal: 7000,
    pmTotal: 2000,
    projectCount: 1,
    runCount: 3,
    modelCount: 1,
  },
  trend: [
    {
      bucket: '2026-07-01',
      total: 9000,
      workflowTotal: 7000,
      pmTotal: 2000,
      inputTokens: 5000,
      outputTokens: 3000,
      cacheReadTokens: 800,
      cacheWriteTokens: 200,
    },
  ],
  prevTrend: [{ bucket: '2026-06-01', total: 8000, workflowTotal: 6000, pmTotal: 2000, inputTokens: 4000, outputTokens: 2500, cacheReadTokens: 700, cacheWriteTokens: 180 }],
  composition: {
    total: 9000,
    inputTokens: 5000,
    outputTokens: 3000,
    cacheReadTokens: 800,
    cacheWriteTokens: 200,
  },
  projects: [
    { projectId: 'p1', name: 'Demo', total: 7000, inputTokens: 4000, outputTokens: 2200, cacheReadTokens: 600, cacheWriteTokens: 200 },
    { projectId: 'p2', name: 'Docs', total: 2000, inputTokens: 1000, outputTokens: 800, cacheReadTokens: 200, cacheWriteTokens: 0 },
  ],
  modelRanking: [
    { modelKey: 'm1', name: 'Model', total: 7000, inputTokens: 4000, outputTokens: 2200, cacheReadTokens: 600, cacheWriteTokens: 200 },
    { modelKey: 'm2', name: 'Model Mini', total: 2000, inputTokens: 1000, outputTokens: 800, cacheReadTokens: 200, cacheWriteTokens: 0 },
  ],
  nodeTypes: [{ name: 'agent', total: 9000 }],
  workflows: [
    { workflowId: 'w1', name: 'wf', total: 7000, inputTokens: 4000, outputTokens: 2200, cacheReadTokens: 600, cacheWriteTokens: 200, kind: 'workflow' },
    { workflowId: 'w2', name: 'review', total: 2000, inputTokens: 1000, outputTokens: 800, cacheReadTokens: 200, cacheWriteTokens: 0, kind: 'workflow' },
  ],
  heatmap: { rows: ['Model'], cols: ['Demo'], grid: [[9000]] },
  topRuns: [
    {
      runId: 'r1',
      title: 'Run',
      projectId: 'p1',
      projectName: 'Demo',
      workflowName: 'wf',
      modelKey: 'm1',
      modelName: 'Model',
      total: 9000,
    },
  ],
  projectTrends: [],
  modelTrends: [],
  filterOptions: {
    projects: [{ key: 'p1', name: 'Demo' }],
    models: [{ key: 'm1', name: 'Model' }],
  },
}

async function openStatsPage(page: import('@playwright/test').Page) {
  await routeApi(page, '**/api/stats/token**', async (route) => {
    const url = new URL(route.request().url())
    const w = url.searchParams.get('window') || '30d'
    const bucketWidth = w === '24h' ? 'hour' : w === 'all' ? 'week' : 'day'
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ ...MOCK_STATS, window: w, bucketWidth }),
    })
  })
  await page.goto('/token-analytics.html')
  await expect(page.getByTestId('shell-main-dashboard')).toBeVisible({ timeout: 15_000 })
  await page.getByRole('link', { name: '设置', exact: true }).click()
  await page.getByRole('link', { name: '统计', exact: true }).click()
  await expect(page.getByTestId('token-analytics-page')).toBeVisible({ timeout: 15_000 })
}

test.describe('Global token analytics', () => {
  test('sidebar stats entry navigates to /stats with chart sections', async ({ page }) => {
    await openStatsPage(page)
    await expect(page.getByTestId('token-analytics-lines')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('token-analytics-pies')).toBeVisible()
    await expect(page.getByTestId('token-analytics-kpis')).toBeVisible()
    await expect(page.getByTestId('token-analytics-section-nav')).toHaveCount(0)
    await expect(page.getByText('用量统计')).toBeVisible()
  })

  test('input-side total plus output matches total KPI', async ({ page }) => {
    await openStatsPage(page)
    const input = page.getByTestId('token-analytics-kpi-input')
    const output = page.getByTestId('token-analytics-kpi-output')
    const total = page.getByTestId('token-analytics-kpi-total')

    await expect(input).toHaveText('6K')
    await expect(output).toHaveText('3K')
    await expect(input).toHaveAttribute('data-token-count', '6000')
    await expect(output).toHaveAttribute('data-token-count', '3000')
    await expect(total).toHaveAttribute('data-token-count', '9000')
    const merge = page.getByTestId('token-analytics-kpi-merge')
    await expect(merge.getByText('缓存读', { exact: true })).not.toBeVisible()
    await expect(merge.getByText('缓存写', { exact: true })).not.toBeVisible()
  })

  test('tablet viewport has no section nav and shows main chart sections', async ({ page }) => {
    await page.setViewportSize({ width: 1024, height: 768 })
    await openStatsPage(page)
    await expect(page.getByTestId('token-analytics-section-nav-mobile')).toHaveCount(0)
    await expect(page.getByTestId('token-analytics-section-nav')).toHaveCount(0)
    await expect(page.getByTestId('token-analytics-lines')).toBeVisible()
  })

  test('line mode tabs switch visible labels', async ({ page }) => {
    await openStatsPage(page)
    await page.getByRole('button', { name: '按项目' }).click()
    await expect(page.getByRole('button', { name: '按项目' })).toHaveClass(/font-semibold/)
    await page.getByRole('button', { name: '按模型' }).click()
    await expect(page.getByRole('button', { name: '按模型' })).toHaveClass(/font-semibold/)
    await page.getByRole('button', { name: '总量（对比上一周期）' }).click()
    await expect(page.getByRole('button', { name: '总量（对比上一周期）' })).toHaveClass(/font-semibold/)
  })

  test('bar dimensions switch without page errors', async ({ page }) => {
    const errors: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    await openStatsPage(page)
    const bars = page.getByTestId('token-analytics-bars')
    await expect(bars).toBeVisible()
    await expect(page.getByTestId('token-analytics-bar-dimension-project')).toHaveClass(/font-semibold/)

    await page.getByTestId('token-analytics-bar-dimension-workflow').click()
    await expect(page.getByTestId('token-analytics-bar-dimension-workflow')).toHaveClass(/font-semibold/)
    await expect(bars).toContainText('各工作流用量堆叠对比')

    await page.getByTestId('token-analytics-bar-dimension-model').click()
    await expect(page.getByTestId('token-analytics-bar-dimension-model')).toHaveClass(/font-semibold/)
    await expect(bars).toContainText('各模型用量堆叠对比')
    expect(errors).toEqual([])
  })

  test('project table link navigates to project board tab', async ({ page }) => {
    await openStatsPage(page)
    await page.getByRole('button', { name: 'Demo' }).click()
    await expect(page.getByTestId('project-board-page')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText('board:p1')).toBeVisible()
  })

  test('top run link navigates to run detail', async ({ page }) => {
    await openStatsPage(page)
    await page.getByRole('button', { name: 'Run' }).click()
    await expect(page.getByTestId('run-detail-page')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByText('run:r1')).toBeVisible()
  })

  test('pricing modal opens for admins with editable rows', async ({ page }) => {
    await openStatsPage(page)
    await page.getByTestId('token-analytics-pricing').click()
    await expect(page.getByTestId('token-pricing-modal')).toBeVisible()
    await expect(page.getByTestId('token-pricing-row').first()).toBeVisible()
    await expect(page.getByTestId('token-pricing-save')).toBeEnabled()
  })

  test('project detail opens the drill-down modal with ledger events and applies the filter', async ({ page }) => {
    const errors: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    await openStatsPage(page)
    await page.getByTestId('token-analytics-project-detail-p1').click()
    const modal = page.getByTestId('token-drill-modal')
    await expect(modal).toBeVisible()
    await expect(page.getByTestId('token-drill-breadcrumb')).toContainText('项目：Demo')
    await page.getByTestId('token-drill-tab-events').click()
    await expect(page.getByTestId('token-events-table')).toContainText('m1')

    await page.getByTestId('token-drill-apply').click()
    await expect(modal).toHaveCount(0)
    await expect(page.getByTestId('token-analytics-filter-project')).toHaveValue('p1')
    expect(errors).toEqual([])
  })
})
