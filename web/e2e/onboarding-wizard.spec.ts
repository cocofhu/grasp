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
 gitBindings?: { items: { credentialId: string }[] }
 agents?: { templateId: string; name: string; modelConfigurationId?: string }[]
 language?: string
}
const key = { id: 'cursor-key', name: 'Team Cursor key', purpose: 'tool_auth', toolId: 'cursor', issuer: 'cursor', secretKind: 'api_key', type: 'ai', provider: 'cursor', configured: true, enabled: true }
const gitKey = { id: 'github-key', name: 'Team GitHub', purpose: 'git', secretKind: 'token', type: 'git', provider: 'github', envKey: 'GITHUB_TOKEN', configured: true, enabled: true }
const model = { id: 'team-model', name: 'Team Cursor model', backend: 'cursor', binding: { kind: 'platform', credentialId: key.id, modelId: 'composer-2.5' }, state: 'active', revision: 1 }

async function mockOnboardingApi(page: Page, empty = false) {
 const state = {
  body: null as BootstrapBody | null,
  failure: '',
  credentials: (empty ? [] : [key, gitKey]) as Record<string, unknown>[],
  configurations: (empty ? [] : [model]) as Record<string, unknown>[],
  connections: [] as Record<string, unknown>[],
  connectionWrites: [] as Record<string, unknown>[],
  credentialWrites: [] as Record<string, unknown>[],
  configurationWrites: [] as Record<string, unknown>[],
 }
 await page.route('**/api/**', async (route) => {
  const pathname = new URL(route.request().url()).pathname
  if (!pathname.startsWith('/api/')) return route.continue()
  const json = (body: unknown, status = 200) => route.fulfill({ status, json: body })
  if (pathname === '/api/agent-teams/templates') return json({ items: TEMPLATES })
  if (pathname === '/api/workspace/model-configurations') {
   if (route.request().method() === 'POST') {
    const body = route.request().postDataJSON()
    state.configurationWrites.push(body)
    const created = { ...body, id: 'created-model', revision: 1 }
    state.configurations.push(created)
    return json(created, 201)
   }
   return json({ items: state.configurations })
  }
  if (pathname === '/api/workspace/model-connections') {
   if (route.request().method() === 'POST') {
    const body = route.request().postDataJSON()
    state.connectionWrites.push(body)
    const created = { ...body, id: 'created-connection', revision: 1 }
    state.connections.push(created)
    return json(created, 201)
   }
   return json({ items: state.connections })
  }
  if (/^\/api\/workspace\/model-connections\/[^/]+\/models$/.test(pathname)) return json({ models: [] })
  if (pathname.startsWith('/api/workspace/model-connections/')) {
   const id = pathname.split('/').at(-1)
   const index = state.connections.findIndex((row) => row.id === id)
   if (index < 0) return json({ error: 'Missing connection' }, 404)
   if (route.request().method() === 'PUT') {
    const body = route.request().postDataJSON()
    state.connectionWrites.push(body)
    state.connections[index] = { ...state.connections[index], ...body }
   }
   return json(state.connections[index])
  }
  if (pathname === '/api/workspace/credentials') {
   if (route.request().method() === 'POST') {
    const body = route.request().postDataJSON()
    state.credentialWrites.push(body)
    const created = { ...body, id: `created-key-${state.credentialWrites.length}`, value: undefined, configured: true, enabled: true, masked: '••••5678' }
    state.credentials.push(created)
    return json(created, 201)
   }
   return json({ items: state.credentials })
  }
  if (pathname === '/api/workspace/tool-capabilities') return json({ items: [{ toolId: 'cursor', modes: ['platform'], protocols: [], adapterState: 'existing' }, { toolId: 'opencode', modes: ['platform', 'connection'], protocols: ['openai-chat-completions'], adapterState: 'existing' }, { toolId: 'deepseek_harness', modes: ['platform', 'connection'], protocols: ['openai-chat-completions', 'anthropic-messages'], adapterState: 'existing' }] })
  if (pathname === '/api/workspace') return json({ name: 'Workspace' })
  if (pathname.endsWith('/bootstrap-onboarding')) {
   state.body = route.request().postDataJSON() as BootstrapBody
   if (state.failure) return json({ error: state.failure }, 400)
   if (!state.body.modelConfigurationId) return json({ error: 'Select a saved model' }, 400)
   return json({ agentIds: state.body.agents?.map((row) => row.name), soloAgentId: 'Solo', workflowId: 'wf-onboard-1', published: true })
  }
  return json({ items: [] })
 })
 return state
}

test.use({ viewport: { width: 1280, height: 800 }, baseURL: 'http://127.0.0.1:5174' })
const next = (page: Page) => page.getByTestId('onboarding-next').click()
const step = (page: Page, id: string) => expect(page.getByTestId(`onboarding-rail-${id}`)).toHaveAttribute('data-active', '1')
async function cancelToolSetup(page: Page) {
 await expect(page.getByTestId('model-configuration-form')).toHaveAttribute('data-step', '1')
 await page.getByTestId('model-configuration-back').click()
 await expect(page.getByTestId('model-binding-configuration-select')).toBeVisible()
}
async function chooseModel(page: Page) {
 await cancelToolSetup(page)
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
 await step(page, 'credentials')
 await expect(page.getByRole('dialog')).toHaveCount(1)
 await expect(page.getByTestId('workspace-add-model')).toHaveCount(0)
 await next(page)
 await step(page, 'model')
 await chooseModel(page)
 await expect(page.locator('select')).toHaveCount(0)
 await next(page)
 await step(page, 'gitCredentials')
 if (withGit) {
  await page.getByTestId('git-credential-add').locator('[data-test="app-select-trigger"]').click()
  await page.locator('[data-test="app-select-option-github-key"]').click()
  await expect(page.getByTestId('git-preferred-github-key')).toHaveCount(0)
 }
 await next(page)
 await step(page, 'git')
 await page.getByTestId('onboarding-git-user-name').fill('Ada Lovelace')
 await page.getByTestId('onboarding-git-user-email').fill('ada@example.com')
 if (withGit) await page.getByTestId('onboarding-repo-url').fill('https://github.com/example/app.git')
 else await page.getByTestId('onboarding-git-skip').click()
 await next(page)
 await step(page, 'team')
 await expect(page.getByTestId('onboarding-solo-agent')).toContainText('Solo')
 await expect(page.locator('[data-testid^="onboarding-team-card-"]')).toHaveCount(4)
}

for (const lang of ['zh-CN', 'en']) {
 test(`onboarding selects a saved model and Git references (${lang})`, async ({ page }) => {
  const state = await mockOnboardingApi(page)
  await walkToTeam(page, lang, true)
  await page.screenshot({ path: path.join(OUT, `solo-default-team-${lang}.png`), animations: 'disabled' })
  const implementName = page.getByTestId('onboarding-team-name-implement')
  await expect(implementName).toHaveAttribute('readonly', '')
  await expect(implementName).toHaveValue(lang === 'en' ? 'Implement' : '实现')
  await next(page)
  await step(page, 'workflow')
  for (const id of ['clarify', 'implement', 'test_review', 'deliver']) await expect(page.getByTestId(`onboarding-preview-node-${id}`)).toBeVisible()
  await page.screenshot({ path: path.join(OUT, `saved-model-review-${lang}.png`) })
  await next(page)
  await expect(page.getByTestId('onboarding-success')).toBeVisible()
  await expect(page.getByTestId('onboarding-success-agents')).toContainText('Solo')
  await expect(page.getByTestId('onboarding-success-agents').locator('li')).toHaveCount(6)
  expect(state.body).toMatchObject({ modelConfigurationId: model.id, gitBindings: { items: [{ credentialId: gitKey.id }] }, language: lang })
  expect(state.body).not.toHaveProperty('apiKey')
  expect(state.body).not.toHaveProperty('acpBackend')
  expect(state.body?.agents?.find((row) => row.templateId === 'implement')?.name).toBe(lang === 'en' ? 'Implement' : '实现')
  await page.getByTestId('onboarding-edit-workflow').click()
  await expect(page.getByTestId('workflow-editor-landing')).toBeVisible()
 })
}

async function expectInlineSetup(page: Page) {
 await expect(page.getByRole('dialog')).toHaveCount(1)
 await expect(page.locator('[aria-modal="true"]')).toHaveCount(1)
 await expect(page.getByTestId('onboarding-wizard')).not.toContainText('保存或取消当前表单后，可继续下一步。')
 await expect(page.getByTestId('onboarding-wizard')).not.toContainText('Save or cancel this form before continuing.')
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
 const panel = page.getByTestId('workspace-credentials-panel')
 await expect(panel.getByRole('dialog')).toHaveCount(0)
 const box = await panel.boundingBox()
 expect(box).not.toBeNull()
 expect(box!.x).toBeGreaterThanOrEqual(0)
 expect(box!.x + box!.width).toBeLessThanOrEqual(page.viewportSize()!.width + 1)
 for (const editor of await page.getByTestId('credential-inline-editor').all()) {
  expect(await editor.evaluate((element) => element.scrollWidth <= element.clientWidth + 1)).toBe(true)
 }
}

for (const width of [1280, 390]) {
 test(`empty installation creates model and Git credentials inline (${width}px)`, async ({ page }, testInfo) => {
  await page.setViewportSize({ width, height: 900 })
  const state = await mockOnboardingApi(page, true)
  await page.goto('/onboarding-wizard.html')
  await expect(page.getByTestId('onboarding-wizard')).toBeVisible()
  await page.screenshot({ path: path.join(OUT, `welcome-${width}.png`), fullPage: true, animations: 'disabled' })
  const dialog = await page.getByRole('dialog').boundingBox()
  expect(dialog).not.toBeNull()
  if (width === 1280) {
   expect(dialog!.x).toBeGreaterThan(0)
   expect(dialog!.y).toBeGreaterThan(0)
   expect(dialog!.width).toBeLessThan(width)
  }
  await next(page)
  await step(page, 'credentials')
  expect(state.body).toBeNull()
  await expect(page.getByTestId('workspace-credentials-panel')).toBeVisible()
  await expect(page.getByTestId('workspace-add-model')).toHaveCount(0)
  await expect(page.getByTestId('model-binding-configuration-select')).toHaveCount(0)
  await expectInlineSetup(page)
  await expect(page.getByTestId('workspace-credential-create-form')).toHaveAttribute('data-credential-step', '1')
  await expect(page.getByTestId('credential-kind-cursor')).toBeVisible()
  await page.getByTestId('credential-kind-cursor').click()
  await page.getByTestId('workspace-credential-create-next').click()
  await expect(page.getByTestId('onboarding-prev')).toBeDisabled()
  await expect(page.getByTestId('onboarding-next')).toBeDisabled()
  await expect(page.getByTestId('workspace-credential-create-form')).toHaveAttribute('data-credential-step', '2')
  await page.getByTestId('workspace-credential-create-alias').fill('My Cursor account')
  await page.getByTestId('workspace-credential-create-value').fill('test-cursor-secret')
  await expectInlineSetup(page)
  await page.screenshot({ path: testInfo.outputPath(`inline-credential-${width}.png`), fullPage: true, animations: 'disabled' })
  await page.getByTestId('workspace-credential-review-next').click()
  await expect(page.getByTestId('workspace-credentials-panel')).not.toContainText('test-cursor-secret')
  await page.getByTestId('workspace-credential-create-submit').click()
  await step(page, 'model')
  await expect(page.getByTestId('model-configuration-form')).toHaveAttribute('data-step', '1')
  await expect(page.getByTestId('workspace-credential-row')).toHaveCount(0)
  await expect(page.getByTestId('model-binding-configuration-select')).toHaveCount(0)
  await expect(page.getByTestId('onboarding-next')).toBeHidden()
  await expectInlineSetup(page)
  await page.getByTestId('model-configuration-name').fill('My first Cursor model')
  await page.screenshot({ path: path.join(OUT, `inline-model-${width}.png`), fullPage: true, animations: 'disabled' })
  await page.getByTestId('model-configuration-next').click()
  await expect(page.getByTestId('model-configuration-add-credential')).toBeVisible()
  await page.getByTestId('model-configuration-credential-choice').locator('[data-test="app-select-trigger"]').click()
  await page.locator('[data-test="app-select-option-platform:created-key-1"]').click()
  await expect(page.getByTestId('model-configuration-credential-choice')).toContainText('My Cursor account')
  await expectInlineSetup(page)
  await page.getByTestId('model-configuration-next').click()
  await page.getByTestId('model-configuration-save').click()
  await step(page, 'gitCredentials')
  expect(state.credentialWrites).toHaveLength(1)
  expect(state.configurationWrites).toEqual([expect.objectContaining({ name: 'My first Cursor model', backend: 'cursor', binding: { kind: 'platform', credentialId: 'created-key-1' } })])
  await expect(page.getByTestId('workspace-credential-create-form')).toHaveAttribute('data-credential-step', '1')
  await expect(page.getByTestId('credential-kind-cursor')).toHaveCount(0)
  await expect(page.getByTestId('credential-kind-model_api')).toHaveCount(0)
  await page.getByTestId('credential-kind-github').click()
  await page.getByTestId('workspace-credential-create-next').click()
  await page.getByTestId('workspace-credential-create-alias').fill('My GitHub account')
  await page.getByTestId('workspace-credential-create-value').fill('test-github-secret')
  await expectInlineSetup(page)
  await page.getByTestId('workspace-credential-review-next').click()
  await page.getByTestId('workspace-credential-create-submit').click()
  await expect(page.getByTestId('git-credential-row-created-key-2')).toContainText('My GitHub account')
  await expect(page.getByTestId('workspace-credentials-panel')).not.toContainText('test-github-secret')
  await next(page)
  await step(page, 'git')
  await page.getByTestId('onboarding-git-user-name').fill('Ada Lovelace')
  await page.getByTestId('onboarding-git-user-email').fill('ada@example.com')
  await page.getByTestId('onboarding-repo-url').fill('https://github.com/example/app.git')
  await next(page)
  await step(page, 'team')
  await next(page)
  await next(page)
  await expect(page.getByTestId('onboarding-success')).toBeVisible()
  await expect(page.getByTestId('onboarding-success-agents')).toContainText('Solo')
  await expect(page.getByTestId('onboarding-success-agents').locator('li')).toHaveCount(6)
  expect(state.body).toMatchObject({ modelConfigurationId: 'created-model', gitBindings: { items: [{ credentialId: 'created-key-2' }] }, gitUserName: 'Ada Lovelace', repoUrl: 'https://github.com/example/app.git' })
  expect(JSON.stringify(state.body)).not.toContain('test-cursor-secret')
  expect(JSON.stringify(state.body)).not.toContain('test-github-secret')
  await expect(page.getByRole('dialog')).toHaveCount(1)
 })
}

test('empty installation configures an API account and model connection inline', async ({ page }) => {
 const state = await mockOnboardingApi(page, true)
 await page.goto('/onboarding-wizard.html')
 await next(page)
 await step(page, 'credentials')
 await expect(page.getByTestId('workspace-credential-create-form')).toHaveAttribute('data-credential-step', '1')
 await expect(page.getByTestId('credential-kind-model_api')).toBeVisible()
 await page.getByTestId('credential-kind-model_api').click()
 await page.getByTestId('workspace-credential-create-next').click()
 await expect(page.getByTestId('workspace-credential-create-form')).toHaveAttribute('data-credential-step', '2')
 await page.getByTestId('workspace-credential-create-alias').fill('My DeepSeek account')
 await page.getByTestId('workspace-credential-create-value').fill('test-api-secret')
 await expect(page.getByTestId('model-credential-connection-base-url')).toHaveValue('https://api.deepseek.com/v1')
 await expectInlineSetup(page)
 await page.getByTestId('workspace-credential-review-next').click()
 await page.getByTestId('workspace-credential-create-submit').click()
 await step(page, 'model')
 await expect(page.getByTestId('model-configuration-form')).toHaveAttribute('data-step', '1')
 await expect(page.getByTestId('workspace-credential-row')).toHaveCount(0)
 await expect(page.getByTestId('model-binding-configuration-select')).toHaveCount(0)
 await page.getByTestId('model-configuration-name').fill('My API model')
 await page.getByTestId('model-configuration-backend-opencode').click()
 await page.getByTestId('model-configuration-next').click()
 await page.getByTestId('model-configuration-credential-choice').locator('[data-test="app-select-trigger"]').click()
 await page.locator('[data-test="app-select-option-connection:created-connection"]').click()
 await expect(page.getByTestId('model-configuration-credential-choice')).toContainText('My DeepSeek account')
 await expectInlineSetup(page)
 await page.getByTestId('model-configuration-next').click()
 await page.getByTestId('model-configuration-model-id').locator('[data-test="app-select-trigger"]').click()
 const panel = page.locator('[data-test="app-select-panel"]:not([aria-hidden="true"])')
 await panel.locator('[data-test="app-select-search"]').fill('deepseek-chat')
 await panel.locator('[data-test="app-select-custom"]').click()
 await page.getByTestId('model-configuration-save').click()
 await step(page, 'gitCredentials')
 expect(state.credentialWrites).toEqual([expect.objectContaining({ purpose: 'model_api', issuer: 'deepseek', value: 'test-api-secret' })])
 expect(state.connections).toEqual([expect.objectContaining({ credentialId: 'created-key-1', baseUrl: 'https://api.deepseek.com/v1', models: [{ id: 'deepseek-chat', source: 'manual' }] })])
 expect(state.configurationWrites).toEqual([expect.objectContaining({ backend: 'opencode', binding: { kind: 'connection', connectionId: 'created-connection', modelId: 'deepseek-chat' } })])
 expect(JSON.stringify(state.connectionWrites)).not.toContain('test-api-secret')
 await page.getByTestId('workspace-credentials-panel').getByRole('button', { name: '取消' }).click()
 await next(page)
 await step(page, 'git')
 await page.getByTestId('onboarding-git-user-name').fill('Ada Lovelace')
 await page.getByTestId('onboarding-git-user-email').fill('ada@example.com')
 await page.getByTestId('onboarding-git-skip').click()
 await next(page)
 await next(page)
 await next(page)
 await expect(page.getByTestId('onboarding-success')).toBeVisible()
 expect(state.body?.modelConfigurationId).toBe('created-model')
 expect(JSON.stringify(state.body)).not.toContain('test-api-secret')
})

for (const backend of ['opencode', 'deepseek_harness']) {
 test(`creating a missing official ${backend} credential stays inside the current coding-tool draft`, async ({ page }) => {
  const state = await mockOnboardingApi(page)
  await page.goto('/onboarding-wizard.html')
  await next(page)
  await next(page)
  await step(page, 'model')
  await expect(page.getByTestId('model-configuration-form')).toHaveAttribute('data-step', '1')
  await page.getByTestId('model-configuration-name').fill('Keep tool draft')
  await page.getByTestId(`model-configuration-backend-${backend}`).click()
  await page.getByTestId('model-configuration-next').click()
  await page.getByTestId('model-configuration-add-credential').click()
  await expect(page.getByTestId('workspace-credential-create-form')).toHaveAttribute('data-credential-step', '2')
  await expect(page.getByTestId('model-credential-issuer')).toHaveCount(0)
  await expect(page.getByTestId('model-credential-connection-base-url')).toHaveCount(0)
  await expectInlineSetup(page)
  await page.getByTestId('workspace-credential-create-alias').fill('My official tool account')
  await page.getByTestId('workspace-credential-create-value').fill('test-official-tool-secret')
  await page.getByTestId('workspace-credential-review-next').click()
  await page.getByTestId('workspace-credential-create-submit').click()
  await step(page, 'model')
  await expect(page.getByTestId('model-configuration-form')).toHaveAttribute('data-step', '2')
  await expect(page.getByTestId('model-configuration-credential-choice')).toContainText('My official tool account')
  await page.getByTestId('model-configuration-back').click()
  await expect(page.getByTestId('model-configuration-name')).toHaveValue('Keep tool draft')
  await page.getByTestId('model-configuration-next').click()
  await page.getByTestId('model-configuration-next').click()
  await page.getByTestId('model-configuration-save').click()
  await step(page, 'gitCredentials')
  expect(state.configurationWrites).toEqual([expect.objectContaining({ name: 'Keep tool draft', backend, binding: { kind: 'platform', credentialId: 'created-key-1' } })])
  expect(state.credentialWrites).toEqual([expect.objectContaining({ purpose: 'tool_auth', toolId: backend, provider: backend, secretKind: 'api_key', envKey: backend === 'opencode' ? 'GRASP_OPENCODE_API_KEY' : 'GRASP_DEEPSEEK_API_KEY' })])
  expect(state.connectionWrites).toHaveLength(0)
  expect(JSON.stringify(state.configurationWrites)).not.toContain('test-official-tool-secret')
  await expect(page.getByRole('dialog')).toHaveCount(1)
 })
}

test('the four workflow roles stay locked and an empty Solo is created separately', async ({ page }) => {
 const state = await mockOnboardingApi(page)
 await walkToTeam(page)
 await expect(page.getByTestId('onboarding-team-toggle-test_review')).toHaveCount(0)
 await expect(page.getByTestId('onboarding-team-name-test_review')).toHaveAttribute('readonly', '')
 await expect(page.getByTestId('onboarding-team-name-test_review')).toHaveValue('测试评审')
 await next(page)
 await expect(page.getByTestId('onboarding-preview-node-test_review')).toBeVisible()
 await expect(page.getByTestId('onboarding-preview-node-Solo')).toHaveCount(0)
 await expect(page.getByTestId('onboarding-preview-node-solo')).toHaveCount(0)
 await next(page)
 await expect(page.getByTestId('onboarding-success')).toBeVisible()
 await expect(page.getByTestId('onboarding-success-agents')).toContainText('Solo')
 await expect(page.getByTestId('onboarding-success-agents').locator('li')).toHaveCount(6)
 expect(state.body?.agents).toHaveLength(4)
 expect(state.body?.agents?.map((row) => row.templateId)).toEqual(['clarify', 'implement', 'test_review', 'deliver'])
 expect(state.body?.agents?.map((row) => row.name)).toEqual(['需求澄清', '实现', '测试评审', '交付'])
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
 for (let i = 0; i < 4; i++) await page.getByTestId('onboarding-prev').click()
 await step(page, 'model')
 await expect(page.getByTestId('model-configuration-name')).toHaveValue(model.name)
 await cancelToolSetup(page)
 await expect(page.getByTestId('model-binding-configuration-select')).toContainText(model.name)
})
