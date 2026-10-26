import { test, expect } from '@playwright/test'
import { readFile } from 'node:fs/promises'

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
  modelRanking: [
    { modelKey: 'm1', name: 'Model', total: 7000, inputTokens: 4000, outputTokens: 2200, cacheReadTokens: 600, cacheWriteTokens: 200 },
    { modelKey: 'm2', name: 'Model Mini', total: 2000, inputTokens: 1000, outputTokens: 800, cacheReadTokens: 200, cacheWriteTokens: 0 },
  ],
  nodeTypes: [{ name: 'agent', total: 9000 }],
  workflows: [
    { workflowId: 'w1', name: 'wf', total: 7000, inputTokens: 4000, outputTokens: 2200, cacheReadTokens: 600, cacheWriteTokens: 200, kind: 'workflow' },
    { workflowId: 'w2', name: 'review', total: 2000, inputTokens: 1000, outputTokens: 800, cacheReadTokens: 200, cacheWriteTokens: 0, kind: 'workflow' },
  ],
  heatmap: { rows: ['Model'], cols: ['wf'], grid: [[9000]] },
  topRuns: [
    {
      runId: 'r1',
      title: 'Run',
      workflowName: 'wf',
      modelKey: 'm1',
      modelName: 'Model',
      total: 9000,
    },
  ],
  modelTrends: [],
  filterOptions: {
    workflows: [{ key: 'w1', name: 'wf' }],
    models: [{ key: 'm1', name: 'Model' }],
  },
}

async function openStatsPage(page: import('@playwright/test').Page, overrides: Record<string, unknown> = {}) {
  await page.goto('/token-analytics.html')
  await expect(page.getByTestId('shell-main-dashboard')).toBeVisible({ timeout: 15_000 })
  // This harness stubs window.fetch, so network routes cannot replace its stats.
  await page.evaluate((stats) => {
    const harnessFetch = window.fetch
    window.fetch = async (input, init) => {
      const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url, location.origin)
      if (url.pathname !== '/api/stats/token') return harnessFetch(input, init)
      const selectedWindow = url.searchParams.get('window') || 'all'
      const bucketWidth = selectedWindow === '24h' ? 'hour' : selectedWindow === 'all' ? 'week' : 'day'
      return new Response(JSON.stringify({ ...stats, window: selectedWindow, bucketWidth }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }
  }, { ...MOCK_STATS, ...overrides })
  const statsLink = page.getByTestId('sidebar-app-rail').getByRole('link', { name: '用量统计', exact: true })
  await statsLink.click()
  await expect(page.getByTestId('token-analytics-page')).toBeVisible({ timeout: 15_000 })
  await expect(statsLink).toHaveAttribute('aria-current', 'page')
  await expect(page.getByTestId('sidebar-settings-title')).toHaveCount(0)
}

const SKEWED_STATS = {
  kpi: { ...MOCK_STATS.kpi, total: 1_000_000, workflowTotal: 981_000, studioTotal: 10_000, pmTotal: 9_000 },
  filterOptions: {
    ...MOCK_STATS.filterOptions,
    workflows: [{ key: 'w-dominant', name: '默认工作流' }],
    nodeTypes: [{ key: 'agent', name: 'agent' }],
  },
  tree: [
    { key: 'w-dominant', name: '默认工作流', kind: 'workflow', value: 981_000, children: [{ key: 'agent', name: 'agent', kind: 'nodeType', value: 981_000 }] },
    { key: '_studio', name: 'Studio', kind: 'studio', value: 10_000, children: [{ key: 'agent', name: 'agent', kind: 'nodeType', value: 10_000 }] },
    { key: '_pm', name: 'PM', kind: 'pm', value: 9_000, children: [{ key: 'agent', name: 'agent', kind: 'nodeType', value: 9_000 }] },
  ],
}

test.describe('Global token analytics', () => {
  test('sidebar stats entry navigates to /stats with chart sections', async ({ page }) => {
    await openStatsPage(page)
    await expect(page.getByTestId('token-analytics-lines')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('token-analytics-pies')).toBeVisible()
    await expect(page.getByTestId('token-analytics-kpis')).toBeVisible()
    await expect(page.getByTestId('token-analytics-section-nav')).toHaveCount(0)
    await expect(page.getByRole('heading', { name: '用量统计', exact: true })).toBeVisible()
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

    await page.getByTestId('token-analytics-bar-dimension-workflow').click()
    await expect(page.getByTestId('token-analytics-bar-dimension-workflow')).toHaveClass(/font-semibold/)
    await expect(bars).toContainText('各工作流用量堆叠对比')

    await page.getByTestId('token-analytics-bar-dimension-model').click()
    await expect(page.getByTestId('token-analytics-bar-dimension-model')).toHaveClass(/font-semibold/)
    await expect(bars).toContainText('各模型用量堆叠对比')
    expect(errors).toEqual([])
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

  test('model detail opens the drill-down modal with ledger events and applies the filter', async ({ page }) => {
    const errors: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    await openStatsPage(page)
    await page.getByTestId('token-analytics-models-table').getByRole('button', { name: 'Model', exact: true }).click()
    const modal = page.getByTestId('token-drill-modal')
    await expect(modal).toBeVisible()
    await expect(page.getByTestId('token-drill-breadcrumb')).toContainText('模型：Model')
    await page.getByTestId('token-drill-tab-events').click()
    await expect(page.getByTestId('token-events-table')).toContainText('m1')

    await page.getByTestId('token-drill-apply').click()
    await expect(modal).toHaveCount(0)
    await expect(page.getByTestId('token-analytics-chip-modelKey')).toContainText('模型：Model')
    await page.getByTestId('token-analytics-filter-btn').click()
    await expect(page.getByTestId('token-analytics-filter-model').getByRole('combobox')).toHaveText('Model')
    expect(errors).toEqual([])
  })

  for (const viewport of [{ width: 1440, height: 1000 }, { width: 390, height: 844 }]) {
    test(`skewed usage keeps small sources readable at ${viewport.width}px`, async ({ page }) => {
      // Enter through the desktop app rail, then exercise the responsive stats view.
      await page.setViewportSize({ width: 1440, height: viewport.height })
      await openStatsPage(page, SKEWED_STATS)
      await page.setViewportSize(viewport)
      const structure = page.getByTestId('token-analytics-structure')
      await structure.scrollIntoViewIfNeeded()
      await expect(structure.getByTestId('token-structure-row')).toHaveCount(3)
      for (const [key, name, total, share] of [
        ['w-dominant', '默认工作流', '981K', '98.1%'],
        ['_studio', 'Agent Studio', '10K', '1.0%'],
        ['_pm', '工作区助手', '9K', '0.9%'],
      ]) {
        const row = structure.locator(`[data-testid="token-structure-row"][data-key="${key}"]`)
        await expect(row.getByTestId('token-structure-source').getByText(name, { exact: true })).toBeVisible()
        await expect(row.getByTestId('token-structure-total')).toHaveText(total)
        await expect(row.getByTestId('token-structure-share')).toHaveText(share)
        const rowBox = await row.boundingBox()
        expect(rowBox!.width).toBeGreaterThan(200)
        expect(rowBox!.height).toBeGreaterThan(40)
      }
      expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(1)
      const structureBox = await structure.boundingBox()
      expect(structureBox!.x).toBeGreaterThanOrEqual(0)
      expect(structureBox!.x + structureBox!.width).toBeLessThanOrEqual(viewport.width + 1)
    })
  }

  test('usage structure preserves workflow and node type drill scope', async ({ page }) => {
    await openStatsPage(page, SKEWED_STATS)
    const row = page.locator('[data-testid="token-structure-row"][data-key="w-dominant"]')
    await row.getByTestId('token-structure-source').click()
    await expect(page.getByTestId('token-drill-modal')).toBeVisible()
    await expect(page.getByTestId('token-drill-crumb-0')).toHaveText('工作流：默认工作流')
    await page.getByRole('button', { name: 'Close', exact: true }).click()
    await expect(page.getByTestId('token-drill-modal')).toHaveCount(0)

    await row.getByTestId('token-structure-node').click()
    await expect(page.getByTestId('token-drill-crumb-0')).toHaveText('工作流：默认工作流')
    await expect(page.getByTestId('token-drill-crumb-1')).toHaveText('节点类型：agent')
    await page.getByTestId('token-drill-apply').click()
    await expect(page.getByTestId('token-analytics-chip-workflowId')).toContainText('默认工作流')
    await expect(page.getByTestId('token-analytics-chip-nodeType')).toContainText('agent')
  })

  test('detail tabs show one full-width table and preserve workflow drill', async ({ page }) => {
    await openStatsPage(page)
    const models = page.getByTestId('token-analytics-models-table')
    const workflows = page.getByTestId('token-analytics-workflows-table')
    await expect(models).toBeVisible()
    await expect(workflows).not.toBeVisible()
    await page.getByTestId('token-analytics-detail-tab-workflows').click()
    await expect(workflows).toBeVisible()
    await expect(models).not.toBeVisible()
    await expect(workflows.getByTestId('token-analytics-workflow-row')).toHaveCount(2)
    await workflows.getByTestId('token-analytics-workflow-detail').first().click()
    await expect(page.getByTestId('token-drill-crumb-0')).toHaveText('工作流：wf')
    await page.getByRole('button', { name: 'Close', exact: true }).click()
    await page.getByTestId('token-analytics-detail-tab-models').click()
    await expect(models).toBeVisible()
    await expect(workflows).not.toBeVisible()
  })

  test('run list expands, exports every run while collapsed, and opens an expanded run', async ({ page }) => {
    const topRuns = Array.from({ length: 12 }, (_, index) => ({
      ...MOCK_STATS.topRuns[0], runId: `r${index + 1}`, title: `Run ${index + 1}`, total: 12_000 - index * 500,
    }))
    await openStatsPage(page, { topRuns })
    const rows = page.getByTestId('token-analytics-run-row')
    await expect(rows).toHaveCount(8)
    await expect(page.getByRole('button', { name: 'Run 12', exact: true })).toHaveCount(0)

    const downloadPromise = page.waitForEvent('download')
    await page.getByTestId('token-analytics-export-runs').click()
    const download = await downloadPromise
    expect(download.suggestedFilename()).toBe('token-usage-runs.csv')
    const csv = await readFile((await download.path())!, 'utf8')
    expect(csv.trim().split(/\r?\n/)).toHaveLength(13)
    expect(csv).toContain('Run 12,r12,wf,Model')

    const toggle = page.getByTestId('token-analytics-runs-toggle')
    await toggle.click()
    await expect(rows).toHaveCount(12)
    await toggle.click()
    await expect(rows).toHaveCount(8)
    await toggle.click()
    await page.getByRole('button', { name: 'Run 12', exact: true }).click()
    await expect(page.getByTestId('run-detail-page')).toHaveText('run:r12')
  })
})
