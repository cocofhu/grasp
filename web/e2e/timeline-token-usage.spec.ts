import { expect, test, type Page } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { CLARIFY_CAPS, RESEARCH_CAPS } from '../src/test/capsFixtures'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const shotDir = path.join(__dirname, '..', 'test-results', 'timeline-token-shots')

const nodes = [
  { id: 'start', type: 'input', label: '开始', position: { x: 0, y: 0 }, config: {} },
  { id: 'research', type: 'agent', caps: RESEARCH_CAPS, label: '代码调研', position: { x: 180, y: 0 }, config: {} },
  { id: 'react', type: 'agent', caps: CLARIFY_CAPS, label: '需求澄清', position: { x: 360, y: 0 }, config: {} },
]

function buildRun(opts: {
  withUsage: boolean
  zeroUsage?: boolean
  durationSec?: number
}) {
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
  }

  return {
    id: 'run-timeline-token-e2e',
    workflowId: 'wf-timeline-token',
    workflowName: 'timeline-token-usage',
    workflowVersion: 1,
    status: 'completed',
    trigger: 'manual',
    startedAt: '2026-07-24T00:00:00Z',
    durationSec: opts.durationSec ?? 186,
    progress: 1,
    nodeRuns: Object.fromEntries(
      Object.entries(nodeExecutions).map(([id, list]) => [id, list[list.length - 1]]),
    ),
    nodeExecutions,
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

async function mockRun(page: Page, run: ReturnType<typeof buildRun>) {
  await page.route('**/api/runs/**', async (route) => {
    const url = new URL(route.request().url())
    if (url.pathname.endsWith('/events')) {
      await route.fulfill({ json: { events: [], nextCursor: '', hasMore: false, live: false } })
      return
    }
    if (url.pathname.endsWith('/sandbox-log')) {
      await route.fulfill({ json: { content: '', live: false, found: false } })
      return
    }
    if (url.pathname === '/api/runs/run-timeline-token-e2e') {
      await route.fulfill({ json: run })
      return
    }
    // reuse harness id from run-detail-real.html bootstrap
    if (url.pathname === '/api/runs/run-responsive-e2e') {
      await route.fulfill({ json: { ...run, id: 'run-responsive-e2e' } })
      return
    }
    await route.fulfill({ status: 404, json: { error: 'not mocked' } })
  })
}

async function openTimeline(page: Page, run: ReturnType<typeof buildRun>) {
  await page.setViewportSize({ width: 1280, height: 900 })
  await mockRun(page, run)
  await page.goto('/run-detail-real.html')
  await expect(page.getByTestId('run-detail-root')).toBeVisible()
  await page.getByTestId('view-mode-timeline').click()
  await expect(page.getByTestId('timeline-footer')).toBeVisible()
}

test('有用量：环节合计+四分量，底部总 token / token/s / 墙钟', async ({ page }) => {
  const run = buildRun({ withUsage: true })
  await openTimeline(page, run)

  // start: no usage → tokens —
  const tokenLabels = page.getByTestId('timeline-tokens')
  await expect(tokenLabels.first()).toContainText('—')

  // research: 4200+1800+960+120 = 7080
  await expect(page.getByText('7080').or(page.getByText('7,080')).first()).toBeVisible()
  const parts = page.getByTestId('timeline-token-parts')
  await expect(parts.first()).toContainText('输入')
  await expect(parts.first()).toContainText('输出')
  await expect(parts.first()).toContainText('缓存读')
  await expect(parts.first()).toContainText('缓存写')

  // footer: 7080 + (3100+920+400+0=4420) + (2400+780+220+40=3440) = 14940
  // wall 186 → 14940/186 ≈ 80.3
  const total = page.getByTestId('timeline-total-tokens')
  await expect(total).toContainText(/14,?940/)
  await expect(page.getByTestId('timeline-token-rate')).toContainText('80.3')
  await expect(page.getByTestId('timeline-wall-clock')).toContainText(/:/)

  // no footer parts
  await expect(page.getByTestId('timeline-footer').getByTestId('timeline-token-parts')).toHaveCount(0)

  await page.screenshot({
    path: path.join(shotDir, 'timeline-rich-usage.png'),
    fullPage: true,
  })
})

test('无用量：各行与底部总 token/token/s 为 —，墙钟仍显示', async ({ page }) => {
  const run = buildRun({ withUsage: false })
  await openTimeline(page, run)

  const tokens = page.getByTestId('timeline-tokens')
  const count = await tokens.count()
  expect(count).toBeGreaterThan(0)
  for (let i = 0; i < count; i++) {
    await expect(tokens.nth(i)).toContainText('—')
  }
  await expect(page.getByTestId('timeline-token-parts')).toHaveCount(0)
  await expect(page.getByTestId('timeline-total-tokens')).toContainText('—')
  await expect(page.getByTestId('timeline-token-rate')).toContainText('—')
  await expect(page.getByTestId('timeline-wall-clock')).not.toContainText('—')

  await page.screenshot({
    path: path.join(shotDir, 'timeline-empty-usage.png'),
    fullPage: true,
  })
})

test('已上报全 0：tokens 0 与总 token 0（非 —）', async ({ page }) => {
  const run = buildRun({ withUsage: true, zeroUsage: true })
  await openTimeline(page, run)

  await expect(page.getByTestId('timeline-tokens').nth(1)).toContainText(/\b0\b/)
  await expect(page.getByTestId('timeline-token-parts').first()).toBeVisible()
  const total = page.getByTestId('timeline-total-tokens')
  await expect(total).toContainText('0')
  await expect(total).not.toContainText('—')

  await page.screenshot({
    path: path.join(shotDir, 'timeline-zero-usage.png'),
    fullPage: true,
  })
})

test('墙钟为 0 时 token/s 为 —', async ({ page }) => {
  const run = buildRun({ withUsage: true, durationSec: 0 })
  await openTimeline(page, run)
  await expect(page.getByTestId('timeline-total-tokens')).toContainText(/14,?940|7,?080/)
  await expect(page.getByTestId('timeline-token-rate')).toContainText('—')
})
