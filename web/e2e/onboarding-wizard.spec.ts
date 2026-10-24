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
 modelConfigurationId?: string
 gitBindings?: { items: { credentialId: string }[]; preferredCredentialId?: string }
 agents?: { templateId: string; name: string; modelConfigurationId?: string }[]
 language?: string
}
const key = { id: 'cursor-key', name: 'Team Cursor key', purpose: 'tool_auth', toolId: 'cursor', issuer: 'cursor', secretKind: 'api_key', type: 'ai', provider: 'cursor', configured: true, enabled: true }
const gitKey = { id: 'github-key', name: 'Team GitHub', purpose: 'git', secretKind: 'token', type: 'git', provider: 'github', envKey: 'GITHUB_TOKEN', configured: true, enabled: true }
const model = { id: 'team-model', name: 'Team Cursor model', backend: 'cursor', binding: { kind: 'platform', credentialId: key.id, modelId: 'composer-2.5' }, state: 'active', revision: 1 }

async function mockOnboardingApi(page: Page, empty = false) {
 const state = { body: null as BootstrapBody | null, failure: '' }
 await page.route('**/api/**', async (route) => {
  const pathname = new URL(route.request().url()).pathname
  if (!pathname.startsWith('/api/')) return route.continue()
  const json = (body: unknown, status = 200) => route.fulfill({ status, json: body })
  if (pathname === '/api/agent-teams/templates') return json({ items: TEMPLATES })
  if (pathname === '/api/workspace/model-configurations') return json({ items: empty ? [] : [model] })
  if (pathname === '/api/workspace/model-connections') return json({ items: [] })
  if (pathname === '/api/workspace/credentials') return json({ items: empty ? [] : [key, gitKey] })
  if (pathname === '/api/workspace/tool-capabilities') return json({ items: [{ toolId: 'cursor', modes: ['platform'], protocols: [], adapterState: 'existing' }] })
  if (pathname === '/api/workspace') return json({ name: 'Workspace' })
  if (pathname.endsWith('/bootstrap-onboarding')) {
   state.body = route.request().postDataJSON() as BootstrapBody
   if (state.failure) return json({ error: state.failure }, 400)
   if (!state.body.modelConfigurationId) return json({ error: 'Select a saved model' }, 400)
   return json({ agentIds: state.body.agents?.map((row) => row.name), workflowId: 'wf-onboard-1', published: true })
  }
  return json({ items: [] })
 })
 return state
}

test.use({ viewport: { width: 1280, height: 800 } })
const next = (page: Page) => page.getByTestId('onboarding-next').click()
const step = (page: Page, id: string) => expect(page.getByTestId(`onboarding-rail-${id}`)).toHaveAttribute('data-active', '1')
async function chooseModel(page: Page) {
 const picker = page.getByTestId('model-binding-configuration-select')
 await picker.locator('[data-test="app-select-trigger"]').click()
 const panel = page.locator('[data-test="app-select-panel"]:not([aria-hidden="true"])')
 await panel.locator('[data-test="app-select-search"]').fill('Team Cursor')
 await panel.locator('[data-test="app-select-option-team-model"]').click()
}
async function walkToTeam(page: Page, lang = 'zh-CN', withGit = false) {
 await page.goto(`/onboarding-wizard.html?lang=${lang}`)
 if (lang === 'en') await page.getByTestId('onboarding-language-en').click()
 await next(page)
 await step(page, 'model')
 await chooseModel(page)
 await expect(page.locator('select')).toHaveCount(0)
 await next(page)
 await step(page, 'git')
 await page.getByTestId('onboarding-git-user-name').fill('Ada Lovelace')
 await page.getByTestId('onboarding-git-user-email').fill('ada@example.com')
 if (withGit) {
  await page.getByTestId('onboarding-repo-url').fill('https://github.com/example/app.git')
  await page.getByTestId('git-credential-add').locator('[data-test="app-select-trigger"]').click()
  await page.locator('[data-test="app-select-option-github-key"]').click()
  await expect(page.getByTestId('git-preferred-github-key')).toHaveAttribute('aria-pressed', 'true')
 } else await page.getByTestId('onboarding-git-skip').click()
 await next(page)
 await step(page, 'team')
}

for (const lang of ['zh-CN', 'en']) {
 test(`onboarding selects a saved model and Git references (${lang})`, async ({ page }) => {
  const state = await mockOnboardingApi(page)
  await walkToTeam(page, lang, true)
  await page.getByTestId('onboarding-team-name-implement').fill(lang === 'en' ? 'BuildApp' : '编码')
  await next(page)
  await step(page, 'workflow')
  for (const id of ['clarify', 'implement', 'test_review', 'deliver']) await expect(page.getByTestId(`onboarding-preview-node-${id}`)).toBeVisible()
  await page.screenshot({ path: path.join(OUT, `saved-model-review-${lang}.png`) })
  await next(page)
  await expect(page.getByTestId('onboarding-success')).toBeVisible()
  expect(state.body).toMatchObject({ modelConfigurationId: model.id, gitBindings: { items: [{ credentialId: gitKey.id }], preferredCredentialId: gitKey.id }, language: lang })
  expect(state.body).not.toHaveProperty('apiKey')
  expect(state.body).not.toHaveProperty('acpBackend')
  expect(state.body?.agents?.find((row) => row.templateId === 'implement')?.name).toBe(lang === 'en' ? 'BuildApp' : '编码')
  await page.getByTestId('onboarding-edit-workflow').click()
  await expect(page.getByTestId('workflow-editor-landing')).toBeVisible()
 })
}

test('model selection is required and initial setup can manage credentials and models', async ({ page }) => {
 const state = await mockOnboardingApi(page, true)
 await page.goto('/onboarding-wizard.html')
 await next(page)
 await next(page)
 await step(page, 'model')
 expect(state.body).toBeNull()
 await page.getByTestId('onboarding-manage-models').click()
 await expect(page.getByTestId('workspace-credentials-panel')).toBeVisible()
 await page.getByTestId('workspace-credential-create').click()
 await expect(page.getByTestId('workspace-add-credential')).toBeVisible()
 await expect(page.getByTestId('workspace-add-model')).toBeVisible()
})

test('optional roles are removed from preview and bootstrap', async ({ page }) => {
 const state = await mockOnboardingApi(page)
 await walkToTeam(page)
 await page.getByTestId('onboarding-team-toggle-test_review').uncheck()
 await next(page)
 await expect(page.getByTestId('onboarding-preview-node-test_review')).toHaveCount(0)
 await next(page)
 await expect(page.getByTestId('onboarding-success')).toBeVisible()
 expect(state.body?.agents?.map((row) => row.templateId)).toEqual(['clarify', 'implement', 'deliver'])
 expect(state.body?.gitBindings).toEqual({ items: [] })
})

test('configuration errors keep the wizard open with its selections', async ({ page }) => {
 const state = await mockOnboardingApi(page)
 state.failure = 'The selected model configuration is disabled'
 await walkToTeam(page, 'en')
 await next(page)
 await next(page)
 await expect(page.getByTestId('onboarding-create-error')).toContainText(state.failure)
 await step(page, 'workflow')
 await page.getByTestId('onboarding-prev').click()
 await page.getByTestId('onboarding-prev').click()
 await page.getByTestId('onboarding-prev').click()
 await step(page, 'model')
 await expect(page.getByTestId('model-binding-configuration-select')).toContainText(model.name)
})
