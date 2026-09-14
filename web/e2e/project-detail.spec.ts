import { test, expect } from '@playwright/test'

const MOCK_PROJECT = {
  id: 'proj-1',
  name: 'Demo Project',
  description: 'Project for e2e',
  variables: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

const MOCK_WORKFLOWS = [
  {
    id: 'wf-1',
    name: 'Alpha Pipeline',
    description: 'First workflow',
    status: 'published',
    version: 2,
    updatedAt: '2026-01-01T00:00:00Z',
    lastRunAt: '2026-01-02T00:00:00Z',
    needsRepo: false,
    nodes: [],
    edges: [],
  },
  {
    id: 'wf-2',
    name: 'Beta Pipeline',
    description: '',
    status: 'draft',
    version: 1,
    updatedAt: '2026-01-03T00:00:00Z',
    needsRepo: false,
    nodes: [],
    edges: [],
  },
  {
    id: 'wf-3',
    name: 'Gamma Pipeline',
    description: 'Last row for overflow check',
    status: 'published',
    version: 1,
    updatedAt: '2026-01-04T00:00:00Z',
    needsRepo: false,
    nodes: [],
    edges: [],
  },
]

async function gotoProjectDetail(
  page: import('@playwright/test').Page,
  opts: { width: number; height?: number; tab?: string; keepDefaultTab?: boolean } = { width: 390 },
) {
  await page.setViewportSize({ width: opts.width, height: opts.height ?? 844 })
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
        body: JSON.stringify(MOCK_WORKFLOWS),
      })
      return
    }
    await route.continue()
  })
  await page.route('**/api/runs**', async (route) => {
    if (route.request().method() === 'GET') {
      const url = new URL(route.request().url())
      const projectId = url.searchParams.get('projectId')
      const status = url.searchParams.get('status') || ''
      const items =
        projectId === 'proj-1' && status === 'running'
          ? [
              {
                id: 'run-p1',
                workflowId: 'wf-1',
                workflowName: 'Alpha Pipeline',
                title: '本项目Run',
                status: 'running',
                trigger: 'manual',
                startedAt: '2026-07-18T12:00:00Z',
                durationSec: 10,
                progress: 40,
                currentNodeLabel: '实现',
                nodeRuns: {},
                artifacts: [],
              },
            ]
          : []
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          items,
          total: items.length,
          page: 1,
          pageSize: 100,
          hasMore: false,
        }),
      })
      return
    }
    await route.continue()
  })
  await page.route('**/api/projects/proj-1/pm-leader', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ enabled: false }),
    })
  })
  await page.route('**/api/projects/proj-1/shared-agent-config', async (route) => {
    const method = route.request().method()
    if (method === 'GET' || method === 'PUT') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(MOCK_SHARED_AGENT_EMPTY),
      })
      return
    }
    await route.continue()
  })
  await page.route('**/api/agents', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([{ name: 'demo-agent', projectId: 'proj-1' }]),
      })
      return
    }
    await route.continue()
  })
  await page.route('**/api/projects/*/token-stats**', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          window: '30d',
          bucketWidth: 'day',
          timezone: 'UTC',
          empty: true,
          trend: [],
          composition: {
            inputTokens: 0,
            outputTokens: 0,
            cacheReadTokens: 0,
            cacheWriteTokens: 0,
            total: 0,
          },
          workflows: [],
        }),
      })
      return
    }
    await route.continue()
  })
  const qs = opts.tab ? `?tab=${encodeURIComponent(opts.tab)}` : ''
  await page.goto(`/project-detail.html${qs}`)
  await expect(page.getByRole('heading', { name: 'Demo Project' })).toBeVisible({ timeout: 10_000 })
  await expect(page.getByRole('button', { name: '看板' })).toBeVisible()
  await expect(page.getByRole('button', { name: '流水线' })).toBeVisible()
  // Workflow tests operate on the pipelines tab (board is now the default).
  if (!opts.keepDefaultTab && opts.tab !== 'board') {
    await page.getByRole('button', { name: '流水线' }).click()
    await expect(page.getByRole('button', { name: '导入' })).toBeVisible({ timeout: 5_000 })
  }
}

test.describe('ProjectDetailView 流水线操作列', () => {
  test('桌面：图标+文字五操作与导入/新建工具栏', async ({ page }) => {
    await gotoProjectDetail(page, { width: 1280, height: 800 })

    await expect(page.locator('article')).toHaveCount(0)
    const table = page.locator('table')
    await expect(table).toBeVisible()
    await expect(table.getByRole('columnheader', { name: '操作' })).toBeVisible()

    await expect(page.getByRole('button', { name: '导入' })).toBeVisible()
    await expect(page.getByRole('button', { name: /新建工作流/ })).toBeVisible()

    // 新建工作流与导入等高对齐，且箭头未换行撑高按钮 (plan g1.2 / g1.3 / f2)
    const importBox = await page.getByRole('button', { name: '导入' }).boundingBox()
    const newButton = page.getByTestId('new-workflow-button')
    const newBox = await newButton.boundingBox()
    expect(importBox && newBox).toBeTruthy()
    expect(newBox!.height).toBe(importBox!.height)
    expect(newBox!.y).toBe(importBox!.y)
    const contentOverflow = await newButton.evaluate((el) => ({
      scrollHeight: el.scrollHeight,
      clientHeight: el.clientHeight,
    }))
    expect(contentOverflow.scrollHeight).toBeLessThanOrEqual(contentOverflow.clientHeight + 1)
    // plus + chevron-down 两个内联图标与文字同行
    await expect(newButton.locator('svg')).toHaveCount(2)

    const actionCell = table.locator('tbody tr').first().locator('td').last()
    await expect(actionCell.getByRole('button', { name: '编辑' })).toBeVisible()
    await expect(actionCell.getByRole('button', { name: '运行' })).toBeVisible()
    await expect(actionCell.getByRole('button', { name: '复制' })).toBeVisible()
    await expect(actionCell.getByRole('button', { name: '导出' })).toBeVisible()
    await expect(actionCell.getByRole('button', { name: '删除' })).toBeVisible()
  })

  test('窄屏：卡片列表 + 运行主按钮', async ({ page }) => {
    await gotoProjectDetail(page, { width: 390 })

    const cards = page.locator('article')
    await expect(cards).toHaveCount(3)
    await expect(page.locator('table')).toHaveCount(0)

    const runBtn = cards.first().getByRole('button', { name: '运行' })
    await expect(runBtn).toBeVisible()
    const box = await runBtn.boundingBox()
    expect(box?.height).toBeGreaterThanOrEqual(44)
  })

  test('新建工作流下拉以 SVG 分流，从零直接进入空白编辑器', async ({ page }) => {
    await gotoProjectDetail(page, { width: 1280, height: 800 })

    const button = page.getByTestId('new-workflow-button')
    await button.click()
    const menu = page.getByTestId('new-workflow-menu')
    await expect(menu).toBeVisible()
    await expect(menu.locator('svg')).toHaveCount(2)
    await expect(menu).not.toContainText('⚡')

    await page.getByTestId('new-workflow-scratch').click()
    // This fixture uses Vue Router memory history, so assert the matched route
    // component instead of the unchanged browser URL.
    await expect(page.getByTestId('new-edit-page')).toBeVisible()
  })

  test('默认基线弹窗要求工作流名称和仓库 URL 后才能创建', async ({ page }) => {
    await gotoProjectDetail(page, { width: 1280, height: 800 })

    await page.getByTestId('new-workflow-button').click()
    await page.getByTestId('new-workflow-baseline').click()
    await expect(page.getByRole('dialog')).toContainText('从默认基线创建')

    const create = page.getByTestId('create-baseline-workflow')
    await expect(create).toBeDisabled()
    const workflowName = page.getByTestId('baseline-workflow-name')
    await expect(workflowName).toHaveAttribute('placeholder', '例如 需求对齐流水线')
    await expect(page.getByRole('dialog').locator('svg')).toHaveCount(3)

    await page.getByPlaceholder('仓库地址 https://…/repo.git').fill('https://github.com/acme/app.git')
    await expect(create).toBeDisabled()
    await workflowName.fill('需求对齐流水线')
    await expect(create).toBeEnabled()
    await page.getByRole('dialog').getByRole('button', { name: '添加仓库' }).click()
    await expect(page.getByPlaceholder('仓库地址 https://…/repo.git')).toHaveCount(2)

    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toHaveCount(0)
  })

  test('窄屏最后一行更多菜单四项均可见可点', async ({ page }) => {
    await gotoProjectDetail(page, { width: 390, height: 700 })

    const lastCard = page.locator('article').last()
    const more = lastCard.getByRole('button', { name: '更多操作' })
    await more.scrollIntoViewIfNeeded()
    await more.click()
    await expect(more).toHaveAttribute('aria-expanded', 'true')

    const menu = lastCard.getByRole('menu')
    await expect(menu).toBeVisible()
    await expect(menu.getByRole('menuitem', { name: '编辑' })).toBeVisible()
    await expect(menu.getByRole('menuitem', { name: '复制' })).toBeVisible()
    await expect(menu.getByRole('menuitem', { name: '导出' })).toBeVisible()
    await expect(menu.getByRole('menuitem', { name: '删除' })).toBeVisible()

    // All four items must be within the viewport (not clipped by overflow).
    for (const name of ['编辑', '复制', '导出', '删除']) {
      const item = menu.getByRole('menuitem', { name })
      await expect(item).toBeInViewport()
    }

    await menu.getByRole('menuitem', { name: '删除' }).click()
    await expect(page.getByText('删除工作流 · Gamma Pipeline')).toBeVisible({ timeout: 5_000 })
  })

  test('窄屏运行 click.stop 不误跳编辑器', async ({ page }) => {
    await gotoProjectDetail(page, { width: 390 })

    const card = page.locator('article').first()
    await card.getByRole('button', { name: '运行' }).click()
    await expect(page.getByText(/启动运行/)).toBeVisible({ timeout: 5_000 })
    await expect(page.getByTestId('edit-page')).toHaveCount(0)
  })

  test('桌面导出 Modal 可打开', async ({ page }) => {
    await gotoProjectDetail(page, { width: 1280, height: 800 })

    const actionCell = page.locator('table tbody tr').first().locator('td').last()
    await actionCell.getByRole('button', { name: '导出' }).click()
    await expect(page.getByText('导出工作流 · Alpha Pipeline')).toBeVisible({ timeout: 5_000 })
  })
})

const MOCK_PROJECT_WITH_VARS = {
  ...MOCK_PROJECT,
  variables: [
    {
      name: 'region',
      value: 'cn',
      type: 'string',
      secret: false,
      desc: '部署区域',
    },
  ],
}

const MOCK_SHARED_AGENT = {
  projectId: 'proj-1',
  acpBackend: 'cursor',
  defaultProjectId: '',
  gitCredentialType: '',
  files: [],
  mcp: [],
  env: { API_URL: 'https://example.com' },
  layout: { configRoot: '/root/.cursor', workspaceDir: '/root/workspace' },
  prompts: {},
}

const MOCK_SHARED_AGENT_EMPTY = {
  ...MOCK_SHARED_AGENT,
  env: {},
}

async function gotoProjectDetailWithProject(
  page: import('@playwright/test').Page,
  project: typeof MOCK_PROJECT,
  opts: { width: number; height?: number } = { width: 1280, height: 800 },
) {
  await page.setViewportSize({ width: opts.width, height: opts.height ?? 800 })
  await page.route('**/api/projects/proj-1', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(project),
      })
      return
    }
    if (route.request().method() === 'PUT') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(project),
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
        body: JSON.stringify(MOCK_WORKFLOWS),
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
      body: JSON.stringify({ enabled: false }),
    })
  })
  await page.route('**/api/projects/*/token-stats**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        window: '30d',
        bucketWidth: 'day',
        timezone: 'UTC',
        empty: true,
        trend: [],
        composition: {
          inputTokens: 0,
          outputTokens: 0,
          cacheReadTokens: 0,
          cacheWriteTokens: 0,
          total: 0,
        },
        workflows: [],
      }),
    })
  })
  await page.goto('/project-detail.html')
  await expect(page.getByRole('heading', { name: 'Demo Project' })).toBeVisible({ timeout: 10_000 })
}

test.describe('ProjectDetailView 共享 Agent / 工作流变量面板布局', () => {
  test('页头无「合并规则」且无项目删除', async ({ page }) => {
    await gotoProjectDetailWithProject(page, MOCK_PROJECT)

    const headerRow = page.getByTestId('project-detail-header-row')
    await expect(headerRow.getByTestId('project-token-stat')).toBeVisible()
    await expect(headerRow).toHaveCSS('align-items', 'flex-start')
    await expect(page.getByTestId('project-detail-title-row').getByTestId('project-token-stat')).toHaveCount(0)
    await expect(headerRow.getByRole('button', { name: '删除' })).toHaveCount(0)
    await expect(headerRow.getByRole('button', { name: '合并规则' })).toHaveCount(0)
  })

  test('共享 Agent：提示文案 + 子页签 + env 可编辑保存', async ({ page }) => {
    let saveBody: Record<string, unknown> | null = null
    await page.setViewportSize({ width: 1280, height: 800 })
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
          body: JSON.stringify(MOCK_WORKFLOWS),
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
        body: JSON.stringify({ enabled: false }),
      })
    })
    await page.route('**/api/agents', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([{ name: 'demo-agent', projectId: 'proj-1' }]),
        })
        return
      }
      await route.continue()
    })
    await page.route('**/api/projects/proj-1/shared-agent-config', async (route) => {
      const method = route.request().method()
      if (method === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(MOCK_SHARED_AGENT),
        })
        return
      }
      if (method === 'PUT') {
        saveBody = route.request().postDataJSON()
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ ...MOCK_SHARED_AGENT, ...(saveBody || {}) }),
        })
        return
      }
      await route.continue()
    })
    await page.route('**/api/projects/*/token-stats**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          window: '30d',
          bucketWidth: 'day',
          timezone: 'UTC',
          empty: true,
          trend: [],
          composition: {
            inputTokens: 0,
            outputTokens: 0,
            cacheReadTokens: 0,
            cacheWriteTokens: 0,
            total: 0,
          },
          workflows: [],
        }),
      })
    })
    await page.goto('/project-detail.html')
    await expect(page.getByRole('heading', { name: 'Demo Project' })).toBeVisible({ timeout: 10_000 })

    await page.getByRole('button', { name: '共享Agent配置' }).click()
    await expect(page).toHaveURL(/tab=sharedAgent/)
    const panel = page.getByTestId('project-shared-agent-panel')
    await expect(panel).toBeVisible()
    await expect(page.getByTestId('shared-agent-hint')).toHaveCount(0)
    const help = page.getByTestId('shared-agent-help')
    await expect(help.locator('summary')).toHaveText('?')
    await help.locator('summary').click()
    await expect(page.getByTestId('shared-agent-help-text')).toContainText(/extend/)
    await expect(page.getByTestId('shared-agent-subtab-files')).toBeVisible()
    await expect(page.getByTestId('shared-agent-subtab-mcp')).toBeVisible()
    await expect(page.getByTestId('shared-agent-subtab-env')).toBeVisible()
    await expect(page.getByTestId('shared-agent-subtab-prompts')).toBeVisible()
    await expect(page.getByTestId('shared-agent-subtab-meta')).toBeVisible()
    await expect(page.getByTestId('shared-agent-subtab-test')).toHaveCount(0)
    await expect(page.getByTestId('shared-agent-subtab-data')).toHaveCount(0)

    await page.getByTestId('shared-agent-subtab-env').click()
    await expect(page.getByPlaceholder('KEY').first()).toHaveValue('API_URL')
    await page.getByRole('button', { name: /添加/ }).first().click()
    const rows = page.locator('input[placeholder="KEY"]')
    await rows.last().fill('NEW_KEY')
    await page.locator('input[placeholder="value"]').last().fill('1')
    await page.getByTestId('shared-agent-save').click()
    await expect.poll(() => saveBody).not.toBeNull()
    expect((saveBody as { env?: Record<string, string> }).env?.API_URL).toBe('https://example.com')
    expect((saveBody as { env?: Record<string, string> }).env?.NEW_KEY).toBe('1')
  })

  test('工作流变量空态同构面板', async ({ page }) => {
    await gotoProjectDetailWithProject(page, MOCK_PROJECT)
    await page.getByRole('button', { name: '工作流变量' }).click()

    const shell = page.getByTestId('workflow-vars-empty-shell')
    await expect(shell).toBeVisible()
    const box = await shell.boundingBox()
    expect(box?.height).toBeGreaterThanOrEqual(360)
    expect(box?.width).toBeGreaterThan(900)
    expect(box!.y + box!.height).toBeGreaterThan(800 - 80)

    const addBtn = shell.getByRole('button', { name: '添加一行' })
    await expect(addBtn).toHaveClass(/bg-accent/)
    await expect(page.getByRole('button', { name: '合并规则' })).toHaveCount(0)
    await expect(page.getByTestId('project-detail-merge-rules')).toHaveCount(0)
    await expect(page.getByText('启动运行时作为 ${vars.*} 默认值，不整表注入沙箱环境。')).toHaveCount(0)
    await expect(page.getByText(/\$\{vars\.\*\}/)).toHaveCount(0)
    await expect(page.getByText(/不整表注入沙箱环境/)).toHaveCount(0)

    await addBtn.click()
    await expect(page.getByText('暂无工作流变量')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '合并规则' })).toHaveCount(0)
    await expect(page.getByText(/不整表注入沙箱环境/)).toHaveCount(0)
  })

  test('工作流变量有数据：表头 + 底栏 + 次行描述', async ({ page }) => {
    await gotoProjectDetailWithProject(page, MOCK_PROJECT_WITH_VARS)
    await page.getByRole('button', { name: '工作流变量' }).click()

    const panel = page.getByTestId('workflow-vars-data-panel')
    await expect(panel.getByText('名称', { exact: true })).toBeVisible()
    await expect(panel.getByText('默认值', { exact: true })).toBeVisible()
    await expect(panel.getByText('类型', { exact: true })).toBeVisible()
    await expect(panel.getByText('操作', { exact: true })).toBeVisible()
    await expect(panel.getByPlaceholder('描述（可选）')).toHaveValue('部署区域')

    const foot = panel.getByTestId('workflow-vars-footer')
    await expect(foot.getByRole('button', { name: '添加一行' })).toHaveClass(/border/)
    await expect(foot.getByRole('button', { name: '保存' })).toHaveClass(/bg-accent/)
    const footBox = await foot.boundingBox()
    expect(footBox).toBeTruthy()
    expect(footBox!.y + footBox!.height).toBeGreaterThan(800 - 80)

    await expect(page.getByRole('button', { name: '合并规则' })).toHaveCount(0)
    await expect(page.getByTestId('project-detail-merge-rules')).toHaveCount(0)
    await expect(page.getByText('启动运行时作为 ${vars.*} 默认值，不整表注入沙箱环境。')).toHaveCount(0)
    await expect(page.getByText(/\$\{vars\.\*\}/)).toHaveCount(0)
    await expect(page.getByText(/不整表注入沙箱环境/)).toHaveCount(0)
  })
})

test.describe('ProjectDetailView 项目信息面板', () => {
  test('进入 Tab：近全宽壳 + 壳内顶栏 + 左删右存 + 删除确认可取消', async ({ page }) => {
    await gotoProjectDetailWithProject(page, MOCK_PROJECT)
    await page.getByRole('button', { name: '项目信息' }).click()

    const panel = page
      .locator('.border.border-line.bg-surface')
      .filter({ hasText: '修改名称、描述或未知模型显示名后点击保存' })
      .first()
    await expect(panel).toBeVisible()
    await expect(panel.getByRole('heading', { name: '项目信息' })).toBeVisible()
    await expect(panel.getByPlaceholder('简要说明项目用途')).toBeVisible()

    const box = await panel.boundingBox()
    expect(box?.width).toBeGreaterThan(900)

    const footer = panel.getByTestId('project-meta-footer')
    const deleteBtn = footer.getByTestId('project-meta-delete')
    const saveBtn = footer.getByRole('button', { name: '保存' })
    await expect(deleteBtn).toBeVisible()
    await expect(deleteBtn).toHaveClass(/text-err/)
    await expect(saveBtn).toBeVisible()
    await expect(saveBtn).toHaveClass(/bg-accent/)
    // 无改动时保存仍可点（现网 savingMeta，非 Demo dirty-disable）（g2.1 / g3.2）
    await expect(saveBtn).toBeEnabled()

    // 左删右存：删除在保存左侧（g2.1 / g3.2）
    const deleteBox = await deleteBtn.boundingBox()
    const saveBox = await saveBtn.boundingBox()
    expect(deleteBox && saveBox).toBeTruthy()
    expect(deleteBox!.x).toBeLessThan(saveBox!.x)

    // 脚栏贴近主区底，消除卡外页底空洞（g1.1 / g1.2 / g3.2）
    const footerBox = await footer.boundingBox()
    expect(footerBox).toBeTruthy()
    expect(footerBox!.y + footerBox!.height).toBeGreaterThan(800 - 80)

    // 点击删除 → 既有确认弹窗 → 取消后仍停留详情且项目未删
    await deleteBtn.click()
    await expect(page.getByText('删除项目 · Demo Project')).toBeVisible({ timeout: 5_000 })
    await expect(page.getByText('仅当项目下已无流水线时可删除')).toBeVisible()
    await page.getByRole('button', { name: '取消' }).click()
    await expect(page.getByText('删除项目 · Demo Project')).toHaveCount(0)
    await expect(page.getByRole('heading', { name: 'Demo Project' })).toBeVisible()
    await expect(panel.getByTestId('project-meta-delete')).toBeVisible()
  })

  test('保存后页头名称同步；其它 Tab 仍可切换（g2.2 / g3.2）', async ({ page }) => {
    let savedName = MOCK_PROJECT.name
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.route('**/api/projects/proj-1', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ ...MOCK_PROJECT, name: savedName }),
        })
        return
      }
      if (route.request().method() === 'PATCH' || route.request().method() === 'PUT') {
        const body = route.request().postDataJSON() as { name?: string; description?: string }
        savedName = body.name?.trim() || savedName
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            ...MOCK_PROJECT,
            name: savedName,
            description: body.description ?? MOCK_PROJECT.description,
          }),
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
          body: JSON.stringify(MOCK_WORKFLOWS),
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
        body: JSON.stringify({ enabled: false }),
      })
    })
    await page.route('**/api/projects/proj-1/cron-jobs', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ items: [] }),
        })
        return
      }
      await route.continue()
    })
    await page.route('**/api/projects/proj-1/channel**', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ channel: null, secretsKeyConfigured: false }),
        })
        return
      }
      await route.continue()
    })
    await page.route('**/api/projects/*/token-stats**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          window: '30d',
          bucketWidth: 'day',
          timezone: 'UTC',
          empty: true,
          trend: [],
          composition: {
            inputTokens: 0,
            outputTokens: 0,
            cacheReadTokens: 0,
            cacheWriteTokens: 0,
            total: 0,
          },
          workflows: [],
        }),
      })
    })
    await page.goto('/project-detail.html')
    await expect(page.getByRole('heading', { name: 'Demo Project' })).toBeVisible({ timeout: 10_000 })
    await page.getByRole('button', { name: '项目信息' }).click()

    await page.locator('#project-meta-name').fill('Renamed Project')
    await page.getByTestId('project-meta-footer').getByRole('button', { name: '保存' }).click()
    await expect(page.getByRole('heading', { name: 'Renamed Project' })).toBeVisible({ timeout: 5_000 })

    await page.getByRole('button', { name: '共享Agent配置' }).click()
    await expect(page.getByTestId('project-shared-agent-panel')).toBeVisible()
    await expect(page).toHaveURL(/tab=sharedAgent/)
    await page.getByRole('button', { name: '定时任务' }).click()
    await expect(page.getByText('暂无定时任务')).toBeVisible()
    await expect(page).toHaveURL(/tab=cronJobs/)
    const notifyTab = page.getByTestId('project-tab-notify')
    await notifyTab.scrollIntoViewIfNeeded()
    await notifyTab.click()
    await expect(page.getByTestId('project-notify-panel')).toBeVisible()
    await expect(page).toHaveURL(/tab=notify/)
    await page.getByRole('button', { name: '项目信息' }).click()
    await expect(page.getByTestId('project-meta-footer')).toBeVisible()
    await expect(page.locator('#project-meta-name')).toHaveValue('Renamed Project')
    await expect(page).toHaveURL(/tab=meta/)
  })
})

test.describe('ProjectDetailView 看板首 Tab 与深链', () => {
  test('打开项目详情默认落在看板并仅拉本项目 Run', async ({ page }) => {
    await gotoProjectDetail(page, { width: 1280, height: 800, keepDefaultTab: true })
    const tabs = page.getByTestId('project-detail-tabs').locator('button')
    await expect(tabs.first()).toHaveText('看板')
    await expect(page.getByTestId('project-board-panel')).toBeVisible()
    await expect(page.getByTestId('board-view')).toBeVisible()
    await expect(page.getByText('本项目Run')).toBeVisible({ timeout: 5_000 })
    await expect(page.getByRole('button', { name: '导入' })).toHaveCount(0)
    await expect(page).toHaveURL(/tab=board/)
  })

  test('?tab=board 直达看板；切换流水线后仍可回看板', async ({ page }) => {
    await gotoProjectDetail(page, { width: 1280, height: 800, tab: 'board' })
    await expect(page.getByTestId('project-board-panel')).toBeVisible()
    await expect(page).toHaveURL(/tab=board/)
    await page.getByRole('button', { name: '流水线' }).click()
    await expect(page.getByRole('button', { name: '导入' })).toBeVisible()
    await expect(page).toHaveURL(/tab=workflows/)
    await page.getByRole('button', { name: '看板' }).click()
    await expect(page.getByTestId('project-board-panel')).toBeVisible()
    await expect(page.getByText('本项目Run')).toBeVisible()
    await expect(page).toHaveURL(/tab=board/)
  })

  test('非法 tab 回落看板', async ({ page }) => {
    await gotoProjectDetail(page, { width: 1280, height: 800, tab: 'not-a-real-tab', keepDefaultTab: true })
    await expect(page.getByTestId('project-board-panel')).toBeVisible()
    await expect(page).toHaveURL(/tab=board/)
  })
})

test.describe('ProjectDetailView PM Leader 设置内收', () => {
  test('顶栏无独立设置/记忆 Tab；旧深链落到设置子视图', async ({ page }) => {
    await gotoProjectDetail(page, { width: 1280, height: 800, tab: 'pmSettings', keepDefaultTab: true })
    await expect(page.getByTestId('project-tab-pmSettings')).toHaveCount(0)
    await expect(page.getByTestId('project-tab-pmMemory')).toHaveCount(0)
    await expect(page.getByTestId('project-tab-cronJobs')).toBeVisible()
    await expect(page.getByTestId('project-pm-settings-view')).toBeVisible()
    await expect(page.getByRole('button', { name: '返回咨询' })).toBeVisible()
    await expect(page).toHaveURL(/tab=pmLeader/)
  })

  test('旧 ?tab=pmMemory 落到看板并展示迁移提示', async ({ page }) => {
    await gotoProjectDetail(page, { width: 1280, height: 800, tab: 'pmMemory', keepDefaultTab: true })
    await expect(page.getByTestId('project-tab-pmMemory')).toHaveCount(0)
    await expect(page.getByTestId('project-board-panel')).toBeVisible()
    await expect(page.getByTestId('pm-memory-migration-banner')).toBeVisible()
    await expect(page.getByTestId('pm-memory-go-studio')).toBeVisible()
    await expect(page).toHaveURL(/tab=board/)
  })

  test('定时任务 Tab 展示空态列表', async ({ page }) => {
    await page.route('**/api/projects/proj-1/cron-jobs', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ items: [] }),
        })
        return
      }
      await route.continue()
    })
    await gotoProjectDetail(page, { width: 1280, height: 800, tab: 'cronJobs', keepDefaultTab: true })
    await expect(page.getByTestId('project-cron-jobs-panel')).toBeVisible()
    await expect(page.getByText('暂无定时任务')).toBeVisible()
    await expect(page).toHaveURL(/tab=cronJobs/)
  })

  test('未启用空态进入设置；切离顶栏再回不保留设置', async ({ page }) => {
    await gotoProjectDetail(page, { width: 1280, height: 800, tab: 'pmLeader', keepDefaultTab: true })
    await expect(page.getByText('项目管理未启用')).toBeVisible()
    // Permanently removed: no Studio memory migration guide on PM Leader.
    await expect(page.getByTestId('pm-studio-memory-guide')).toHaveCount(0)
    await expect(page.getByTestId('pm-open-studio-memory')).toHaveCount(0)
    await page.getByRole('button', { name: '前往设置' }).click()
    await expect(page.getByTestId('project-pm-settings-view')).toBeVisible()

    await page.getByTestId('project-tab-cronJobs').click()
    await expect(page.getByTestId('project-pm-settings-view')).toHaveCount(0)
    await page.getByTestId('project-tab-pmLeader').click()
    await expect(page.getByText('项目管理未启用')).toBeVisible()
    await expect(page.getByTestId('project-pm-settings-view')).toHaveCount(0)
  })

  test('已启用齿轮入口进入设置；保存后回到咨询', async ({ page }) => {
    let pmEnabled = true
    await page.setViewportSize({ width: 1280, height: 800 })
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
          body: JSON.stringify(MOCK_WORKFLOWS),
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
    await page.route('**/api/agents', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([{ name: 'agent-1' }]),
        })
        return
      }
      await route.continue()
    })
    await page.route('**/api/projects/proj-1/pm/threads**', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ items: [] }),
        })
        return
      }
      await route.continue()
    })
    await page.route('**/api/projects/proj-1/pm-leader', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            enabled: pmEnabled,
            agentAvailable: pmEnabled,
            agentConfigRef: pmEnabled ? 'agent-1' : '',
          }),
        })
        return
      }
      if (route.request().method() === 'PUT') {
        pmEnabled = true
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            enabled: true,
            agentAvailable: true,
            agentConfigRef: 'agent-1',
          }),
        })
        return
      }
      await route.continue()
    })

    await page.goto('/project-detail.html?tab=pmLeader')
    await expect(page.getByRole('heading', { name: 'Demo Project' })).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('pm-chat-open-settings')).toBeVisible()

    await page.getByTestId('pm-chat-open-settings').click()
    await expect(page.getByTestId('project-pm-settings-view')).toBeVisible()

    await page.getByTestId('pm-leader-save').click()
    await expect(page.getByTestId('project-pm-settings-view')).toHaveCount(0)
    await expect(page.getByTestId('pm-chat-open-settings')).toBeVisible()
  })
})

test.describe('ProjectDetailView PM Leader 滚动', () => {
  const PM_MESSAGES = Array.from({ length: 24 }, (_, i) => ({
    id: `msg-${i}`,
    role: i % 2 === 0 ? 'user' : 'assistant',
    content: `PM Leader 历史消息 ${i + 1}：`.padEnd(80, '内容'),
    status: 'ok',
  }))

  async function gotoPmLeaderChat(page: import('@playwright/test').Page, opts?: { captureWs?: boolean }) {
    await page.setViewportSize({ width: 1280, height: 800 })
    if (opts?.captureWs) {
      await page.addInitScript(() => {
        class MockPmWebSocket {
          static CONNECTING = 0
          static OPEN = 1
          static CLOSING = 2
          static CLOSED = 3
          readyState = MockPmWebSocket.OPEN
          onmessage: ((ev: MessageEvent) => void) | null = null
          onerror: (() => void) | null = null
          onclose: (() => void) | null = null
          send() {}
          close() {}
          addEventListener(type: string, listener: EventListener) {
            if (type === 'open') queueMicrotask(() => listener(new Event('open')))
          }
          removeEventListener() {}
          dispatchEvent(ev: Event) {
            if (ev.type === 'message' && this.onmessage) this.onmessage(ev as MessageEvent)
            return true
          }
        }
        const sockets: MockPmWebSocket[] = []
        ;(window as unknown as { __pmTestSockets?: MockPmWebSocket[] }).__pmTestSockets = sockets
        const Patched = function (this: MockPmWebSocket) {
          const ws = new MockPmWebSocket()
          sockets.push(ws)
          return ws
        } as unknown as typeof WebSocket
        Patched.prototype = MockPmWebSocket.prototype
        Patched.CONNECTING = MockPmWebSocket.CONNECTING
        Patched.OPEN = MockPmWebSocket.OPEN
        Patched.CLOSING = MockPmWebSocket.CLOSING
        Patched.CLOSED = MockPmWebSocket.CLOSED
        window.WebSocket = Patched
      })
    }
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
          body: JSON.stringify(MOCK_WORKFLOWS),
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
    await page.route('**/api/projects/proj-1/pm/threads**', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ items: [{ id: 'thr-1', title: '迭代回顾' }] }),
        })
        return
      }
      await route.continue()
    })
    await page.route('**/api/projects/proj-1/pm/threads/thr-1/messages**', async (route) => {
      const method = route.request().method()
      if (method === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ items: PM_MESSAGES }),
        })
        return
      }
      if (method === 'POST') {
        const body = route.request().postDataJSON() as { content?: string; role?: string }
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            id: 'msg-new',
            role: body.role || 'user',
            content: body.content || '',
            status: 'ok',
          }),
        })
        return
      }
      await route.continue()
    })
    await page.route('**/api/projects/proj-1/pm/threads/thr-1/draft**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ draft: null, live: false, hasFinal: false }),
      })
    })
    await page.route('**/api/projects/proj-1/pm/threads/thr-1/sandbox**', async (route) => {
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

    await page.goto('/project-detail.html?tab=pmLeader')
    await expect(page.getByRole('heading', { name: 'Demo Project' })).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('pm-message-scroller')).toBeVisible({ timeout: 10_000 })
  }

  test('消息区独立滚动且整页无纵向溢出', async ({ page }) => {
    await gotoPmLeaderChat(page)

    const scroller = page.getByTestId('pm-message-scroller')
    const overflow = await scroller.evaluate((el) => ({
      scrollHeight: el.scrollHeight,
      clientHeight: el.clientHeight,
      overflowY: getComputedStyle(el).overflowY,
    }))
    expect(overflow.overflowY).toBe('auto')
    expect(overflow.scrollHeight).toBeGreaterThan(overflow.clientHeight)

    const pageScroll = await page.evaluate(() => ({
      docScrollHeight: document.documentElement.scrollHeight,
      docClientHeight: document.documentElement.clientHeight,
    }))
    expect(pageScroll.docScrollHeight).toBeLessThanOrEqual(pageScroll.docClientHeight + 2)
  })

  test('上滚后流式输出不抢滚动位置', async ({ page }) => {
    await gotoPmLeaderChat(page, { captureWs: true })

    const scroller = page.getByTestId('pm-message-scroller')
    await page.getByPlaceholder('输入问题，Enter 发送；可粘贴或选择附件').fill('测试 stick')
    await page.getByRole('button', { name: '发送' }).click()
    await expect(page.getByTestId('pm-stream-bubble')).toBeVisible({ timeout: 10_000 })

    await scroller.evaluate((el) => {
      el.scrollTop = 120
      el.dispatchEvent(new Event('scroll'))
    })
    await page.waitForTimeout(50)
    const scrollTopBefore = await scroller.evaluate((el) => el.scrollTop)

    await page.evaluate(() => {
      const sockets = (window as unknown as { __pmTestSockets?: WebSocket[] }).__pmTestSockets
      const socket = sockets?.[sockets.length - 1]
      if (!socket) throw new Error('no pm websocket')
      const chunk = {
        op: 'event',
        data: {
          type: 'session_update',
          update: {
            sessionUpdate: 'agent_message_chunk',
            content: { type: 'text', text: '流式 token' },
          },
        },
      }
      socket.dispatchEvent(
        new MessageEvent('message', {
          data: JSON.stringify({ type: 'acp', data: chunk, seq: 99 }),
        }),
      )
    })

    await page.waitForTimeout(100)
    const scrollTopAfter = await scroller.evaluate((el) => el.scrollTop)
    expect(scrollTopAfter).toBe(scrollTopBefore)
  })
})

const MOCK_PM_THREADS = {
  items: [
    {
      id: 'thread-1',
      title: '项目整体进度如何？',
      createdAt: '2026-01-01T00:00:00Z',
      updatedAt: '2026-01-02T00:00:00Z',
    },
  ],
}

async function gotoPmLeaderMobile(
  page: import('@playwright/test').Page,
  opts: { threads?: typeof MOCK_PM_THREADS; messages?: unknown[] } = {},
) {
  const threads = opts.threads ?? MOCK_PM_THREADS
  const messages = opts.messages ?? []

  await page.setViewportSize({ width: 390, height: 844 })
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
        body: JSON.stringify(MOCK_WORKFLOWS),
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
  await page.route('**/api/agents', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([{ name: 'agent-1' }]),
      })
      return
    }
    await route.continue()
  })
  await page.route('**/api/projects/proj-1/pm/threads**', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(threads),
      })
      return
    }
    if (route.request().method() === 'POST') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'thread-new',
          title: '新会话',
          createdAt: '2026-01-03T00:00:00Z',
          updatedAt: '2026-01-03T00:00:00Z',
        }),
      })
      return
    }
    await route.continue()
  })
  await page.route('**/api/projects/proj-1/pm/threads/*/messages**', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: messages }),
      })
      return
    }
    await route.continue()
  })
  await page.route('**/api/projects/proj-1/pm-leader', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          enabled: true,
          agentAvailable: true,
          agentConfigRef: 'agent-1',
        }),
      })
      return
    }
    await route.continue()
  })

  await page.goto('/project-detail.html?tab=pmLeader')
  await expect(page.getByRole('heading', { name: 'Demo Project' })).toBeVisible({ timeout: 10_000 })
}

test.describe('ProjectDetailView PM Leader 移动端 390px', () => {
  test('启用后初始仅会话列表全宽可见', async ({ page }) => {
    await gotoPmLeaderMobile(page)

    const aside = page.getByTestId('pm-threads-aside')
    const chat = page.getByTestId('pm-chat-section')
    await expect(aside).toBeVisible()
    await expect(chat).toHaveCount(0)

    const asideBox = await aside.boundingBox()
    expect(asideBox?.width).toBeGreaterThanOrEqual(350)
  })

  test('选会话后聊天区全宽、列表不可见', async ({ page }) => {
    await gotoPmLeaderMobile(page)

    await page.getByRole('button', { name: '项目整体进度如何？' }).click()

    const aside = page.getByTestId('pm-threads-aside')
    const chat = page.getByTestId('pm-chat-section')
    await expect(chat).toBeVisible()
    await expect(aside).toHaveCount(0)

    const chatBox = await chat.boundingBox()
    expect(chatBox?.width).toBeGreaterThanOrEqual(350)
  })

  test('点击新建进入全宽聊天视图', async ({ page }) => {
    await gotoPmLeaderMobile(page)

    await page.getByRole('button', { name: '新建' }).click()

    const aside = page.getByTestId('pm-threads-aside')
    const chat = page.getByTestId('pm-chat-section')
    await expect(chat).toBeVisible()
    await expect(aside).toHaveCount(0)

    const chatBox = await chat.boundingBox()
    expect(chatBox?.width).toBeGreaterThanOrEqual(350)
  })

  test('完整导航：选会话→聊天→返回列表→设置→返回聊天', async ({ page }) => {
    await gotoPmLeaderMobile(page)

    await page.getByRole('button', { name: '项目整体进度如何？' }).click()
    await expect(page.getByTestId('pm-chat-section')).toBeVisible()
    await expect(page.getByTestId('pm-threads-aside')).toHaveCount(0)

    await page.getByTestId('pm-mobile-back-to-threads').click()
    await expect(page.getByTestId('pm-threads-aside')).toBeVisible()
    await expect(page.getByTestId('pm-chat-section')).toHaveCount(0)

    await page.getByRole('button', { name: '项目整体进度如何？' }).click()
    await expect(page.getByTestId('pm-chat-open-settings')).toBeVisible()

    await page.getByTestId('pm-chat-open-settings').click()
    await expect(page.getByTestId('project-pm-settings-view')).toBeVisible()

    await page.getByTestId('pm-settings-back').click()
    await expect(page.getByTestId('project-pm-settings-view')).toHaveCount(0)
    await expect(page.getByTestId('pm-chat-open-settings')).toBeVisible()
  })

  test('主要操作按钮触控目标 ≥44px', async ({ page }) => {
    await gotoPmLeaderMobile(page)

    await page.getByRole('button', { name: '项目整体进度如何？' }).click()

    const backBtn = page.getByTestId('pm-mobile-back-to-threads')
    const attachBtn = page.getByTestId('pm-chat-attach')
    const sendBtn = page.getByTestId('pm-chat-send')
    await expect(backBtn).toBeVisible()
    await expect(attachBtn).toBeVisible()
    await expect(sendBtn).toBeVisible()

    const backBox = await backBtn.boundingBox()
    const attachBox = await attachBtn.boundingBox()
    const sendBox = await sendBtn.boundingBox()
    expect(backBox?.height).toBeGreaterThanOrEqual(44)
    expect(attachBox?.height).toBeGreaterThanOrEqual(44)
    expect(sendBox?.height).toBeGreaterThanOrEqual(44)

    await page.getByTestId('pm-chat-open-settings').click()
    await expect(page.getByTestId('project-pm-settings-view')).toBeVisible()

    const settingsBackBtn = page.getByTestId('pm-settings-back')
    await expect(settingsBackBtn).toBeVisible()
    const settingsBackBox = await settingsBackBtn.boundingBox()
    expect(settingsBackBox?.height).toBeGreaterThanOrEqual(44)
  })
})

test.describe('ProjectDetailView PM Leader QQ Channel 侧栏', () => {
  test('双处 QQ 标签、只读文案、右键详情与空标题占位', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
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
          body: JSON.stringify(MOCK_WORKFLOWS),
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
    await page.route('**/api/agents', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([{ name: 'agent-1' }]),
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
    await page.route('**/api/projects/proj-1/pm/threads**', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            items: [
              {
                id: 'thr-qq',
                title: '',
                userId: 'qq:c2c:u1',
                createdAt: '2026-01-03T00:00:00Z',
                updatedAt: '2026-01-03T00:00:00Z',
              },
              {
                id: 'thr-web',
                title: '对接发布流程',
                userId: 'alice',
                createdAt: '2026-01-02T00:00:00Z',
                updatedAt: '2026-01-02T00:00:00Z',
              },
            ],
          }),
        })
        return
      }
      await route.continue()
    })
    await page.route('**/api/projects/proj-1/pm/threads/*/messages**', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            items: [{ id: 'm1', role: 'user', content: '单聊消息', status: 'ok' }],
          }),
        })
        return
      }
      await route.continue()
    })
    await page.route('**/api/projects/proj-1/pm/threads/*/draft**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ draft: null, live: false, hasFinal: false }),
      })
    })

    await page.goto('/project-detail.html?tab=pmLeader')
    await expect(page.getByRole('heading', { name: 'Demo Project' })).toBeVisible({ timeout: 10_000 })

    const channelItem = page.locator('[data-channel="1"]')
    await expect(channelItem).toBeVisible()
    await expect(channelItem).toContainText('qq:c2c:u1')
    await expect(channelItem.getByTestId('pm-qq-tag')).toBeVisible()
    await expect(channelItem.getByTestId('pm-thread-delete')).toHaveCount(0)

    await expect(page.getByTestId('pm-qq-tag-header')).toBeVisible()
    await expect(page.getByTestId('pm-channel-readonly')).toBeVisible()
    await expect(page.getByTestId('pm-channel-readonly')).toContainText('渠道会话不可在 Web 改写')
    await expect(page.getByTestId('pm-channel-readonly')).toContainText('可点击缩略图查看大图')
    await expect(page.getByTestId('pm-chat-send')).toHaveCount(0)

    await channelItem.click({ button: 'right' })
    await expect(page.getByTestId('pm-channel-ctx-menu')).toBeVisible()
    await expect(page.getByTestId('pm-channel-ctx-menu')).toContainText('查看详情')
    await page.getByTestId('pm-channel-ctx-detail').click()
    await expect(page.getByTestId('pm-channel-detail-title')).toHaveText('qq:c2c:u1')
    await expect(page.getByTestId('pm-channel-detail-source')).toContainText('来自 QQ Channel')
    await page.getByTestId('pm-channel-detail-ok').click()

    await page.locator('[data-channel="0"]').click()
    await expect(page.getByTestId('pm-qq-tag-header')).toHaveCount(0)
    await expect(page.getByTestId('pm-chat-send')).toBeVisible()
    await expect(page.getByTestId('pm-channel-readonly')).toHaveCount(0)
  })
})
