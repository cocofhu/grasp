import { expect, test, type Page } from '@playwright/test'

const MOBILE_VIEWS = [
  '/login',
  '/dashboard',
  '/stats',
  '/board',
  '/workflows',
  '/solo',
  '/settings/projects',
  '/settings/assistant',
  '/settings/schedules',
  '/settings/notification-policy',
  '/settings/external-mcp',
  '/settings/shared-agent',
  '/artifacts',
  '/notifications',
  '/agents',
  '/sandboxes',
  '/requirements',
  '/assistant',
  '/settings',
  '/settings/variables',
  '/settings/credentials',
  '/settings/audit',
  '/runs',
  '/runs/run-1',
  '/runs/run-1/live',
  '/runs/run-1/nodes/clarify',
  '/workflows/wf-1/edit',
  '/sandboxes/1/console',
] as const

const TEXT_CONTROL = [
  'input:not([type="checkbox"]):not([type="radio"]):not([type="range"]):not([type="file"]):not([type="hidden"])',
  'textarea',
  'select',
].join(',')

async function waitForWorkspace(page: Page, view: string) {
  await page.goto(`/workspace.html?view=${encodeURIComponent(view)}`, { waitUntil: 'domcontentloaded' })
  await expect(page.locator('#app')).toBeVisible()
  await page.waitForTimeout(250)
}

async function assertViewportContract(page: Page) {
  const metrics = await page.evaluate(() => ({
    innerWidth: window.innerWidth,
    documentClientWidth: document.documentElement.clientWidth,
    bodyClientWidth: document.body.clientWidth,
    documentScrollWidth: document.documentElement.scrollWidth,
    bodyScrollWidth: document.body.scrollWidth,
    scale: window.visualViewport?.scale || 1,
  }))
  const contentWidth = Math.max(metrics.documentScrollWidth, metrics.bodyScrollWidth)
  const viewportWidth = Math.max(metrics.innerWidth, metrics.documentClientWidth, metrics.bodyClientWidth)
  expect(contentWidth, `document overflow: ${JSON.stringify(metrics)}`).toBeLessThanOrEqual(viewportWidth + 1)
  expect(viewportWidth).toBeGreaterThan(0)
  expect(metrics.scale, `page zoom on load: ${JSON.stringify(metrics)}`).toBeLessThanOrEqual(1.05)
}

async function assertFormZoomContract(page: Page) {
  const controls = page.locator(TEXT_CONTROL)
  const count = await controls.count()
  const visible = [] as number[]
  for (let index = 0; index < count; index++) {
    if (await controls.nth(index).isVisible().catch(() => false)) visible.push(index)
  }

  for (const index of visible) {
    const control = controls.nth(index)
    const fontSize = await control.evaluate((el) => Number.parseFloat(getComputedStyle(el).fontSize))
    expect(fontSize, `control ${index} should avoid mobile focus zoom`).toBeGreaterThanOrEqual(16)
    await control.focus()
    await page.waitForTimeout(40)
    const scale = await page.evaluate(() => window.visualViewport?.scale || 1)
    expect(scale, `focus zoom on control ${index}`).toBeLessThanOrEqual(1.05)
    await control.evaluate((el) => (el as HTMLElement).blur())
  }
}

test.describe('mobile browser contract', () => {
  for (const view of MOBILE_VIEWS) {
    test(`${view} has no viewport overflow or focus zoom`, async ({ page }) => {
      const pageErrors: string[] = []
      page.on('pageerror', (error) => pageErrors.push(error.message))
      await waitForWorkspace(page, view)
      await expect(page.locator('#app')).toBeVisible()
      await page.waitForTimeout(450)
      await assertViewportContract(page)
      await assertFormZoomContract(page)
      expect(pageErrors).toEqual([])
    })
  }

  test('public approval shell has no viewport overflow or focus zoom', async ({ page }) => {
    const pageErrors: string[] = []
    page.on('pageerror', (error) => pageErrors.push(error.message))
    await page.goto('/gate-share-link.html?scene=public', { waitUntil: 'domcontentloaded' })
    await expect(page.getByTestId('public-gate-root')).toBeVisible({ timeout: 10_000 })
    await page.waitForTimeout(250)
    await assertViewportContract(page)
    await assertFormZoomContract(page)
    expect(pageErrors).toEqual([])
  })
})
