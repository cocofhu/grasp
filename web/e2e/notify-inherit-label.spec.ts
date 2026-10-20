import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'
import { routeApi } from './helpers/apiRoute'
/**
 * Temporary acceptance: en Inherit + nowrap; zh 跟随工作区; mode patch.
 * Uses the production workflow directory and workspace notification settings.
 */
import { test, expect } from '@playwright/test'

test.beforeEach(async ({ page }) => { await installWorkspaceApiDefaults(page) })
import path from 'node:path'
import fs from 'node:fs'

const shotDir = path.join('/tmp', 'notify-inherit-shots')
fs.mkdirSync(shotDir, { recursive: true })

const MOCK_WORKSPACE = {
  name: 'Demo Workspace',
  description: 'Notify Inherit accept',
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
    description: 'heavy',
    status: 'published',
    version: 2,
    updatedAt: '2026-01-01T00:00:00Z',
    lastRunAt: '2026-01-02T00:00:00Z',
    needsRepo: false,
    notifyPolicy: { mode: 'custom', events: ['waiting_human', 'failed'] },
    nodes: [],
    edges: [],
  },
  {
    id: 'wf-light',
    name: 'Light',
    description: 'light',
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

async function setupHarness(page: import('@playwright/test').Page) {
  let lastWorkflowSave: unknown = null
  const workflows = structuredClone(MOCK_WORKFLOWS)

  await routeApi(page, '**/api/workspace/channel**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        channel: {
          id: 'c1',
          type: 'qq',
          enabled: true,
          cronDeliver: false,
          cronDeliverTarget: 'group:123',
        },
      }),
    })
  })

  await routeApi(page, '**/api/workspace', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_WORKSPACE),
      })
      return
    }
    await route.continue()
  })

  await routeApi(page, /\/api\/workflows(\/|$|\?)/, async (route) => {
    const method = route.request().method()
    const url = route.request().url()
    if (method === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(workflows),
      })
      return
    }
    if (method === 'PATCH' && url.includes('/notify-policy')) {
      const payload = route.request().postDataJSON() as {
        notifyPolicy?: { mode: string; events?: string[] }
      }
      const idMatch = url.match(/\/workflows\/([^/]+)\/notify-policy/)
      const id = idMatch?.[1]
      lastWorkflowSave = { id, notifyPolicy: payload?.notifyPolicy, via: 'patch-notify-policy' }
      const idx = workflows.findIndex((w) => w.id === id)
      if (idx >= 0 && payload.notifyPolicy) {
        workflows[idx] = {
          ...workflows[idx],
          notifyPolicy: {
            mode: payload.notifyPolicy.mode,
            events: payload.notifyPolicy.events || [],
          },
        }
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(idx >= 0 ? workflows[idx] : { id, ...payload }),
      })
      return
    }
    await route.continue()
  })

  await routeApi(page, '**/api/runs**', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: [], total: 0, page: 1, pageSize: 100, hasMore: false }),
      })
      return
    }
    await route.continue()
  })

  await routeApi(page, '**/api/workspace/pm-leader', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ enabled: false }),
    })
  })



  return {
    getLastWorkflowSave: () => lastWorkflowSave,
  }
}

function assertSingleLine(box: { height: number } | null, label: string) {
  expect(box, label).not.toBeNull()
  expect(box!.height, `${label} should be single-line height`).toBeLessThan(36)
}

test.describe('Notify Inherit label acceptance', () => {
  test('en desktop: compact Inherit selector, mode patch', async ({ page }) => {
    const harness = await setupHarness(page)
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.goto('/workspace-config.html?view=/workflows&lang=en')

    const cell = page.getByTestId('wf-notify-cell').first()
    await expect(cell).toBeVisible({ timeout: 10_000 })

    const inheritBtn = page.locator('[data-workflow-id="wf-inherit"]').getByTestId('wf-notify-mode').locator('[data-test="app-select-trigger"]')
    await expect(inheritBtn).toBeVisible()
    await expect(inheritBtn).toHaveText('Inherit')

    assertSingleLine(await inheritBtn.boundingBox(), 'desktop Inherit button')

    await page.setViewportSize({ width: 900, height: 800 })
    await expect(inheritBtn).toBeVisible()
    assertSingleLine(await inheritBtn.boundingBox(), 'narrow desktop Inherit')

    await page.screenshot({
      path: path.join(shotDir, '01-en-desktop-inherit.png'),
      fullPage: true,
    })

    const inheritRow = page.locator('tbody tr', { hasText: 'Inherit Default' })
    await inheritRow.getByTestId('wf-notify-mode').locator('[data-test="app-select-trigger"]').click()
    await page.getByRole('option', { name: 'Custom', exact: true }).click()
    await expect.poll(() => {
      const s = harness.getLastWorkflowSave() as { id?: string; notifyPolicy?: { mode: string } } | null
      return s?.id === 'wf-inherit' ? s.notifyPolicy?.mode : null
    }).toBe('custom')
    await inheritRow.getByTestId('wf-notify-mode').locator('[data-test="app-select-trigger"]').click()
    await page.getByRole('option', { name: 'Inherit', exact: true }).click()
    await expect.poll(() => {
      const s = harness.getLastWorkflowSave() as { id?: string; notifyPolicy?: { mode: string } } | null
      return s?.id === 'wf-inherit' ? s.notifyPolicy?.mode : null
    }).toBe('inherit')

    await page.screenshot({
      path: path.join(shotDir, '02-en-desktop-after-toggle.png'),
      fullPage: true,
    })
  })

  test('en mobile: compact Inherit selector fits the separated workflow row', async ({ page }) => {
    await setupHarness(page)
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/workspace-config.html?view=/workflows&lang=en')

    const inline = page.locator('[data-workflow-id="wf-inherit"]').getByTestId('wf-notify-inline')
    const selector = inline.getByTestId('wf-notify-mode').locator('[data-test="app-select-trigger"]')
    await expect(selector).toBeVisible({ timeout: 10_000 })
    await expect(selector).toHaveText('Inherit')
    await selector.click()
    await expect(page.getByRole('option', { name: 'Inherit', exact: true })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('option')).toHaveText(['Off', 'Inherit', 'Custom'])
    await page.keyboard.press('Escape')
    await expect(page.getByRole('listbox')).toBeHidden()
    const bounds = await selector.boundingBox()
    expect(bounds!.height).toBeGreaterThanOrEqual(44)
    expect(bounds!.width).toBeGreaterThan(80)
    expect(bounds!.x).toBeGreaterThanOrEqual(0)
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(390)
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)

    await page.screenshot({ path: path.join(shotDir, '03-en-mobile-inherit.png'), fullPage: true })
  })

  test('zh-CN: 跟随工作区 single-line', async ({ page }) => {
    await setupHarness(page)
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.goto('/workspace-config.html?view=/settings/notification-policy')

    await expect(page.getByTestId('workspace-notify-panel')).toBeVisible()
    await expect(page.locator('text=跟随工作区').first()).toBeVisible()

    await page.screenshot({
      path: path.join(shotDir, '04-zh-notify-hint.png'),
      fullPage: true,
    })

    await page.goto('/workspace-config.html?view=/workflows')
    const inheritBtn = page.locator('[data-workflow-id="wf-inherit"]').getByTestId('wf-notify-mode').locator('[data-test="app-select-trigger"]')
    await expect(inheritBtn).toBeVisible({ timeout: 10_000 })
    assertSingleLine(await inheritBtn.boundingBox(), 'zh desktop 跟随工作区')

    await page.screenshot({
      path: path.join(shotDir, '05-zh-desktop-follow.png'),
      fullPage: true,
    })
  })

  test('en notify tab hint uses Inherit', async ({ page }) => {
    await setupHarness(page)
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.goto('/workspace-config.html?view=/settings/notification-policy&lang=en')

    await expect(page.getByTestId('workspace-notify-panel')).toBeVisible()
    await expect(page.getByText(/choose “Inherit”|choose "Inherit"/)).toBeVisible()

    await page.screenshot({
      path: path.join(shotDir, '06-en-notify-hint-inherit.png'),
      fullPage: true,
    })
  })
})
