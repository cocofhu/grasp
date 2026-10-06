import { test, expect, type Page } from '@playwright/test'
import path from 'node:path'
import fs from 'node:fs'
import { dismissOnboardingIfOpen, seedOnboardingDismissed } from './helpers/onboarding'

const MOCK_PROJECT = {
  id: 'proj-1',
  name: 'Grasp Project',
  description: 'Audit dual-mode e2e',
  variables: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

const RUN_A = 'run-8f2a1111'
const RUN_B = 'run-3c911111'
const RUN_C = 'run-aa11cccc'
const RUN_D = 'run-bb22dddd'

const MOCK_EVENTS = [
  {
    id: 'e1',
    projectId: 'proj-1',
    occurredAt: '2026-07-26T10:42:03Z',
    actor: 'system',
    actorDisplay: '系统',
    unattributable: true,
    callerKind: 'system',
    action: 'run.start',
    resourceType: 'run',
    resourceId: RUN_A,
    resource: `run/${RUN_A}`,
    runId: RUN_A,
    nodeId: '',
    outcome: 'ok',
    summary: 'Run 开始',
    payload: { runId: RUN_A },
  },
  {
    id: 'e2',
    projectId: 'proj-1',
    occurredAt: '2026-07-26T10:42:11Z',
    actor: 'system',
    actorDisplay: '系统',
    unattributable: true,
    callerKind: 'system',
    action: 'mcp.call',
    resourceType: 'mcp',
    resourceId: 'read_artifact',
    resource: 'mcp/read_artifact',
    runId: RUN_A,
    nodeId: 'research_2wn4',
    outcome: 'ok',
    summary: '读取产物 research.json',
    payload: { tool: 'read_artifact', runId: RUN_A },
  },
  {
    id: 'e3',
    projectId: 'proj-1',
    occurredAt: '2026-07-26T10:43:02Z',
    actor: 'system',
    actorDisplay: '系统',
    unattributable: true,
    callerKind: 'system',
    action: 'mcp.call',
    resourceType: 'mcp',
    resourceId: 'write_artifact',
    resource: 'mcp/write_artifact',
    runId: RUN_A,
    nodeId: 'visual_bqc5',
    outcome: 'ok',
    summary: '写入产物 page.html',
    payload: { tool: 'write_artifact' },
  },
  {
    id: 'e4',
    projectId: 'proj-1',
    occurredAt: '2026-07-26T10:44:18Z',
    actor: 'alice',
    actorDisplay: 'alice',
    unattributable: false,
    callerKind: 'pm',
    action: 'gate.decide',
    resourceType: 'gate',
    resourceId: 'visual',
    resource: 'gate/visual',
    runId: RUN_A,
    nodeId: 'gate_vis01',
    outcome: 'ok',
    summary: '门禁通过',
    payload: { decision: 'approve' },
  },
  {
    id: 'e8',
    projectId: 'proj-1',
    occurredAt: '2026-07-26T10:43:20Z',
    actor: 'system',
    actorDisplay: '系统',
    unattributable: true,
    callerKind: 'system',
    action: 'mcp.call',
    resourceType: 'mcp',
    resourceId: 'write_artifact',
    resource: 'mcp/write_artifact',
    runId: RUN_A,
    nodeId: 'visual_bqc5',
    outcome: 'fail',
    summary: '写入产物 page.html 失败 · content too large',
    payload: { tool: 'write_artifact', error: 'content too large' },
  },
  {
    id: 'e5',
    projectId: 'proj-1',
    occurredAt: '2026-07-26T06:05:22Z',
    actor: 'apikey',
    actorDisplay: 'API Key',
    unattributable: false,
    callerKind: 'apikey',
    action: 'run.start',
    resourceType: 'run',
    resourceId: RUN_B,
    resource: `run/${RUN_B}`,
    runId: RUN_B,
    nodeId: '',
    outcome: 'ok',
    summary: 'API Key 触发',
    payload: {},
  },
  {
    id: 'e6',
    projectId: 'proj-1',
    occurredAt: '2026-07-26T06:06:01Z',
    actor: 'system',
    actorDisplay: '系统',
    unattributable: true,
    callerKind: 'system',
    action: 'mcp.call',
    resourceType: 'mcp',
    resourceId: 'read_artifact',
    resource: 'mcp/read_artifact',
    runId: RUN_B,
    nodeId: 'research_2wn4',
    outcome: 'fail',
    summary: '读取产物 research.json 失败 · not found',
    payload: { error: 'not_found' },
  },
  {
    id: 'e7',
    projectId: 'proj-1',
    occurredAt: '2026-07-25T12:01:33Z',
    actor: 'apikey',
    actorDisplay: 'API Key',
    unattributable: false,
    callerKind: 'apikey',
    action: 'project.config',
    resourceType: 'project',
    resourceId: 'proj-1',
    resource: 'project/proj-1',
    runId: '',
    nodeId: '',
    outcome: 'ok',
    summary: '修改可见性',
    payload: {},
  },
  {
    id: 'e10',
    projectId: 'proj-1',
    occurredAt: '2026-07-26T08:10:00Z',
    actor: 'system',
    actorDisplay: '系统',
    unattributable: true,
    callerKind: 'system',
    action: 'run.start',
    resourceType: 'run',
    resourceId: RUN_C,
    resource: `run/${RUN_C}`,
    runId: RUN_C,
    nodeId: '',
    outcome: 'ok',
    summary: 'Run C 开始',
    payload: { runId: RUN_C },
  },
  {
    id: 'e11',
    projectId: 'proj-1',
    occurredAt: '2026-07-26T08:10:08Z',
    actor: 'system',
    actorDisplay: '系统',
    unattributable: true,
    callerKind: 'system',
    action: 'mcp.call',
    resourceType: 'mcp',
    resourceId: 'read_artifact',
    resource: 'mcp/read_artifact',
    runId: RUN_C,
    nodeId: 'research_2wn4',
    outcome: 'ok',
    summary: '读取产物 research.json',
    payload: { tool: 'read_artifact', runId: RUN_C },
  },
  {
    id: 'e12',
    projectId: 'proj-1',
    occurredAt: '2026-07-26T08:10:16Z',
    actor: 'system',
    actorDisplay: '系统',
    unattributable: true,
    callerKind: 'system',
    action: 'mcp.call',
    resourceType: 'mcp',
    resourceId: 'write_artifact',
    resource: 'mcp/write_artifact',
    runId: RUN_C,
    nodeId: 'visual_bqc5',
    outcome: 'ok',
    summary: '写入产物 page.html',
    payload: { tool: 'write_artifact' },
  },
  {
    id: 'e20',
    projectId: 'proj-1',
    occurredAt: '2026-07-26T09:00:00Z',
    actor: 'system',
    actorDisplay: '系统',
    unattributable: true,
    callerKind: 'system',
    action: 'run.start',
    resourceType: 'run',
    resourceId: RUN_D,
    resource: `run/${RUN_D}`,
    runId: RUN_D,
    nodeId: '',
    outcome: 'ok',
    summary: 'Run D 开始',
    payload: { runId: RUN_D },
  },
  {
    id: 'e21',
    projectId: 'proj-1',
    occurredAt: '2026-07-26T09:00:04Z',
    actor: 'system',
    actorDisplay: '系统',
    unattributable: true,
    callerKind: 'system',
    action: 'project.config',
    resourceType: 'project',
    resourceId: 'proj-1',
    resource: 'project/proj-1',
    runId: RUN_D,
    nodeId: '',
    outcome: 'ok',
    summary: '单分组配置变更',
    payload: {},
  },
]

function facetsFor(runId?: string | null) {
  const runs = [
    {
      runId: RUN_A,
      label: '2026-07-26 18:42 · 8f2a1111',
      sub: '成功 · 含 MCP',
    },
    {
      runId: RUN_B,
      label: '2026-07-26 14:05 · 3c911111',
      sub: '失败 · 含 MCP',
    },
    {
      runId: RUN_C,
      label: '2026-07-26 16:10 · aa11cccc',
      sub: '成功 · 含 MCP',
    },
    {
      runId: RUN_D,
      label: '2026-07-26 17:00 · bb22dddd',
      sub: '成功',
    },
  ]
  const inRun = runId
    ? MOCK_EVENTS.filter((e) => e.runId === runId)
    : MOCK_EVENTS
  const nodeIds = [...new Set(inRun.map((e) => e.nodeId).filter(Boolean))]
  const resources = [
    ...new Map(
      inRun
        .filter((e) => e.resource)
        .map((e) => [
          e.resource,
          {
            resource: e.resource,
            resourceType: e.resourceType,
            resourceId: e.resourceId,
          },
        ]),
    ).values(),
  ]
  return {
    runs,
    nodes: nodeIds.map((id) => ({ nodeId: id, label: id })),
    resources,
    actors: ['alice', 'system'],
  }
}

function filterEvents(url: URL) {
  const runId = url.searchParams.get('runId') || ''
  const nodeId = url.searchParams.get('nodeId') || ''
  const callerKind = url.searchParams.get('callerKind') || ''
  const resource = url.searchParams.get('resource') || ''
  const search = (url.searchParams.get('search') || '').toLowerCase()
  let rows = MOCK_EVENTS.slice()
  if (runId) rows = rows.filter((e) => e.runId === runId)
  if (nodeId) rows = rows.filter((e) => e.nodeId === nodeId)
  if (callerKind) rows = rows.filter((e) => e.callerKind === callerKind)
  if (resource) {
    rows = rows.filter(
      (e) =>
        e.resource === resource ||
        e.resource.startsWith(resource + '/') ||
        e.resource.includes(resource),
    )
  }
  if (search) {
    rows = rows.filter((e) =>
      `${e.summary} ${e.resource} ${e.action}`.toLowerCase().includes(search),
    )
  }
  rows.sort((a, b) => (a.occurredAt < b.occurredAt ? 1 : -1))
  const mcp = rows.filter((e) => e.action === 'mcp.call').length
  const fail = rows.filter((e) => e.outcome === 'fail').length
  const page = Number(url.searchParams.get('page') || 1)
  const pageSize = Number(url.searchParams.get('pageSize') || 10)
  const start = (page - 1) * pageSize
  return {
    items: rows.slice(start, start + pageSize),
    total: rows.length,
    page,
    pageSize,
    hasMore: start + pageSize < rows.length,
    stats: { total: rows.length, mcp, fail },
  }
}

async function stubProjectApis(page: Page) {
  await seedOnboardingDismissed(page, 'proj-1')
  await page.route('**/api/projects/proj-1', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_PROJECT),
      })
      return
    }
    await route.continue()
  })
  await page.route('**/api/workflows**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([]),
    })
  })
  await page.route('**/api/runs**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ items: [], total: 0, page: 1, pageSize: 100, hasMore: false }),
    })
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
  await page.route('**/api/projects/proj-1/audit/facets**', async (route) => {
    const url = new URL(route.request().url())
    const runId = url.searchParams.get('runId')
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(facetsFor(runId)),
    })
  })
  await page.route('**/api/projects/proj-1/audit?**', async (route) => {
    const url = new URL(route.request().url())
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(filterEvents(url)),
    })
  })
  await page.route('**/api/projects/proj-1/audit/export**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ exported: true }),
    })
  })
}

const SHOT_DIR = '/tmp/audit-e2e-shots'

test.describe('审计 Tab 双模式 Demo 验收', () => {
  test.beforeAll(() => {
    fs.mkdirSync(SHOT_DIR, { recursive: true })
  })

  test('默认按 Run：三列分组；语义摘要辅行；无独立动作/资源/调用者列', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await stubProjectApis(page)
    await page.goto('/project-detail.html?tab=audit&theme=light')
    await dismissOnboardingIfOpen(page)

    const panel = page.getByTestId('project-audit-panel')
    await expect(panel).toBeVisible({ timeout: 15_000 })

    await expect(page.getByTestId('project-audit-mode-run')).toHaveClass(/on/)
    await expect(page.getByRole('button', { name: '按 Run 查看' })).toBeVisible()
    await expect(page.getByRole('button', { name: '全部日志' })).toBeVisible()
    await expect(page.locator('body')).not.toContainText('张三')
    await expect(page.locator('body')).not.toContainText('动作类型')
    await expect(page.locator('body')).not.toContainText('旧摘要')

    // Run mode hides event pagination; count lives on the toolbar (not a dedicated strip)
    await expect(page.getByTestId('project-audit-stats')).toBeVisible()
    await expect(page.getByTestId('project-audit-page-size')).toHaveCount(0)
    await expect(panel.locator('select')).toHaveCount(0)

    const list = page.getByTestId('project-audit-list')
    await expect(list).toBeVisible()
    await expect(list).toHaveAttribute('data-layout', 'groups')
    await expect(page.getByRole('columnheader', { name: '时间' })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: '摘要' })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: '结果' })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: '调用者' })).toHaveCount(0)
    await expect(page.getByRole('columnheader', { name: '动作' })).toHaveCount(0)
    await expect(page.getByRole('columnheader', { name: '资源' })).toHaveCount(0)
    await expect(page.getByText('mcp.call')).toHaveCount(0)
    await expect(page.getByTestId('project-audit-stats')).toContainText('MCP')
    await expect(page.getByTestId('project-audit-stats')).toContainText('成功')

    await expect(page.getByTestId('project-audit-run')).toContainText('Run')
    await expect(page.getByTestId('project-audit-run')).toContainText('2026-07-26 18:42 · 8f2a1111')
    await expect(page.getByTestId('project-audit-resource')).toContainText('资源')
    await expect(page.getByTestId('project-audit-time')).toContainText('时间')
    await expect(panel.locator('h4')).toHaveCount(0)
    await expect(panel.locator('.chip').filter({ hasText: /^Run\b/ })).toHaveCount(0)

    const exportBtn = page.getByTestId('project-audit-export')
    await expect(exportBtn).toHaveText('导出')
    await expect(page.getByRole('button', { name: '导出 JSON' })).toHaveCount(0)

    await page.screenshot({ path: path.join(SHOT_DIR, '01-run-mode-mcp.png'), fullPage: true })

    await page.getByTestId('project-audit-resource').locator('button.audit-dd-trig').click()
    const resPanel = page.locator('.audit-dd-panel').filter({ visible: true })
    await expect(resPanel).toBeVisible()
    await resPanel.locator('.audit-dd-opt', { hasText: 'mcp/read_artifact' }).first().click()
    await expect(page.getByTestId('project-audit-group-research_2wn4')).toHaveAttribute('data-open', 'true')
    await expect(page.getByTestId('project-audit-list').locator('tbody tr.row')).toHaveCount(1)
    await expect(page.getByText('读取产物 research.json')).toBeVisible()
    await expect(page.getByText('mcp / read_artifact').first()).toBeVisible()
    await expect(page.getByTestId('project-audit-chips')).toContainText('mcp/read_artifact')

    await page.getByTestId('project-audit-event-e2').click()
    await expect(page.getByTestId('project-audit-payload')).toBeVisible()
    await expect(page.locator('.detail-meta code').filter({ hasText: 'mcp.call' })).toBeVisible()

    await page.screenshot({ path: path.join(SHOT_DIR, '01b-resource-narrow.png'), fullPage: true })
  })

  test('全部日志：四列扁表时间/节点/摘要/结果；保留调用者筛选与分页', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await stubProjectApis(page)
    await page.goto('/project-detail.html?tab=audit&theme=light')
    await dismissOnboardingIfOpen(page)
    await expect(page.getByTestId('project-audit-panel')).toBeVisible({ timeout: 15_000 })

    await page.getByTestId('project-audit-mode-all').click()
    await expect(page.getByTestId('project-audit-mode-all')).toHaveClass(/on/)
    await expect(page.getByTestId('project-audit-caller')).toBeVisible()
    await expect(page.getByTestId('project-audit-run')).toHaveCount(0)
    await expect(page.getByTestId('project-audit-node')).toHaveCount(0)
    await expect(page.getByTestId('project-audit-page-size')).toBeVisible()
    await expect(page.getByTestId('project-audit-pager-info')).toBeVisible()

    const wrap = page.getByTestId('project-audit-list')
    await expect(wrap).toHaveAttribute('data-layout', 'table')
    await expect(page.getByRole('columnheader', { name: '时间' })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: '节点' })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: '摘要' })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: '结果' })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: '所属 Run' })).toHaveCount(0)
    await expect(page.getByRole('columnheader', { name: '调用者' })).toHaveCount(0)
    await expect(page.getByRole('columnheader', { name: '动作' })).toHaveCount(0)
    await expect(page.getByText('调研 · 2wn4').first()).toBeVisible()
    await expect(page.getByText('读取产物 research.json').first()).toBeVisible()

    await page.getByTestId('project-audit-caller').locator('button.audit-dd-trig').click()
    const callerPanel = page.locator('.audit-dd-panel').filter({ visible: true })
    await expect(callerPanel).toBeVisible()
    await expect(callerPanel.locator('.audit-dd-opt', { hasText: '项目管理' })).toBeVisible()
    await expect(callerPanel.locator('.audit-dd-opt', { hasText: '用户 API Key' })).toBeVisible()
    await expect(callerPanel.locator('.audit-dd-opt', { hasText: '系统' })).toBeVisible()
    await callerPanel.locator('.audit-dd-opt', { hasText: '系统' }).click()

    await expect(page.locator('body')).not.toContainText('张三')
    await page.screenshot({ path: path.join(SHOT_DIR, '02-all-mode-caller.png'), fullPage: true })
  })

  test('无 Run 空态可切全部日志', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await stubProjectApis(page)
    await page.route('**/api/projects/proj-1/audit/facets**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ runs: [], nodes: [], resources: [], actors: [] }),
      })
    })
    await page.goto('/project-detail.html?tab=audit&theme=light')
    await dismissOnboardingIfOpen(page)
    await expect(page.getByTestId('project-audit-empty-runs')).toBeVisible({ timeout: 15_000 })
    await page.getByTestId('project-audit-empty-runs').getByRole('button', { name: '全部日志' }).click()
    await expect(page.getByTestId('project-audit-mode-all')).toHaveClass(/on/)
    await page.screenshot({ path: path.join(SHOT_DIR, '03-empty-runs.png'), fullPage: true })
  })

  test('移动视口 ~390px：筛选摘要默认收起 + 事件卡片（plan g5.2）', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await stubProjectApis(page)
    await page.goto('/project-detail.html?tab=audit&theme=light')
    await dismissOnboardingIfOpen(page)

    const panel = page.getByTestId('project-audit-panel')
    await expect(panel).toBeVisible({ timeout: 15_000 })

    // Default: one-line summary, not four stacked dropdowns
    const summary = page.getByTestId('project-audit-filter-summary')
    await expect(summary).toBeVisible()
    await expect(summary).toContainText('Run')
    await expect(page.getByTestId('project-audit-filters-editor')).toBeHidden()
    await expect(page.getByTestId('project-audit-search')).toBeVisible()
    await expect(page.getByTestId('project-audit-export')).toBeVisible()
    await expect(page.getByTestId('project-audit-mode-run')).toBeVisible()

    // Event cards (no wide table)
    const list = page.getByTestId('project-audit-list')
    await expect(list).toHaveAttribute('data-layout', 'cards')
    await expect(list.locator('table')).toHaveCount(0)
    await expect(page.getByTestId('project-audit-event-e1')).toBeVisible()
    await expect(page.getByTestId('project-audit-event-e2')).toBeVisible()

    // Collapsed filters keep panel chrome compact (search/export/meta only — not 4 stacked dropdowns).
    // Measure gap from summary to the first event card (newest), not a specific id.
    const layoutGap = await page.evaluate(() => {
      const s = document.querySelector('[data-testid="project-audit-filter-summary"]')
      const c = document.querySelector('[data-testid="project-audit-list"] .event-card')
      if (!s || !c) return -1
      return c.getBoundingClientRect().top - s.getBoundingClientRect().top
    })
    expect(layoutGap).toBeGreaterThan(0)
    expect(layoutGap).toBeLessThan(280)

    await page.screenshot({ path: path.join(SHOT_DIR, '04-mobile-collapsed.png'), fullPage: true })

    // Expand filters → full-width triggers align with search
    await summary.click()
    const editor = page.getByTestId('project-audit-filters-editor')
    await expect(editor).toBeVisible()
    await expect(page.getByTestId('project-audit-run')).toBeVisible()
    await expect(page.getByTestId('project-audit-node')).toBeVisible()
    await expect(page.getByTestId('project-audit-resource')).toBeVisible()
    await expect(page.getByTestId('project-audit-time')).toBeVisible()

    const searchBox = page.getByTestId('project-audit-search')
    const runTrig = page.getByTestId('project-audit-run').locator('button.audit-dd-trig')
    const searchW = await searchBox.evaluate((el) => {
      const wrap = el.closest('.search') || el
      return wrap.getBoundingClientRect().width
    })
    const runW = await runTrig.evaluate((el) => el.getBoundingClientRect().width)
    expect(Math.abs(searchW - runW)).toBeLessThan(12)

    // Dropdown panel stays within viewport
    await runTrig.click()
    const ddPanel = page.locator('.audit-dd-panel').filter({ visible: true })
    await expect(ddPanel).toBeVisible()
    const panelBox = await ddPanel.boundingBox()
    expect(panelBox).toBeTruthy()
    expect(panelBox!.x).toBeGreaterThanOrEqual(0)
    expect(panelBox!.x + panelBox!.width).toBeLessThanOrEqual(390 + 2)
    await page.keyboard.press('Escape')

    await page.screenshot({ path: path.join(SHOT_DIR, '04b-mobile-filters-open.png'), fullPage: true })

    // Card inline expand
    await page.getByTestId('project-audit-event-e2').click()
    await expect(page.getByTestId('project-audit-payload')).toBeVisible()
    await expect(page.getByTestId('project-audit-event-e2')).toContainText('调用者')

    // All-logs mode shares card layout
    await page.getByTestId('project-audit-mode-all').click()
    await expect(page.getByTestId('project-audit-mode-all')).toHaveClass(/on/)
    await expect(page.getByTestId('project-audit-list')).toHaveAttribute('data-layout', 'cards')
    await expect(page.getByTestId('project-audit-filter-summary')).toContainText('调用者')

    await page.screenshot({ path: path.join(SHOT_DIR, '04c-mobile-all-mode.png'), fullPage: true })
  })

  test('宽屏仍为表 + 横向筛选（plan g4.1 / g5.2）', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await stubProjectApis(page)
    await page.goto('/project-detail.html?tab=audit&theme=light')
    await dismissOnboardingIfOpen(page)
    await expect(page.getByTestId('project-audit-panel')).toBeVisible({ timeout: 15_000 })

    await expect(page.getByTestId('project-audit-filter-summary')).toHaveCount(0)
    await expect(page.getByTestId('project-audit-list')).toHaveAttribute('data-layout', 'groups')
    // Collapsed groups keep tables in DOM but hidden; only fail-open groups show a table.
    await expect(page.locator('[data-testid="project-audit-list"] .group[data-open="true"] table')).toBeVisible()
    await expect(page.getByTestId('project-audit-run')).toBeVisible()
    await expect(page.getByTestId('project-audit-search')).toBeVisible()
  })

  test('组头可读名与失败组默认展开（g5.2）', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await stubProjectApis(page)
    await page.goto('/project-detail.html?tab=audit&theme=light')
    await dismissOnboardingIfOpen(page)
    await expect(page.getByTestId('project-audit-panel')).toBeVisible({ timeout: 15_000 })

    await expect(page.getByTestId('project-audit-group-head-research_2wn4')).toContainText('调研 · 2wn4')
    await expect(page.getByTestId('project-audit-group-head-research_2wn4')).toContainText('research_2wn4')
    await expect(page.getByTestId('project-audit-group-head-visual_bqc5')).toContainText('视觉 · bqc5')
    await expect(page.getByTestId('project-audit-group-head-gate_vis01')).toContainText('门禁 · vis01')
    await expect(page.getByTestId('project-audit-group-head-_system')).toContainText('系统/未归属')

    await expect(page.getByTestId('project-audit-group-visual_bqc5')).toHaveAttribute('data-open', 'true')
    await expect(page.getByTestId('project-audit-group-research_2wn4')).toHaveAttribute('data-open', 'false')
    await expect(page.getByTestId('project-audit-group-gate_vis01')).toHaveAttribute('data-open', 'false')
    await expect(page.getByTestId('project-audit-event-e8')).toBeVisible()
    await expect(page.getByText('写入产物 page.html 失败 · content too large')).toBeVisible()
    await expect(page.getByTestId('project-audit-group-visual_bqc5')).toContainText('失败')
    await expect(page.getByTestId('project-audit-group-visual_bqc5')).toContainText('成功')

    await page.getByTestId('project-audit-group-head-research_2wn4').click()
    await expect(page.getByTestId('project-audit-group-research_2wn4')).toHaveAttribute('data-open', 'true')
    await expect(page.getByTestId('project-audit-event-e2')).toBeVisible()

    await page.getByTestId('project-audit-node').locator('button.audit-dd-trig').click()
    const nodePanel = page.locator('.audit-dd-panel').filter({ visible: true })
    await expect(nodePanel.locator('.audit-dd-opt', { hasText: '调研 · 2wn4' })).toBeVisible()
    await page.keyboard.press('Escape')

    await page.getByTestId('project-audit-mode-all').click()
    await expect(page.getByTestId('project-audit-list')).toHaveAttribute('data-layout', 'table')
    await page.getByTestId('project-audit-mode-run').click()
    await expect(page.getByTestId('project-audit-list')).toHaveAttribute('data-layout', 'groups')
    await expect(page.getByTestId('project-audit-export')).toBeVisible()
  })

  test('全成功 Run：单分组与多分组均默认全展开且事件行可见', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await stubProjectApis(page)
    await page.goto('/project-detail.html?tab=audit&theme=light')
    await dismissOnboardingIfOpen(page)
    await expect(page.getByTestId('project-audit-panel')).toBeVisible({ timeout: 15_000 })

    async function pickRun(label: string) {
      await page.getByTestId('project-audit-run').locator('button.audit-dd-trig').click()
      const panel = page.locator('.audit-dd-panel').filter({ visible: true })
      await expect(panel).toBeVisible()
      await panel.locator('.audit-dd-opt', { hasText: label }).first().click()
    }

    await pickRun('2026-07-26 16:10 · aa11cccc')
    await expect(page.getByTestId('project-audit-group-_system')).toHaveAttribute('data-open', 'true')
    await expect(page.getByTestId('project-audit-group-research_2wn4')).toHaveAttribute('data-open', 'true')
    await expect(page.getByTestId('project-audit-group-visual_bqc5')).toHaveAttribute('data-open', 'true')
    await expect(page.getByTestId('project-audit-event-e10')).toBeVisible()
    await expect(page.getByTestId('project-audit-event-e11')).toBeVisible()
    await expect(page.getByTestId('project-audit-event-e12')).toBeVisible()
    await expect(page.getByText('Run C 开始')).toBeVisible()

    await pickRun('2026-07-26 17:00 · bb22dddd')
    await expect(page.getByTestId('project-audit-group-_system')).toHaveAttribute('data-open', 'true')
    await expect(page.getByTestId('project-audit-event-e20')).toBeVisible()
    await expect(page.getByTestId('project-audit-event-e21')).toBeVisible()
    await expect(page.getByText('单分组配置变更')).toBeVisible()
    await expect(page.getByTestId('project-audit-panel').locator('h4')).toHaveCount(0)
    await expect(page.getByTestId('project-audit-panel').locator('.chip').filter({ hasText: /^Run\b/ })).toHaveCount(0)
  })

  test('有权但 0 条事件时空态可切全部日志', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await stubProjectApis(page)
    await page.route('**/api/projects/proj-1/audit?**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          items: [],
          total: 0,
          page: 1,
          pageSize: 10,
          hasMore: false,
          stats: { total: 0, mcp: 0, fail: 0 },
        }),
      })
    })
    await page.goto('/project-detail.html?tab=audit&theme=light')
    await dismissOnboardingIfOpen(page)
    await expect(page.getByTestId('project-audit-empty')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('project-audit-empty-all')).toBeVisible()
    await expect(page.getByTestId('project-audit-list')).toHaveCount(0)
    await page.getByTestId('project-audit-empty-all').click()
    await expect(page.getByTestId('project-audit-mode-all')).toHaveClass(/on/)
  })
})
