import { expect, test, type Page } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { CLARIFY_CAPS, RESEARCH_CAPS } from '../src/test/capsFixtures'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const shotDir = path.join(__dirname, '..', 'test-results', 'execution-stats-token-shots')

const nodes = [
  { id: 'start', type: 'input', label: '开始', position: { x: 0, y: 0 }, config: {} },
  { id: 'research', type: 'agent', caps: RESEARCH_CAPS, label: '代码调研', position: { x: 180, y: 0 }, config: {} },
  { id: 'react', type: 'agent', caps: CLARIFY_CAPS, label: '需求澄清', position: { x: 360, y: 0 }, config: {} },
  { id: 'gate', type: 'human_gate', label: '人工门禁', position: { x: 540, y: 0 }, config: {} },
]

function buildRun(opts: {
  withUsage: boolean
  zeroUsage?: boolean
  durationSec?: number
  id?: string
  /** Demo-aligned single process usage + durations (wall/nodeSum/gap). */
  demoAligned?: boolean
}) {
  const id = opts.id ?? 'run-stats-token-e2e'
  if (opts.demoAligned) {
    return {
      id,
      workflowId: 'wf-stats-token',
      workflowName: 'execution-stats-token-usage',
      workflowVersion: 1,
      status: 'completed',
      trigger: 'manual',
      startedAt: '2026-07-24T00:00:00Z',
      durationSec: opts.durationSec ?? 3703,
      progress: 1,
      nodeRuns: {},
      nodeExecutions: {
        research: [
          {
            nodeId: 'research',
            iteration: 1,
            status: 'completed',
            startedAt: '2026-07-24T00:00:00Z',
            durationSec: 3458,
            outputs: {},
            usage: {
              inputTokens: 1_245_800,
              outputTokens: 892_455,
              cacheReadTokens: 7_201_000,
              cacheWriteTokens: 306_000,
            },
          },
        ],
        react: [
          {
            nodeId: 'react',
            iteration: 1,
            status: 'completed',
            startedAt: '2026-07-24T00:57:38Z',
            durationSec: 0,
            outputs: {},
          },
        ],
      },
      artifacts: [],
      trace: [],
      vars: [],
      nodes,
      edges: [
        { id: 'e1', source: 'start', target: 'research' },
        { id: 'e2', source: 'research', target: 'react' },
      ],
    }
  }

  const researchUsage = opts.withUsage
    ? opts.zeroUsage
      ? { inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheWriteTokens: 0 }
      : { inputTokens: 4200, outputTokens: 1800, cacheReadTokens: 960, cacheWriteTokens: 120 }
    : undefined
  const reactUsage = opts.withUsage
    ? opts.zeroUsage
      ? undefined
      : { inputTokens: 3100, outputTokens: 920, cacheReadTokens: 400, cacheWriteTokens: 0 }
    : undefined

  const nodeExecutions: Record<string, unknown[]> = {
    start: [
      {
        nodeId: 'start',
        iteration: 1,
        status: 'completed',
        startedAt: '2026-07-24T00:00:00Z',
        durationSec: 0,
        outputs: {},
      },
    ],
    research: [
      {
        nodeId: 'research',
        iteration: 1,
        status: 'completed',
        startedAt: '2026-07-24T00:00:01Z',
        durationSec: 72,
        outputs: { summary: 'ok' },
        ...(researchUsage ? { usage: researchUsage } : {}),
      },
    ],
    react: [
      {
        nodeId: 'react',
        iteration: 1,
        status: 'completed',
        startedAt: '2026-07-24T00:01:13Z',
        durationSec: 48,
        outputs: {},
        ...(reactUsage ? { usage: reactUsage } : {}),
      },
      {
        nodeId: 'react',
        iteration: 2,
        status: 'completed',
        startedAt: '2026-07-24T00:02:01Z',
        durationSec: 36,
        outputs: {},
        ...(opts.withUsage && !opts.zeroUsage
          ? {
              usage: {
                inputTokens: 2400,
                outputTokens: 780,
                cacheReadTokens: 220,
                cacheWriteTokens: 40,
              },
            }
          : {}),
      },
    ],
    gate: [
      {
        nodeId: 'gate',
        iteration: 1,
        status: 'completed',
        startedAt: '2026-07-24T00:02:40Z',
        durationSec: 30,
        outputs: {},
      },
    ],
  }

  return {
    id,
    workflowId: 'wf-stats-token',
    workflowName: 'execution-stats-token-usage',
    workflowVersion: 1,
    status: 'completed',
    trigger: 'manual',
    startedAt: '2026-07-24T00:00:00Z',
    durationSec: opts.durationSec ?? 186,
    progress: 1,
    nodeRuns: Object.fromEntries(
      Object.entries(nodeExecutions).map(([nid, list]) => [nid, list[list.length - 1]]),
    ),
    nodeExecutions,
    artifacts: [],
    trace: [],
    vars: [],
    nodes,
    edges: [
      { id: 'e1', source: 'start', target: 'research' },
      { id: 'e2', source: 'research', target: 'react' },
      { id: 'e3', source: 'react', target: 'gate' },
    ],
  }
}

async function mockRuns(page: Page, primary: ReturnType<typeof buildRun>, secondary?: ReturnType<typeof buildRun>) {
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

    // listRuns
    if (pathName === '/api/runs' || pathName === '/api/runs/') {
      const items = [primary, ...(secondary ? [secondary] : [])].map((r) => ({
        id: r.id,
        durationSec: r.durationSec,
        startedAt: r.startedAt,
        createdAt: r.startedAt,
        status: r.status,
        workflowId: r.workflowId,
        workflowName: r.workflowName,
      }))
      await route.fulfill({ json: { items, total: items.length } })
      return
    }

    if (pathName === `/api/runs/${primary.id}` || pathName === '/api/runs/run-responsive-e2e') {
      await route.fulfill({ json: { ...primary, id: pathName.endsWith('run-responsive-e2e') ? 'run-responsive-e2e' : primary.id } })
      return
    }
    if (secondary && pathName === `/api/runs/${secondary.id}`) {
      await route.fulfill({ json: secondary })
      return
    }
    await route.fulfill({ status: 404, json: { error: 'not mocked' } })
  })
}

async function openStats(page: Page, run: ReturnType<typeof buildRun>, secondary?: ReturnType<typeof buildRun>) {
  await page.setViewportSize({ width: 1280, height: 900 })
  await mockRuns(page, run, secondary)
  await page.goto('/run-detail-real.html')
  await expect(page.getByTestId('run-detail-root')).toBeVisible()
  await page.getByTestId('view-mode-stats').click()
  await expect(page.getByTestId('execution-stats-panel')).toBeVisible()
}

test('单次有用量：紧凑主值 / tip 细节 / 口径说明 / 排行辅列；无 NEW；同源时间线', async ({ page }) => {
  const run = buildRun({ withUsage: true })
  await openStats(page, run)

  // 7080 + 4420 + 3440 = 14940 → compact 14.9K token；wall 186 → 80.3/s
  const total = page.getByTestId('stats-kpi-total-tokens')
  await expect(page.getByTestId('stats-kpi-total-tokens-value')).toHaveText(/14\.9K token/)
  await expect(total).toContainText('有用量环节合计')
  const rate = page.getByTestId('stats-kpi-token-rate')
  await expect(page.getByTestId('stats-kpi-token-rate-value')).toHaveText('80.3/s')
  await expect(rate).toContainText('÷ 总耗时')

  // dual hue: token vs time colors
  const tokenColor = await total.locator('.stats-kpi-token').evaluate((el) => getComputedStyle(el).color)
  const timeColor = await page
    .getByTestId('stats-kpi-wall')
    .locator('.stats-kpi-time')
    .evaluate((el) => getComputedStyle(el).color)
  expect(tokenColor).not.toBe(timeColor)
  expect(tokenColor).toMatch(/rgb\(11,\s*110,\s*153\)/)
  expect(timeColor).toMatch(/rgb\(180,\s*83,\s*9\)/)

  // hover total tokens → full count + four parts (incl. cache read); tip closes on leave
  await page.getByTestId('stats-kpi-total-tokens-value').hover()
  const tip = page.getByTestId('stats-kpi-total-tokens-tip')
  await expect(tip).toBeVisible()
  await expect(tip).toContainText(/14,?940 token/)
  await expect(page.getByTestId('stats-kpi-token-part-input')).toBeVisible()
  await expect(page.getByTestId('stats-kpi-token-part-output')).toBeVisible()
  await expect(page.getByTestId('stats-kpi-token-part-cacheRead')).toContainText(/缓存读/)
  await expect(page.getByTestId('stats-kpi-token-part-cacheWrite')).toBeVisible()
  await page.screenshot({
    path: path.join(shotDir, 'stats-token-tip-parts.png'),
  })
  await page.getByTestId('stats-kpi-wall-value').hover()
  await expect(page.getByTestId('stats-kpi-total-tokens-tip')).toHaveCount(0)

  const rankTokens = page.getByTestId('stats-rank-tokens')
  expect(await rankTokens.count()).toBeGreaterThan(0)
  // gate has no usage → —
  await expect(page.getByText('人工门禁').first()).toBeVisible()
  const panelText = await page.getByTestId('execution-stats-panel').innerText()
  expect(panelText).not.toContain('NEW')
  // no four-part breakdown in stats ranking (only tip); tip closed above
  expect(panelText).not.toMatch(/缓存读|缓存写/)

  // pie/bottleneck still present (duration axis)
  await expect(page.getByTestId('stats-pie-query')).toBeVisible()

  await page.screenshot({
    path: path.join(shotDir, 'stats-single-with-usage.png'),
    fullPage: true,
  })

  // homologous with timeline footer (timeline keeps full count, not compact)
  await page.getByTestId('view-mode-timeline').click()
  await expect(page.getByTestId('timeline-footer')).toBeVisible()
  await expect(page.getByTestId('timeline-total-tokens')).toContainText(/14,?940/)
  await expect(page.getByTestId('timeline-token-rate')).toContainText('80.3')

  await page.screenshot({
    path: path.join(shotDir, 'timeline-homologous.png'),
    fullPage: true,
  })
})

test('单次全未上报：总 token / token/s / 辅列均为 — 非 0', async ({ page }) => {
  const run = buildRun({ withUsage: false })
  await openStats(page, run)

  const total = page.getByTestId('stats-kpi-total-tokens')
  await expect(total).toContainText('—')
  await expect(total).toContainText('全部未上报')
  const totalText = await total.innerText()
  expect(totalText).not.toMatch(/\b0\b/)
  const rate = page.getByTestId('stats-kpi-token-rate')
  await expect(rate).toContainText('—')
  await expect(rate).toContainText('无总量则无速率')

  const rankTokens = page.getByTestId('stats-rank-tokens')
  const n = await rankTokens.count()
  expect(n).toBeGreaterThan(0)
  for (let i = 0; i < n; i++) {
    await expect(rankTokens.nth(i)).toContainText('—')
  }

  await page.screenshot({
    path: path.join(shotDir, 'stats-single-no-usage.png'),
    fullPage: true,
  })
})

test('已上报全 0：总 token 显示 0（非 —）', async ({ page }) => {
  const run = buildRun({ withUsage: true, zeroUsage: true })
  await openStats(page, run)
  const total = page.getByTestId('stats-kpi-total-tokens')
  await expect(total).toContainText('0')
  await expect(total).not.toContainText('—')
})

test('多次：总/平均 token 忽略无用量 Run；先展 picker 再勾选', async ({ page }) => {
  const withUsage = buildRun({ withUsage: true, id: 'run-stats-token-e2e' })
  const without = buildRun({
    withUsage: false,
    id: 'run-stats-token-e2e-2',
    durationSec: 90,
  })
  // harness bootstraps run-responsive-e2e; remap primary id via mock
  const harnessPrimary = { ...withUsage, id: 'run-responsive-e2e' }
  await openStats(page, harnessPrimary, without)

  // switch to multi via parent tabs when present, else panel tabs
  const multiTab = page.getByRole('button', { name: /多次执行聚合|Multi-run/ })
  await multiTab.click()
  await expect(page.getByTestId('stats-kpi-sum-tokens')).toBeVisible({ timeout: 10_000 })

  // g1.3 / g5.2: picker collapsed by default; open then ensure secondary is selected
  await expect(page.getByTestId('stats-multi-picker')).toHaveCount(0)
  await page.getByTestId('stats-multi-picker-toggle').click()
  await expect(page.getByTestId('stats-multi-picker')).toBeVisible()

  const row2 = page.locator('[data-testid="stats-multi-picker-row"][data-run-id="run-stats-token-e2e-2"]')
  await expect(row2).toBeVisible()
  if ((await row2.getAttribute('aria-pressed')) !== 'true') {
    await row2.click()
  }

  await expect(page.getByTestId('stats-kpi-sum-tokens-value')).toHaveText(/14\.9K token/)
  await expect(page.getByTestId('stats-kpi-sum-tokens')).toContainText('只统计有上报用量的执行')
  await expect(page.getByTestId('stats-kpi-avg-tokens-value')).toHaveText(/14\.9K token/)
  await expect(page.getByTestId('stats-kpi-avg-tokens')).toContainText(/按 1 次有用量的执行取平均/)

  const multiRate = page.getByTestId('stats-kpi-multi-token-rate')
  await expect(multiRate).toContainText('总 token ÷ 所选执行总耗时')
  await expect(page.getByTestId('stats-kpi-process-count')).toContainText('过程执行次数')

  const panelText = await page.getByTestId('execution-stats-panel').innerText()
  expect(panelText).not.toMatch(/\busage\b/i)
  expect(panelText).not.toContain('分母=')
  expect(panelText).not.toContain('Σ')

  await expect(page.getByTestId('stats-selected-runs-title')).toBeVisible()
  await expect(page.getByTestId('stats-multi-compare-bar')).toBeVisible()

  await page.screenshot({
    path: path.join(shotDir, 'stats-multi-mixed-usage.png'),
    fullPage: true,
  })
})

test('维度切换：节点/类型排行均有 Tokens 辅列', async ({ page }) => {
  const run = buildRun({ withUsage: true })
  await openStats(page, run)

  for (const label of [/按节点合并|By node/, /按类型|By type/]) {
    await page.getByRole('button', { name: label }).click()
    const rankTokens = page.getByTestId('stats-rank-tokens')
    expect(await rankTokens.count()).toBeGreaterThan(0)
  }

  await page.screenshot({
    path: path.join(shotDir, 'stats-rank-by-type.png'),
    fullPage: true,
  })
})

test('Demo 对齐：紧凑主值 + 时长/四分量/算式 tip + focus', async ({ page }) => {
  const run = buildRun({ withUsage: true, demoAligned: true })
  await openStats(page, run)

  await expect(page.getByTestId('stats-kpi-wall-value')).toHaveText('1.03h')
  await expect(page.getByTestId('stats-kpi-node-sum-value')).toHaveText('57.6m')
  await expect(page.getByTestId('stats-kpi-gap-value')).toHaveText('4.1m')
  await expect(page.getByTestId('stats-kpi-total-tokens-value')).toHaveText(/9\.65M token/)
  await expect(page.getByTestId('stats-kpi-token-rate-value')).toHaveText('2.60K/s')

  // bottoms:口径副文案 unchanged
  await expect(page.getByTestId('stats-kpi-wall')).toContainText('占比默认分母')
  await expect(page.getByTestId('stats-kpi-node-sum')).toContainText('各过程耗时之和')
  await expect(page.getByTestId('stats-kpi-gap')).toContainText('空闲')
  await expect(page.getByTestId('stats-kpi-gap')).toContainText('过程未占用的墙钟')
  await expect(page.getByTestId('stats-kpi-gap')).not.toContainText('差额')
  await expect(page.getByTestId('stats-kpi-total-tokens')).toContainText('有用量环节合计')
  await expect(page.getByTestId('stats-kpi-token-rate')).toContainText('÷ 总耗时')

  await page.screenshot({
    path: path.join(shotDir, 'demo-compact-mains.png'),
  })

  await page.getByTestId('stats-kpi-wall-value').hover()
  await expect(page.getByTestId('stats-kpi-wall-tip')).toContainText('01:01:43')
  await expect(page.getByTestId('stats-kpi-wall-tip')).toContainText('3703 秒')
  await page.screenshot({ path: path.join(shotDir, 'demo-wall-tip.png') })

  await page.getByTestId('stats-kpi-total-tokens-value').hover()
  await expect(page.getByTestId('stats-kpi-total-tokens-tip')).toContainText(/9,?645,?255 token/)
  await expect(page.getByTestId('stats-kpi-token-part-cacheRead')).toContainText('7,201,000')
  await expect(page.getByTestId('stats-kpi-token-part-cacheRead')).toContainText(/7\.2M/)
  await page.screenshot({ path: path.join(shotDir, 'demo-token-parts-tip.png') })

  await page.getByTestId('stats-kpi-token-rate-value').hover()
  await expect(page.getByTestId('stats-kpi-token-rate-tip')).toContainText(
    /9,?645,?255 ÷ 3703 秒 ≈ 2604\.7/,
  )
  await page.screenshot({ path: path.join(shotDir, 'demo-rate-tip.png') })

  // keyboard focus opens tip; blur closes
  await page.getByTestId('stats-kpi-gap-value').focus()
  await expect(page.getByTestId('stats-kpi-gap-tip')).toContainText('04:05')
  await expect(page.getByTestId('stats-kpi-gap-tip')).toContainText('245 秒')
  await page.getByTestId('stats-kpi-gap-value').blur()
  await expect(page.getByTestId('stats-kpi-gap-tip')).toHaveCount(0)

  // no product expand button for tips
  await expect(page.getByRole('button', { name: /展开/ })).toHaveCount(0)
})
