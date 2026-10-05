import { expect, test, type Page } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const shotDir = path.join(__dirname, '..', '..', '..', 'test-screenshots')

const MOCK_WORKFLOWS = [
  {
    id: 'wf-1',
    name: '快速上手·轻量',
    description: '',
    status: 'published',
    version: 1,
    updatedAt: '2026-01-01T00:00:00Z',
    needsRepo: false,
    nodes: [],
    edges: [],
  },
]

async function mockApi(
  page: Page,
  opts: { filterEmpty?: boolean; gatesDelayMs?: number; failGates?: { current: boolean } } = {},
) {
  await page.route('**/api/**', async (route) => {
    // Skip Vite module URLs like /@fs/.../src/lib/api/api.ts (pathname is not /api/...)
    if (!new URL(route.request().url()).pathname.startsWith('/api/')) {
      await route.continue()
      return
    }
    const url = new URL(route.request().url())
    const p = url.pathname
    if (p.includes('/gates')) {
      if (opts.failGates?.current) {
        await route.fulfill({ status: 500, json: { error: 'gates boom' } })
        return
      }
      if (opts.gatesDelayMs) {
        await new Promise((resolve) => setTimeout(resolve, opts.gatesDelayMs))
      }
      const filtered = opts.filterEmpty || !!url.searchParams.get('wf')
      await route.fulfill({
        json: filtered ? { items: [], total: 5 } : { items: [], total: 0 },
      })
      return
    }
    if (p.includes('/workflows')) {
      await route.fulfill({ json: MOCK_WORKFLOWS })
      return
    }
    if (p.includes('/projects')) {
      await route.fulfill({ json: [] })
      return
    }
    if (p.includes('/health')) {
      await route.fulfill({ json: { status: 'ok', ready: true } })
      return
    }
    await route.fulfill({ status: 404, json: { error: 'not mocked' } })
  })
}

function emptyCard(page: Page) {
  return page.locator('.card').filter({ hasText: /没有待审批项|该工作流没有待审批项/ })
}

async function emptyMetrics(page: Page) {
  return page.evaluate(() => {
    const cards = Array.from(document.querySelectorAll('.card')).filter((el) =>
      /没有待审批项|该工作流没有待审批项/.test(el.textContent || ''),
    )
    const card = cards[0] as HTMLElement | undefined
    const empty = card?.firstElementChild as HTMLElement | undefined
    const heading = Array.from(document.querySelectorAll('h2')).find((el) =>
      (el.textContent || '').includes('待办'),
    ) as HTMLElement | undefined
    function box(el: Element | null | undefined) {
      if (!el) return null
      const r = (el as HTMLElement).getBoundingClientRect()
      return { top: r.top, bottom: r.bottom, height: r.height, width: r.width }
    }
    const cardBox = box(card)
    const emptyBox = box(empty)
    const headingBox = box(heading)
    const cs = card ? getComputedStyle(card) : null
    return {
      cardBox,
      emptyBox,
      headingBox,
      viewportH: window.innerHeight,
      viewportW: window.innerWidth,
      cardRadius: cs?.borderRadius || '',
      cardOverflow: cs?.overflow || '',
      cardScrollH: card?.scrollHeight ?? 0,
      cardClientH: card?.clientHeight ?? 0,
      bgBase: getComputedStyle(document.documentElement).getPropertyValue('--c-base').trim(),
    }
  })
}

function expectFillGeometry(
  m: Awaited<ReturnType<typeof emptyMetrics>>,
  opts: { minViewportRatio?: number } = {},
) {
  const minRatio = opts.minViewportRatio ?? 0.7
  expect(m.cardBox).not.toBeNull()
  expect(m.emptyBox).not.toBeNull()
  // Desktop aligns inbox-unified-budget (card > 70% viewport). Mobile header stacks
  // (title + tools) so remaining ratio is lower; still must fill leftover height.
  expect(m.cardBox!.height).toBeGreaterThan(m.viewportH * minRatio)
  // No large void under the card — only AppShell bottom padding (pb-4 / md:pb-6) remains.
  expect(m.viewportH - m.cardBox!.bottom).toBeLessThan(48)
  expect(m.cardBox!.bottom).toBeGreaterThan(m.viewportH * 0.9)
  // Must be stretched vs content-sized EmptyState (~py-14 + icon), not a short bar.
  expect(m.cardBox!.height).toBeGreaterThan(m.emptyBox!.height + 80)
  // EmptyState roughly vertically centered in the card (not stuck to top/bottom).
  const cardMid = m.cardBox!.top + m.cardBox!.height / 2
  const emptyMid = m.emptyBox!.top + m.emptyBox!.height / 2
  expect(Math.abs(cardMid - emptyMid)).toBeLessThan(48)
  expect(m.emptyBox!.top).toBeGreaterThan(m.cardBox!.top + 8)
  expect(m.cardBox!.bottom).toBeGreaterThan(m.emptyBox!.bottom + 8)
}

async function expectEmptyCardClasses(page: Page) {
  const className = (await emptyCard(page).getAttribute('class')) || ''
  expect(className.split(/\s+/)).toEqual(
    expect.arrayContaining(['card', 'flex', 'min-h-0', 'flex-1', 'flex-col', 'items-center', 'justify-center', 'overflow-auto']),
  )
}

test.describe('Inbox empty fill layout (plan g2.3)', () => {
  test('desktop empty inbox: card fills remaining height, EmptyState centered (g1.2 g2.3 g3.1)', async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await mockApi(page)
    await page.goto('/inbox-empty-fill.html?theme=light')
    await expect(page.getByRole('heading', { name: '待办' })).toBeVisible({ timeout: 15_000 })
    await expect(emptyCard(page)).toBeVisible()
    await expect(page.getByText('没有待审批项')).toBeVisible()

    const m = await emptyMetrics(page)
    await page.screenshot({
      path: path.join(shotDir, 'inbox-empty-desktop-light.png'),
      fullPage: false,
    })
    testInfo.annotations.push({ type: 'metrics', description: JSON.stringify(m) })

    expectFillGeometry(m)
    await expectEmptyCardClasses(page)
    // .card uses rounded-lg → card role 12px (no global radius:0 !important)
    const radiusPx = parseFloat(String(m.cardRadius).split(' ')[0] || '0')
    expect(radiusPx).toBeGreaterThanOrEqual(11)
    expect(radiusPx).toBeLessThanOrEqual(13)
  })

  test('mobile empty inbox: same fill + center geometry (g1.1 g2.3)', async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await mockApi(page)
    await page.goto('/inbox-empty-fill.html?theme=light')
    await expect(page.getByRole('heading', { name: '待办' })).toBeVisible({ timeout: 15_000 })
    await expect(page.getByText('没有待审批项')).toBeVisible()

    const m = await emptyMetrics(page)
    await page.screenshot({
      path: path.join(shotDir, 'inbox-empty-mobile-light.png'),
      fullPage: false,
    })
    testInfo.annotations.push({ type: 'metrics', description: JSON.stringify(m) })
    expectFillGeometry(m, { minViewportRatio: 0.55 })
    await expectEmptyCardClasses(page)
  })

  test('workflow filter empty uses same fill wrapper + workflow copy (g1.3 g2.3)', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await mockApi(page, { filterEmpty: true })
    await page.goto('/inbox-empty-fill.html?theme=light&wf=wf-1')
    await expect(page.getByText('该工作流没有待审批项')).toBeVisible({ timeout: 15_000 })
    const m = await emptyMetrics(page)
    expectFillGeometry(m)
    await expectEmptyCardClasses(page)
  })

  test('dark theme empty card still fills without large base-color void (g3.1)', async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await mockApi(page)
    await page.goto('/inbox-empty-fill.html?theme=dark')
    await expect(page.getByText('没有待审批项')).toBeVisible({ timeout: 15_000 })
    const m = await emptyMetrics(page)
    await page.screenshot({
      path: path.join(shotDir, 'inbox-empty-desktop-dark.png'),
      fullPage: false,
    })
    testInfo.annotations.push({ type: 'metrics', description: JSON.stringify(m) })
    expectFillGeometry(m)
  })

  test('short viewport: chrome stays, empty card scrolls instead of overflowing (g1.3 g2.3)', async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 360 })
    await mockApi(page)
    await page.goto('/inbox-empty-fill.html?theme=light')
    await expect(page.getByRole('heading', { name: '待办' })).toBeVisible({ timeout: 15_000 })
    await expect(emptyCard(page)).toBeVisible()

    const m = await emptyMetrics(page)
    expect(m.headingBox).not.toBeNull()
    expect(m.headingBox!.top).toBeGreaterThanOrEqual(0)
    expect(m.cardBox).not.toBeNull()
    // Card stays inside the viewport (does not blow past chrome).
    expect(m.cardBox!.bottom).toBeLessThanOrEqual(m.viewportH + 1)
    expect(m.cardBox!.top).toBeGreaterThan(m.headingBox!.bottom - 1)
    // overflow-auto allows internal scroll when EmptyState exceeds card height.
    expect(['auto', 'overlay', 'scroll']).toContain(m.cardOverflow)
  })
})

test.describe('Inbox first-load skeleton / fail (plan g3.3)', () => {
  test('delayed GET /gates shows 6 skeletons and never flashes empty copy (g3.3)', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await mockApi(page, { gatesDelayMs: 1800 })
    await page.goto('/inbox-empty-fill.html?theme=light')
    await expect(page.getByRole('heading', { name: '待办' })).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('inbox-list-skeleton')).toBeVisible()
    await expect(page.getByTestId('inbox-pending-card-skeleton')).toHaveCount(6)
    await expect(page.getByText('列表加载后可选择一项')).toBeVisible()
    await expect(page.getByText('没有待审批项')).toHaveCount(0)
    await expect(page.getByText('该工作流没有待审批项')).toHaveCount(0)
    await expect(page.getByRole('heading', { name: '待办' })).toBeVisible()
    await expect(page.getByRole('button', { name: '刷新' })).toBeVisible()

    await expect(page.getByText('没有待审批项')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('inbox-list-skeleton')).toHaveCount(0)
    await expectEmptyCardClasses(page)
  })

  test('first GET /gates 500 shows load-failed + retry, not empty copy (g3.3)', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    const failGates = { current: true }
    await mockApi(page, { failGates })
    await page.goto('/inbox-empty-fill.html?theme=light')
    await expect(page.getByRole('heading', { name: '待办' })).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('inbox-list-failed')).toBeVisible()
    await expect(page.getByText('加载失败')).toBeVisible()
    await expect(page.getByText('无法获取列表，请稍后重试')).toBeVisible()
    await expect(page.getByTestId('inbox-list-retry')).toBeVisible()
    await expect(page.getByText('没有待审批项')).toHaveCount(0)

    failGates.current = false
    await page.getByTestId('inbox-list-retry').click()
    await expect(page.getByText('没有待审批项')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('inbox-list-failed')).toHaveCount(0)
    await expectEmptyCardClasses(page)
  })
})
