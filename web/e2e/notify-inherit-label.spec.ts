/**
 * Temporary acceptance: en Inherit + nowrap; zh 跟随项目; no Follow project; mode patch.
 * Uses project-detail.html?lang=en (harness supports lang query).
 */
import { test, expect } from '@playwright/test'
import path from 'node:path'
import fs from 'node:fs'

const shotDir = path.join('/tmp', 'notify-inherit-shots')
fs.mkdirSync(shotDir, { recursive: true })

const MOCK_PROJECT = {
  id: 'proj-1',
  name: 'Demo Project',
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

  await page.route('**/api/projects/proj-1/channel**', async (route) => {
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

  await page.route(/\/api\/workflows(\/|$|\?)/, async (route) => {
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

  await page.route('**/api/runs**', async (route) => {
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

  return {
    getLastWorkflowSave: () => lastWorkflowSave,
  }
}

function assertSingleLine(box: { height: number } | null, label: string) {
  expect(box, label).not.toBeNull()
  expect(box!.height, `${label} should be single-line height`).toBeLessThan(36)
}

test.describe('Notify Inherit label acceptance', () => {
  test('en desktop: Inherit single-line, no Follow project, mode patch', async ({ page }) => {
    const harness = await setupHarness(page)
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.goto('/project-detail.html?lang=en')
    await expect(page.getByRole('heading', { name: 'Demo Project' })).toBeVisible({ timeout: 15_000 })

    await page.getByTestId('project-tab-workflows').click()
    const cell = page.getByTestId('wf-notify-cell').first()
    await expect(cell).toBeVisible({ timeout: 10_000 })

    const inheritBtn = page.locator('[data-testid="wf-notify-cell"] button', { hasText: /^Inherit$/ }).first()
    await expect(inheritBtn).toBeVisible()
    await expect(page.locator('body')).not.toContainText('Follow project')

    assertSingleLine(await inheritBtn.boundingBox(), 'desktop Inherit button')

    await page.setViewportSize({ width: 900, height: 800 })
    await expect(inheritBtn).toBeVisible()
    assertSingleLine(await inheritBtn.boundingBox(), 'narrow desktop Inherit')

    await page.screenshot({
      path: path.join(shotDir, '01-en-desktop-inherit.png'),
      fullPage: true,
    })

    const inheritRow = page.locator('tbody tr', { hasText: 'Inherit Default' })
    await inheritRow.getByRole('button', { name: 'Custom' }).click()
    await expect.poll(() => {
      const s = harness.getLastWorkflowSave() as { id?: string; notifyPolicy?: { mode: string } } | null
      return s?.id === 'wf-inherit' ? s.notifyPolicy?.mode : null
    }).toBe('custom')
    await inheritRow.getByRole('button', { name: 'Inherit' }).click()
    await expect.poll(() => {
      const s = harness.getLastWorkflowSave() as { id?: string; notifyPolicy?: { mode: string } } | null
      return s?.id === 'wf-inherit' ? s.notifyPolicy?.mode : null
    }).toBe('inherit')

    await page.screenshot({
      path: path.join(shotDir, '02-en-desktop-after-toggle.png'),
      fullPage: true,
    })
  })

  test('en mobile: Inherit single-line on wf-notify-inline', async ({ page }) => {
    await setupHarness(page)
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/project-detail.html?lang=en')
    await expect(page.getByRole('heading', { name: 'Demo Project' })).toBeVisible({ timeout: 15_000 })

    await page.getByTestId('project-tab-workflows').click()
    const inline = page.getByTestId('wf-notify-inline').first()
    await expect(inline).toBeVisible({ timeout: 10_000 })

    const offBtn = inline.getByRole('button', { name: /^Off$/ })
    const inheritBtn = inline.getByRole('button', { name: /^Inherit$/ })
    const customBtn = inline.getByRole('button', { name: /^Custom$/ })
    await expect(offBtn).toBeVisible()
    await expect(inheritBtn).toBeVisible()
    await expect(customBtn).toBeVisible()
    assertSingleLine(await inheritBtn.boundingBox(), 'mobile Inherit')
    assertSingleLine(await customBtn.boundingBox(), 'mobile Custom')
    await expect(page.locator('body')).not.toContainText('Follow project')

    const metrics = await inline.evaluate((root) => {
      const seg = root.querySelector('div.inline-flex') as HTMLElement | null
      const buttons = [...root.querySelectorAll('button')].map((b) => {
        const el = b as HTMLElement
        const r = el.getBoundingClientRect()
        return {
          text: (el.textContent || '').trim(),
          clientWidth: el.clientWidth,
          scrollWidth: el.scrollWidth,
          whiteSpace: getComputedStyle(el).whiteSpace,
          left: r.left,
          right: r.right,
          width: r.width,
        }
      })
      return {
        overflow: seg ? getComputedStyle(seg).overflow : null,
        overflowX: seg ? getComputedStyle(seg).overflowX : null,
        clientWidth: seg?.clientWidth ?? 0,
        scrollWidth: seg?.scrollWidth ?? 0,
        buttons,
      }
    })

    const clipped = metrics.clientWidth + 1 < metrics.scrollWidth
    if (clipped) {
      expect(['auto', 'scroll'], 'segment may scroll but must not silently clip').toContain(
        metrics.overflowX,
      )
    } else {
      expect(metrics.clientWidth, 'segment fully visible').toBeGreaterThanOrEqual(metrics.scrollWidth - 1)
    }
    expect(metrics.overflow === 'hidden' && clipped, 'must not overflow-hidden clip Inherit/Custom').toBe(false)

    for (const btn of metrics.buttons) {
      expect(btn.whiteSpace, `${btn.text} nowrap`).toBe('nowrap')
      expect(btn.width, `${btn.text} visible width`).toBeGreaterThan(12)
      expect(btn.right, `${btn.text} within 390 viewport`).toBeLessThanOrEqual(391)
      expect(btn.left, `${btn.text} not clipped left`).toBeGreaterThanOrEqual(-1)
    }

    await page.screenshot({
      path: path.join(shotDir, '03-en-mobile-inherit.png'),
      fullPage: true,
    })
  })

  test('zh-CN: 跟随项目 single-line; hint has no Follow project', async ({ page }) => {
    await setupHarness(page)
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.goto('/project-detail.html?tab=notify')
    await expect(page.getByRole('heading', { name: 'Demo Project' })).toBeVisible({ timeout: 15_000 })

    await expect(page.getByTestId('project-notify-panel')).toBeVisible()
    await expect(page.locator('text=跟随项目').first()).toBeVisible()
    await expect(page.locator('body')).not.toContainText('Follow project')

    await page.screenshot({
      path: path.join(shotDir, '04-zh-notify-hint.png'),
      fullPage: true,
    })

    await page.getByTestId('project-tab-workflows').click()
    const inheritBtn = page.locator('[data-testid="wf-notify-cell"] button', { hasText: '跟随项目' }).first()
    await expect(inheritBtn).toBeVisible({ timeout: 10_000 })
    assertSingleLine(await inheritBtn.boundingBox(), 'zh desktop 跟随项目')

    await page.screenshot({
      path: path.join(shotDir, '05-zh-desktop-follow.png'),
      fullPage: true,
    })
  })

  test('en notify tab hint uses Inherit not Follow project', async ({ page }) => {
    await setupHarness(page)
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.goto('/project-detail.html?lang=en&tab=notify')
    await expect(page.getByRole('heading', { name: 'Demo Project' })).toBeVisible({ timeout: 15_000 })

    await expect(page.getByTestId('project-notify-panel')).toBeVisible()
    await expect(page.getByText(/choose “Inherit”|choose "Inherit"/)).toBeVisible()
    await expect(page.locator('body')).not.toContainText('Follow project')

    await page.screenshot({
      path: path.join(shotDir, '06-en-notify-hint-inherit.png'),
      fullPage: true,
    })
  })
})
