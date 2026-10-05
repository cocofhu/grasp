import { expect, test, type Page } from '@playwright/test'
import { CLARIFY_CAPS } from '../src/test/capsFixtures'

function wf(id: string, name: string) {
  return {
    id,
    name,
    description: '开发前澄清 + 计划',
    status: 'published',
    version: 1,
    updatedAt: '2026-08-10T12:00:00Z',
    needsRepo: false,
    showOnHome: true,
    nodes: [
      { id: 'in', type: 'input', label: '开始', position: { x: 0, y: 0 }, config: {} },
      { id: 'ap', type: 'agent', caps: CLARIFY_CAPS, label: '澄清', position: { x: 0, y: 0 }, config: {} },
    ],
    edges: [{ id: 'e1', source: 'in', target: 'ap' }],
  }
}

const visible = [wf('wf-approve', '自我迭代PRO'), wf('wf-lite', '快速澄清 Lite')]
let hiddenIds = new Set<string>()
let patchFail = false
const patchCalls: { id: string; body: unknown }[] = []

async function mockHomeApis(page: Page) {
  hiddenIds = new Set()
  patchFail = false
  patchCalls.length = 0

  await page.route('**/api/workflows**', async (route) => {
    const req = route.request()
    const url = req.url()
    if (req.method() === 'PATCH' && url.includes('/home-visibility')) {
      const id = url.split('/workflows/')[1]?.split('/')[0] || ''
      const body = req.postDataJSON()
      patchCalls.push({ id, body })
      if (patchFail) {
        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'patch failed' }),
        })
        return
      }
      hiddenIds.add(id)
      const found = visible.find((w) => w.id === id)
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ...found, showOnHome: false }),
      })
      return
    }
    if (req.method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(visible.filter((w) => !hiddenIds.has(w.id))),
      })
      return
    }
    await route.continue()
  })

  await page.route('**/api/projects**', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: [], total: 0 }),
      })
      return
    }
    await route.continue()
  })
}

async function openDashboard(page: Page) {
  await mockHomeApis(page)
  await page.goto('/dashboard-home-chat.html?memory=1&projectId=proj-1')
  await expect(page.getByTestId('dashboard-view')).toBeVisible({ timeout: 15_000 })
  await expect(page.getByTestId('home-workflow-card-wf-approve')).toBeVisible()
}

test.describe('首页工作流卡片菜单', () => {
  test('右键打开带 SVG 的隐藏/编辑菜单', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await openDashboard(page)
    await page.getByTestId('home-workflow-card-wf-approve').click({ button: 'right' })
    const menu = page.getByTestId('home-workflow-menu')
    await expect(menu).toBeVisible()
    await expect(page.getByTestId('home-workflow-menu-hide')).toContainText('隐藏')
    await expect(page.getByTestId('home-workflow-menu-edit')).toContainText('编辑')
    await expect(page.getByTestId('home-workflow-menu-hide').locator('svg')).toHaveCount(1)
    await expect(page.getByTestId('home-workflow-menu-edit').locator('svg')).toHaveCount(1)
    await page.screenshot({ path: '/tmp/e2e-menu-contextmenu.png' })
  })

  test('more 图标打开同一菜单且不改选中，编辑跳转编辑器', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await openDashboard(page)
    await expect(page.getByTestId('home-workflow-card-wf-approve')).toHaveClass(/home-shell__card--selected/)
    await page.getByTestId('home-workflow-more-wf-lite').click()
    await expect(page.getByTestId('home-workflow-menu')).toBeVisible()
    await expect(page.getByTestId('home-workflow-card-wf-approve')).toHaveClass(/home-shell__card--selected/)
    await page.screenshot({ path: '/tmp/e2e-menu-more.png' })
    await page.getByTestId('home-workflow-menu-edit').click()
    await expect(page.getByTestId('workflow-editor-page')).toHaveText('wf-lite')
    await page.screenshot({ path: '/tmp/e2e-edit-route.png' })
  })

  test('触摸长按约 500ms 打开菜单，滑动取消', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await openDashboard(page)
    const card = page.getByTestId('home-workflow-card-wf-approve')
    const box = await card.boundingBox()
    if (!box) throw new Error('missing card box')
    const x = box.x + box.width / 2
    const y = box.y + box.height / 2

    await page.dispatchEvent('[data-testid="home-workflow-card-wf-approve"]', 'pointerdown', {
      pointerType: 'touch',
      clientX: x,
      clientY: y,
    })
    await page.dispatchEvent('[data-testid="home-workflow-card-wf-approve"]', 'pointermove', {
      pointerType: 'touch',
      clientX: x + 40,
      clientY: y,
    })
    await page.waitForTimeout(550)
    await expect(page.getByTestId('home-workflow-menu')).toHaveCount(0)

    await page.dispatchEvent('[data-testid="home-workflow-card-wf-approve"]', 'pointerdown', {
      pointerType: 'touch',
      clientX: x,
      clientY: y,
    })
    await page.waitForTimeout(550)
    await expect(page.getByTestId('home-workflow-menu')).toBeVisible()
    await page.screenshot({ path: '/tmp/e2e-menu-longpress.png' })
  })

  test('隐藏成功后卡片离开轨道并回退选中', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await openDashboard(page)
    await page.getByTestId('home-workflow-card-wf-approve').click({ button: 'right' })
    await page.getByTestId('home-workflow-menu-hide').click()
    await expect(page.getByTestId('home-workflow-card-wf-approve')).toHaveCount(0)
    await expect(page.getByTestId('home-workflow-card-wf-lite')).toHaveClass(/home-shell__card--selected/)
    expect(patchCalls).toEqual([{ id: 'wf-approve', body: { showOnHome: false } }])
    await page.screenshot({ path: '/tmp/e2e-after-hide.png' })
  })

  test('隐藏失败时卡片仍在', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await openDashboard(page)
    patchFail = true
    await page.getByTestId('home-workflow-card-wf-approve').click({ button: 'right' })
    await page.getByTestId('home-workflow-menu-hide').click()
    await expect(page.getByTestId('home-workflow-card-wf-approve')).toBeVisible()
    await expect(page.getByTestId('home-workflow-card-wf-lite')).toBeVisible()
    await page.screenshot({ path: '/tmp/e2e-hide-fail.png' })
  })
})
