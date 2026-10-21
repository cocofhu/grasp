import { test, expect } from '@playwright/test'

function stubRun(partial: {
  id: string
  trigger: string
  title?: string
  status?: string
}) {
  return {
    id: partial.id,
    workflowId: 'wf-1',
    workflowName: 'Trigger Demo',
    title: partial.title || partial.id,
    status: partial.status || 'completed',
    trigger: partial.trigger,
    startedAt: '2026-07-18T12:00:00Z',
    durationSec: 60,
    progress: 1,
    currentNodeLabel: '完成',
    nodeRuns: {},
    artifacts: [],
  }
}

const MIXED_RUNS = [
  stubRun({ id: 'run-manual-code', trigger: 'manual', title: '标准 manual' }),
  stubRun({ id: 'run-api-code', trigger: 'api', title: '标准 api' }),
  stubRun({ id: 'run-pm-code', trigger: 'pm_mcp', title: '标准 pm_mcp' }),
  stubRun({ id: 'run-channel', trigger: 'channel', title: '未映射-channel' }),
  stubRun({ id: 'run-free', trigger: 'qq:cron-timezone-bug', title: '未映射-自由串' }),
]

async function mockApis(page: import('@playwright/test').Page) {
  await page.route('**/api/workflows**', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([{ id: 'wf-1', name: 'Trigger Demo', status: 'published' }]),
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
        body: JSON.stringify({
          items: MIXED_RUNS,
          total: MIXED_RUNS.length,
          page: 1,
          pageSize: 20,
          hasMore: false,
        }),
      })
      return
    }
    await route.continue()
  })
}

async function gotoRunList(
  page: import('@playwright/test').Page,
  locale: 'zh' | 'en' = 'zh',
) {
  await page.setViewportSize({ width: 1280, height: 900 })
  await mockApis(page)
  const qs = locale === 'en' ? '?locale=en' : ''
  await page.goto(`/run-list-trigger.html${qs}`)
  await expect(page.locator('table')).toBeVisible({ timeout: 15_000 })
}

/** Trigger now sits in the run cell's meta line; the default (manual) is omitted. */
function triggerLabel(page: import('@playwright/test').Page, title: string) {
  return page.locator('tr', { hasText: title }).getByTestId('run-trigger')
}

test.describe('RunListView 触发方式展示', () => {
  test('中文：三码映射，自由串原样，手动（默认）不显示', async ({ page }) => {
    await gotoRunList(page, 'zh')
    await expect(page.locator('tr', { hasText: '标准 manual' })).toBeVisible()
    await expect(triggerLabel(page, '标准 manual')).toHaveCount(0)
    await expect(triggerLabel(page, '标准 api')).toHaveText('API')
    await expect(triggerLabel(page, '标准 pm_mcp')).toHaveText('工作区助手 MCP')
    await expect(triggerLabel(page, '未映射-channel')).toHaveText('channel')
    await expect(triggerLabel(page, '未映射-自由串')).toHaveText('qq:cron-timezone-bug')
    const triggerTexts = await page.getByTestId('run-trigger').allTextContents()
    expect(triggerTexts).toEqual(
      expect.arrayContaining(['API', '工作区助手 MCP', 'channel', 'qq:cron-timezone-bug']),
    )
    expect(triggerTexts).not.toContain('手动')
  })

  test('英文：映射为英文，Manual 不显示', async ({ page }) => {
    await gotoRunList(page, 'en')
    await expect(page.locator('tr', { hasText: '标准 manual' })).toBeVisible()
    await expect(triggerLabel(page, '标准 manual')).toHaveCount(0)
    await expect(triggerLabel(page, '标准 api')).toHaveText('API')
    await expect(triggerLabel(page, '标准 pm_mcp')).toHaveText('Workspace assistant MCP')
    await expect(triggerLabel(page, '未映射-channel')).toHaveText('channel')
  })
})
