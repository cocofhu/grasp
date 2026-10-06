import { test, expect, type Page, type Route } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { dismissOnboardingIfOpen, seedOnboardingDismissed } from './helpers/onboarding'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const SHOT_DIR = path.join(__dirname, '..', 'test-results', 'pm-tail-window')

const MOCK_PROJECT = {
  id: 'proj-1',
  name: 'Demo Project',
  description: 'Project for e2e',
  variables: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

function buildMessages(total: number) {
  return Array.from({ length: total }, (_, i) => ({
    id: `m-${String(i + 1).padStart(3, '0')}`,
    role: i % 2 === 0 ? 'user' : 'assistant',
    content: `历史消息 #${i + 1}：`.padEnd(60, '内容'),
    status: 'ok',
  }))
}

async function stubCommonApis(page: Page) {
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
  await page.route('**/api/runs**', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: [], total: 0, page: 1, pageSize: 20, hasMore: false }),
      })
      return
    }
    await route.continue()
  })
  await page.route('**/api/projects/proj-1/pm-leader', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        enabled: true,
        agentAvailable: true,
        agentConfigRef: 'agent-1',
      }),
    })
  })
  await page.route('**/api/projects/proj-1/pm/threads/*/draft**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ draft: null, live: false, hasFinal: false }),
    })
  })
  await page.route('**/api/projects/proj-1/pm/threads/*/sandbox**', async (route) => {
    if (route.request().method() === 'POST') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ sandbox: { id: 1, status: 'running' }, preamble: '' }),
      })
      return
    }
    await route.continue()
  })
  await page.route('**/api/sandboxes/1', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ id: 1, status: 'running' }),
    })
  })
}

async function gotoPmLeader(page: Page) {
  await page.setViewportSize({ width: 1280, height: 800 })
  await seedOnboardingDismissed(page, 'proj-1')
  await stubCommonApis(page)
  await page.goto('/project-detail.html?tab=pmLeader')
  await dismissOnboardingIfOpen(page)
  await expect(page.getByRole('heading', { name: 'Demo Project' })).toBeVisible({ timeout: 15_000 })
  await expect(page.getByTestId('pm-message-scroller')).toBeVisible({ timeout: 15_000 })
}

async function stubLongThreadMessages(page: Page, all: ReturnType<typeof buildMessages>) {
  const messageGets: { limit: string | null; before: string | null }[] = []

  await page.route('**/api/projects/proj-1/pm/threads**', async (route: Route) => {
    const url = new URL(route.request().url())
    if (route.request().method() === 'GET' && !url.pathname.includes('/messages')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          items: [{ id: 'thr-long', title: '长会话', userId: 'alice' }],
        }),
      })
      return
    }
    await route.continue()
  })

  await page.route('**/api/projects/proj-1/pm/threads/*/messages**', async (route: Route) => {
    if (route.request().method() !== 'GET') {
      await route.continue()
      return
    }
    const url = new URL(route.request().url())
    const limitRaw = url.searchParams.get('limit')
    const before = url.searchParams.get('before')
    messageGets.push({ limit: limitRaw, before })

    if (!limitRaw) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: all }),
      })
      return
    }

    const limit = Number(limitRaw)
    if (!before) {
      const items = all.slice(Math.max(0, all.length - limit))
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items, hasMore: all.length > limit }),
      })
      return
    }

    const idx = all.findIndex((m) => m.id === before)
    const older = idx > 0 ? all.slice(0, idx) : []
    const pageItems = older.slice(Math.max(0, older.length - limit))
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ items: pageItems, hasMore: older.length > limit }),
    })
  })

  return messageGets
}

test.describe('PM Leader tail window + scroll-up lazyload', () => {
  test('长会话首屏只拉最近 20 条并默认滚底', async ({ page }) => {
    const all = buildMessages(45)
    const messageGets = await stubLongThreadMessages(page, all)
    await gotoPmLeader(page)

    await expect.poll(() => messageGets.length).toBeGreaterThanOrEqual(1)
    expect(messageGets[0].limit).toBe('20')
    expect(messageGets[0].before).toBeNull()

    await expect(page.locator('[data-msg-id]')).toHaveCount(20)
    await expect(page.getByTestId('pm-history-tip')).toContainText('上滑加载更早')
    await expect(page.locator('[data-msg-id="m-045"]')).toBeAttached()
    await expect(page.locator('[data-msg-id="m-026"]')).toBeAttached()
    await expect(page.locator('[data-msg-id="m-001"]')).toHaveCount(0)

    const scroller = page.getByTestId('pm-message-scroller')
    await page.screenshot({ path: path.join(SHOT_DIR, '01-entry-should-be-bottom.png'), fullPage: false })
    // Wait for loading pane → content swap; scrollBottom must survive that transition.
    await expect
      .poll(
        async () =>
          scroller.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight),
        { timeout: 2500 },
      )
      .toBeLessThanOrEqual(8)
  })

  test('触顶自动 prepend 更早 20 条直至最早', async ({ page }) => {
    const all = buildMessages(45)
    const messageGets = await stubLongThreadMessages(page, all)
    await gotoPmLeader(page)

    await expect(page.locator('[data-msg-id]')).toHaveCount(20)
    const scroller = page.getByTestId('pm-message-scroller')
    await scroller.evaluate((el) => {
      el.scrollTop = 0
      el.dispatchEvent(new Event('scroll'))
    })

    await expect.poll(() => messageGets.some((g) => g.before === 'm-026')).toBeTruthy()
    await expect(page.locator('[data-msg-id]')).toHaveCount(40)
    await expect(page.locator('[data-msg-id="m-006"]')).toBeVisible()
    await expect(page.getByTestId('pm-history-tip')).toContainText('上滑加载更早')
    await page.screenshot({ path: path.join(SHOT_DIR, '02-after-lazyload-prepend.png'), fullPage: false })

    await scroller.evaluate((el) => {
      el.scrollTop = 0
      el.dispatchEvent(new Event('scroll'))
    })
    await expect(page.locator('[data-msg-id]')).toHaveCount(45)
    await expect(page.getByTestId('pm-history-tip')).toContainText('已到最早')
    await page.screenshot({ path: path.join(SHOT_DIR, '03-reached-earliest.png'), fullPage: false })
  })

  test('短会话一次加载全部，触顶不再请求', async ({ page }) => {
    const short = buildMessages(8)
    const messageGets: string[] = []

    await page.route('**/api/projects/proj-1/pm/threads**', async (route: Route) => {
      const url = new URL(route.request().url())
      if (route.request().method() === 'GET' && !url.pathname.includes('/messages')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            items: [{ id: 'thr-short', title: '短会话', userId: 'alice' }],
          }),
        })
        return
      }
      await route.continue()
    })

    await page.route('**/api/projects/proj-1/pm/threads/*/messages**', async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue()
        return
      }
      messageGets.push(route.request().url())
      const url = new URL(route.request().url())
      const limitRaw = url.searchParams.get('limit')
      const items = short
      if (!limitRaw) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ items }),
        })
        return
      }
      const limit = Number(limitRaw)
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          items: items.slice(Math.max(0, items.length - limit)),
          hasMore: items.length > limit,
        }),
      })
    })

    await gotoPmLeader(page)
    await expect(page.locator('[data-msg-id]')).toHaveCount(8)
    await expect(page.getByTestId('pm-history-tip')).toContainText('已到最早')
    expect(messageGets).toHaveLength(1)

    const scroller = page.getByTestId('pm-message-scroller')
    await scroller.evaluate((el) => {
      el.scrollTop = 0
      el.dispatchEvent(new Event('scroll'))
    })
    await page.waitForTimeout(300)
    expect(messageGets).toHaveLength(1)
    await page.screenshot({ path: path.join(SHOT_DIR, '04-short-session.png'), fullPage: false })
  })

  test('lazyload 失败可再次触顶重试', async ({ page }) => {
    const all = buildMessages(30)
    let earlierCalls = 0

    await page.route('**/api/projects/proj-1/pm/threads**', async (route: Route) => {
      const url = new URL(route.request().url())
      if (route.request().method() === 'GET' && !url.pathname.includes('/messages')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            items: [{ id: 'thr-long', title: '长会话', userId: 'alice' }],
          }),
        })
        return
      }
      await route.continue()
    })

    await page.route('**/api/projects/proj-1/pm/threads/*/messages**', async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue()
        return
      }
      const url = new URL(route.request().url())
      const limitRaw = url.searchParams.get('limit')
      const before = url.searchParams.get('before')
      if (!limitRaw) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ items: all }),
        })
        return
      }
      const limit = Number(limitRaw)
      if (!before) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            items: all.slice(Math.max(0, all.length - limit)),
            hasMore: all.length > limit,
          }),
        })
        return
      }
      earlierCalls += 1
      if (earlierCalls === 1) {
        await route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom"}' })
        return
      }
      const idx = all.findIndex((m) => m.id === before)
      const older = idx > 0 ? all.slice(0, idx) : []
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          items: older.slice(Math.max(0, older.length - limit)),
          hasMore: older.length > limit,
        }),
      })
    })

    await gotoPmLeader(page)
    await expect(page.locator('[data-msg-id]')).toHaveCount(20)

    const scroller = page.getByTestId('pm-message-scroller')
    await scroller.evaluate((el) => {
      el.scrollTop = 0
      el.dispatchEvent(new Event('scroll'))
    })
    await expect(page.getByTestId('pm-history-tip')).toContainText('再次滚到顶部可重试')
    await expect(page.locator('[data-msg-id]')).toHaveCount(20)
    await page.screenshot({ path: path.join(SHOT_DIR, '05-lazyload-fail.png'), fullPage: false })

    await scroller.evaluate((el) => {
      el.scrollTop = 200
      el.dispatchEvent(new Event('scroll'))
    })
    await page.waitForTimeout(80)
    await scroller.evaluate((el) => {
      el.scrollTop = 0
      el.dispatchEvent(new Event('scroll'))
    })
    await expect(page.locator('[data-msg-id]')).toHaveCount(30)
    await expect(page.getByTestId('pm-history-tip')).toContainText('已到最早')
    await page.screenshot({ path: path.join(SHOT_DIR, '06-lazyload-retry-ok.png'), fullPage: false })
  })
})
