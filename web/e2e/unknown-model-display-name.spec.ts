import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'
import { routeApi } from './helpers/apiRoute'
import { test, expect } from '@playwright/test'

test.beforeEach(async ({ page }) => { await installWorkspaceApiDefaults(page) })
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { dismissOnboardingIfOpen, seedOnboardingDismissed } from './helpers/onboarding'
import { RESEARCH_CAPS } from '../src/test/capsFixtures'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const shotDir = path.join(__dirname, '..', 'test-results', 'unknown-model-display-shots')

const BASE_WORKSPACE = {
  name: 'Demo Workspace',
  description: 'Workspace for unknown-model display e2e',
  variables: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

function aliasedTokenStats() {
  return {
    window: '30d',
    bucketWidth: 'day',
    timezone: 'Asia/Shanghai',
    empty: false,
    trend: [
      {
        bucket: '2026-07-20',
        total: 180,
        workflowTotal: 180,
        pmTotal: 0,
        inputTokens: 80,
        outputTokens: 60,
        cacheReadTokens: 20,
        cacheWriteTokens: 20,
      },
    ],
    composition: {
      inputTokens: 80,
      outputTokens: 60,
      cacheReadTokens: 20,
      cacheWriteTokens: 20,
      total: 180,
    },
    workflows: [{ workflowId: 'wf-a', name: 'approve-main', total: 180, kind: 'workflow' }],
    // Backend buildModelStats: Name=alias, ModelKey stays 未知/未分桶, Unknown=true
    modelComposition: [
      { modelKey: 'gpt-5', name: 'gpt-5', total: 100 },
      { modelKey: '未知/未分桶', name: 'gpt-5', total: 80, unknown: true },
    ],
    modelRanking: [
      { modelKey: 'gpt-5', name: 'gpt-5', total: 100 },
      { modelKey: '未知/未分桶', name: 'gpt-5', total: 80, unknown: true },
    ],
  }
}

test.describe('未知模型显示名', () => {
  test('工作区信息：配置/保存/清空显示名', async ({ page }) => {
    await seedOnboardingDismissed(page)
    await page.setViewportSize({ width: 1280, height: 900 })

    let workspace = { ...BASE_WORKSPACE, unknownModelDisplayName: '' as string }
    let lastPatch: Record<string, unknown> | null = null

    await routeApi(page, '**/api/workspace', async (route) => {
      const method = route.request().method()
      if (method === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(workspace),
        })
        return
      }
      if (method === 'PATCH') {
        lastPatch = route.request().postDataJSON() as Record<string, unknown>
        const next = String(lastPatch.unknownModelDisplayName ?? '')
        workspace = { ...workspace, unknownModelDisplayName: next, name: String(lastPatch.name ?? workspace.name) }
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(workspace),
        })
        return
      }
      await route.continue()
    })
    await routeApi(page, '**/api/workflows**', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([]),
        })
        return
      }
      await route.continue()
    })
    await routeApi(page, '**/api/runs**', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ items: [], total: 0, page: 1, pageSize: 20, hasMore: false }),
        })
        return
      }
      await route.continue()
    })
    await routeApi(page, '**/api/workspace/pm-leader', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ enabled: false }),
      })
    })
    await routeApi(page, '**/api/agents**', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([]),
        })
        return
      }
      await route.continue()
    })

    await routeApi(page, '**/api/settings', async (route) => {
      await route.fulfill({
        json: {
          brand: { product_name: 'Grasp', home_subtitle: '让工作流协调每一次交付' },
          items: [
            { key: 'max_concurrent_runs', value: 4, min: 1, source: 'db', locked: false },
            { key: 'max_test_sandboxes', value: 3, min: 1, source: 'db', locked: false },
          ],
        },
      })
    })
    await routeApi(page, '**/api/sandboxes', async (route) => {
      await route.fulfill({ json: [] })
    })

    await page.goto('/workspace-config.html?view=/settings/workspace')
    await dismissOnboardingIfOpen(page)
    await expect(page.getByRole('heading', { name: '常规', exact: true })).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('settings-save-actions')).toBeVisible()
    await expect(page.getByTestId('nav-settings-chrome').getByRole('link', { name: '工作区信息' })).toHaveCount(0)

    const input = page.getByTestId('workspace-meta-unknown-display')
    await expect(input).toBeVisible()
    await expect(page.getByText('未知模型显示名', { exact: true })).toBeVisible()
    await expect(input).toHaveAttribute('placeholder', /例如 gpt-5/)
    await expect(page.getByText(/不改变已记录的数据/)).toBeVisible()
    // 空态/帮助不出现「分桶」行话（标签「未知模型显示名」本身合规）
    const help = page.locator('label[for="workspace-meta-unknown-display"]').locator('..')
    await expect(help).not.toContainText('分桶')

    await page.screenshot({ path: path.join(shotDir, '01-meta-empty.png'), fullPage: false })

    await input.fill('gpt-5')
    await page.getByTestId('settings-save-actions').getByRole('button', { name: '保存', exact: true }).click()
    await expect.poll(() => lastPatch?.unknownModelDisplayName).toBe('gpt-5')
    await expect(input).toHaveValue('gpt-5')

    await page.screenshot({ path: path.join(shotDir, '02-meta-saved-gpt5.png'), fullPage: false })

    await page.getByTestId('workspace-meta-unknown-display-clear').click()
    await expect(input).toHaveValue('')
    expect(lastPatch?.unknownModelDisplayName).toBe('gpt-5')
    await page.getByTestId('settings-save-actions').getByRole('button', { name: '保存', exact: true }).click()
    await expect.poll(() => lastPatch?.unknownModelDisplayName).toBe('')
    await expect(input).toHaveValue('')
  })

  function globalModelStats(stats: ReturnType<typeof aliasedTokenStats>) {
    return {
      ...stats,
      kpi: { total: 180, inputTokens: 80, outputTokens: 60, cacheReadTokens: 20, cacheWriteTokens: 20, workflowTotal: 180, pmTotal: 0, runCount: 1, modelCount: 2 },
      sources: [{ key: 'workflow', name: 'workflow', total: 180 }],
      nodeTypes: [], topRuns: [], prevTrend: [], modelTrends: [],
      filterOptions: { models: stats.modelRanking.map((model) => ({ key: model.modelKey, name: model.name })) },
    }
  }

  test('用量统计：模型别名无未知角标、非未知灰，同名两行不合并', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await routeApi(page, '**/api/stats/token**', async (route) => route.fulfill({ json: globalModelStats(aliasedTokenStats()) }))
    await page.goto('/workspace-config.html?view=/stats')
    const table = page.getByTestId('token-analytics-models-table')
    await expect(table).toBeVisible()
    const rows = table.locator('tbody tr')
    await expect(rows).toHaveCount(2)
    await expect(rows.nth(0)).toContainText('gpt-5')
    await expect(rows.nth(1)).toContainText('gpt-5')
    const unknownRow = table.locator('[data-unknown="1"]')
    await expect(unknownRow).toHaveCount(1)
    await expect(unknownRow).toHaveAttribute('data-model-key', '未知/未分桶')
    await expect(table.getByTestId('unknown-model-badge')).toHaveCount(0)
    await expect(unknownRow.locator('.token-model-swatch')).not.toHaveCSS('background-color', 'rgb(113, 113, 122)')
    await expect(page.getByTestId('token-analytics-model-share').locator('canvas')).toBeVisible()
    await page.screenshot({ path: path.join(shotDir, '03-usage-alias-no-badge.png'), fullPage: false })
  })

  test('用量统计：未设显示名时未知桶仍有角标与灰色', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    const stats = aliasedTokenStats()
    stats.modelRanking[1] = { modelKey: '未知/未分桶', name: '未知模型', total: 80, unknown: true }
    await routeApi(page, '**/api/stats/token**', async (route) => route.fulfill({ json: globalModelStats(stats) }))
    await page.goto('/workspace-config.html?view=/stats')
    const table = page.getByTestId('token-analytics-models-table')
    const unknownRow = table.locator('[data-unknown="1"]')
    await expect(unknownRow).toContainText('未知模型')
    await expect(unknownRow).not.toContainText('未知/未分桶')
    await expect(unknownRow.getByTestId('unknown-model-badge')).toHaveCount(1)
    await expect(unknownRow.locator('.token-model-swatch')).toHaveCSS('background-color', 'rgb(113, 113, 122)')
    await expect(page.getByTestId('token-analytics-model-share').locator('canvas')).toBeVisible()
  })

  test('Run 按模型明细：显示别名且无未知角标，来源列仍为未知模型', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })

    const nodes = [
      { id: 'start', type: 'input', label: '开始', position: { x: 0, y: 0 }, config: {} },
      { id: 'research', type: 'agent', caps: RESEARCH_CAPS, label: '调研', position: { x: 180, y: 0 }, config: {} },
    ]
    const usage = {
      inputTokens: 100,
      outputTokens: 50,
      cacheReadTokens: 10,
      cacheWriteTokens: 5,
    }
    const usageByModel = {
      gpt_5: {
        inputTokens: 60,
        outputTokens: 30,
        cacheReadTokens: 5,
        cacheWriteTokens: 2,
        source: 'upstream',
      },
      '未知/未分桶': {
        inputTokens: 40,
        outputTokens: 20,
        cacheReadTokens: 5,
        cacheWriteTokens: 3,
        source: 'unknown',
      },
    }
    // API keys are model names; adjust to match TOKEN_USAGE_UNKNOWN_MODEL constant
    const byModel = {
      'gpt-5': usageByModel.gpt_5,
      '未知/未分桶': usageByModel['未知/未分桶'],
    }
    const run = {
      id: 'run-responsive-e2e',
      workflowId: 'wf-alias',
      workflowName: 'alias-demo',
      workflowVersion: 1,
      status: 'completed',
      trigger: 'manual',
      startedAt: '2026-07-24T00:00:00Z',
      durationSec: 120,
      progress: 1,
      nodeRuns: {
        research: {
          nodeId: 'research',
          status: 'completed',
          startedAt: '2026-07-24T00:00:01Z',
          durationSec: 100,
          outputs: {},
          usage,
          usageByModel: byModel,
        },
      },
      nodeExecutions: {
        research: [
          {
            nodeId: 'research',
            iteration: 1,
            status: 'completed',
            startedAt: '2026-07-24T00:00:01Z',
            durationSec: 100,
            outputs: {},
            usage,
            usageByModel: byModel,
          },
        ],
      },
      artifacts: [],
      trace: [],
      vars: [],
      nodes,
      edges: [{ id: 'e1', source: 'start', target: 'research' }],
    }

    await routeApi(page, '**/api/runs**', async (route) => {
      const url = new URL(route.request().url())
      const pathName = url.pathname
      if (pathName.endsWith('/events')) {
        await route.fulfill({ json: { events: [], nextCursor: '', hasMore: false, live: false } })
        return
      }
      if (pathName.endsWith('/sandbox-log')) {
        await route.fulfill({ json: { content: '', live: false, found: false } })
        return
      }
      if (pathName === '/api/runs' || pathName === '/api/runs/') {
        await route.fulfill({
          json: {
            items: [{ id: run.id, status: run.status, workflowId: run.workflowId, workflowName: run.workflowName }],
            total: 1,
          },
        })
        return
      }
      if (pathName.includes(`/api/runs/${run.id}`)) {
        await route.fulfill({ json: run })
        return
      }
      await route.fulfill({ status: 404, json: { error: 'not mocked' } })
    })
    await routeApi(page, '**/api/workflows/**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'wf-alias',
          name: 'alias-demo',
          status: 'published',
          version: 1,
          nodes,
          edges: [{ id: 'e1', source: 'start', target: 'research' }],
        }),
      })
    })
    await routeApi(page, '**/api/workspace', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ...BASE_WORKSPACE, unknownModelDisplayName: 'gpt-5' }),
      })
    })

    await page.goto('/run-detail-real.html')
    await expect(page.getByTestId('run-detail-root')).toBeVisible({ timeout: 15_000 })
    await page.getByTestId('view-mode-stats').click()
    await expect(page.getByTestId('execution-stats-panel')).toBeVisible()

    const table = page.getByTestId('run-token-by-model')
    await expect(table).toBeVisible({ timeout: 10_000 })
    const unkRow = table.locator('[data-unknown="1"]')
    await expect(unkRow).toHaveCount(1)
    await expect(unkRow).toContainText('gpt-5')
    // 已设名：模型列无「未知」角标
    await expect(unkRow.getByTestId('unknown-model-badge')).toHaveCount(0)
    // 来源徽章属性标签不随显示名改变
    await expect(unkRow).toContainText('未知模型')
    await expect(unkRow).not.toContainText('未知/未分桶')
    // 模型名区域不显示默认桶名（已被别名覆盖）；来源徽章显示新默认名
    await expect(unkRow.locator('[title="gpt-5"]')).toContainText('gpt-5')
    await expect(unkRow.locator('[title="gpt-5"]')).not.toContainText('未知模型')
    await expect(unkRow.locator('[title="gpt-5"]')).not.toContainText('未知/未分桶')

    await page.screenshot({
      path: path.join(shotDir, '04-run-table-alias.png'),
      fullPage: false,
    })
  })
})
