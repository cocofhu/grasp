import { test, expect } from '@playwright/test'
import path from 'node:path'
import { routeApi } from './helpers/apiRoute'
import { dismissOnboardingIfOpen, seedOnboardingDismissed } from './helpers/onboarding'

function stubRun(partial: {
  id: string
  status: string
  title?: string
}) {
  return {
    id: partial.id,
    workflowId: 'wf-1',
    workflowName: 'Demo Workflow',
    title: partial.title,
    status: partial.status,
    trigger: 'manual',
    startedAt: '2026-07-18T12:00:00Z',
    durationSec: 120,
    progress: 40,
    currentNodeLabel: '实现',
    nodeRuns: {},
    artifacts: [],
  }
}

function dayBucket(offset: number, totals: { total: number; workflowTotal: number; pmTotal: number }) {
  const d = new Date(2026, 6, 11 + offset)
  const bucket = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  return {
    bucket,
    ...totals,
    inputTokens: Math.floor(totals.total * 0.4),
    outputTokens: Math.floor(totals.total * 0.3),
    cacheReadTokens: Math.floor(totals.total * 0.2),
    cacheWriteTokens: Math.max(0, totals.total - Math.floor(totals.total * 0.9)),
  }
}

/** Near-30d series: left-edge 2026-07-11 is a 0-value point (g4.5 hover). */
const TREND_30D_LEFT_ZERO = Array.from({ length: 30 }, (_, i) => {
  if (i === 0) return dayBucket(0, { total: 0, workflowTotal: 0, pmTotal: 0 })
  if (i === 22) return dayBucket(i, { total: 124_000_000, workflowTotal: 96_000_000, pmTotal: 28_000_000 })
  const total = i * 12_000
  const pm = Math.floor(total * 0.25)
  return dayBucket(i, { total, workflowTotal: total - pm, pmTotal: pm })
})

const STATS_30D = {
  window: '30d',
  bucketWidth: 'day',
  timezone: 'Asia/Shanghai',
  empty: false,
  trend: TREND_30D_LEFT_ZERO,
  composition: {
    inputTokens: 800,
    outputTokens: 650,
    cacheReadTokens: 350,
    cacheWriteTokens: 200,
    total: 2000,
  },
  workflows: [
    // Fixed order: Top workflows (desc) → PM → other (no mid-insert PM / 12PM34)
    { workflowId: 'wf-a', name: 'approve-main', total: 1200, kind: 'workflow' },
    { workflowId: 'wf-b', name: 'doc-review', total: 300, kind: 'workflow' },
    { name: 'PM', total: 500, kind: 'pm' },
    { name: '其他', total: 200, other: true, kind: 'other' },
  ],
}

const STATS_7D = {
  ...STATS_30D,
  window: '7d',
  trend: TREND_30D_LEFT_ZERO.slice(-7),
  composition: { ...STATS_30D.composition, total: 900 },
  workflows: [
    { workflowId: 'wf-a', name: 'approve-main', total: 600 },
    { workflowId: 'wf-b', name: 'doc-review', total: 300 },
  ],
}

const STATS_24H = {
  ...STATS_30D,
  window: '24h',
  bucketWidth: 'hour',
  trend: [
    {
      bucket: '2026-07-24T20',
      total: 40,
      workflowTotal: 30,
      pmTotal: 10,
      inputTokens: 16,
      outputTokens: 12,
      cacheReadTokens: 8,
      cacheWriteTokens: 4,
    },
    {
      bucket: '2026-07-24T21',
      total: 0,
      workflowTotal: 0,
      pmTotal: 0,
      inputTokens: 0,
      outputTokens: 0,
      cacheReadTokens: 0,
      cacheWriteTokens: 0,
    },
    {
      bucket: '2026-07-25T08',
      total: 80,
      workflowTotal: 60,
      pmTotal: 20,
      inputTokens: 32,
      outputTokens: 24,
      cacheReadTokens: 16,
      cacheWriteTokens: 8,
    },
  ],
  composition: { ...STATS_30D.composition, total: 120 },
}

const STATS_ALL = {
  window: 'all',
  bucketWidth: 'week',
  timezone: 'Asia/Shanghai',
  empty: false,
  trend: [
    {
      bucket: '2026-W28',
      total: 5000,
      inputTokens: 2000,
      outputTokens: 1800,
      cacheReadTokens: 800,
      cacheWriteTokens: 400,
    },
  ],
  composition: {
    inputTokens: 2000,
    outputTokens: 1800,
    cacheReadTokens: 800,
    cacheWriteTokens: 400,
    total: 5000,
  },
  workflows: [{ workflowId: 'wf-a', name: 'approve-main', total: 5000 }],
}

const EMPTY_STATS = {
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
}

async function mockBoardShell(page: import('@playwright/test').Page) {
  // routeApi: pathname must start with /api/ so Vite /src/lib/api/** modules are not mocked.
  await routeApi(page, '**/api/stats/dashboard', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ running: 1, waitingHuman: 0, failed: 0, completed: 1 }),
      })
      return
    }
    await route.continue()
  })

  await routeApi(page, '**/api/runs**', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.continue()
      return
    }
    const url = new URL(route.request().url())
    if (!url.searchParams.get('projectId')) {
      await route.fulfill({
        status: 400,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'projectId required' }),
      })
      return
    }
    const status = url.searchParams.get('status') || ''
    const items =
      status === 'running'
        ? [stubRun({ id: 'run-1', status: 'running', title: '看板运行中' })]
        : status === 'completed'
          ? [stubRun({ id: 'run-2', status: 'completed', title: '看板已完成' })]
          : []
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ items, total: items.length, page: 1, pageSize: 20, hasMore: false }),
    })
  })
}

test.describe('看板 Token 统计图', () => {
  test('有数据：默认近 30 天三图布局、切窗清空旧数据、失败重试', async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await mockBoardShell(page)

    let failNext = false
    const seenWindows: string[] = []
    await routeApi(page, '**/api/projects/*/token-stats**', async (route) => {
      if (route.request().method() !== 'GET') {
        await route.continue()
        return
      }
      const url = new URL(route.request().url())
      const w = url.searchParams.get('window') || '30d'
      seenWindows.push(w)
      if (failNext) {
        failNext = false
        await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'timeout' }) })
        return
      }
      if (w === '24h') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(STATS_24H),
        })
        return
      }
      if (w === '7d') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(STATS_7D),
        })
        return
      }
      if (w === 'all') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(STATS_ALL),
        })
        return
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(STATS_30D),
      })
    })

    await page.goto('/board.html?start=project-board&memory=1&projectId=proj-1')
    await expect(page.getByTestId('board-view')).toBeVisible({ timeout: 15_000 })
    const panel = page.getByTestId('token-stats-panel')
    await expect(panel).toBeVisible()
    await expect(page.getByTestId('token-stats-window-30d')).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByTestId('token-stats-window-badge')).toContainText('近 30 天')
    await expect(page.getByTestId('token-stats-charts')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('token-stats-trend-card')).toBeVisible()
    await expect(page.getByTestId('token-stats-comp-card')).toBeVisible()
    await expect(page.getByTestId('token-stats-rank-card')).toBeVisible()
    await expect(page.getByTestId('token-trend-wrap')).toBeVisible()
    await expect(page.getByTestId('token-trend-chart')).toBeVisible()
    await expect(page.getByTestId('token-trend-chart').locator('canvas')).toBeVisible()
    // Regression: no non-uniform SVG stretch path for the trend chart
    await expect(page.getByTestId('token-trend-svg')).toHaveCount(0)
    await expect(page.locator('[data-testid="token-trend-wrap"] svg[preserveAspectRatio="none"]')).toHaveCount(0)
    await expect(page.getByTestId('token-donut-chart')).toBeVisible()
    await expect(page.getByTestId('token-donut-chart').locator('canvas')).toBeVisible()
    await expect(page.getByTestId('token-stats-comp-card')).toContainText('用量构成')
    await expect(page.getByTestId('token-stats-comp-card')).not.toContainText('四分量')
    await expect(page.getByTestId('token-donut-legend')).toHaveCount(0)
    await expect(page.getByTestId('token-donut-chart')).not.toContainText('总量')
    await expect(panel).toContainText('approve-main')
    await expect(panel).toContainText('项目管理')
    await expect(panel).toContainText('其他')
    await expect(page.getByTestId('token-trend-legend')).toHaveCount(0)
    const canvas = page.getByTestId('token-trend-chart').locator('canvas')
    await expect(canvas).toBeVisible()
    const trendTip = page.locator('.token-trend-tooltip')
    await expect(async () => {
      const box = await canvas.boundingBox()
      expect(box).toBeTruthy()
      const ys = [box!.height - 28, Math.floor(box!.height * 0.55), 80, 100]
      const xs = [40, 48, 56, 64, 72, 84, 96, 120, 160, 200, 240]
      for (const y of ys) {
        for (const x of xs) {
          if (x >= box!.width - 4 || y >= box!.height - 4) continue
          await canvas.hover({ position: { x, y } })
          if (await trendTip.first().isVisible().catch(() => false)) return
        }
      }
      throw new Error('trend tooltip not shown')
    }).toPass({ timeout: 12_000 })
    await expect(trendTip.first()).toBeVisible()
    await expect(trendTip.first().locator('[data-tip-row="workflow"]')).toContainText('工作流')
    await expect(trendTip.first().locator('[data-tip-row="pm"]')).toContainText('项目管理')
    await expect(panel).toContainText('消耗排行')
    await expect(panel).toContainText('按来源堆叠')
    await expect(page.getByTestId('token-rank-list').locator('[data-kind="pm"]')).toBeVisible()

    // Continuous numeric badges: Top→PM→其他 → 1,2,3,· (no 12PM34 / no "PM" text badge)
    const rankList = page.getByTestId('token-rank-list')
    const kinds = await rankList.locator('li').evaluateAll((lis) =>
      lis.map((li) => li.getAttribute('data-kind')),
    )
    expect(kinds).toEqual(['workflow', 'workflow', 'pm', 'other'])
    const badges = await rankList.locator('li').evaluateAll((lis) =>
      lis.map((li) => {
        const badge = li.querySelector('span')
        return (badge?.textContent || '').trim()
      }),
    )
    expect(badges).toEqual(['1', '2', '3', '·'])

    // Panel sits above Run columns
    const panelBox = await panel.boundingBox()
    const colBox = await page.getByTestId('run-board-column').first().boundingBox()
    expect(panelBox && colBox).toBeTruthy()
    expect(panelBox!.y).toBeLessThan(colBox!.y)

    // No collapse control
    await expect(panel.getByRole('button', { name: /折叠|收起|展开/ })).toHaveCount(0)
    await expect(page.getByTestId('token-stats-retry')).toHaveCount(0)

    const readyShot = path.join(testInfo.outputDir, 'token-stats-ready.png')
    await page.screenshot({ path: readyShot, fullPage: true })
    await testInfo.attach('token-stats-ready', { path: readyShot, contentType: 'image/png' })

    // Switch window: loading clears charts
    await routeApi(page, '**/api/projects/*/token-stats**', async (route) => {
      const url = new URL(route.request().url())
      const w = url.searchParams.get('window') || '30d'
      seenWindows.push(`delayed-${w}`)
      await new Promise((r) => setTimeout(r, 400))
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(w === '7d' ? STATS_7D : STATS_30D),
      })
    })
    await page.getByTestId('token-stats-window-7d').click()
    await expect(page.getByTestId('token-stats-loading')).toBeVisible()
    await expect(page.getByTestId('token-stats-charts')).toHaveCount(0)
    await expect(page.getByTestId('token-stats-charts')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('token-stats-window-badge')).toContainText('近 7 天')
    await expect(page.getByTestId('token-stats-window-7d')).toHaveAttribute('aria-selected', 'true')
    // After window switch: Chart.js canvas still hosts trend; no SVG none stretch
    await expect(page.getByTestId('token-trend-chart').locator('canvas')).toBeVisible()
    await expect(page.locator('[data-testid="token-trend-wrap"] svg[preserveAspectRatio="none"]')).toHaveCount(0)
    const wrapBox = await page.getByTestId('token-trend-wrap').boundingBox()
    expect(wrapBox).toBeTruthy()
    expect(wrapBox!.height).toBeGreaterThanOrEqual(180)
    expect(wrapBox!.height).toBeLessThanOrEqual(240)

    // Failure + retry
    failNext = true
    await routeApi(page, '**/api/projects/*/token-stats**', async (route) => {
      const url = new URL(route.request().url())
      const w = url.searchParams.get('window') || '30d'
      if (failNext) {
        failNext = false
        await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'timeout' }) })
        return
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(w === 'all' ? STATS_ALL : STATS_30D),
      })
    })
    await page.getByTestId('token-stats-window-all').click()
    await expect(page.getByTestId('token-stats-error')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('token-stats-retry')).toBeVisible()
    await expect(page.getByTestId('token-stats-charts')).toHaveCount(0)

    const errShot = path.join(testInfo.outputDir, 'token-stats-error.png')
    await page.screenshot({ path: errShot, fullPage: true })
    await testInfo.attach('token-stats-error', { path: errShot, contentType: 'image/png' })

    await page.getByTestId('token-stats-retry').click()
    await expect(page.getByTestId('token-stats-charts')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('token-stats-trend-card')).toContainText('按周')
  })

  test('空数据：整区空状态且不画伪造全 0 图', async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await mockBoardShell(page)
    await routeApi(page, '**/api/projects/*/token-stats**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(EMPTY_STATS),
      })
    })

    await page.goto('/board.html?start=project-board&memory=1&projectId=proj-1')
    await expect(page.getByTestId('token-stats-panel')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('token-stats-empty')).toBeVisible()
    await expect(page.getByTestId('token-stats-charts')).toHaveCount(0)
    await expect(page.getByTestId('token-trend-chart')).toHaveCount(0)
    await expect(page.getByTestId('token-trend-svg')).toHaveCount(0)
    await expect(page.getByTestId('token-donut-chart')).toHaveCount(0)
    await expect(page.getByTestId('token-stats-empty')).toContainText('未上报不会显示为 0')
    await expect(page.getByTestId('token-stats-empty')).toContainText('用量构成')
    await expect(page.getByTestId('token-stats-empty')).toContainText('暂无已上报的用量构成数据')
    await expect(page.getByTestId('token-stats-empty')).not.toContainText('四分量')

    const emptyShot = path.join(testInfo.outputDir, 'token-stats-empty.png')
    await page.screenshot({ path: emptyShot, fullPage: true })
    await testInfo.attach('token-stats-empty', { path: emptyShot, contentType: 'image/png' })
  })

  test('窄屏下方两图可堆叠且统计区仍在 Run 列上方', async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await mockBoardShell(page)
    await routeApi(page, '**/api/projects/*/token-stats**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(STATS_30D),
      })
    })

    await page.goto('/board.html?start=project-board&memory=1&projectId=proj-1')
    await expect(page.getByTestId('token-stats-charts')).toBeVisible({ timeout: 15_000 })
    const comp = await page.getByTestId('token-stats-comp-card').boundingBox()
    const rank = await page.getByTestId('token-stats-rank-card').boundingBox()
    expect(comp && rank).toBeTruthy()
    expect(rank!.y).toBeGreaterThan(comp!.y + 40)

    const narrowShot = path.join(testInfo.outputDir, 'token-stats-narrow.png')
    await page.screenshot({ path: narrowShot, fullPage: true })
    await testInfo.attach('token-stats-narrow', { path: narrowShot, contentType: 'image/png' })
  })

  test('项目详情看板 Tab 展示 Token 统计区且页头 totalTokens 不变', async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    // Empty workflows stub would otherwise auto-open onboarding and intercept clicks.
    await seedOnboardingDismissed(page, 'proj-1')
    await routeApi(page, '**/api/projects/proj-1', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            id: 'proj-1',
            name: 'Grasp',
            description: 'Grasp Project',
            workflowCount: 3,
            totalTokens: 128400,
            workflowTokens: 100000,
            pmTokens: 28400,
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-07-25T03:42:00Z',
            variables: [],
          }),
        })
        return
      }
      await route.continue()
    })
    await routeApi(page, '**/api/workflows**', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' })
    })
    // Onboarding stays closed via the dismiss seed above, not via this agents stub.
    await routeApi(page, '**/api/agents**', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([{ id: 'agent-stub', name: 'StubAgent', projectId: 'proj-1' }]),
        })
        return
      }
      await route.continue()
    })
    await routeApi(page, '**/api/runs**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: [], total: 0, page: 1, pageSize: 20, hasMore: false }),
      })
    })
    await routeApi(page, '**/api/projects/proj-1/pm-leader', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ enabled: false }),
      })
    })
    await routeApi(page, '**/api/projects/proj-1/cron-jobs', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' })
    })
    await routeApi(page, '**/api/projects/*/token-stats**', async (route) => {
      const url = new URL(route.request().url())
      const w = url.searchParams.get('window') || '30d'
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(w === 'all' ? STATS_ALL : STATS_30D),
      })
    })

    await page.goto('/project-detail.html?theme=light&tab=board')
    await dismissOnboardingIfOpen(page)
    const headerStat = page.getByTestId('project-token-stat')
    await expect(headerStat).toBeVisible({ timeout: 15_000 })
    await expect(headerStat).toContainText('128.4K')
    await expect(headerStat).not.toContainText('全部历史 · 含工作流与项目管理（上线后）')

    await expect(page.getByTestId('token-stats-panel')).toBeVisible()
    await expect(page.getByTestId('token-stats-charts')).toBeVisible()

    await page.getByTestId('token-stats-window-all').click()
    await expect(page.getByTestId('token-stats-window-badge')).toContainText('全部历史')
    await expect(headerStat).toContainText('128.4K')
    await expect(headerStat).not.toContainText('全部历史 · 含工作流与项目管理（上线后）')

    await headerStat.hover()
    const tip = page.getByTestId('token-detail-tip')
    await expect(tip).toBeVisible()
    await expect(tip.locator('[data-tip-row="workflow"]')).toContainText('工作流')
    await expect(tip.locator('[data-tip-row="pm"]')).toContainText('项目管理')
    await expect(tip.getByTestId('token-detail-tip-breakdown')).toContainText('合计')

    const detailShot = path.join(testInfo.outputDir, 'project-detail-token-stats.png')
    await page.screenshot({ path: detailShot, fullPage: true })
    await testInfo.attach('project-detail-token-stats', { path: detailShot, contentType: 'image/png' })
  })

  test('近 30 天左端 0 值点 hover：文案完整、盒子不裁切、离开隐藏、窄屏仍可见 (g4.5)', async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await mockBoardShell(page)
    await routeApi(page, '**/api/projects/*/token-stats**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(STATS_30D),
      })
    })

    await page.goto('/board.html?start=project-board&memory=1&projectId=proj-1')
    await expect(page.getByTestId('token-stats-charts')).toBeVisible({ timeout: 15_000 })
    const canvas = page.getByTestId('token-trend-chart').locator('canvas')
    await expect(canvas).toBeVisible()

    async function hoverLeftZeroPoint() {
      const box = await canvas.boundingBox()
      expect(box).toBeTruthy()
      const ys = [box!.height - 28, Math.floor(box!.height * 0.55)]
      const xs = [40, 48, 56, 64, 72, 84, 96]
      for (const y of ys) {
        for (const x of xs) {
          if (x >= box!.width - 4 || y >= box!.height - 4) continue
          await canvas.hover({ position: { x, y } })
          const loc = page.locator('.token-trend-tooltip')
          if (await loc.first().isVisible().catch(() => false)) {
            const text = await loc.first().innerText()
            if (text.includes('07-11')) return
          }
        }
      }
      throw new Error('left-edge 07-11 tooltip not shown')
    }

    await expect(async () => {
      await hoverLeftZeroPoint()
    }).toPass({ timeout: 12_000 })

    const tip = page.locator('.token-trend-tooltip').first()
    await expect(tip).toContainText('07-11')
    await expect(tip.locator('[data-tip-row="workflow"]')).toContainText('工作流')
    await expect(tip.locator('[data-tip-row="pm"]')).toContainText('项目管理')
    const tipText = (await tip.innerText()).replace(/\s+/g, ' ')
    expect(tipText).toMatch(/07-11\s*·\s*0/)
    expect(tipText).not.toMatch(/(^|\s)-11\s*·/)

    const tipBox = await tip.boundingBox()
    expect(tipBox).toBeTruthy()
    expect(tipBox!.x).toBeGreaterThanOrEqual(0)
    expect(tipBox!.y).toBeGreaterThanOrEqual(0)
    expect(tipBox!.x + tipBox!.width).toBeLessThanOrEqual(1280 + 1)
    expect(tipBox!.y + tipBox!.height).toBeLessThanOrEqual(900 + 1)

    const hoverShot = path.join(testInfo.outputDir, 'token-trend-tooltip-left-zero.png')
    await page.screenshot({ path: hoverShot, fullPage: true })
    await testInfo.attach('token-trend-tooltip-left-zero', { path: hoverShot, contentType: 'image/png' })

    await page.mouse.move(4, 4)
    await expect(page.locator('.token-trend-tooltip')).toHaveCount(0)

    await page.setViewportSize({ width: 390, height: 844 })
    await expect(page.getByTestId('token-trend-wrap')).toBeVisible()
    const panelBox = await page.getByTestId('token-stats-panel').boundingBox()
    const wrapBox = await page.getByTestId('token-trend-wrap').boundingBox()
    expect(panelBox && wrapBox).toBeTruthy()
    expect(wrapBox!.x).toBeGreaterThanOrEqual(panelBox!.x - 1)
    expect(wrapBox!.x + wrapBox!.width).toBeLessThanOrEqual(panelBox!.x + panelBox!.width + 2)

    await expect(async () => {
      await hoverLeftZeroPoint()
    }).toPass({ timeout: 12_000 })
    const narrowTip = page.locator('.token-trend-tooltip').first()
    await expect(narrowTip).toContainText('07-11')
    const narrowTipBox = await narrowTip.boundingBox()
    expect(narrowTipBox).toBeTruthy()
    expect(narrowTipBox!.x).toBeGreaterThanOrEqual(0)
    expect(narrowTipBox!.y).toBeGreaterThanOrEqual(0)
    expect(narrowTipBox!.x + narrowTipBox!.width).toBeLessThanOrEqual(390 + 1)
    expect(narrowTipBox!.y + narrowTipBox!.height).toBeLessThanOrEqual(844 + 1)

    const narrowHoverShot = path.join(testInfo.outputDir, 'token-trend-tooltip-narrow.png')
    await page.screenshot({ path: narrowHoverShot, fullPage: true })
    await testInfo.attach('token-trend-tooltip-narrow', { path: narrowHoverShot, contentType: 'image/png' })
  })
})
