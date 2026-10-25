import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'
import { routeApi } from './helpers/apiRoute'
import { test, expect, type Locator, type Page } from '@playwright/test'

/**
 * Bounding-box acceptance for the workflow directory:
 * status dot and policy name share the column-title left edge,
 * and the status copy shares a baseline with the policy name.
 */

const MOCK_WORKSPACE = {
  name: 'Demo Workspace',
  description: 'Catalog align',
  variables: [],
  notifyPolicy: {
    enabled: true,
    defaultEvents: ['waiting_human', 'failed'],
    waitingHumanTemplate: '',
    failedTemplate: '',
    completedTemplate: '',
  },
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

const MOCK_WORKFLOWS = [
  {
    id: 'wf-self',
    name: 'Self Iter',
    description: 'heavy description under the name',
    status: 'published',
    version: 2,
    updatedAt: '2026-01-01T00:00:00Z',
    needsRepo: false,
    notifyPolicy: { mode: 'custom', events: ['waiting_human', 'failed'] },
    nodes: [],
    edges: [],
  },
  {
    id: 'wf-light',
    name: 'Light',
    description: 'draft with a description',
    status: 'draft',
    version: 1,
    updatedAt: '2026-01-03T00:00:00Z',
    needsRepo: false,
    notifyPolicy: { mode: 'off', events: [] },
    nodes: [],
    edges: [],
  },
  {
    id: 'wf-inherit',
    name: 'Inherit Default',
    description: '',
    status: 'published',
    version: 1,
    updatedAt: '2026-01-04T00:00:00Z',
    needsRepo: false,
    notifyPolicy: { mode: 'inherit' },
    nodes: [],
    edges: [],
  },
]

async function setupHarness(page: Page, options?: { delayNotify?: boolean }) {
  let releaseNotify: (() => void) | null = null
  let lastWorkflowSave: unknown = null
  const workflows = structuredClone(MOCK_WORKFLOWS)

  await routeApi(page, '**/api/workspace/channel**', async (route) => {
    await route.fulfill({
      json: { channel: { id: 'c1', type: 'qq', enabled: true, cronDeliver: false, cronDeliverTarget: 'group:123' } },
    })
  })
  await routeApi(page, '**/api/workspace', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({ json: MOCK_WORKSPACE })
      return
    }
    await route.continue()
  })
  await routeApi(page, /\/api\/workflows(\/|$|\?)/, async (route) => {
    const method = route.request().method()
    const url = route.request().url()
    if (method === 'GET') {
      await route.fulfill({ json: workflows })
      return
    }
    if (method === 'PATCH' && url.includes('/notify-policy')) {
      if (options?.delayNotify) await new Promise<void>((resolve) => { releaseNotify = resolve })
      const payload = route.request().postDataJSON() as { notifyPolicy?: { mode: string; events?: string[] } }
      const id = url.match(/\/workflows\/([^/]+)\/notify-policy/)?.[1]
      lastWorkflowSave = { id, notifyPolicy: payload?.notifyPolicy, via: 'patch-notify-policy' }
      const idx = workflows.findIndex((w) => w.id === id)
      if (idx >= 0 && payload.notifyPolicy) {
        workflows[idx] = {
          ...workflows[idx],
          notifyPolicy: { mode: payload.notifyPolicy.mode, events: payload.notifyPolicy.events || [] },
        }
      }
      await route.fulfill({ json: idx >= 0 ? workflows[idx] : { id, ...payload } })
      return
    }
    await route.continue()
  })
  await routeApi(page, '**/api/runs**', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({ json: { items: [], total: 0, page: 1, pageSize: 100, hasMore: false } })
      return
    }
    await route.continue()
  })
  await routeApi(page, '**/api/workspace/pm-leader', async (route) => {
    await route.fulfill({ json: { enabled: false } })
  })

  return {
    getLastWorkflowSave: () => lastWorkflowSave,
    releaseNotify: () => releaseNotify?.(),
  }
}

/** Typographic start of the first glyph, not its ink side bearing. */
async function textOriginX(locator: Locator): Promise<number> {
  return locator.evaluate((el) => {
    const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT, {
      acceptNode(node) {
        return node.textContent?.trim() ? NodeFilter.FILTER_ACCEPT : NodeFilter.FILTER_REJECT
      },
    })
    const text = walker.nextNode()
    if (!text) {
      const box = el.getBoundingClientRect()
      const style = getComputedStyle(el)
      return box.left + parseFloat(style.borderLeftWidth) + parseFloat(style.paddingLeft)
    }
    const range = document.createRange()
    range.setStart(text, 0)
    range.collapse(true)
    const rect = range.getClientRects()[0]
    return rect ? rect.x : range.getBoundingClientRect().x
  })
}

/** Alphabetic baseline of the single-line label. */
async function baselineY(locator: Locator): Promise<number> {
  return locator.evaluate((el) => {
    const marker = document.createElement('span')
    marker.style.cssText = 'display:inline-block;width:0;height:0;vertical-align:baseline;padding:0;margin:0;border:0;line-height:0;font-size:0;overflow:hidden;'
    el.appendChild(marker)
    const y = marker.getBoundingClientRect().top
    marker.remove()
    return y
  })
}

function within(actual: number, expected: number, label: string) {
  expect(Math.abs(actual - expected), `${label}: ${actual} vs ${expected}`).toBeLessThanOrEqual(1)
}

async function openWorkflows(page: Page, query: string) {
  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto(`/workspace-config.html?${query}`)
  await expect(page.getByTestId('workflows-desktop-table')).toBeVisible({ timeout: 10_000 })
  await expect(page.locator('.workflow-directory thead')).toBeVisible()
}

test.describe('workflow catalog column alignment', () => {
  test.beforeEach(async ({ page }) => { await installWorkspaceApiDefaults(page) })

  test('zh desktop: titles, baselines, draft/published, expanded events, disabled save', async ({ page }) => {
    const harness = await setupHarness(page, { delayNotify: true })
    await openWorkflows(page, 'view=/workflows')

    const statusHeader = page.locator('.workflow-directory thead th').nth(1)
    const policyHeader = page.locator('.workflow-directory thead th').nth(2)
    await expect(statusHeader).toHaveText('状态')
    await expect(policyHeader).toHaveText('通知策略')
    const statusX = await textOriginX(statusHeader)
    const policyX = await textOriginX(policyHeader)

    const rows = [
      { id: 'wf-light', label: '关闭', status: '草稿', published: false },
      { id: 'wf-inherit', label: '跟随工作区', status: '已发布', published: true },
      { id: 'wf-self', label: '自定义', status: '已发布', published: true },
    ]
    for (const rowSpec of rows) {
      const row = page.locator(`[data-workflow-id="${rowSpec.id}"]`)
      const label = row.locator('.workflow-notify-select .truncate')
      const statusLabel = row.locator('.workflow-status-label')
      const dot = row.locator('.workflow-status-dot')
      await expect(label).toHaveText(rowSpec.label)
      await expect(statusLabel).toHaveText(rowSpec.status)
      await expect(label).toHaveCSS('white-space', 'nowrap')
      const labelBox = await label.boundingBox()
      expect(labelBox!.height, `${rowSpec.label} stays one line`).toBeLessThanOrEqual(22)
      expect(await label.evaluate((el) => el.scrollWidth - el.clientWidth), `${rowSpec.label} is fully visible`).toBeLessThanOrEqual(1)
      within(await textOriginX(label), policyX, `${rowSpec.label} left`)
      within((await dot.boundingBox())!.x, statusX, `${rowSpec.status} dot left`)
      within(await baselineY(statusLabel), await baselineY(label), `${rowSpec.label} baseline`)
      await expect(row.locator('.workflow-status')).toHaveAttribute('data-status', rowSpec.published ? 'published' : 'draft')
    }
    const draftDot = await page.locator('[data-workflow-id="wf-light"] .workflow-status-dot').evaluate((el) => getComputedStyle(el).backgroundColor)
    const publishedDot = await page.locator('[data-workflow-id="wf-inherit"] .workflow-status-dot').evaluate((el) => getComputedStyle(el).backgroundColor)
    expect(draftDot).not.toBe(publishedDot)

    const described = page.locator('[data-workflow-id="wf-self"]')
    await expect(described.locator('.workflow-description')).toBeVisible()
    const customTrigger = described.locator('[data-test="app-select-trigger"]')
    await described.getByTestId('wf-notify-edit-events').click()
    const events = described.getByTestId('wf-notify-events')
    await expect(events).toBeVisible()
    const triggerBox = await customTrigger.boundingBox()
    const eventsBox = await events.boundingBox()
    expect(eventsBox!.y).toBeGreaterThanOrEqual(triggerBox!.y + triggerBox!.height - 1)
    within(await textOriginX(described.locator('.workflow-notify-select .truncate')), policyX, 'expanded custom left')
    const statusBaseline = await baselineY(described.locator('.workflow-status-label'))
    const triggerBaseline = await baselineY(described.locator('.workflow-notify-select .truncate'))
    within(statusBaseline, triggerBaseline, 'expanded status baseline')
    const statusBox = await described.locator('.workflow-status-label').boundingBox()
    const statusMid = statusBox!.y + statusBox!.height / 2
    const triggerMid = triggerBox!.y + triggerBox!.height / 2
    const eventsMid = eventsBox!.y + eventsBox!.height / 2
    expect(Math.abs(statusMid - triggerMid)).toBeLessThan(Math.abs(statusMid - eventsMid))

    const inherit = page.locator('[data-workflow-id="wf-inherit"]')
    const inheritLabel = inherit.locator('.workflow-notify-select .truncate')
    const beforeDisable = await textOriginX(inheritLabel)
    await inherit.locator('[data-test="app-select-trigger"]').click()
    await page.getByRole('option', { name: '关闭', exact: true }).click()
    await expect(inherit.locator('[data-test="app-select-trigger"]')).toBeDisabled()
    within(await textOriginX(inheritLabel), beforeDisable, 'disabled policy left')
    within(await textOriginX(inheritLabel), policyX, 'disabled policy vs title')
    await expect(inheritLabel).toHaveText('跟随工作区')
    harness.releaseNotify()
    await expect.poll(() => (harness.getLastWorkflowSave() as { notifyPolicy?: { mode?: string } } | null)?.notifyPolicy?.mode).toBe('off')
    await expect(inheritLabel).toHaveText('关闭')
    within(await textOriginX(inheritLabel), policyX, 'saved off left')
    expect(await inherit.locator('[data-test="app-select-trigger"]').evaluate((el) => el.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
  })

  test('en desktop: Off, Inherit and Custom share the notify title', async ({ page }) => {
    await setupHarness(page)
    await openWorkflows(page, 'view=/workflows&lang=en&theme=dark')
    const policyX = await textOriginX(page.locator('.workflow-directory thead th').nth(2))
    await expect(page.locator('.workflow-directory thead th').nth(2)).toHaveText('Notify policy')
    for (const rowSpec of [
      { id: 'wf-light', label: 'Off' },
      { id: 'wf-inherit', label: 'Inherit' },
      { id: 'wf-self', label: 'Custom' },
    ]) {
      const row = page.locator(`[data-workflow-id="${rowSpec.id}"]`)
      const label = row.locator('.workflow-notify-select .truncate')
      await expect(label).toHaveText(rowSpec.label)
      await expect(label).toHaveCSS('white-space', 'nowrap')
      expect((await label.boundingBox())!.height).toBeLessThanOrEqual(22)
      expect(await label.evaluate((el) => el.scrollWidth - el.clientWidth)).toBeLessThanOrEqual(1)
      within(await textOriginX(label), policyX, `${rowSpec.label} left`)
      within(await baselineY(row.locator('.workflow-status-label')), await baselineY(label), `${rowSpec.label} baseline`)
    }
  })

  test('narrow grid keeps the split and drops the policy indent', async ({ page }) => {
    await setupHarness(page)
    await page.setViewportSize({ width: 900, height: 800 })
    await page.goto('/workspace-config.html?view=/workflows')
    const row = page.locator('[data-workflow-id="wf-inherit"]')
    await expect(row).toBeVisible({ timeout: 10_000 })
    await expect.poll(() => page.locator('.workflow-directory thead').evaluate((el) => getComputedStyle(el).display)).toBe('none')

    const name = row.locator('.workflow-name')
    const status = row.locator('.workflow-status')
    const notify = row.locator('.workflow-notify-cell')
    const home = row.locator('.workflow-home-cell')
    const label = row.locator('.workflow-notify-select .truncate')
    const nameBox = await name.boundingBox()
    const statusBox = await status.boundingBox()
    const notifyBox = await notify.boundingBox()
    const homeBox = await home.boundingBox()
    expect(statusBox!.x).toBeGreaterThan(nameBox!.x + nameBox!.width - 2)
    expect(statusBox!.y).toBeLessThan(nameBox!.y + nameBox!.height)
    expect(notifyBox!.y).toBeGreaterThan(nameBox!.y + nameBox!.height - 1)
    expect(homeBox!.x).toBeGreaterThan(notifyBox!.x)
    expect(Math.abs((homeBox!.y + homeBox!.height / 2) - (notifyBox!.y + notifyBox!.height / 2))).toBeLessThan(24)
    await expect(label).toHaveText('跟随工作区')
    expect((await label.boundingBox())!.height).toBeLessThanOrEqual(22)
    within(await textOriginX(label), await textOriginX(name), 'narrow policy vs row content')
    expect(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth)).toBe(false)
  })
})
