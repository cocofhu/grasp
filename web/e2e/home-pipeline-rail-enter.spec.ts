/**
 * Browser acceptance: home pipeline whole-rail enter (plan g1 / g2).
 * Harness: dashboard-home-chat.html
 */
import { expect, test, type Page } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

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
    projectId: 'proj-1',
    nodes: [
      { id: 'in', type: 'input', label: '开始', position: { x: 0, y: 0 }, config: {} },
      { id: 'ap', type: 'approve', label: '澄清', position: { x: 0, y: 0 }, config: {} },
      { id: 'out', type: 'output', label: '结束', position: { x: 0, y: 0 }, config: {} },
    ],
    edges: [
      { id: 'e1', source: 'in', target: 'ap' },
      { id: 'e2', source: 'ap', target: 'out' },
    ],
  }
}

async function mockProjects(page: Page) {
  await page.route('**/api/projects**', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          items: [{ id: 'proj-1', name: '综合项目组', description: '', variables: [] }],
          total: 1,
        }),
      })
      return
    }
    await route.continue()
  })
}

test.describe('首页流水线整轨进场', () => {
  test('延迟加载：等待空白无加载文案，成功后立刻 ready 且含加卡', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await mockProjects(page)

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
    await expect(page.getByTestId('home-pipelines-loading')).toHaveCount(0)
    await expect(page.getByTestId('home-pipeline-enter')).toHaveCount(0)
    await expect(page.getByTestId('home-new-workflow')).toHaveCount(0)
    await expect(page.getByText('加载中')).toHaveCount(0)
    await page.screenshot({ path: path.join(shotDir, '01-loading-blank.png'), fullPage: true })

    release()
    const enter = page.getByTestId('home-pipeline-enter')
    await expect(enter).toBeVisible({ timeout: 10_000 })
    await expect(enter).toHaveClass(/home-pipeline-enter--ready/)
    await expect(page.getByTestId('home-pipeline-card-wf-approve')).toBeVisible()
    await expect(page.getByTestId('home-new-workflow')).toBeVisible()

    // mid-animation + settled frames
    await page.waitForTimeout(120)
    await page.screenshot({ path: path.join(shotDir, '02-enter-ready.png'), fullPage: true })
    await page.waitForTimeout(400)
    await page.screenshot({ path: path.join(shotDir, '03-enter-settled.png'), fullPage: true })
  })

  test('空列表：空态与加卡同组立刻进场', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await mockProjects(page)
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
    const enter = page.getByTestId('home-pipeline-enter')
    await expect(enter).toBeVisible({ timeout: 10_000 })
    await expect(enter).toHaveClass(/home-pipeline-enter--ready/)
    await expect(page.getByTestId('home-pipelines-empty')).toBeVisible()
    await expect(page.getByTestId('home-new-workflow')).toBeVisible()
    await page.screenshot({ path: path.join(shotDir, '04-empty-rail.png'), fullPage: true })
  })

  test('失败后重试成功：立刻揭示整轨', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await mockProjects(page)
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
    await expect(page.getByTestId('home-pipeline-enter')).toHaveCount(0)
    await page.screenshot({ path: path.join(shotDir, '05-load-error.png'), fullPage: true })

    await page.getByTestId('dashboard-retry').click()
    const enter = page.getByTestId('home-pipeline-enter')
    await expect(enter).toBeVisible({ timeout: 10_000 })
    await expect(enter).toHaveClass(/home-pipeline-enter--ready/)
    await expect(page.getByTestId('home-pipeline-card-wf-approve')).toBeVisible()
    await page.screenshot({ path: path.join(shotDir, '06-retry-success.png'), fullPage: true })
  })

  test('多卡同组：8 张卡与加卡在同一 enter 容器', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await mockProjects(page)
    const many = Array.from({ length: 8 }, (_, i) =>
      approveWorkflow(`wf-many-${i}`, `流水线 ${i + 1}`),
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
    const enter = page.getByTestId('home-pipeline-enter')
    await expect(enter).toBeVisible({ timeout: 10_000 })
    await expect(enter).toHaveClass(/home-pipeline-enter--ready/)
    for (let i = 0; i < 8; i++) {
      await expect(enter.getByTestId(`home-pipeline-card-wf-many-${i}`)).toBeVisible()
    }
    await expect(enter.getByTestId('home-new-workflow')).toBeVisible()
    await page.waitForTimeout(450)
    await page.screenshot({ path: path.join(shotDir, '07-many-cards.png'), fullPage: true })
  })
})
