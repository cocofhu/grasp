import { expect, test, type Page } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const shotDir = path.join(path.dirname(fileURLToPath(import.meta.url)), '../../../test-screenshots')

/** First navigation compiles the shell on a cold dev server; unrelated to the expand budget. */
const PAGE_READY_MS = 30_000

async function mockApi(page: Page) {
  await page.route('**/api/**', async (route) => {
    if (!new URL(route.request().url()).pathname.startsWith('/api/')) {
      await route.continue()
      return
    }
    const p = new URL(route.request().url()).pathname
    if (p.includes('/gates')) {
      await route.fulfill({ json: { items: [], total: 0 } })
      return
    }
    if (p.includes('/workflows')) {
      await route.fulfill({ json: [] })
      return
    }
    if (p.includes('/runs')) {
      await route.fulfill({ json: { items: [], total: 0, page: 1, pageSize: 20, hasMore: false } })
      return
    }
    if (p.includes('/health') || p.includes('/live')) {
      await route.fulfill({ json: { status: 'ok', ready: true } })
      return
    }
    if (p.includes('/platform') || p.includes('/status')) {
      await route.fulfill({
        json: {
          cumulativeTokens: null,
          todayTokens: null,
          runningCount: 0,
          queuedCount: 0,
          asOf: '2026-08-19T00:00:00Z',
          timezone: 'UTC',
        },
      })
      return
    }
    await route.fulfill({ json: {} })
  })
}

async function openGates(page: Page) {
  await mockApi(page)
  await page.setViewportSize({ width: 1280, height: 800 })
  await page.goto('/desktop-sidebar-hide.html?start=/gates')
  await expect(page.getByTestId('page-gates')).toBeVisible({ timeout: PAGE_READY_MS })
}

function sidebar(page: Page) {
  return page.getByTestId('app-desktop-sidebar')
}

/**
 * Arm an in-page stopwatch: capture-phase click timestamp → first frame the sidebar
 * has width. Measuring inside the page keeps the budget about the app, not about
 * CDP round-trips or a cold dev server stealing CPU from the poll loop.
 */
async function armExpandStopwatch(page: Page) {
  await page.evaluate(() => {
    const w = window as unknown as { __sidebarExpandMs?: number | 'timeout' }
    delete w.__sidebarExpandMs
    document.addEventListener(
      'click',
      () => {
        const aside = document.querySelector('[data-testid="app-desktop-sidebar"]')
        if (!aside) return
        const start = performance.now()
        const tick = () => {
          if (aside.getBoundingClientRect().width > 0) {
            w.__sidebarExpandMs = performance.now() - start
            return
          }
          if (performance.now() - start > 5_000) {
            w.__sidebarExpandMs = 'timeout'
            return
          }
          requestAnimationFrame(tick)
        }
        requestAnimationFrame(tick)
      },
      { capture: true, once: true },
    )
  })
}

/** Read the armed stopwatch and assert the app opened the sidebar within budget. */
async function expectExpandedWithin(page: Page, budgetMs: number) {
  await expect
    .poll(
      () =>
        page.evaluate(
          () => (window as unknown as { __sidebarExpandMs?: number | 'timeout' }).__sidebarExpandMs,
        ),
      { timeout: 10_000 },
    )
    .not.toBe(undefined)
  const elapsed = await page.evaluate(
    () => (window as unknown as { __sidebarExpandMs?: number | 'timeout' }).__sidebarExpandMs,
  )
  expect(elapsed, 'sidebar never regained width after the ball click').not.toBe('timeout')
  test.info().annotations.push({
    type: 'metrics',
    description: JSON.stringify({ sidebarExpandMs: Math.round(elapsed as number), budgetMs }),
  })
  expect(elapsed as number).toBeLessThan(budgetMs)
}

/** Wait for expanded→0 width transition so toBeHidden / edge.x are not sampled mid-animation. */
async function expectSidebarCollapsed(page: Page) {
  const aside = sidebar(page)
  await expect(aside).toHaveCSS('width', '0px')
  await expect
    .poll(async () => aside.evaluate((el) => el.getBoundingClientRect().width))
    .toBe(0)
  await expect(aside).toBeHidden()
}

test.describe('desktop sidebar hide', () => {
  // A cold vite dev server transforms the whole shell on the first navigation, which
  // took >75s on a contended box; the 250ms expand budget is asserted by the in-page
  // stopwatch, not by this ceiling.
  test.describe.configure({ timeout: 120_000 })

  test('hide from brand, open from floating ball, persist, no toast', async ({ page }) => {
    await openGates(page)
    const aside = sidebar(page)
    await expect(aside).toBeVisible()
    const expandedWidth = await page.locator('main').evaluate((el) => el.getBoundingClientRect().width)

    await page.screenshot({ path: path.join(shotDir, '01-desktop-expanded.png') })

    const hide = page.getByTestId('desktop-nav-hide')
    await expect(hide).toBeVisible()
    const hideBox = await hide.boundingBox()
    expect(hideBox?.width).toBeGreaterThanOrEqual(44)
    expect(hideBox?.height).toBeGreaterThanOrEqual(44)
    await expect(hide).toHaveAttribute('aria-label', '隐藏导航')

    await hide.click()
    await expectSidebarCollapsed(page)
    const ball = page.getByTestId('floating-nav-ball')
    await expect(ball).toBeVisible()
    await expect(page.getByTestId('desktop-nav-open')).toHaveCount(0)
    await expect(page.getByTestId('desktop-nav-edge-open')).toHaveCount(0)
    const hiddenWidth = await page.locator('main').evaluate((el) => el.getBoundingClientRect().width)
    expect(hiddenWidth).toBeGreaterThan(expandedWidth + 200)

    const toast = page.locator('[class*="toast"], [data-testid*="toast"]')
    await expect(toast).toHaveCount(0)

    await page.screenshot({ path: path.join(shotDir, '02-desktop-hidden-floating-ball.png') })

    const ballBox = await ball.boundingBox()
    expect(ballBox?.width).toBeGreaterThanOrEqual(44)
    expect(ballBox?.height).toBeGreaterThanOrEqual(44)
    expect(ballBox?.x ?? 99).toBeLessThan(40)
    expect((ballBox?.y ?? 0) + (ballBox?.height ?? 0)).toBeGreaterThan(700)
    await expect(ball).toHaveAttribute('aria-label', '打开导航')
    await armExpandStopwatch(page)
    await ball.click()
    await expectExpandedWithin(page, 250)
    await expect(aside).toBeVisible()
    await expect(page.getByTestId('floating-nav-ball')).toBeHidden()

    await hide.click()
    expect(await page.evaluate(() => localStorage.getItem('grasp-sidebar-hidden'))).toBe('true')
    await page.reload()
    await expect(page.getByTestId('page-gates')).toBeVisible({ timeout: PAGE_READY_MS })
    await expectSidebarCollapsed(page)
    await expect(page.getByTestId('floating-nav-ball')).toBeVisible()
  })

  test('full pages also use bottom-left floating ball (no edge-open)', async ({ page }) => {
    await mockApi(page)
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/desktop-sidebar-hide.html?start=/runs/r1')
    await expect(page.getByTestId('page-run')).toBeVisible({ timeout: PAGE_READY_MS })
    await expect(page.getByTestId('desktop-nav-hide')).toBeVisible()
    await page.getByTestId('desktop-nav-hide').click()
    await expectSidebarCollapsed(page)
    const ball = page.getByTestId('floating-nav-ball')
    await expect(ball).toBeVisible()
    const box = await ball.boundingBox()
    expect(box?.width).toBeGreaterThanOrEqual(44)
    expect(box?.height).toBeGreaterThanOrEqual(44)
    expect(box?.x ?? 99).toBeLessThan(40)
    await expect(page.getByTestId('desktop-nav-edge-open')).toHaveCount(0)
    await expect(page.getByTestId('app-full-main')).not.toHaveClass(/pl-11/)
    await page.screenshot({ path: path.join(shotDir, '03-full-run-hidden-ball.png') })

    await armExpandStopwatch(page)
    await ball.click()
    await expectExpandedWithin(page, 250)
    await expect(sidebar(page)).toBeVisible()
    await expect(page.getByTestId('floating-nav-ball')).toBeHidden()

    await page.getByTestId('desktop-nav-hide').click()
    await page.goto('/desktop-sidebar-hide.html?start=/workflows/wf-1/edit')
    await expect(page.getByTestId('page-editor')).toBeVisible({ timeout: PAGE_READY_MS })
    await expect(page.getByTestId('floating-nav-ball')).toBeVisible()
    await expect(page.getByTestId('desktop-nav-edge-open')).toHaveCount(0)
    await page.screenshot({ path: path.join(shotDir, '04-full-editor-hidden-ball.png') })

    await page.goto('/desktop-sidebar-hide.html?start=/sandboxes/42/console')
    await expect(page.getByTestId('page-console')).toBeVisible({ timeout: PAGE_READY_MS })
    await expect(page.getByTestId('floating-nav-ball')).toBeVisible()
    await page.screenshot({ path: path.join(shotDir, '05-full-console-hidden-ball.png') })

    await page.goto('/desktop-sidebar-hide.html?start=/gates')
    await expect(page.getByTestId('page-gates')).toBeVisible({ timeout: PAGE_READY_MS })
    await expect(page.getByTestId('desktop-nav-edge-open')).toHaveCount(0)
    await expect(page.getByTestId('floating-nav-ball')).toBeVisible()
    await page.screenshot({ path: path.join(shotDir, '06-back-to-gates-floating-ball.png') })
  })

  test('mobile floating ball opens drawer; returning to desktop keeps remembered hide', async ({
    page,
  }) => {
    await openGates(page)
    await page.getByTestId('desktop-nav-hide').click()
    await expectSidebarCollapsed(page)

    await page.setViewportSize({ width: 390, height: 844 })
    await expect(page.getByTestId('floating-nav-ball')).toBeVisible()
    await expect(page.getByTestId('mobile-nav-toggle')).toHaveCount(0)
    await expect(page.locator('.bg-black\\/50')).toHaveCount(0)
    await page.screenshot({ path: path.join(shotDir, '07-mobile-after-desktop-hide.png') })

    await page.getByTestId('floating-nav-ball').click()
    await expect(page.locator('.bg-black\\/50')).toBeVisible()
    const drawer = page.getByTestId('mobile-nav-drawer')
    await expect(drawer).toBeVisible()
    // Desktop sidebar remains in DOM (md:hidden); scope chrome controls to the open drawer.
    await expect(drawer.getByTestId('shell-chrome-controls')).toBeVisible()
    await page.screenshot({ path: path.join(shotDir, '08-mobile-drawer-open.png') })

    await page.setViewportSize({ width: 1280, height: 800 })
    await expectSidebarCollapsed(page)
    await expect(page.getByTestId('floating-nav-ball')).toBeVisible()
    await expect(page.locator('.bg-black\\/50')).not.toBeVisible()
    await page.screenshot({ path: path.join(shotDir, '09-desktop-still-hidden.png') })
  })
})
