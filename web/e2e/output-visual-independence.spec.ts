/**
 * Browser acceptance for multi-visual independence + unexecuted hide + distinguishable fail copy.
 * Uses the existing run-completed-output deep-link harness (mocked API).
 */
import { test, expect, type Page } from '@playwright/test'
import type { Artifact, OutputCard } from '../src/lib/shared/types'
import { PAGE_CAPS } from '../src/test/capsFixtures'

const nodes = [
  { id: 'start', type: 'input', label: '开始', position: { x: 0, y: 0 }, config: {} },
  { id: 'visual_a', type: 'agent', caps: PAGE_CAPS, label: '视觉网页', position: { x: 180, y: 0 }, config: {} },
  { id: 'visual_l6zc', type: 'agent', caps: PAGE_CAPS, label: '', position: { x: 360, y: 0 }, config: {} },
  { id: 'end', type: 'output', label: '结束', position: { x: 540, y: 0 }, config: {} },
]

const pageA = `<!doctype html><html><body><h1 id="mark-a">page-a-workflow-shell</h1></body></html>`
const pageB = `<!doctype html><html><body><h1 id="mark-b">page-b-artifact-density</h1></body></html>`

function visualCard(partial: Partial<OutputCard> & Pick<OutputCard, 'index' | 'title' | 'artifactName' | 'nodeId'>): OutputCard {
  return {
    template: `{{nodes.${partial.nodeId}.outputs.page}}`,
    typeTag: '自定义产物',
    status: 'ok',
    outputKey: 'page',
    ...partial,
  }
}

function mockRun(cards: OutputCard[], artifacts: Artifact[]) {
  return {
    id: 'run-completed-e2e',
    workflowId: 'wf-completed-e2e',
    workflowName: '自我迭代',
    workflowVersion: 1,
    status: 'completed',
    trigger: 'manual',
    startedAt: '2026-08-01T00:00:00Z',
    durationSec: 120,
    progress: 1,
    nodeRuns: {
      start: { nodeId: 'start', status: 'completed', startedAt: '2026-08-01T00:00:00Z', outputs: {} },
      visual_a: {
        nodeId: 'visual_a',
        status: 'completed',
        startedAt: '2026-08-01T00:01:00Z',
        outputs: { page: pageA },
      },
      end: {
        nodeId: 'end',
        status: 'completed',
        startedAt: '2026-08-01T00:02:00Z',
        outputs: { outputCards: cards },
      },
    },
    nodeExecutions: {},
    artifacts,
    trace: [],
    vars: [],
    nodes,
    edges: [
      { id: 'e1', source: 'start', target: 'visual_a' },
      { id: 'e2', source: 'visual_a', target: 'end' },
    ],
  }
}

async function mockApi(
  page: Page,
  cards: OutputCard[],
  artifacts: Artifact[],
  contents: Record<string, string>,
  fetchedIds?: string[],
) {
  const run = mockRun(cards, artifacts)
  await page.route('**/api/runs/**', async (route) => {
    const url = new URL(route.request().url())
    if (url.pathname.endsWith('/events')) {
      await route.fulfill({ json: { events: [], nextCursor: '', hasMore: false, live: false } })
      return
    }
    if (url.pathname.endsWith('/sandbox-log') || url.pathname.includes('/sandbox')) {
      await route.fulfill({ json: { content: '', live: false, found: false } })
      return
    }
    if (url.pathname.includes('/runs/run-completed-e2e')) {
      await route.fulfill({ json: run })
      return
    }
    await route.fulfill({ status: 404, json: { error: 'not mocked' } })
  })
  await page.route('**/api/workflows/**', async (route) => {
    await route.fulfill({
      json: { id: 'wf-completed-e2e', name: '自我迭代', status: 'published', version: 1, nodes, edges: [] },
    })
  })
  await page.route('**/api/artifacts/**', async (route) => {
    const url = new URL(route.request().url())
    const match = url.pathname.match(/\/artifacts\/([^/]+)\/content$/)
    if (match) fetchedIds?.push(match[1])
    if (match && contents[match[1]] != null) {
      await route.fulfill({ json: { id: match[1], content: contents[match[1]] } })
      return
    }
    await route.fulfill({ status: 404, json: { error: 'not mocked' } })
  })
}

async function openOutput(
  page: Page,
  cards: OutputCard[],
  artifacts: Artifact[] = [],
  contents: Record<string, string> = {},
  fetchedIds?: string[],
) {
  await page.setViewportSize({ width: 1280, height: 900 })
  await mockApi(page, cards, artifacts, contents, fetchedIds)
  await page.goto('/run-completed-output.html?node=end&tab=output')
  await expect(page.getByTestId('run-detail-root')).toBeVisible({ timeout: 15_000 })
}

test.describe('运行产出：未执行隐藏 + 多视觉独立 + 可区分失败', () => {
  test('未执行 visual_l6zc 不出卡，列表无误报失败文案', async ({ page }) => {
    await openOutput(page, [
      visualCard({
        index: 1,
        title: '网页预览 · 视觉网页',
        artifactName: 'visual_a.page.html',
        nodeId: 'visual_a',
        markdown: pageA,
      }),
    ])
    await expect(page.getByTestId('output-result-cards')).toBeVisible()
    await expect(page.getByTestId('output-result-list')).toHaveCount(0)
    await expect(page.getByTestId('output-result-detail-bar')).toContainText('网页预览 · 视觉网页')
    await expect(page.getByTestId('run-detail-right-panel')).not.toContainText('网页预览 · visual_l6zc')
    await expect(page.getByTestId('run-detail-right-panel')).not.toContainText('来源节点失败 / 无产物')
    await expect(page.getByTestId('run-detail-right-panel')).not.toContainText('上游节点无输出')
    await page.screenshot({ path: '/tmp/e2e-hide-unexecuted.png', fullPage: true })
  })

  test('全部隐藏时空态不算失败', async ({ page }) => {
    await openOutput(page, [])
    await expect(page.getByTestId('node-output-empty')).toBeVisible()
    await expect(page.getByTestId('node-output-empty')).toContainText('本次没有可预览的结果卡')
    await expect(page.getByTestId('node-output-empty')).toContainText('这不是加载失败')
    await expect(page.getByTestId('run-detail-right-panel')).not.toContainText('来源节点失败 / 无产物')
    await expect(page.getByTestId('run-detail-right-panel')).not.toContainText('上游节点无输出')
    await page.screenshot({ path: '/tmp/e2e-all-hidden-empty.png', fullPage: true })
  })

  test('双视觉卡预览 HTML 不同且不读全局 page.html', async ({ page }) => {
    const cards: OutputCard[] = [
      visualCard({
        index: 1,
        title: '网页预览 · 视觉网页',
        artifactName: 'visual_a.page.html',
        nodeId: 'visual_a',
        markdown: pageA,
      }),
      visualCard({
        index: 2,
        title: '网页预览 · visual_l6zc',
        artifactName: 'visual_l6zc.page.html',
        nodeId: 'visual_l6zc',
        markdown: pageB,
      }),
    ]
    const artifacts: Artifact[] = [
      {
        id: 'a-global',
        name: 'page.html',
        kind: 'html',
        nodeId: 'visual_l6zc',
        runId: 'run-completed-e2e',
        workflowName: '自我迭代',
        sizeBytes: pageB.length,
        createdAt: '2026-08-01T00:00:00Z',
      },
      {
        id: 'a-va',
        name: 'visual_a.page.html',
        kind: 'html',
        nodeId: 'visual_a',
        runId: 'run-completed-e2e',
        workflowName: '自我迭代',
        sizeBytes: pageA.length,
        createdAt: '2026-08-01T00:00:00Z',
      },
      {
        id: 'a-vb',
        name: 'visual_l6zc.page.html',
        kind: 'html',
        nodeId: 'visual_l6zc',
        runId: 'run-completed-e2e',
        workflowName: '自我迭代',
        sizeBytes: pageB.length,
        createdAt: '2026-08-01T00:00:00Z',
      },
    ]
    const fetched: string[] = []
    await openOutput(page, cards, artifacts, { 'a-va': pageA, 'a-vb': pageB, 'a-global': '<html>WRONG-GLOBAL</html>' }, fetched)

    await expect(page.getByTestId('output-result-list')).toBeVisible()
    await expect(page.getByTestId('output-result-card-toggle-0')).toContainText('网页预览 · 视觉网页')
    await expect(page.getByTestId('output-result-card-toggle-1')).toContainText('网页预览 · visual_l6zc')
    await expect(page.getByTestId('output-result-list-kind-0')).toHaveText('HTML')
    await expect(page.getByTestId('output-result-list-kind-1')).toHaveText('HTML')

    const iframe0 = page.locator('iframe').first()
    await expect(iframe0).toBeVisible()
    await expect.poll(async () => iframe0.getAttribute('sandbox')).toBe('allow-scripts allow-forms')
    await expect.poll(async () => iframe0.getAttribute('sandbox')).not.toContain('allow-same-origin')
    await expect.poll(async () => {
      return iframe0.evaluate((el) => (el as HTMLIFrameElement).srcdoc || '')
    }).toContain('page-a-workflow-shell')
    await expect.poll(async () => {
      return iframe0.evaluate((el) => (el as HTMLIFrameElement).srcdoc || '')
    }).not.toContain('page-b-artifact-density')
    await expect.poll(async () => {
      return iframe0.evaluate((el) => (el as HTMLIFrameElement).srcdoc || '')
    }).not.toContain('WRONG-GLOBAL')
    await page.screenshot({ path: '/tmp/e2e-dual-visual-a.png', fullPage: true })

    await page.getByTestId('output-result-card-toggle-1').click()
    const iframe1 = page.locator('iframe').first()
    await expect.poll(async () => {
      return iframe1.evaluate((el) => (el as HTMLIFrameElement).srcdoc || '')
    }).toContain('page-b-artifact-density')
    await expect.poll(async () => {
      return iframe1.evaluate((el) => (el as HTMLIFrameElement).srcdoc || '')
    }).not.toContain('page-a-workflow-shell')
    await expect.poll(async () => iframe1.getAttribute('sandbox')).toBe('allow-scripts allow-forms')
    await page.screenshot({ path: '/tmp/e2e-dual-visual-b.png', fullPage: true })

    expect(fetched).toContain('a-va')
    expect(fetched).toContain('a-vb')
    expect(fetched).not.toContain('a-global')
  })

  test('真实缺页失败卡标题为缺少可展示产出', async ({ page }) => {
    await openOutput(page, [
      visualCard({
        index: 1,
        title: '网页预览 · 视觉网页',
        artifactName: 'visual_a.page.html',
        nodeId: 'visual_a',
        markdown: pageA,
      }),
      {
        index: 2,
        template: '{{nodes.visual_l6zc.outputs.page}}',
        title: '网页预览 · visual_l6zc',
        typeTag: '来源失败',
        status: 'failed',
        nodeId: 'visual_l6zc',
        outputKey: 'page',
        failTitle: '缺少可展示产出',
        errorReason: '来源已执行完成但没有可供展示的产出',
      },
    ])
    await page.getByTestId('output-result-card-toggle-1').click()
    await expect(page.getByTestId('output-result-fail-title')).toHaveText('缺少可展示产出')
    await expect(page.getByTestId('output-result-card-body-1')).toContainText('来源已执行完成但没有可供展示的产出')
    await expect(page.getByTestId('run-detail-right-panel')).not.toContainText('来源节点失败 / 无产物')
    await expect(page.getByTestId('run-detail-right-panel')).not.toContainText('上游节点无输出')
    await page.screenshot({ path: '/tmp/e2e-real-missing-page.png', fullPage: true })
  })

  test('来源状态失败与取消文案可区分', async ({ page }) => {
    await openOutput(page, [
      {
        index: 1,
        template: '{{nodes.visual_l6zc.outputs.page}}',
        title: '网页预览 · visual_l6zc',
        typeTag: '来源失败',
        status: 'failed',
        nodeId: 'visual_l6zc',
        failTitle: '来源状态失败',
        errorReason: '上游节点状态：failed',
      },
      {
        index: 2,
        template: '{{nodes.visual_a.outputs.page}}',
        title: '网页预览 · 视觉网页',
        typeTag: '来源失败',
        status: 'failed',
        nodeId: 'visual_a',
        failTitle: '来源已取消',
        errorReason: '上游节点状态：cancelled',
      },
    ])
    await expect(page.getByTestId('output-result-fail-title')).toHaveText('来源状态失败')
    await expect(page.getByTestId('output-result-card-body-0')).toContainText('上游节点状态：failed')
    await page.screenshot({ path: '/tmp/e2e-source-failed.png', fullPage: true })
    await page.getByTestId('output-result-card-toggle-1').click()
    await expect(page.getByTestId('output-result-fail-title')).toHaveText('来源已取消')
    await expect(page.getByTestId('output-result-card-body-1')).toContainText('上游节点状态：cancelled')
    await page.screenshot({ path: '/tmp/e2e-source-cancelled.png', fullPage: true })
  })
})
