import { expect, test, type Page } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const shotDir = path.join(__dirname, '..', '..', '..', 'test-screenshots')

async function mockApi(page: Page) {
  await page.route('**/api/**', async (route) => {
    // Skip Vite module URLs like /@fs/.../src/lib/api/api.ts (pathname is not /api/...)
    if (!new URL(route.request().url()).pathname.startsWith('/api/')) {
      await route.continue()
      return
    }
    const url = new URL(route.request().url())
    if (url.pathname.includes('/preview-issues')) {
      await route.fulfill({ json: { issues: [] } })
      return
    }
    if (url.pathname.includes('/artifacts/')) {
      await route.fulfill({
        json: {
          content: JSON.stringify({
            title: '需求',
            summary: 'Inbox 桌面详情应拉满视口',
          }),
          etag: 'e1',
          updatedAt: '2026-07-18T00:00:00Z',
          sizeBytes: 64,
        },
      })
      return
    }
    if (url.pathname.includes('/primary-artifacts') || url.pathname.includes('/gate/')) {
      await route.fulfill({ status: 400, json: { error: 'offline' } })
      return
    }
    await route.fulfill({ status: 404, json: { error: 'not mocked' } })
  })
}

async function metrics(page: Page) {
  return page.evaluate(() => {
    const card = document.querySelector('[data-testid="inbox-detail-card"]') as HTMLElement | null
    const budget = document.querySelector('[data-testid="content-fit-budget"]') as HTMLElement | null
    const preview = document.querySelector('[data-testid="content-fit-preview"]') as HTMLElement | null
    const upstream = document.querySelector('[data-testid="upstream-context"]') as HTMLElement | null
    const form = document.querySelector('[data-testid="content-fit-form"]') as HTMLElement | null
    const confirm = form
      ? Array.from(form.querySelectorAll('button')).find((b) => b.textContent?.includes('确认并流转'))
      : null
    const grid = document.querySelector('[data-testid="inbox-desktop-panel"]') as HTMLElement | null
    const taskCol = document.querySelector('[data-testid="inbox-task-col"]')

    function box(el: Element | null | undefined) {
      if (!el) return null
      const r = (el as HTMLElement).getBoundingClientRect()
      return { top: r.top, bottom: r.bottom, height: r.height, width: r.width }
    }

    const cardBox = box(card)
    const budgetBox = box(budget)
    const previewBox = box(preview)
    const upstreamBox = box(upstream)
    const confirmBox = box(confirm)
    const gridBox = box(grid)
    const gridTemplateColumns = grid ? getComputedStyle(grid).gridTemplateColumns : ''

    const gapPreviewToUpstream =
      previewBox && upstreamBox ? upstreamBox.top - previewBox.bottom : null
    const gapUpstreamToCardBottom =
      upstreamBox && cardBox ? cardBox.bottom - upstreamBox.bottom : null
    const gapBudgetToCardBottom =
      budgetBox && cardBox ? cardBox.bottom - budgetBox.bottom : null

    return {
      hasBudget: !!budget,
      budgetMaxHeight: budget?.style.maxHeight || '',
      previewMaxHeight: preview?.style.maxHeight || '',
      cardBox,
      budgetBox,
      previewBox,
      upstreamBox,
      confirmBox,
      gridBox,
      gridTemplateColumns,
      hasTaskCol: !!taskCol,
      gapPreviewToUpstream,
      gapUpstreamToCardBottom,
      gapBudgetToCardBottom,
      viewportH: window.innerHeight,
      viewportW: window.innerWidth,
      budgetCapPx: window.innerHeight * 0.6,
    }
  })
}

/** Detail panel fills the main area. The 320px task column is gone. */
function expectDesktopPanel(m: Awaited<ReturnType<typeof metrics>>) {
  expect(m.hasTaskCol).toBe(false)
  expect(m.cardBox).not.toBeNull()
  expect(m.cardBox!.width).toBeGreaterThan(m.viewportW * 0.7)
  // Stretched detail card must be tall enough that a 60vh cap would leave a void.
  expect(m.cardBox!.height).toBeGreaterThan(m.budgetCapPx + 80)
}

test.describe('Inbox unified preview budget layout', () => {
  test.use({ viewport: { width: 1440, height: 900 } })

  test('collapsed upstream: no large void under upstream; budget fills stage', async ({
    page,
  }, testInfo) => {
    await mockApi(page)
    await page.goto('/inbox-unified-budget.html?theme=light')
    await expect(page.getByTestId('content-fit-budget')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('upstream-context')).toBeVisible()

    const m = await metrics(page)
    await page.screenshot({
      path: path.join(shotDir, 'inbox-budget-collapsed.png'),
      fullPage: false,
    })
    testInfo.annotations.push({ type: 'metrics', description: JSON.stringify(m) })

    expectDesktopPanel(m)
    expect(m.hasBudget).toBe(true)
    // Inbox path: no 60vh maxHeight on budget (review v1)
    expect(m.budgetMaxHeight).toBe('')
    expect(m.previewMaxHeight).toBe('')
    expect(m.gapPreviewToUpstream).not.toBeNull()
    expect(m.gapPreviewToUpstream!).toBeLessThan(4)
    // Outer void under upstream must not be a large hollow (f2). Allow chrome/padding ≤ 48px.
    expect(m.gapUpstreamToCardBottom).not.toBeNull()
    expect(m.gapUpstreamToCardBottom!).toBeLessThan(48)
    // Detail card stretches with grid (g1)
    expect(m.cardBox!.height).toBeGreaterThan(m.viewportH * 0.7)
    // Budget fills stage — not capped below card
    expect(m.gapBudgetToCardBottom!).toBeLessThan(48)
    // Confirm reachable near viewport bottom
    expect(m.confirmBox).not.toBeNull()
    expect(m.viewportH - m.confirmBox!.bottom).toBeLessThan(64)
  })

  test('short HTML: blank stays in preview shell; no void under upstream', async ({ page }) => {
    await mockApi(page)
    await page.goto('/inbox-unified-budget.html?theme=light&short=1')
    await expect(page.getByTestId('content-fit-budget')).toBeVisible({ timeout: 15_000 })
    const m = await metrics(page)
    await page.screenshot({
      path: path.join(shotDir, 'inbox-budget-short.png'),
      fullPage: false,
    })
    expectDesktopPanel(m)
    expect(m.gapPreviewToUpstream!).toBeLessThan(4)
    expect(m.gapUpstreamToCardBottom!).toBeLessThan(48)
    // Preview shell still occupies meaningful height inside budget (not collapsed short bar)
    expect(m.previewBox!.height).toBeGreaterThan(120)
  })

  test('expand upstream: still no outer void; budget fills stage', async ({ page }) => {
    await mockApi(page)
    await page.goto('/inbox-unified-budget.html?theme=light')
    await expect(page.getByTestId('upstream-context-toggle')).toBeVisible({ timeout: 15_000 })
    await page.getByTestId('upstream-context-toggle').click()
    await expect(page.getByTestId('upstream-context-body')).toBeVisible()
    const m = await metrics(page)
    await page.screenshot({
      path: path.join(shotDir, 'inbox-budget-expanded.png'),
      fullPage: false,
    })
    expectDesktopPanel(m)
    expect(m.budgetMaxHeight).toBe('')
    expect(m.gapUpstreamToCardBottom!).toBeLessThan(48)
    expect(m.gapBudgetToCardBottom!).toBeLessThan(48)
    // Preview yields height but stays usable
    expect(m.previewBox!.height).toBeGreaterThan(80)
  })

  test('without upstream: card still stretched, no large bottom void under budget', async ({
    page,
  }) => {
    await mockApi(page)
    await page.goto('/inbox-unified-budget.html?theme=light&noUpstream=1')
    await expect(page.getByTestId('content-fit-budget')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('upstream-context')).toHaveCount(0)
    const m = await metrics(page)
    await page.screenshot({
      path: path.join(shotDir, 'inbox-budget-no-upstream.png'),
      fullPage: false,
    })
    expectDesktopPanel(m)
    expect(m.cardBox!.height).toBeGreaterThan(m.viewportH * 0.7)
    expect(m.gapBudgetToCardBottom!).toBeLessThan(48)
  })
})
