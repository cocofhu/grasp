import { test, expect } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { dismissOnboardingIfOpen, seedOnboardingDismissed } from './helpers/onboarding'
import { RESEARCH_CAPS } from '../src/test/capsFixtures'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const shotDir = path.join(__dirname, '..', 'test-results', 'unknown-model-display-shots')

const BASE_PROJECT = {
  id: 'proj-1',
  name: 'Demo Project',
  description: 'Project for unknown-model display e2e',
  variables: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

function emptyTokenStats() {
  return {
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
    modelComposition: [],
    modelRanking: [],
  }
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
  test('项目信息：配置/保存/清空显示名', async ({ page }) => {
    await seedOnboardingDismissed(page, 'proj-1')
    await page.setViewportSize({ width: 1280, height: 900 })

    let project = { ...BASE_PROJECT, unknownModelDisplayName: '' as string }
    let lastPatch: Record<string, unknown> | null = null

    await page.route('**/api/projects/proj-1', async (route) => {
      const method = route.request().method()
      if (method === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(project),
        })
        return
      }
      if (method === 'PATCH') {
        lastPatch = route.request().postDataJSON() as Record<string, unknown>
        const next = String(lastPatch.unknownModelDisplayName ?? '')
        project = { ...project, unknownModelDisplayName: next, name: String(lastPatch.name ?? project.name) }
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(project),
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
          body: JSON.stringify([]),
        })
        return
      }
      await route.continue()
    })
    await page.route('**/api/runs**', async (route) => {
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
    await page.route('**/api/projects/proj-1/pm-leader', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ enabled: false }),
      })
    })
    await page.route('**/api/projects/*/token-stats**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(emptyTokenStats()),
      })
    })
    await page.route('**/api/agents**', async (route) => {
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

    await page.goto('/project-detail.html?tab=meta')
    await dismissOnboardingIfOpen(page)
    await expect(page.getByTestId('project-meta-footer')).toBeVisible({ timeout: 15_000 })

    const input = page.getByTestId('project-meta-unknown-display')
    await expect(input).toBeVisible()
    await expect(page.getByText('未知模型显示名', { exact: true })).toBeVisible()
    await expect(input).toHaveAttribute('placeholder', /例如 gpt-5/)
    await expect(page.getByText(/不改变已记录的数据/)).toBeVisible()
    // 空态/帮助不出现「分桶」行话（标签「未知模型显示名」本身合规）
    const help = page.locator('label[for="project-meta-unknown-display"]').locator('..')
    await expect(help).not.toContainText('分桶')

    await page.screenshot({ path: path.join(shotDir, '01-meta-empty.png'), fullPage: false })

    await input.fill('gpt-5')
    await page.getByTestId('project-meta-footer').getByRole('button', { name: '保存' }).click()
    await expect.poll(() => lastPatch?.unknownModelDisplayName).toBe('gpt-5')
    await expect(input).toHaveValue('gpt-5')

    await page.screenshot({ path: path.join(shotDir, '02-meta-saved-gpt5.png'), fullPage: false })

    await page.getByTestId('project-meta-unknown-display-clear').click()
    await expect.poll(() => lastPatch?.unknownModelDisplayName).toBe('')
    await expect(input).toHaveValue('')
  })

  test('看板排行/构成：别名无未知角标、非未知灰，同名两行不合并', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await seedOnboardingDismissed(page, 'proj-1')

    await page.route('**/api/stats/dashboard', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ running: 0, waitingHuman: 0, failed: 0, completed: 1 }),
      })
    })
    await page.route('**/api/runs**', async (route) => {
      if (route.request().method() !== 'GET') {
        await route.continue()
        return
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: [], total: 0, page: 1, pageSize: 20, hasMore: false }),
      })
    })
    await page.route('**/api/projects/*/token-stats**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(aliasedTokenStats()),
      })
    })

    await page.goto('/board.html?start=project-board&memory=1&projectId=proj-1')
    await expect(page.getByTestId('board-view')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('token-stats-charts')).toBeVisible({ timeout: 10_000 })

    const rank = page.getByTestId('token-model-rank')
    await expect(rank).toBeVisible()
    const rankRows = rank.locator('> li')
    await expect(rankRows).toHaveCount(2)
    // 两行都显示 gpt-5（真实桶 + 未知别名）；已设名后未知行无「未知」角标
    await expect(rank).toContainText('gpt-5')
    await expect(rank.getByTestId('unknown-model-badge')).toHaveCount(0)
    await expect(rank).not.toContainText('未知/未分桶')
    await expect(rank).not.toContainText('未知模型')
    const unkRank = rank.locator('[data-unknown="1"]')
    await expect(unkRank).toHaveCount(1)
    // 条色非未知灰 #71717A
    await expect(unkRank.locator('.h-full')).not.toHaveCSS('background-color', 'rgb(113, 113, 122)')

    const legend = page.getByTestId('token-model-legend')
    await expect(legend).toHaveCount(0)
    const pie = page.getByTestId('token-model-pie')
    await expect(pie).toBeVisible()
    await expect(pie.locator('canvas')).toBeVisible()

    await page.screenshot({
      path: path.join(shotDir, '03-board-alias-no-badge.png'),
      fullPage: false,
    })
  })

  test('看板：未设显示名时未知桶仍有角标与灰色', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await seedOnboardingDismissed(page, 'proj-1')

    const unnamedStats = {
      ...aliasedTokenStats(),
      modelComposition: [
        { modelKey: 'gpt-5', name: 'gpt-5', total: 100 },
        { modelKey: '未知/未分桶', name: '未知模型', total: 80, unknown: true },
      ],
      modelRanking: [
        { modelKey: 'gpt-5', name: 'gpt-5', total: 100 },
        { modelKey: '未知/未分桶', name: '未知模型', total: 80, unknown: true },
      ],
    }

    await page.route('**/api/stats/dashboard', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ running: 0, waitingHuman: 0, failed: 0, completed: 1 }),
      })
    })
    await page.route('**/api/runs**', async (route) => {
      if (route.request().method() !== 'GET') {
        await route.continue()
        return
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: [], total: 0, page: 1, pageSize: 20, hasMore: false }),
      })
    })
    await page.route('**/api/projects/*/token-stats**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(unnamedStats),
      })
    })

    await page.goto('/board.html?start=project-board&memory=1&projectId=proj-1')
    await expect(page.getByTestId('token-model-rank')).toBeVisible({ timeout: 15_000 })

    const rank = page.getByTestId('token-model-rank')
    const unkRank = rank.locator('[data-unknown="1"]')
    await expect(unkRank).toContainText('未知模型')
    await expect(unkRank).not.toContainText('未知/未分桶')
    await expect(unkRank.getByTestId('unknown-model-badge')).toHaveCount(1)
    await expect(unkRank.locator('.h-full')).toHaveCSS('background-color', 'rgb(113, 113, 122)')

    const pie = page.getByTestId('token-model-pie')
    await expect(pie).toBeVisible()
    await expect(pie.locator('canvas')).toBeVisible()
    await expect(page.getByTestId('token-model-legend')).toHaveCount(0)
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

    await page.route('**/api/runs**', async (route) => {
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
    await page.route('**/api/workflows/**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'wf-alias',
          name: 'alias-demo',
          projectId: 'proj-1',
          status: 'published',
          version: 1,
          nodes,
          edges: [{ id: 'e1', source: 'start', target: 'research' }],
        }),
      })
    })
    await page.route('**/api/projects/proj-1', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ...BASE_PROJECT, unknownModelDisplayName: 'gpt-5' }),
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
