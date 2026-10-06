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
  {
    id: 'deliver',
    embedName: 'DeliverAgent',
    roleLabelZh: '交付',
    summary: '测试评审通过后合入目标分支并创建或复用 MR/PR',
    capabilities: {
      interaction: 'auto',
      reads: ['*'],
      writes: [{ schema: 'merge_request', required: true }],
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
      })
      return
    }
    await json([])
  })
  return state
}

test.use({ viewport: { width: 1280, height: 720 } })

async function next(page: Page) {
  await page.getByTestId('onboarding-next').click()
}

async function expectStep(page: Page, id: string) {
  await expect(page.getByTestId(`onboarding-rail-${id}`)).toHaveAttribute('data-active', '1')
}

/** Every page must fit the dialog without scrolling and must not show env var names or sandbox paths. */
async function expectNoScroll(page: Page, testId = 'onboarding-body') {
  const body = page.getByTestId(testId)
  const overflow = await body.evaluate((el) => el.scrollHeight - el.clientHeight)
  expect(overflow).toBeLessThanOrEqual(1)
  const railOverflow = await page.getByTestId('onboarding-rail').evaluate((el) => el.scrollHeight - el.clientHeight)
  expect(railOverflow).toBeLessThanOrEqual(1)
  const text = await page.getByTestId('onboarding-wizard').innerText()
  expect(text).not.toMatch(/\b[A-Z][A-Z0-9]*_[A-Z0-9_]+\b|\/root\//)
}

async function fillKey(page: Page, apiKey: string) {
  await page.locator('[data-test="opencode-provider"] [data-test="app-select-trigger"]').click()
  await page.locator('[data-test="app-select-option-deepseek"]').click()
  await page.locator('[data-test="opencode-model"] [data-test="app-select-trigger"]').click()
  await page.locator('[data-test="app-select-option-deepseek/deepseek-v4-pro"]').click()
  await page.getByTestId('onboarding-api-key').fill(apiKey)
}

async function fillIdentity(page: Page) {
  await page.getByTestId('onboarding-git-user-name').fill('Ada Lovelace')
  await page.getByTestId('onboarding-git-user-email').fill('ada@example.com')
}

/** From preferences through model, key and Git to the team step. */
async function walkToTeam(page: Page, apiKey: string, opts: { skipGit?: boolean } = {}) {
  await next(page)
  await expectStep(page, 'model')
  await next(page)
  await expectStep(page, 'key')
  await fillKey(page, apiKey)
  await next(page)
  await expectStep(page, 'git')
  await fillIdentity(page)
  if (opts.skipGit) await page.getByTestId('onboarding-git-skip').click()
  await next(page)
  await expectStep(page, 'team')
}

test('首次安装分步引导：偏好 → 模型 → 密钥 → Git → 团队 → 工作流 → 完成（zh）', async ({ page }) => {
  const state = await mockOnboardingApi(page)

  await page.goto('/onboarding-wizard.html', { waitUntil: 'networkidle' })
  await expect(page.getByTestId('onboarding-wizard-root')).toBeVisible()
  await expect(page.getByTestId('onboarding-empty-desc')).toContainText('默认工作流')
  await expect(page.getByTestId('onboarding-language-zh-CN')).toHaveAttribute('aria-checked', 'true')
  for (const label of ['偏好', '模型后端', 'API Key', 'Git', '团队', '工作流预览', '完成']) {
    await expect(page.locator('.onb-step-title', { hasText: label }).first()).toBeVisible()
  }
  await expectStep(page, 'prefs')
  await expect(page.getByTestId('onboarding-prev')).toHaveCount(0)
  await page.screenshot({ path: path.join(OUT, '01-prefs.png') })
  await expectNoScroll(page)

  await next(page)
  await expectStep(page, 'model')
  await page.screenshot({ path: path.join(OUT, '02-model.png') })
  await expectNoScroll(page)
  await page.getByTestId('onboarding-path-cli').click()
  await page.screenshot({ path: path.join(OUT, '02-model-cli.png') })
  await expectNoScroll(page)
  await next(page)
  await expectStep(page, 'key')
  await page.screenshot({ path: path.join(OUT, '03-key-cli.png') })
  await expectNoScroll(page)
  await page.getByTestId('onboarding-prev').click()
  await page.getByTestId('onboarding-path-apiKey').click()

  await next(page)
  await expectStep(page, 'key')
  await next(page)
  await expectStep(page, 'key')
  await fillKey(page, 'crsr_e2e_test_key')
  await page.screenshot({ path: path.join(OUT, '03-key.png') })
  await expectNoScroll(page)

  await next(page)
  await expectStep(page, 'git')
  await next(page)
  await expectStep(page, 'git')
  await fillIdentity(page)
  await page.getByTestId('onboarding-repo-url').fill('https://github.com/org/web.git')
  await expect(page.getByTestId('onboarding-repo-hint')).toContainText('web')
  await page.getByTestId('onboarding-git-type-github_https').click()
  await page.screenshot({ path: path.join(OUT, '04-git.png') })
  await expectNoScroll(page)
  await page.getByTestId('onboarding-git-type-github_https').click()
  await next(page)

  await expectStep(page, 'team')
  const clarify = page.getByTestId('onboarding-team-card-clarify')
  await expect(clarify).toContainText('需求澄清')
  await expect(clarify).toContainText('多轮对话澄清')
  await expect(clarify).toContainText('需求规格')
  await expect(page.getByTestId('onboarding-team-preview-clarify')).toHaveText('可启动应用预览')
  await expect(page.getByTestId('onboarding-team-card-test_review')).toContainText('测试结果')
  await expect(page.getByTestId('onboarding-team-toggle-clarify')).toHaveCount(0)
  await expect(page.getByTestId('onboarding-team-toggle-test_review')).toBeChecked()
  await expect(page.getByTestId('onboarding-team-name-implement')).toHaveValue('实现')
  await page.getByTestId('onboarding-team-model-implement').fill('deepseek/deepseek-v4-pro')
  await page.screenshot({ path: path.join(OUT, '05-team.png') })
  await expectNoScroll(page)
  await next(page)

  await expect(page.getByTestId('onboarding-rail-workflow')).toHaveAttribute('data-active', '1')
  const preview = page.getByTestId('onboarding-workflow-preview')
  for (const id of ['input', 'clarify', 'implement', 'test_review', 'deliver', 'output']) {
    await expect(preview.getByTestId(`onboarding-preview-node-${id}`)).toBeVisible()
  }
  await expect(preview.getByText('未通过')).toBeVisible()
  await expect(page.getByTestId('onboarding-review-repo')).toContainText('web')
  await expect(page.getByTestId('onboarding-review-workflow')).toContainText('默认工作流')
  await page.screenshot({ path: path.join(OUT, '06-workflow.png') })
  await expectNoScroll(page)

  await next(page)
  await expect(page.getByTestId('onboarding-success')).toBeVisible()
  await expect(page.getByTestId('onboarding-rail-done')).toHaveAttribute('data-active', '1')
  await expect(page.getByTestId('onboarding-success-agents')).toContainText('测试评审')
  await expect(page.getByText('默认工作流（已发布）')).toBeVisible()
  await expect(page.getByTestId('onboarding-run-once')).toContainText('运行一次')
  await expect(page.getByTestId('onboarding-edit-workflow')).toContainText('去编辑工作流')
  await page.screenshot({ path: path.join(OUT, '07-done.png') })
  await expectNoScroll(page, 'onboarding-success')

  expect(state.bootstrapBody?.repoUrl).toBe('https://github.com/org/web.git')
  expect(state.bootstrapBody?.agents).toEqual([
    { templateId: 'clarify', name: '需求澄清' },
    { templateId: 'implement', name: '实现', model: 'deepseek/deepseek-v4-pro' },
    { templateId: 'test_review', name: '测试评审' },
    { templateId: 'deliver', name: '交付' },
  ])

  await page.getByTestId('onboarding-edit-workflow').click()
  await expect(page.getByTestId('workflow-editor-landing')).toBeVisible()
  await expect(page.getByTestId('onboarding-wizard')).toHaveCount(0)
})

test('unchecking 测试评审 trims the preview and the bootstrap request', async ({ page }) => {
  const state = await mockOnboardingApi(page)
  await page.goto('/onboarding-wizard.html', { waitUntil: 'networkidle' })
  await walkToTeam(page, 'crsr_trim', { skipGit: true })

  await page.getByTestId('onboarding-team-toggle-test_review').uncheck()
  await page.screenshot({ path: path.join(OUT, 'trim-team.png') })
  await page.getByTestId('onboarding-next').click()
  await expect(page.getByTestId('onboarding-preview-node-test_review')).toHaveCount(0)
  await expect(page.getByTestId('onboarding-workflow-note')).toContainText('实现完成后直接进入下一步')
  await page.screenshot({ path: path.join(OUT, 'trim-workflow.png') })
  await page.getByTestId('onboarding-next').click()

  await expect(page.getByTestId('onboarding-success')).toBeVisible()
  expect(state.bootstrapBody?.agents?.map((a) => a.templateId)).toEqual(['clarify', 'implement', 'deliver'])
  expect(state.bootstrapBody?.repoUrl).toBeUndefined()
})

/** English pages must not leak Chinese text (template labels, default names, server copy). */
async function expectNoChinese(page: Page) {
  const text = await page.getByTestId('onboarding-wizard').innerText()
  const lines = text.split('\n').filter((l) => /[\u3400-\u9fff]/.test(l) && l.trim() !== '简体中文')
  expect(lines).toEqual([])
}

test('English walkthrough: every step is English, fits without scrolling, and saves English names', async ({ page }) => {
  const state = await mockOnboardingApi(page)
  await page.goto('/onboarding-wizard.html', { waitUntil: 'networkidle' })
  await page.getByTestId('onboarding-language-en').click()
  await expect(page.locator('.onb-step-title', { hasText: 'Preferences' })).toBeVisible()
  await expect(page.getByTestId('onboarding-empty-desc')).toContainText('Default Workflow')
  const check = async (shot: string) => {
    await page.screenshot({ path: path.join(OUT, `en-${shot}.png`) })
    await expectNoScroll(page)
    await expectNoChinese(page)
  }
  await check('01-prefs')

  await next(page)
  await expectStep(page, 'model')
  await check('02-model')
  await next(page)
  await expectStep(page, 'key')
  await fillKey(page, 'crsr_e2e_en')
  await check('03-key')
  await next(page)
  await expectStep(page, 'git')
  await fillIdentity(page)
  await check('04-git')
  await next(page)

  await expectStep(page, 'team')
  await expect(page.getByTestId('onboarding-team-card-test_review')).toContainText('Test & review')
  await expect(page.getByTestId('onboarding-team-preview-clarify')).toHaveText('App preview')
  await expect(page.getByTestId('onboarding-team-name-clarify')).toHaveValue('Clarify')
  await expect(page.getByTestId('onboarding-team-name-test_review')).toHaveValue('TestReview')
  await check('05-team')
  await next(page)

  await expectStep(page, 'workflow')
  const preview = page.getByTestId('onboarding-workflow-preview')
  await expect(preview.getByTestId('onboarding-preview-node-clarify')).toContainText('Clarify')
  await expect(preview.getByText('Fail', { exact: true })).toBeVisible()
  await expect(page.getByTestId('onboarding-review-workflow')).toContainText('Default Workflow')
  await check('06-workflow')
  await next(page)

  await expect(page.getByText('Default Workflow (published)')).toBeVisible()
  await expect(page.getByTestId('onboarding-run-once')).toContainText('Run once')
  await page.screenshot({ path: path.join(OUT, 'en-07-done.png') })
  await expectNoScroll(page, 'onboarding-success')
  await expectNoChinese(page)
  expect(state.bootstrapBody?.agents).toEqual([
    { templateId: 'clarify', name: 'Clarify' },
    { templateId: 'implement', name: 'Implement' },
    { templateId: 'test_review', name: 'TestReview' },
    { templateId: 'deliver', name: 'Deliver' },
  ])
  expect(state.bootstrapBody?.featureHint).toBeUndefined()
  expect(state.bootstrapBody?.repos).toBeUndefined()
})

test('a missing server encryption key shows plain copy, not config names', async ({ page }) => {
  await mockOnboardingApi(page)
  await page.route('**/api/projects/*/bootstrap-onboarding', (route) =>
    route.fulfill({
      status: 412,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'encrypt credential: 加密主密钥未配置(config: security.secrets_key 或 GRASP_SECRETS_KEY)', code: 'secrets_key_missing' }),
    }),
  )
  await page.goto('/onboarding-wizard.html', { waitUntil: 'networkidle' })
  await walkToTeam(page, 'crsr_nokey', { skipGit: true })
  await next(page)
  await next(page)
  const wizard = page.getByTestId('onboarding-wizard')
  await expect(wizard).toContainText('加密密钥')
  await expect(wizard).not.toContainText('secrets_key')
  await expect(wizard).not.toContainText('GRASP_SECRETS_KEY')
  await page.screenshot({ path: path.join(OUT, 'secrets-key-missing.png') })
})

test('新建项目 create 模式：偏好页填项目名 → 名称带前缀 → create+bootstrap', async ({ page }) => {
  const state = await mockOnboardingApi(page)
  await page.goto('/onboarding-wizard.html?mode=createProject', { waitUntil: 'networkidle' })
  await expect(page.getByTestId('onboarding-project-name')).toBeVisible()
  await expect(page.getByTestId('onboarding-language-zh-CN')).toHaveCount(0)

  await next(page)
  await expectStep(page, 'prefs')

  await page.getByTestId('onboarding-project-name').fill('中国象棋')
  await expectNoScroll(page)
  await walkToTeam(page, 'sk-create-e2e')
  await expect(page.getByTestId('onboarding-team-name-clarify')).toHaveValue('中国象棋需求澄清')
  await page.screenshot({ path: path.join(OUT, 'create-team.png') })
  await page.getByTestId('onboarding-next').click()
  await page.getByTestId('onboarding-next').click()

  await expect(page.getByTestId('onboarding-success')).toBeVisible({ timeout: 10_000 })
  await expect(page.getByText('中国象棋实现')).toBeVisible()
  expect(state.createBody?.name).toBe('中国象棋')
  expect(state.bootstrapPath).toContain('/projects/proj-e2e-created/bootstrap-onboarding')

  await page.getByTestId('onboarding-success-close').click()
  await expect(page.getByTestId('project-agents-landing')).toBeVisible()
})
