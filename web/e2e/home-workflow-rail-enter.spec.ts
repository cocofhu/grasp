/**
 * Browser acceptance: home workflow whole-rail enter (plan g1 / g2).
 * Harness: dashboard-home-chat.html
 */
import { expect, test, type Page } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { CLARIFY_CAPS } from '../src/test/capsFixtures'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const shotDir = path.join(__dirname, '../../.tmp-rail-enter-shots')

function approveWorkflow(id = 'wf-approve', name = '自我迭代PRO') {
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
      { id: 'out', type: 'output', label: '结束', position: { x: 0, y: 0 }, config: {} },
    ],
    edges: [
      { id: 'e1', source: 'in', target: 'ap' },
      { id: 'e2', source: 'ap', target: 'out' },
    ],
  }
}

async function mockWorkspace(page: Page) {
  await page.route('**/api/workspace', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ name: '工作区', description: '', variables: [] }),
      })
      return
    }
    await route.continue()
  })
}

test.describe('首页工作流整轨进场', () => {
  test('窄屏切换工作流与 Solo：顶部在过渡期间保持原位', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 700 })
    await page.addInitScript(() => localStorage.setItem('grasp.home.mode', 'workflow'))
    await mockWorkspace(page)
    await page.route(/\/api\/(workflows|agents|projects|solo\/sessions)(\?.*)?$/, async (route) => {
      await route.fulfill({
        json: route.request().url().includes('/solo/sessions') ? { sessions: [], total: 0 } : [],
      })
    })
    await page.goto('/dashboard-home-chat.html')
    // Match the real shell's bounded height; the harness wrapper is otherwise auto-sized.
    await page.addStyleTag({ content: '#app > div { height: 100%; }' })
    await expect(page.getByTestId('home-new-workflow')).toBeVisible()
    const brand = page.getByTestId('home-brand-block')
    const before = await brand.evaluate((element) => {
      const root = element.closest('.home-shell__content')!
      return {
        top: element.getBoundingClientRect().top,
        inset: element.getBoundingClientRect().top - root.getBoundingClientRect().top,
      }
    })
    // Exercise the compressed inset, where the mobile 68px basis must not override the lock.
    expect(before.inset).toBeLessThan(68)
    for (const mode of ['solo', 'workflow']) {
      const button = page.getByTestId(`home-mode-${mode}`)
      const positions = await button.evaluate((element) => new Promise<number[]>((resolve) => {
        const brand = document.querySelector('[data-testid="home-brand-block"]')!
        const values: number[] = []
        const start = performance.now()
        ;(element as HTMLButtonElement).click()
        function sample() {
          values.push(brand.getBoundingClientRect().top)
          if (performance.now() - start < 600) requestAnimationFrame(sample)
          else resolve(values)
        }
        requestAnimationFrame(sample)
      }))
      await expect(button).toHaveAttribute('aria-selected', 'true')
      expect(Math.max(...positions.map((top) => Math.abs(top - before.top)))).toBeLessThan(0.5)
    }
  })

  test('延迟加载：等待空白无加载文案，成功后立刻 ready 且含加卡', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await mockWorkspace(page)

    let release!: () => void
    const gate = new Promise<void>((r) => {
      release = r
    })

    await page.route('**/api/workflows**', async (route) => {
      if (route.request().method() !== 'GET') {
        await route.continue()
        return
      }
      await gate
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([approveWorkflow()]),
      })
    })

    await page.goto('/dashboard-home-chat.html')
    await expect(page.getByTestId('dashboard-view')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('home-composer')).toBeVisible()

    // g1.1 — blank rail while loading
    await expect(page.getByTestId('home-workflows-loading')).toHaveCount(0)
    await expect(page.getByTestId('home-workflow-enter')).toHaveCount(0)
    await expect(page.getByTestId('home-new-workflow')).toHaveCount(0)
    await expect(page.getByText('加载中')).toHaveCount(0)
    await page.screenshot({ path: path.join(shotDir, '01-loading-blank.png'), fullPage: true })

    release()
    const enter = page.getByTestId('home-workflow-enter')
    await expect(enter).toBeVisible({ timeout: 10_000 })
    await expect(enter).toHaveClass(/home-workflow-enter--ready/)
    await expect(page.getByTestId('home-workflow-card-wf-approve')).toBeVisible()
    await expect(page.getByTestId('home-new-workflow')).toBeVisible()

    // mid-animation + settled frames
    await page.waitForTimeout(120)
    await page.screenshot({ path: path.join(shotDir, '02-enter-ready.png'), fullPage: true })
    await page.waitForTimeout(400)
    await page.screenshot({ path: path.join(shotDir, '03-enter-settled.png'), fullPage: true })
  })

  test('空列表：仅新建卡立刻进场', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await mockWorkspace(page)
    await page.route('**/api/workflows**', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([]),
        })
        return
      }
      await route.continue()
    })

    await page.goto('/dashboard-home-chat.html')
    const enter = page.getByTestId('home-workflow-enter')
    await expect(enter).toBeVisible({ timeout: 10_000 })
    await expect(enter).toHaveClass(/home-workflow-enter--ready/)
    await expect(page.getByTestId('home-workflows-empty')).toHaveCount(0)
    await expect(page.getByTestId('home-go-workflows')).toHaveCount(0)
    await expect(page.getByTestId('home-new-workflow')).toBeVisible()
    await page.screenshot({ path: path.join(shotDir, '04-empty-rail.png'), fullPage: true })
  })

  test('失败后重试成功：立刻揭示整轨', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await mockWorkspace(page)
    let failOnce = true
    await page.route('**/api/workflows**', async (route) => {
      if (route.request().method() !== 'GET') {
        await route.continue()
        return
      }
      if (failOnce) {
        failOnce = false
        await route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom"}' })
        return
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([approveWorkflow()]),
      })
    })

    await page.goto('/dashboard-home-chat.html')
    await expect(page.getByTestId('dashboard-load-error')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('home-workflow-enter')).toHaveCount(0)
    await page.screenshot({ path: path.join(shotDir, '05-load-error.png'), fullPage: true })

    await page.getByTestId('dashboard-retry').click()
    const enter = page.getByTestId('home-workflow-enter')
    await expect(enter).toBeVisible({ timeout: 10_000 })
    await expect(enter).toHaveClass(/home-workflow-enter--ready/)
    await expect(page.getByTestId('home-workflow-card-wf-approve')).toBeVisible()
    await page.screenshot({ path: path.join(shotDir, '06-retry-success.png'), fullPage: true })
  })

  test('多卡同组：8 张卡与加卡在同一 enter 容器', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await mockWorkspace(page)
    const many = Array.from({ length: 8 }, (_, i) =>
      approveWorkflow(`wf-many-${i}`, `工作流 ${i + 1}`),
    )
    await page.route('**/api/workflows**', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(many),
        })
        return
      }
      await route.continue()
    })

    await page.goto('/dashboard-home-chat.html')
    const enter = page.getByTestId('home-workflow-enter')
    await expect(enter).toBeVisible({ timeout: 10_000 })
    await expect(enter).toHaveClass(/home-workflow-enter--ready/)
    for (let i = 0; i < 8; i++) {
      await expect(enter.getByTestId(`home-workflow-card-wf-many-${i}`)).toBeVisible()
    }
    await expect(enter.getByTestId('home-new-workflow')).toBeVisible()
    await page.waitForTimeout(450)
    await page.screenshot({ path: path.join(shotDir, '07-many-cards.png'), fullPage: true })
  })
})
