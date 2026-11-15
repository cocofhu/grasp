import { expect, test, type Page } from '@playwright/test'

// g3.4 / g4.1: every .toolbar-control on a list header must render at the same
// height, including the triggers that carry a .chip count badge.
const DESKTOP_HEIGHT = 34
const MOBILE_HEIGHT = 44

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

async function mockApi(page: Page) {
  await page.route('**/api/**', async (route) => {
    // Skip Vite module URLs like /@fs/.../src/lib/api/api.ts (pathname is not /api/...)
    if (!new URL(route.request().url()).pathname.startsWith('/api/')) {
      await route.continue()
      return
    }
    const p = new URL(route.request().url()).pathname
    if (p.includes('/workflows')) {
      await route.fulfill({ json: MOCK_WORKFLOWS })
      return
    }
    if (p.includes('/health')) {
      await route.fulfill({ json: { status: 'ok', ready: true } })
      return
    }
    if (p.endsWith('/run-tags')) {
      await route.fulfill({ json: { tags: [] } })
      return
    }
    if (p.endsWith('/runs')) {
      await route.fulfill({ json: { items: [], total: 0, page: 1, pageSize: 20, hasMore: false } })
      return
    }
    await route.fulfill({ status: 404, json: { error: 'not mocked' } })
  })
}

async function controlHeights(page: Page) {
  await page.waitForSelector('.toolbar-control')
  return page.evaluate(() =>
    Array.from(document.querySelectorAll('.toolbar-control')).map((el) => ({
      label: (el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 24),
      hasChip: !!el.querySelector('.chip'),
      height: Math.round(el.getBoundingClientRect().height * 100) / 100,
    })),
  )
}

// The gates inbox lost its toolbar in #819; the run list is the list header that still uses these controls.
for (const theme of ['light', 'dark'] as const) {
  test(`run list toolbar filters share one desktop height (${theme})`, async ({ page }) => {
    await mockApi(page)
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.goto(`/run-list-sort.html?theme=${theme}`)
    const controls = await controlHeights(page)
    expect(controls.length).toBeGreaterThanOrEqual(2)
    expect(controls.some((c) => c.hasChip)).toBe(true)
    for (const c of controls) {
      expect(Math.abs(c.height - DESKTOP_HEIGHT), `${c.label} height ${c.height}`).toBeLessThanOrEqual(1)
    }
  })
}

test('run list toolbar filters keep the 44px mobile touch height', async ({ page }) => {
  await mockApi(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/run-list-sort.html')
  const controls = await controlHeights(page)
  expect(controls.length).toBeGreaterThanOrEqual(3)
  for (const c of controls) {
    expect(c.height, `${c.label} height ${c.height}`).toBeGreaterThanOrEqual(MOBILE_HEIGHT)
  }
})
