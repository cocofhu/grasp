import { expect, test, type Page } from '@playwright/test'
import { routeApi } from './helpers/apiRoute'
import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'

const MOBILE_VIEWS = [
  '/login',
  '/dashboard',
  '/stats',
  '/board',
  '/workflows',
  '/solo',
  '/settings/projects',
  '/settings/shared-agent',
  '/artifacts',
  '/notifications',
  '/agents',
  '/sandboxes',
  '/settings',
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
  if (view === '/login') {
    await expect(page.locator('#username')).toBeVisible()
    await expect(page.locator('#password')).toBeVisible()
  }
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

  test('Solo reference types stay visible and can be selected in one tap on a phone', async ({ page }) => {
    const session = { id: 'mobile-references', title: 'Solo references', agentName: 'Solo', status: 'running' }
    const candidates = {
      discussion: [{ kind: 'github_discussion', target: 'demo/web#23', title: 'Preview review', state: 'open', comments: 2, repo: 'demo/web', number: 23 }],
      artifact: [{ kind: 'artifact', target: 'art-checklist', title: 'Checklist.md', fileKind: 'markdown' }],
    }
    await installWorkspaceApiDefaults(page)
    await routeApi(page, '**/api/solo/sessions**', async (route) => {
      const url = new URL(route.request().url())
      if (url.pathname === '/api/solo/sessions') return route.fulfill({ json: { sessions: [session], maxSandboxes: 8 } })
      if (url.pathname.endsWith('/messages')) return route.fulfill({ json: [] })
      if (!url.pathname.endsWith('/references/search')) return route.fulfill({ json: session })
      const lane = url.searchParams.get('lane')
      const items = lane === 'discussion' || lane === 'artifact' ? [...candidates[lane]] : []
      if (lane === 'discussion' && url.searchParams.get('state') === 'all') {
        items.push({ ...candidates.discussion[0]!, target: 'demo/web#22', number: 22, title: 'Archived preview', state: 'closed' })
      }
      return route.fulfill({ json: { lane, items, total: lane === 'discussion' ? 128 : items.length } })
    })
    await page.routeWebSocket('**/api/solo/sessions/*/chat*', ws => ws.send(JSON.stringify({ type: 'session', event: 'queue_state', busy: false, items: [] })))

    for (const [locale, label] of [['zh-CN', '讨论'], ['en', 'Discussion']] as const) {
      await page.goto(`/workspace-config.html?view=/solo/${session.id}&lang=${locale}`)
      const input = page.getByTestId('solo-input')
      const picker = page.getByTestId('solo-reference-picker')
      const types = picker.getByRole('group')
      const discussion = picker.getByTestId('solo-reference-tab-discussion')
      const artifact = picker.getByTestId('solo-reference-tab-artifact')
      for (const width of [320, 390]) {
        await page.setViewportSize({ width, height: 720 })
        await input.fill('@')
        await expect(picker.getByRole('option', { name: /Preview review/ })).toBeVisible()
        await expect(picker.getByRole('option', { name: /Checklist\.md/ })).toBeVisible()
        await expect(types.getByRole('button')).toHaveCount(6)
        await expect(discussion).toHaveText(label)
        await discussion.tap()
        await expect(discussion).toHaveAttribute('aria-pressed', 'true')
        await expect(input).toBeFocused()
        await expect(picker).toContainText('99+')
        const fits = await types.evaluate(el => {
          const bar = el.getBoundingClientRect()
          return Array.from(el.querySelectorAll('button')).every(button => {
            const rect = button.getBoundingClientRect()
            return Math.abs(rect.top - bar.top) < 1 && rect.left >= bar.left - 1 && rect.right <= bar.right + 1 &&
              button.scrollWidth <= button.clientWidth + 1 && rect.height >= 44
          })
        })
        expect(fits).toBe(true)
        const closed = picker.getByRole('checkbox')
        await expect(closed).not.toBeChecked()
        await closed.tap()
        await expect(picker.getByRole('option', { name: /Archived preview/ })).toBeVisible()
        await artifact.tap()
        await expect(artifact).toHaveAttribute('aria-pressed', 'true')
        await expect(closed).toBeHidden()
        await expect(input).toBeFocused()
        await page.keyboard.insertText('check')
        await expect(input).toHaveValue('@check')
        await expect(picker.getByRole('option', { name: /Checklist\.md/ })).toBeVisible()
        await expect(picker.getByTestId('file-type-icon')).toContainText('MD')
        await page.setViewportSize({ width, height: 400 })
        await expect.poll(() => picker.evaluate(el => {
          const panel = el.getBoundingClientRect()
          const field = document.querySelector('[data-testid="solo-input"]')!.getBoundingClientRect()
          return panel.top >= 0 && panel.bottom <= field.top && panel.left >= 0 && panel.right <= window.innerWidth
        })).toBe(true)
        await assertViewportContract(page)
        await input.press('ArrowDown')
        await input.press('Enter')
        await expect(input).toHaveValue('@Checklist.md ')
        await expect(picker).toBeHidden()
        await page.keyboard.insertText('@')
        await expect(picker).toBeVisible()
        const picked = page.getByTestId('solo-composer-references')
        await expect.poll(async () => {
          const panel = await picker.boundingBox()
          const chips = await picked.boundingBox()
          return !!panel && !!chips && panel.y + panel.height <= chips.y
        }).toBe(true)
        await input.press('Escape')
        await input.fill('')
      }
    }
  })

  test('home references follow the caret when only the visual viewport gets shorter', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await installWorkspaceApiDefaults(page)
    await routeApi(page, '**/api/projects', route => route.fulfill({ json: [] }))
    const pageErrors: string[] = []
    page.on('pageerror', error => pageErrors.push(error.message))
    await page.addInitScript(() => localStorage.setItem('grasp.home.mode', 'solo'))
    await routeApi(page, '**/api/solo/references/search?*', route => {
      const lane = new URL(route.request().url()).searchParams.get('lane')
      const items = lane === 'artifact' ? [{ kind: 'artifact', target: 'doc', title: 'Review.md', fileKind: 'markdown' }] : []
      return route.fulfill({ json: { lane, items, total: items.length } })
    })
    await page.goto('/workspace-config.html?view=/dashboard&lang=en')
    const input = page.getByTestId('home-composer-input')
    const picker = page.getByTestId('solo-reference-picker')
    await input.fill('@')
    await expect(picker).toHaveClass(/ref-picker--down/)
    const shortHeight = await picker.evaluate(el => {
      const height = Math.ceil(el.getBoundingClientRect().top) + 35
      Object.defineProperty(window.visualViewport!, 'height', { configurable: true, value: height })
      window.visualViewport!.dispatchEvent(new Event('resize'))
      return height
    })
    await expect(picker).not.toHaveClass(/ref-picker--down/)
    await picker.getByTestId('solo-reference-tab-artifact').tap()
    const bounds = (await picker.boundingBox())!
    expect(bounds.y).toBeGreaterThanOrEqual(0)
    expect(bounds.y + bounds.height).toBeLessThanOrEqual(shortHeight)
    await picker.getByRole('option', { name: /Review\.md/ }).tap()
    await expect(input).toBeFocused()
    await expect(input).toHaveValue('@Review.md ')
    expect(pageErrors).toEqual([])
  })
})
