import { test, expect, type Page } from '@playwright/test'

const BOUND_AGENT = {
  name: 'GraspPM',
  projectId: 'proj-default',
  acpBackend: 'codebuddy',
  files: [
    { path: 'AGENTS.md', content: '# demo' },
    { path: 'skills/demo.md', content: 'skill' },
  ],
  mcp: [{ name: 'artifact-store', command: 'npx', args: [] }],
  env: {
    A: '1',
    B: '2',
    C: '3',
    D: '4',
    E: '5',
    F: '6',
    G: '7',
    H: '8',
  },
  layout: { configRoot: '/root/.codebuddy', workspaceDir: '/root/workspace' },
}

const SAMPLE_JOB = {
  id: 'cron-1',
  name: '每日汇报',
  scheduleKind: 'cron',
  scheduleExpr: '0 9 * * *',
  enabled: true,
  deliverToChannel: false,
  nextRunAt: '2026-07-26T01:00:00Z',
}

async function mockStudioApi(page: Page) {
  const agent = BOUND_AGENT
  let memories = [
    {
      id: 'm1',
      title: '项目约定',
      content: '窄屏数据 Tab 应可用',
      updatedBy: 'tester',
      updatedAt: '2026-07-25T00:00:00Z',
    },
  ]
  let threads = [
    {
      id: 't1',
      title: '会话线程 A',
      kind: 'user',
      updatedAt: '2026-07-25T00:00:00Z',
    },
  ]
  let jobs = [{ ...SAMPLE_JOB }]

  await page.route('**/api/**', async (route) => {
    // Skip Vite module URLs like /@fs/.../src/lib/api/api.ts (pathname is not /api/...)
    if (!new URL(route.request().url()).pathname.startsWith('/api/')) {
      await route.continue()
      return
    }
    const req = route.request()
    const url = new URL(req.url())
    const path = url.pathname.replace(/^\/api/, '')
    const method = req.method()

    if (path === '/auth/me' || path === '/health' || path === '/live') {
      await route.fulfill({ json: { username: 'e2e', expiresAt: '2099-01-01T00:00:00Z', isAdmin: false } })
      return
    }
    if (path === '/projects' && method === 'GET') {
      await route.fulfill({ json: [{ id: 'proj-default', name: 'Default' }] })
      return
    }
    if (path === '/agents' && method === 'GET') {
      await route.fulfill({ json: [agent] })
      return
    }
    if (path === `/agents/${encodeURIComponent(agent.name)}` && method === 'PUT') {
      await route.fulfill({ json: { status: 'ok' } })
      return
    }

    const memList = path.match(/^\/agents\/[^/]+\/memories$/)
    if (memList && method === 'GET') {
      await route.fulfill({ json: { items: memories } })
      return
    }
    if (memList && method === 'POST') {
      const body = req.postDataJSON() as { title: string; content: string }
      const item = {
        id: `m-${memories.length + 1}`,
        title: body.title,
        content: body.content,
        updatedBy: 'e2e',
        updatedAt: '2026-07-25T01:00:00Z',
      }
      memories = [item, ...memories]
      await route.fulfill({ json: item })
      return
    }
    const memOne = path.match(/^\/agents\/[^/]+\/memories\/([^/]+)$/)
    if (memOne && method === 'PUT') {
      const id = memOne[1]
      const body = req.postDataJSON() as { title?: string; content?: string }
      memories = memories.map((m) =>
        m.id === id ? { ...m, title: body.title ?? m.title, content: body.content ?? m.content } : m,
      )
      await route.fulfill({ json: memories.find((m) => m.id === id) })
      return
    }
    if (memOne && method === 'DELETE') {
      memories = memories.filter((m) => m.id !== memOne[1])
      await route.fulfill({ json: { status: 'deleted' } })
      return
    }

    const threadList = path.match(/^\/agents\/[^/]+\/threads$/)
    if (threadList && method === 'GET') {
      await route.fulfill({ json: { items: threads, messageCounts: { t1: 2 } } })
      return
    }
    const threadOne = path.match(/^\/agents\/[^/]+\/threads\/([^/]+)$/)
    if (threadOne && method === 'DELETE') {
      threads = threads.filter((t) => t.id !== threadOne[1])
      await route.fulfill({ json: { status: 'deleted' } })
      return
    }
    if (path.match(/^\/agents\/[^/]+\/threads\/[^/]+\/messages$/) && method === 'GET') {
      await route.fulfill({
        json: {
          items: [
            { id: 'msg1', role: 'user', content: 'hello', createdAt: '2026-07-25T00:00:00Z' },
          ],
          total: 1,
        },
      })
      return
    }

    const cronList = path.match(/^\/agents\/[^/]+\/cron-jobs$/)
    if (cronList && method === 'GET') {
      await route.fulfill({ json: { items: jobs } })
      return
    }
    const cronOne = path.match(/^\/agents\/[^/]+\/cron-jobs\/([^/]+)$/)
    if (cronOne && method === 'PATCH') {
      const id = cronOne[1]
      const body = req.postDataJSON() as { enabled?: boolean; deliverToChannel?: boolean }
      jobs = jobs.map((j) => (j.id === id ? { ...j, ...body } : j))
      await route.fulfill({ json: jobs.find((j) => j.id === id) })
      return
    }
    if (cronOne && method === 'DELETE') {
      jobs = jobs.filter((j) => j.id !== cronOne[1])
      await route.fulfill({ json: { status: 'deleted' } })
      return
    }

    await route.fulfill({ status: 404, json: { error: `not mocked: ${method} ${path}` } })
  })
}

test.describe('Agent Studio 窄屏数据 Tab', () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test('已绑定：数据三子 Tab 可用，MCP 仍桌面完成', async ({ page }) => {
    await mockStudioApi(page)
    await page.goto('/agent-studio-mobile-data.html?agent=GraspPM&studioTab=data&sub=memory')
    await expect(page.getByTestId('agent-studio-mobile-data-root')).toBeVisible({ timeout: 15_000 })

    await expect(page.getByText('建议在桌面使用')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '记忆', exact: true })).toBeVisible()
    await expect(page.getByText('项目约定')).toBeVisible()

    await page.getByRole('button', { name: '上下文', exact: true }).click()
    await expect(page.getByText('会话线程 A')).toBeVisible()

    await page.getByRole('button', { name: '定时任务', exact: true }).click()
    await expect(page.getByTestId('agent-cron-mobile-cards')).toBeVisible()
    await expect(page.getByTestId('agent-cron-desktop-table')).toHaveCount(0)
    await expect(page.getByText('每日汇报')).toBeVisible()
    await expect(page.getByText('推送到渠道')).toBeVisible()
    // No cron create control inside data/jobs (header「新建 Agent」is out of scope)
    await expect(page.getByTestId('agent-cron-create')).toHaveCount(0)
    await expect(page.getByTestId('agent-cron-mobile-cards').getByRole('button', { name: /新建/ })).toHaveCount(0)

    const deliver = page.getByTestId('agent-cron-deliver')
    await deliver.click()
    await expect(deliver).toHaveAttribute('aria-checked', 'true')

    await page.getByRole('button', { name: /^MCP/ }).click()
    await expect(page.getByText('建议在桌面使用')).toBeVisible()
    await expect(page.getByTestId('studio-mobile-back-files')).toBeVisible()
  })

  test('深链 sub=jobs 直达卡片列表', async ({ page }) => {
    await mockStudioApi(page)
    await page.goto('/agent-studio-mobile-data.html?agent=GraspPM&studioTab=data&sub=jobs')
    await expect(page.getByTestId('agent-cron-mobile-cards')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByText('建议在桌面使用')).toHaveCount(0)
    await expect(page.getByText('每日汇报')).toBeVisible()
  })

  test('记忆增删在窄屏可用', async ({ page }) => {
    await mockStudioApi(page)
    await page.goto('/agent-studio-mobile-data.html?agent=GraspPM&studioTab=data&sub=memory')
    await expect(page.getByText('项目约定')).toBeVisible({ timeout: 15_000 })

    await page.getByPlaceholder('标题').fill('新记忆')
    await page.getByPlaceholder('内容').fill('e2e 写入')
    await page.getByRole('button', { name: '添加记忆' }).click()
    await expect(page.getByText('新记忆')).toBeVisible()

    const card = page.locator('.rounded.border').filter({ hasText: '项目约定' })
    await card.getByRole('button', { name: '删除' }).click()
    await expect(page.getByText('项目约定')).toHaveCount(0)
  })
})
