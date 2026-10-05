import { test, expect, type Page } from '@playwright/test'
import path from 'node:path'
import fs from 'node:fs'

const OUT = path.join('/tmp', 'onboarding-e2e-shots')

test.beforeAll(() => {
  fs.mkdirSync(OUT, { recursive: true })
})

const TEMPLATES = [
  {
    id: 'clarify',
    embedName: 'ClarifyAgent',
    roleLabelZh: '需求澄清',
    summary: '多轮对话澄清需求、写出计划',
    capabilities: {
      interaction: 'clarify',
      tools: ['ask_question', 'set_artifact_preview', 'set_preview'],
      reads: ['*'],
      writes: [
        { schema: 'clarified_requirement', required: true },
        { schema: 'plan', required: true },
        { schema: 'research' },
        { schema: 'proposals' },
      ],
    },
  },
  {
    id: 'implement',
    embedName: 'ImplementAgent',
    roleLabelZh: '实现',
    summary: '按计划实现',
    capabilities: {
      interaction: 'auto',
      review: true,
      tools: ['set_preview', 'update_plan_status'],
      reads: ['*'],
      writes: [{ schema: 'implementation_result', required: true }],
    },
  },
  {
    id: 'test_review',
    embedName: 'TestReviewAgent',
    roleLabelZh: '测试评审',
    summary: '执行测试并做代码评审',
    capabilities: {
      interaction: 'auto',
      review: true,
      tools: ['set_preview', 'update_plan_status'],
      reads: ['*'],
      writes: [
        { schema: 'test_result', required: true },
        { schema: 'review', required: true },
      ],
    },
  },
]

type BootstrapBody = {
  apiKey?: string
  repoUrl?: string
  agents?: { templateId: string; name?: string; model?: string }[]
  featureHint?: string
  repos?: unknown
}

type MockState = { bootstrapBody: BootstrapBody | null; bootstrapPath: string; createBody: { name?: string } | null }

async function mockOnboardingApi(page: Page): Promise<MockState> {
  const state: MockState = { bootstrapBody: null, bootstrapPath: '', createBody: null }
  await page.route('**/api/**', async (route) => {
    const url = new URL(route.request().url())
    if (!url.pathname.startsWith('/api/')) {
      await route.continue()
      return
    }
    const pathname = url.pathname
    const method = route.request().method()
    const json = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })

    if (pathname === '/api/opencode/providers' && method === 'GET') {
      await json({ providers: [{ id: 'deepseek', name: 'DeepSeek', models: 1 }] })
      return
    }
    if (/^\/api\/opencode\/providers\/[^/]+\/models$/.test(pathname) && method === 'GET') {
      await json({ models: [{ id: 'deepseek-v4-pro' }] })
      return
    }
    if (pathname === '/api/agent-teams/templates' && method === 'GET') {
      await json({ items: TEMPLATES })
      return
    }
    if (pathname === '/api/projects' && method === 'POST') {
      state.createBody = route.request().postDataJSON() as { name?: string }
      await json({
        id: 'proj-e2e-created',
        name: state.createBody?.name || 'created',
        description: '',
        createdAt: '2026-01-01T00:00:00Z',
        updatedAt: '2026-01-01T00:00:00Z',
      })
      return
    }
    if (pathname.includes('/bootstrap-onboarding') && method === 'POST') {
      const body = route.request().postDataJSON() as BootstrapBody
      state.bootstrapBody = body
      state.bootstrapPath = pathname
      if (!body?.apiKey?.trim()) {
        await json({ error: 'apiKey required' }, 400)
        return
      }
      const prefix = pathname.includes('proj-e2e-created') ? state.createBody?.name || '' : ''
      await json({
        agentIds: (body.agents || []).map(
          (a) => a.name || prefix + TEMPLATES.find((t) => t.id === a.templateId)!.roleLabelZh,
        ),
        workflowId: 'wf-onboard-1',
        published: true,
        groupName: prefix ? `${prefix}项目组` : '默认项目组',
      })
      return
    }
    await json([])
  })
  return state
}

/** Select OpenCode vendor/model, fill API key and Git identity on the connect page. */
async function fillConnect(page: Page, apiKey: string) {
  await page.locator('[data-test="opencode-provider"] [data-test="app-select-trigger"]').click()
  await page.locator('[data-test="app-select-option-deepseek"]').click()
  await page.locator('[data-test="opencode-model"] [data-test="app-select-trigger"]').click()
  await page.locator('[data-test="app-select-option-deepseek/deepseek-v4-pro"]').click()
  await page.getByTestId('onboarding-api-key').fill(apiKey)
  await page.getByTestId('onboarding-git-user-name').fill('Ada Lovelace')
  await page.getByTestId('onboarding-git-user-email').fill('ada@example.com')
}

test('首次安装四步引导：连接 → 团队 → 工作流预览 → 完成（zh）', async ({ page }) => {
  const state = await mockOnboardingApi(page)

  await page.goto('/onboarding-wizard.html', { waitUntil: 'networkidle' })
  await expect(page.getByTestId('onboarding-wizard-root')).toBeVisible()
  await expect(page.getByTestId('onboarding-empty-desc')).toContainText('默认工作流')
  await expect(page.getByTestId('onboarding-language-zh-CN')).toHaveClass(/border-accent/)
  for (const label of ['1. 连接', '2. 团队', '3. 工作流预览', '4. 完成']) {
    await expect(page.getByText(label)).toBeVisible()
  }
  await expect(page.getByTestId('onboarding-section-git')).toBeVisible()
  await page.screenshot({ path: path.join(OUT, '01-connect.png'), fullPage: true })

  await page.getByTestId('onboarding-next').click()
  await expect(page.getByTestId('onboarding-rail-connect')).toHaveAttribute('data-active', '1')

  await fillConnect(page, 'crsr_e2e_test_key')
  await page.getByTestId('onboarding-repo-url').fill('https://github.com/org/web.git')
  await expect(page.getByTestId('onboarding-repo-hint')).toContainText('/root/workspace/web/')
  await page.getByTestId('onboarding-next').click()

  await expect(page.getByTestId('onboarding-rail-team')).toHaveAttribute('data-active', '1')
  const clarify = page.getByTestId('onboarding-team-card-clarify')
  await expect(clarify).toContainText('需求澄清')
  await expect(clarify).toContainText('多轮对话澄清')
  await expect(clarify).toContainText('提问澄清')
  await expect(clarify).toContainText('需求规格')
  await expect(page.getByTestId('onboarding-team-preview-clarify')).toHaveText('可启动应用预览')
  await expect(page.getByTestId('onboarding-team-card-test_review')).toContainText('测试结果')
  await expect(page.getByTestId('onboarding-team-toggle-clarify')).toBeDisabled()
  await expect(page.getByTestId('onboarding-team-name-implement')).toHaveValue('实现')
  await page.getByTestId('onboarding-team-model-implement').fill('deepseek/deepseek-v4-pro')
  await page.screenshot({ path: path.join(OUT, '02-team.png'), fullPage: true })
  await page.getByTestId('onboarding-next').click()

  await expect(page.getByTestId('onboarding-rail-workflow')).toHaveAttribute('data-active', '1')
  const preview = page.getByTestId('onboarding-workflow-preview')
  for (const id of ['input', 'clarify', 'implement', 'test_review', 'output']) {
    await expect(preview.getByTestId(`onboarding-preview-node-${id}`)).toBeVisible()
  }
  await expect(preview.getByText('未通过')).toBeVisible()
  await expect(page.getByTestId('onboarding-review-repo')).toContainText('web')
  await expect(page.getByText('工作流 · 默认工作流')).toBeVisible()
  await page.screenshot({ path: path.join(OUT, '03-workflow.png'), fullPage: true })

  await page.getByText('生成配置').click()
  await expect(page.getByTestId('onboarding-success')).toBeVisible()
  await expect(page.getByTestId('onboarding-rail-done')).toHaveAttribute('data-active', '1')
  await expect(page.getByTestId('onboarding-success-agents')).toContainText('测试评审')
  await expect(page.getByText('默认工作流（已发布）')).toBeVisible()
  await expect(page.getByTestId('onboarding-run-once')).toContainText('运行一次')
  await expect(page.getByTestId('onboarding-edit-workflow')).toContainText('去编辑工作流')
  await page.screenshot({ path: path.join(OUT, '04-done.png'), fullPage: true })

  expect(state.bootstrapBody?.repoUrl).toBe('https://github.com/org/web.git')
  expect(state.bootstrapBody?.agents).toEqual([
    { templateId: 'clarify', name: '需求澄清' },
    { templateId: 'implement', name: '实现', model: 'deepseek/deepseek-v4-pro' },
    { templateId: 'test_review', name: '测试评审' },
  ])

  await page.getByTestId('onboarding-edit-workflow').click()
  await expect(page.getByTestId('workflow-editor-landing')).toBeVisible()
  await expect(page.getByTestId('onboarding-wizard')).toHaveCount(0)
})

test('unchecking 测试评审 trims the preview and the bootstrap request', async ({ page }) => {
  const state = await mockOnboardingApi(page)
  await page.goto('/onboarding-wizard.html', { waitUntil: 'networkidle' })
  await fillConnect(page, 'crsr_trim')
  await page.getByTestId('onboarding-git-skip').click()
  await expect(page.getByTestId('onboarding-git-skipped')).toBeVisible()
  await page.getByTestId('onboarding-next').click()

  await page.getByTestId('onboarding-team-toggle-test_review').uncheck()
  await page.getByTestId('onboarding-next').click()
  await expect(page.getByTestId('onboarding-preview-node-test_review')).toHaveCount(0)
  await expect(page.getByTestId('onboarding-workflow-note')).toContainText('实现完成后直接结束')
  await page.screenshot({ path: path.join(OUT, 'trim-workflow.png'), fullPage: true })
  await page.getByTestId('onboarding-next').click()

  await expect(page.getByTestId('onboarding-success')).toBeVisible()
  expect(state.bootstrapBody?.agents?.map((a) => a.templateId)).toEqual(['clarify', 'implement'])
  expect(state.bootstrapBody?.repoUrl).toBeUndefined()
})

test('onboarding wizard English copy', async ({ page }) => {
  const state = await mockOnboardingApi(page)
  await page.goto('/onboarding-wizard.html', { waitUntil: 'networkidle' })
  await page.getByTestId('onboarding-language-en').click()
  await expect(page.getByText('1. Connect')).toBeVisible()
  await expect(page.getByTestId('onboarding-empty-desc')).toContainText('Default Workflow')

  await fillConnect(page, 'crsr_e2e_en')
  await page.getByTestId('onboarding-next').click()
  await expect(page.getByTestId('onboarding-team-card-test_review')).toContainText('Test & review')
  await expect(page.getByTestId('onboarding-team-preview-clarify')).toHaveText('Can start an app preview')
  await page.getByTestId('onboarding-next').click()
  await expect(page.getByText('Workflow · Default Workflow')).toBeVisible()
  await page.getByText('Generate setup').click()

  await expect(page.getByText('Default Workflow (published)')).toBeVisible()
  await expect(page.getByTestId('onboarding-run-once')).toContainText('Run once')
  expect(state.bootstrapBody?.featureHint).toBeUndefined()
  expect(state.bootstrapBody?.repos).toBeUndefined()
  await page.screenshot({ path: path.join(OUT, 'en-done.png'), fullPage: true })
})

test('新建项目 create 模式：连接页填项目名 → 名称带前缀 → create+bootstrap', async ({ page }) => {
  const state = await mockOnboardingApi(page)
  await page.goto('/onboarding-wizard.html?mode=createProject', { waitUntil: 'networkidle' })
  await expect(page.getByTestId('onboarding-project-name')).toBeVisible()
  await expect(page.getByTestId('onboarding-language-zh-CN')).toHaveCount(0)

  await fillConnect(page, 'sk-create-e2e')
  await page.getByTestId('onboarding-next').click()
  await expect(page.getByTestId('onboarding-rail-connect')).toHaveAttribute('data-active', '1')

  await page.getByTestId('onboarding-project-name').fill('中国象棋')
  await page.getByTestId('onboarding-next').click()
  await expect(page.getByTestId('onboarding-team-name-clarify')).toHaveValue('中国象棋需求澄清')
  await page.screenshot({ path: path.join(OUT, 'create-team.png'), fullPage: true })
  await page.getByTestId('onboarding-next').click()
  await page.getByTestId('onboarding-next').click()

  await expect(page.getByTestId('onboarding-success')).toBeVisible({ timeout: 10_000 })
  await expect(page.getByText('中国象棋实现')).toBeVisible()
  expect(state.createBody?.name).toBe('中国象棋')
  expect(state.bootstrapPath).toContain('/projects/proj-e2e-created/bootstrap-onboarding')

  await page.getByTestId('onboarding-success-close').click()
  await expect(page.getByTestId('project-agents-landing')).toBeVisible()
})
