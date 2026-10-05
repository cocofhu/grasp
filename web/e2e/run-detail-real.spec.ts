import { expect, test, type Locator, type Page } from '@playwright/test'
import { IMPLEMENT_CAPS } from '../src/test/capsFixtures'

const nodes = [
  { id: 'input', type: 'input', label: '读取部署配置与环境变量', position: { x: 0, y: 0 }, config: {} },
  {
    id: 'build',
    type: 'agent',
    label: '构建并推送包含超长仓库名称与提交标识的容器镜像用于验证截断提示',
    position: { x: 180, y: 0 },
    config: { agent_profile: 'release-engineer' },
  },
  {
    id: 'deploy',
    type: 'agent', caps: IMPLEMENT_CAPS,
    label: '部署到 production-ap-southeast-1 集群并等待全部工作负载完成滚动更新',
    position: { x: 360, y: 0 },
    config: { agent_profile: 'deployment-agent' },
  },
  { id: 'notify', type: 'output', label: '健康检查与发布结果通知', position: { x: 540, y: 0 }, config: {} },
]

const nodeRuns = {
  input: {
    nodeId: 'input',
    status: 'completed',
    startedAt: '2026-07-17T00:00:00Z',
    durationSec: 2,
    varsSnapshot: { environment: 'production' },
    outputs: {},
  },
  build: {
    nodeId: 'build',
    status: 'completed',
    startedAt: '2026-07-17T00:00:02Z',
    durationSec: 198,
    varsSnapshot: {
      image:
        'registry.internal.example/platform/web-console-with-a-very-long-repository-name:sha-52e0d8b9ab47d998',
    },
    outputs: {},
  },
  deploy: {
    nodeId: 'deploy',
    status: 'completed',
    startedAt: '2026-07-17T00:03:20Z',
    durationSec: 291,
    varsSnapshot: {
      command:
        'kubectl rollout status deployment/web-console --namespace platform-production --timeout=300s',
    },
    outputs: {},
  },
  notify: {
    nodeId: 'notify',
    status: 'completed',
    startedAt: '2026-07-17T00:08:11Z',
    durationSec: 31,
    varsSnapshot: { endpoint: 'https://console.example.internal/healthz?full-diagnostics=true' },
    outputs: {},
  },
}

const mockRun = {
  id: 'run-responsive-e2e',
  workflowId: 'wf-responsive-e2e',
  workflowName: 'production-deploy-with-an-intentionally-long-workflow-name',
  workflowVersion: 18,
  status: 'completed',
  trigger: 'manual',
  startedAt: '2026-07-17T00:00:00Z',
  durationSec: 522,
  progress: 1,
  branch: 'feature/a-very-long-release-branch-name-that-must-not-expand-the-page',
  git: {
    pushed: true,
    branch: 'feature/a-very-long-release-branch-name-that-must-not-expand-the-page',
    pushedSha: '52e0d8b9ab47d998baecfd91b2049f31a1bf2d70',
    mrUrl: 'https://git.example.internal/platform/web-console/-/merge_requests/1842',
  },
  nodeRuns,
  nodeExecutions: Object.fromEntries(Object.entries(nodeRuns).map(([id, run]) => [id, [run]])),
  artifacts: [],
  trace: [],
  vars: [],
  nodes,
  edges: [
    { id: 'e1', source: 'input', target: 'build' },
    { id: 'e2', source: 'build', target: 'deploy' },
    { id: 'e3', source: 'deploy', target: 'notify' },
  ],
}

async function mockRunApi(page: Page) {
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
    if (url.pathname === '/api/runs/run-responsive-e2e') {
      await route.fulfill({ json: mockRun })
      return
    }
    await route.fulfill({ status: 404, json: { error: 'not mocked' } })
  })
}

async function gotoRealRun(page: Page, width: number, height = 900) {
  await page.setViewportSize({ width, height })
  await mockRunApi(page)
  await page.goto('/run-detail-real.html')
  await expect(page.getByTestId('run-detail-root')).toBeVisible()
  await expect(page.getByTestId('status-pill')).toContainText('已完成')
  await page.getByRole('button', { name: '执行统计' }).click()
  await expect(page.getByTestId('execution-stats-panel')).toBeVisible()
}

/** Production RunDetailView path — does not force stats (for main-view timeline contract). */
async function gotoRealRunMain(page: Page, width: number, height = 900) {
  await page.setViewportSize({ width, height })
  await mockRunApi(page)
  await page.goto('/run-detail-real.html')
  await expect(page.getByTestId('run-detail-root')).toBeVisible()
  await expect(page.getByTestId('status-pill')).toContainText('已完成')
}

/** True if child lies inside parent's scrollable content (not the visible clip). */
async function expectContained(child: Locator, parent: Locator) {
  const report = await child.evaluate((el, parentEl) => {
    if (!(parentEl instanceof HTMLElement) || !parentEl.contains(el)) {
      return { ok: false, reason: 'not-descendant' }
    }
    const cr = el.getBoundingClientRect()
    const pr = parentEl.getBoundingClientRect()
    const left = cr.left - pr.left + parentEl.scrollLeft
    const top = cr.top - pr.top + parentEl.scrollTop
    const pad = 1
    const ok =
      left >= -pad &&
      top >= -pad &&
      left + cr.width <= parentEl.scrollWidth + pad &&
      top + cr.height <= parentEl.scrollHeight + pad
    return {
      ok,
      left,
      top,
      cw: cr.width,
      ch: cr.height,
      sw: parentEl.scrollWidth,
      sh: parentEl.scrollHeight,
    }
  }, await parent.elementHandle())
  expect(report.ok, JSON.stringify(report)).toBe(true)
}

async function expectNoHorizontalOverflow(page: Page) {
  const documentOverflow = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1,
  )
  expect(documentOverflow).toBe(false)

  for (const testId of [
    'run-detail-main',
    'run-detail-content',
    'run-stats-split',
    'run-stats-panel-wrap',
    'execution-stats-panel',
    'execution-stats-scroll',
  ]) {
    const size = await page.getByTestId(testId).evaluate((el) => ({
      clientWidth: el.clientWidth,
      scrollWidth: el.scrollWidth,
    }))
    expect(size.scrollWidth, `${testId} 内部超宽`).toBeLessThanOrEqual(size.clientWidth + 1)
  }
}

for (const width of [390, 430, 767, 768, 769, 1024]) {
  test(`${width}px：真实 RunDetailView 无裁切`, async ({ page }) => {
    await gotoRealRun(page, width)
    await expectNoHorizontalOverflow(page)

    const chart = page.getByTestId('stats-pie-query').first()
    await expectContained(page.getByTestId('stats-pie-svg').first(), chart)
    await expectContained(page.getByTestId('stats-pie-center').first(), chart)
    await expectContained(page.getByTestId('stats-pie-legend').first(), chart)
    await expectContained(page.getByTestId('stats-ranking-label').first(), page.getByTestId('execution-stats-scroll'))
  })
}

test('429/430/431px：饼图按容器宽度切换且内容完整', async ({ page }) => {
  await gotoRealRun(page, 1200)
  const query = page.getByTestId('stats-pie-query').first()
  const layout = page.getByTestId('stats-pie-layout').first()
  const chart = page.getByTestId('stats-pie-chart').first()
  const legend = page.getByTestId('stats-pie-legend').first()

  for (const width of [429, 430, 431]) {
    await query.evaluate((el, value) => {
      const node = el as HTMLElement
      node.style.width = `${value}px`
      node.style.maxWidth = `${value}px`
    }, width)
    await expect(query).toHaveCSS('width', `${width}px`)

    const [chartBox, legendBox] = await Promise.all([chart.boundingBox(), legend.boundingBox()])
    expect(chartBox && legendBox).toBeTruthy()
    if (width < 430) {
      expect(legendBox!.y).toBeGreaterThan(chartBox!.y + chartBox!.height - 1)
    } else {
      const chartCenter = chartBox!.y + chartBox!.height / 2
      const legendCenter = legendBox!.y + legendBox!.height / 2
      expect(Math.abs(legendCenter - chartCenter)).toBeLessThan(2)
    }
    await expectContained(chart, layout)
    await expectContained(legend, layout)
  }
})

/** Reset hover/focus so TruncatedTextTooltip scheduleHide can settle (avoids full-suite flake). */
async function dismissTruncatedTooltip(page: Page, tooltip: Locator) {
  await page.keyboard.press('Escape')
  await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur?.())
  const vp = page.viewportSize()
  if (vp) await page.mouse.move(Math.max(0, vp.width - 2), Math.max(0, vp.height - 2))
  await expect(tooltip).toBeHidden()
}

async function touchToggle(locator: Locator) {
  await locator.evaluate((el) => {
    el.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, pointerType: 'touch' }))
    el.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  })
}

test('截断提示支持 hover、focus、Escape、触摸切换和外部关闭', async ({ page }) => {
  await gotoRealRun(page, 390)
  const label = page.getByTestId('stats-ranking-label').nth(1)
  const tooltip = page.getByTestId('truncated-text-tooltip')
  await label.evaluate((el) => {
    const node = el as HTMLElement
    node.style.flex = '0 0 80px'
    node.style.width = '80px'
  })
  // Allow ResizeObserver to mark overflow before interactions.
  await expect
    .poll(async () => label.evaluate((el) => el.scrollWidth > el.clientWidth + 1))
    .toBe(true)

  await label.hover()
  await expect(tooltip).toBeVisible()
  await expect(tooltip).toContainText('构建并推送')
  await label.dispatchEvent('mouseleave')
  await dismissTruncatedTooltip(page, tooltip)

  await label.focus()
  await expect(tooltip).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(tooltip).toBeHidden()
  await label.blur()
  await dismissTruncatedTooltip(page, tooltip)

  const variableChip = page.getByTestId('timeline-variable-chip').filter({ hasText: 'image' })
  const variableValue = variableChip.getByTestId('timeline-variable-value')
  await variableValue.evaluate((el) => {
    const node = el as HTMLElement
    node.style.flex = '0 0 50px'
    node.style.width = '50px'
  })
  // measure-child: force remeasure after constraining width
  await variableValue.evaluate((el) => {
    el.dispatchEvent(new Event('resize'))
    ;(el as HTMLElement).offsetWidth
  })
  await variableChip.focus()
  await expect(tooltip).toBeVisible()
  await expect(tooltip).toContainText('image = registry.internal.example')
  await page.keyboard.press('Escape')
  await expect(tooltip).toBeHidden()
  await variableChip.blur()
  await dismissTruncatedTooltip(page, tooltip)

  await touchToggle(label)
  await expect(tooltip).toBeVisible()
  await touchToggle(label)
  await expect(tooltip).toBeHidden()

  await touchToggle(label)
  await expect(tooltip).toBeVisible()
  await page.locator('header').click({ position: { x: 2, y: 2 } })
  await expect(tooltip).toBeHidden()
})

async function waitForStablePanelScreenshot(page: Page) {
  const panel = page.getByTestId('execution-stats-panel')
  await page.evaluate(() => document.fonts.ready)
  let lastHeight = -1
  let stableTicks = 0
  while (stableTicks < 3) {
    const height = await panel.evaluate((el) => el.getBoundingClientRect().height)
    if (height > 0 && height === lastHeight) stableTicks += 1
    else stableTicks = 0
    lastHeight = height
    await page.waitForTimeout(100)
  }
}

test('390px：统计区域视觉回归', async ({ page }) => {
  await gotoRealRun(page, 390)
  await waitForStablePanelScreenshot(page)
  const panel = page.getByTestId('execution-stats-panel')
  // Panel height under the run header drifts ±1–2px across Chromium builds (1220 vs
  // 1218). Playwright rejects size mismatch before maxDiffPixelRatio applies, so pin
  // a stable box-sizing height before the locator screenshot.
  await panel.evaluate((el) => {
    const node = el as HTMLElement
    node.style.boxSizing = 'border-box'
    node.style.height = '1210px'
    node.style.maxHeight = '1210px'
    node.style.minHeight = '1210px'
    node.style.overflow = 'hidden'
    node.style.flex = 'none'
  })
  await expect(panel).toHaveScreenshot('run-detail-stats-390.png', {
    animations: 'disabled',
    maxDiffPixelRatio: 0.05,
  })
})

test.describe('主视图时间线移动端契约（生产路径）', () => {
  test('767px 为移动单栏；768/769 为桌面并排（不断言 768=手机时间线）', async ({ page }) => {
    await gotoRealRunMain(page, 767)
    await expect(page.getByTestId('mobile-main-panel-tabs')).toBeVisible()
    await expect(page.getByTestId('view-mode-canvas')).toHaveCount(0)

    await page.setViewportSize({ width: 768, height: 900 })
    await expect(page.getByTestId('mobile-main-panel-tabs')).toHaveCount(0)
    await expect(page.getByTestId('view-mode-canvas')).toBeVisible()
    await expect(page.getByTestId('run-timeline-pane')).toBeVisible()
    await expect(page.getByTestId('run-detail-right-panel')).toBeVisible()

    await page.setViewportSize({ width: 769, height: 900 })
    await expect(page.getByTestId('mobile-main-panel-tabs')).toHaveCount(0)
    await expect(page.getByTestId('view-mode-canvas')).toBeVisible()
  })

  test('390px：默认时间线可见、无画布入口、预选最近节点', async ({ page }) => {
    await gotoRealRunMain(page, 390)

    const timelineTab = page.getByTestId('view-mode-timeline')
    await expect(timelineTab).toBeVisible()
    await expect(timelineTab).toHaveClass(/text-accent/)
    await expect(page.getByTestId('view-mode-canvas')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '画布' })).toHaveCount(0)

    const timelinePane = page.getByTestId('run-timeline-pane')
    await expect(timelinePane).toBeVisible()
    const labels = timelinePane.getByTestId('timeline-node-label')
    await expect(labels.first()).toBeVisible()
    await expect(labels).toHaveCount(4)

    // All completed → preselect last timeline execution (notify)
    await expect(page.getByTestId('run-detail-right-panel')).toContainText('健康检查与发布结果通知')
  })

  test('390px：点选时间线条目联动详情；统计切换后再次进入仍默认时间线', async ({ page }) => {
    await gotoRealRunMain(page, 390)
    const timelinePane = page.getByTestId('run-timeline-pane')
    await expect(timelinePane).toBeVisible()

    // Prefer a non-agent node so the default「概览」tab shows the node title.
    await timelinePane.getByTestId('timeline-node-label').filter({ hasText: '读取部署配置' }).click()
    await expect(page.getByTestId('run-detail-right-panel')).toContainText('读取部署配置')

    await page.getByTestId('view-mode-stats').click()
    await expect(page.getByTestId('execution-stats-panel')).toBeVisible()
    // Stats·single timeline is a different path (not run-timeline-pane).
    await expect(page.getByTestId('run-timeline-pane')).toHaveCount(0)
    await expect(page.getByTestId('run-stats-split')).toBeVisible()

    // Remount = "再次进入"：不记忆执行统计，仍默认时间线
    await page.goto('/run-detail-real.html')
    await expect(page.getByTestId('run-detail-root')).toBeVisible()
    await expect(page.getByTestId('view-mode-timeline')).toHaveClass(/text-accent/)
    await expect(page.getByTestId('run-timeline-pane')).toBeVisible()
    await expect(page.getByTestId('execution-stats-panel')).toHaveCount(0)
  })

  test('桌面选画布后缩到 390px：静默回退时间线', async ({ page }) => {
    await gotoRealRunMain(page, 1024, 768)
    await expect(page.getByTestId('view-mode-canvas')).toBeVisible()
    await page.getByTestId('view-mode-canvas').click()
    await expect(page.getByTestId('view-mode-canvas')).toHaveClass(/text-accent/)
    await expect(page.getByTestId('run-timeline-pane')).toHaveCount(0)

    await page.setViewportSize({ width: 390, height: 844 })
    await expect(page.getByTestId('view-mode-canvas')).toHaveCount(0)
    await expect(page.getByTestId('view-mode-timeline')).toHaveClass(/text-accent/)
    await expect(page.getByTestId('run-timeline-pane')).toBeVisible()
    await expect(page.getByTestId('run-timeline-pane').getByTestId('timeline-node-label').first()).toBeVisible()
  })

  test('1024px：三入口可用；窄屏执行统计·单次堆叠不受损', async ({ page }) => {
    await gotoRealRunMain(page, 1024, 768)
    await expect(page.getByTestId('view-mode-canvas')).toBeVisible()
    await expect(page.getByTestId('view-mode-timeline')).toBeVisible()
    await expect(page.getByTestId('view-mode-stats')).toBeVisible()

    await page.getByTestId('view-mode-timeline').click()
    await expect(page.getByTestId('run-timeline-pane')).toBeVisible()
    await page.getByTestId('view-mode-canvas').click()
    await expect(page.getByTestId('run-timeline-pane')).toHaveCount(0)

    await page.setViewportSize({ width: 390, height: 844 })
    await page.getByTestId('view-mode-stats').click()
    await expect(page.getByTestId('run-stats-split')).toBeVisible()
    await expect(page.getByTestId('execution-stats-panel')).toBeVisible()
    // Stats single keeps its own timeline region (not the main-path pane).
    const split = page.getByTestId('run-stats-split')
    await expect(split).toBeVisible()
    const splitBox = await split.boundingBox()
    expect(splitBox).toBeTruthy()
    expect(splitBox!.height).toBeGreaterThan(240)
  })
})
