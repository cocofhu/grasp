import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'
import { routeApi } from './helpers/apiRoute'
import { test, expect, type Locator, type Page } from '@playwright/test'

/**
 * Bounding-box acceptance for the workflow directory:
 * the status dot shares the column-title left edge in every row.
 */

const MOCK_WORKSPACE = {
  name: 'Demo Workspace',
  description: 'Catalog align',
  variables: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

const MOCK_WORKFLOWS = [
  { id: 'wf-self', name: 'Self Iter', description: 'heavy description under the name', status: 'published', version: 2, updatedAt: '2026-01-01T00:00:00Z', needsRepo: false, nodes: [], edges: [] },
  { id: 'wf-light', name: 'Light', description: 'draft with a description', status: 'draft', version: 1, updatedAt: '2026-01-03T00:00:00Z', needsRepo: false, nodes: [], edges: [] },
  { id: 'wf-plain', name: 'Plain', description: '', status: 'published', version: 1, updatedAt: '2026-01-04T00:00:00Z', needsRepo: false, nodes: [], edges: [] },
]

async function setupHarness(page: Page) {
  await routeApi(page, '**/api/workspace', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({ json: MOCK_WORKSPACE })
      return
    }
    await route.continue()
  })
  await routeApi(page, /\/api\/workflows(\/|$|\?)/, async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({ json: MOCK_WORKFLOWS })
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

  test('zh desktop: status dots share the title edge and draft differs from published', async ({ page }) => {
    await setupHarness(page)
    await openWorkflows(page, 'view=/workflows')

    const headers = page.locator('.workflow-directory thead th')
    await expect(headers.nth(1)).toHaveText('状态')
    const statusX = await textOriginX(headers.nth(1))
    for (const rowSpec of [
      { id: 'wf-light', status: '草稿', published: false },
      { id: 'wf-plain', status: '已发布', published: true },
      { id: 'wf-self', status: '已发布', published: true },
    ]) {
      const row = page.locator(`[data-workflow-id="${rowSpec.id}"]`)
      await expect(row.locator('.workflow-status-label')).toHaveText(rowSpec.status)
      within((await row.locator('.workflow-status-dot').boundingBox())!.x, statusX, `${rowSpec.status} dot left`)
      await expect(row.locator('.workflow-status')).toHaveAttribute('data-status', rowSpec.published ? 'published' : 'draft')
    }
    await expect(page.locator('.workflow-notify-select')).toHaveCount(0)
    const draftDot = await page.locator('[data-workflow-id="wf-light"] .workflow-status-dot').evaluate((el) => getComputedStyle(el).backgroundColor)
    const publishedDot = await page.locator('[data-workflow-id="wf-plain"] .workflow-status-dot').evaluate((el) => getComputedStyle(el).backgroundColor)
    expect(draftDot).not.toBe(publishedDot)
  })

  test('narrow grid keeps status beside the name and home visibility below', async ({ page }) => {
    await setupHarness(page)
    await page.setViewportSize({ width: 900, height: 800 })
    await page.goto('/workspace-config.html?view=/workflows')
    const row = page.locator('[data-workflow-id="wf-plain"]')
    await expect(row).toBeVisible({ timeout: 10_000 })
    await expect.poll(() => page.locator('.workflow-directory thead').evaluate((el) => getComputedStyle(el).display)).toBe('none')

    const nameBox = await row.locator('.workflow-name').boundingBox()
    const statusBox = await row.locator('.workflow-status').boundingBox()
    const homeBox = await row.locator('.workflow-home-cell').boundingBox()
    expect(statusBox!.x).toBeGreaterThan(nameBox!.x + nameBox!.width - 2)
    expect(statusBox!.y).toBeLessThan(nameBox!.y + nameBox!.height)
    expect(homeBox!.y).toBeGreaterThan(nameBox!.y + nameBox!.height - 1)
    expect(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth)).toBe(false)
  })
})
