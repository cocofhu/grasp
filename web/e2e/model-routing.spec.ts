import { expect, test, type Page, type TestInfo } from '@playwright/test'
import { mkdir } from 'node:fs/promises'
import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'
import { routeApi } from './helpers/apiRoute'

const key = { id: 'key-model', name: 'Shared model key', purpose: 'model_api', issuer: 'custom', secretKind: 'api_key', type: 'ai', configured: true, enabled: true, masked: '••••1234' }
const caps = [
  { toolId: 'cursor', modes: ['platform'], protocols: [] },
  { toolId: 'codex', modes: ['platform', 'connection'], protocols: ['openai-responses'] },
  { toolId: 'codebuddy', modes: ['platform', 'connection'], protocols: ['openai-chat-completions'] },
  { toolId: 'opencode', modes: ['platform', 'connection'], protocols: ['openai-chat-completions', 'openai-responses', 'anthropic-messages'] },
  { toolId: 'deepseek_harness', modes: ['platform', 'connection'], protocols: ['openai-chat-completions', 'anthropic-messages'] },
].map((cap) => ({ ...cap, adapterState: 'existing', verification: 'not-run', evidence: [] }))
type ConfigurationRow = { id: string; name: string; backend: string; binding: { kind: string; connectionId?: string; credentialId?: string; modelId?: string; auxiliaryModelIds?: string[] }; state: string; revision: number }
const connection = (id: string, protocol: string, modelId: string) => ({ id, name: id, providerId: 'same-vendor', protocol, baseUrl: `https://${id}.example/v1`, credentialId: key.id, auth: { scheme: 'bearer' }, models: [{ id: modelId, name: modelId, source: 'manual' }], state: 'active', revision: 1 })

async function setup(page: Page, initial = [] as ReturnType<typeof connection>[]) {
  await installWorkspaceApiDefaults(page)
  const credentials: Record<string, unknown>[] = [key]
  const rows = [...initial]
  const keyWrites: Record<string, unknown>[] = []
  const connectionWrites: Record<string, unknown>[] = []
  const agentWrites: Record<string, unknown>[] = []
  const configurations: ConfigurationRow[] = []
  const configurationWrites: Record<string, unknown>[] = []
  const modelCatalogs: Record<string, { models: Record<string, unknown>[]; error?: string }> = {}
  const modelCatalogReads: string[] = []
  const agents: Record<string, unknown>[] = []
  const shared: Record<string, unknown> = { files: [], mcp: [], env: {}, layout: {} }
  const sharedWrites: Record<string, unknown>[] = []
  await routeApi(page, '**/api/workspace/shared-agent-config', async (route) => {
    if (route.request().method() === 'PUT') {
      const body = route.request().postDataJSON()
      sharedWrites.push(body)
      for (const key of Object.keys(shared)) delete shared[key]
      Object.assign(shared, body)
    }
    await route.fulfill({ json: shared })
  })
  await routeApi(page, '**/api/workspace/credentials', async (route) => {
    if (route.request().method() === 'POST') {
      const body = route.request().postDataJSON()
      keyWrites.push(body)
      const created = { ...body, id: 'new-key', value: undefined, configured: true, enabled: true, masked: '••••5678' }
      credentials.push(created)
      return route.fulfill({ status: 201, json: created })
    }
    await route.fulfill({ json: { items: credentials } })
  })
  await routeApi(page, '**/api/workspace/tool-capabilities', async (route) => route.fulfill({ json: { items: caps } }))
  await routeApi(page, '**/api/workspace/model-connections', async (route) => {
    if (route.request().method() === 'POST') {
      const body = route.request().postDataJSON()
      connectionWrites.push(body)
      const created = { ...body, id: 'new-service', revision: 1 }
      rows.push(created)
      return route.fulfill({ status: 201, json: created })
    }
    await route.fulfill({ json: { items: rows } })
  })
  await routeApi(page, '**/api/workspace/model-connections/*', async (route) => {
    const id = new URL(route.request().url()).pathname.split('/').at(-1)
    const index = rows.findIndex((row) => row.id === id)
    if (index < 0) return route.fulfill({ status: 404, json: { error: 'missing service' } })
    if (route.request().method() === 'PUT') {
      const body = route.request().postDataJSON()
      connectionWrites.push(body)
      rows[index] = { ...rows[index]!, ...body, revision: rows[index]!.revision + 1 }
    }
    await route.fulfill({ json: rows[index] })
  })
  await routeApi(page, '**/api/workspace/model-connections/*/models', async (route) => {
    const id = decodeURIComponent(new URL(route.request().url()).pathname.split('/').at(-2) || '')
    modelCatalogReads.push(id)
    await route.fulfill({ json: modelCatalogs[id] || { models: [] } })
  })
  await routeApi(page, '**/api/workspace/model-configurations', async (route) => {
    if (route.request().method() === 'POST') {
      const body = route.request().postDataJSON()
      configurationWrites.push(body)
      const created = { ...body, id: 'configured-model', revision: 1, state: body.state || 'active' }
      configurations.push(created)
      return route.fulfill({ status: 201, json: created })
    }
    await route.fulfill({ json: { items: configurations } })
  })
  await routeApi(page, '**/api/workspace/model-configurations/*', async (route) => {
    const id = new URL(route.request().url()).pathname.split('/').at(-1)
    const index = configurations.findIndex((row) => row.id === id)
    if (index < 0) return route.fulfill({ status: 404, json: { error: 'missing configuration' } })
    if (route.request().method() === 'DELETE') { configurations.splice(index, 1); return route.fulfill({ json: { status: 'ok' } }) }
    if (route.request().method() === 'PUT') { const body = route.request().postDataJSON(); configurationWrites.push(body); configurations[index] = { ...configurations[index], ...body, revision: configurations[index]!.revision + 1 } }
    await route.fulfill({ json: configurations[index] })
  })
  await routeApi(page, '**/api/agents', async (route) => {
    if (route.request().method() === 'POST') {
      const body = route.request().postDataJSON()
      agentWrites.push(body)
      agents.push(body)
      return route.fulfill({ json: body })
    }
    await route.fulfill({ json: agents })
  })
  await routeApi(page, '**/api/agents/*', async (route) => {
    const name = decodeURIComponent(new URL(route.request().url()).pathname.split('/').at(-1) || '')
    const index = agents.findIndex((agent) => agent.name === name)
    if (index < 0) return route.fallback()
    if (route.request().method() === 'PUT') {
      const body = route.request().postDataJSON()
      agents[index] = body
      agentWrites.push(body)
    }
    await route.fulfill({ json: agents[index] })
  })
  return { rows, keyWrites, connectionWrites, agentWrites, configurations, configurationWrites, agents, credentials, shared, sharedWrites, modelCatalogs, modelCatalogReads }
}

async function openCredentials(page: Page) {
  await page.goto('/workspace-config.html?view=/settings/credentials')
  await expect(page.getByTestId('workspace-credentials-panel')).toBeVisible()
}

async function chooseCredential(page: Page, value: string) {
  await page.getByTestId('model-configuration-credential-choice').locator('[data-test="app-select-trigger"]').click()
  await page.locator('[data-test="app-select-panel"]:not([aria-hidden="true"])').locator(`[data-test="app-select-option-${value}"]`).click()
}

async function enterModel(page: Page, value: string) {
  await page.getByTestId('model-configuration-model-id').locator('[data-test="app-select-trigger"]').click()
  const panel = page.locator('[data-test="app-select-panel"]:not([aria-hidden="true"])')
  await panel.locator('[data-test="app-select-search"]').fill(value)
  await panel.locator('[data-test="app-select-custom"]').click()
}

async function settleVisualTransitions(page: Page) {
  await page.evaluate(async () => {
    await new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())))
    await Promise.allSettled(document.getAnimations().filter((animation) => animation.effect?.getComputedTiming().iterations !== Infinity).map((animation) => animation.finished))
  })
}

async function chooseBoundConfiguration(page: Page, value: string) {
  const inherit = page.getByTestId('agent-inherit-model')
  if (await inherit.count() && await inherit.getAttribute('aria-checked') === 'true') await inherit.click()
  await page.getByTestId('model-binding-configuration-select').locator('[data-test="app-select-trigger"]').click()
  const panel = page.locator('[data-test="app-select-panel"]:not([aria-hidden="true"])')
  await panel.locator('[data-test="app-select-search"]').fill('Team')
  await panel.locator(`[data-test="app-select-option-${value}"]`).click()
}

async function addModelConfiguration(page: Page) {
  await page.getByTestId('workspace-credential-create').click()
  await page.getByTestId('workspace-add-model').click()
  await expect(page.getByTestId('model-configuration-form')).toBeVisible()
}

async function captureRuntimeBindings(page: Page, name: string, testInfo: TestInfo) {
  await mkdir(testInfo.outputDir, { recursive: true })
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 960 })
    if (width === 390 && name === 'agent-inherit-model-git') {
      await expect(page.getByTestId('studio-mobile-back-files')).toBeVisible()
    } else if (name === 'agent-inherit-model-git') {
      await expect(page.getByTestId('model-binding-inherited-summary')).toBeVisible()
    } else {
      await expect(page.getByTestId('model-binding-configuration-select')).toBeVisible()
    }
    await page.screenshot({ path: testInfo.outputPath(`${name}-${width}.png`), fullPage: true, animations: 'disabled' })
  }
  await page.setViewportSize({ width: 1440, height: 960 })
}

test('API accounts edit Base on one page without a separate connections list or another credential selection', async ({ page }, testInfo) => {
  const initial = connection('account-api', 'openai-chat-completions', 'vendor/original/model')
  const state = await setup(page, [initial])
  state.credentials[0] = { ...key, issuer: 'deepseek', provider: 'deepseek' }
  state.credentials.push({ id: 'cli-key', name: 'CursorDefault', purpose: 'tool_auth', toolId: 'cursor', secretKind: 'api_key', type: 'ai', configured: true, enabled: true })
  await openCredentials(page)
  await expect(page.getByTestId('workspace-credential-section-coding').getByRole('heading', { name: 'Agent CLI', level: 3 })).toBeVisible()
  await expect(page.getByTestId('workspace-credential-section-api').getByRole('heading', { name: 'API 账号', level: 3 })).toBeVisible()
  await expect(page.getByTestId('model-connections-manager')).toHaveCount(0)
  await expect(page.getByTestId('model-connection-add')).toHaveCount(0)
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 960 })
    await page.screenshot({ path: testInfo.outputPath(`credential-categories-${width}.png`), fullPage: true, animations: 'disabled' })
  }
  await page.getByTestId('workspace-credential-edit-key-model').click()
  await page.getByTestId('model-credential-edit-service-account-api').click()
  await expect(page.getByTestId('api-account-identity')).toContainText(key.name)
  await expect(page.getByTestId('api-account-identity').locator('[data-provider-logo="deepseek"]')).toBeVisible()
  for (const field of ['model-connection-credential', 'model-connection-provider', 'model-connection-name', 'model-connection-next']) await expect(page.getByTestId(field)).toHaveCount(0)
  await expect(page.getByTestId('model-connection-base-url')).toHaveValue(initial.baseUrl)
  await page.getByTestId('model-connection-base-url').fill('https://updated.example.test/v1')
  await page.screenshot({ path: testInfo.outputPath('api-account-address-390.png'), fullPage: true, animations: 'disabled' })
  await page.getByTestId('model-connection-save').click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(state.keyWrites).toHaveLength(0)
  expect(state.rows).toHaveLength(1)
  expect(state.connectionWrites).toHaveLength(1)
  expect(state.connectionWrites[0]).toMatchObject({ credentialId: key.id, name: initial.name, providerId: initial.providerId, models: initial.models, state: initial.state, baseUrl: 'https://updated.example.test/v1' })
  expect(state.connectionWrites[0]).not.toHaveProperty('value')
  await page.reload()
  await page.getByTestId('workspace-credential-edit-key-model').click()
  await page.getByTestId('model-credential-edit-service-account-api').click()
  await expect(page.getByTestId('model-connection-base-url')).toHaveValue('https://updated.example.test/v1')
})

test('CodeBuddy offers Chat connections and filters unsupported Responses and Messages services', async ({ page }) => {
  const state = await setup(page, [connection('chat-service', 'openai-chat-completions', 'vendor/chat'), connection('responses-service', 'openai-responses', 'vendor/responses'), { ...connection('messages-service', 'anthropic-messages', 'vendor/messages'), auth: { scheme: 'x-api-key' } }])
  await openCredentials(page)
  await addModelConfiguration(page)
  await page.getByTestId('model-configuration-name').fill('CodeBuddy Chat')
  await page.getByTestId('model-configuration-backend-codebuddy').click()
  await page.getByTestId('model-configuration-next').click()
  const credential = page.getByTestId('model-configuration-credential-choice')
  await credential.locator('[data-test="app-select-trigger"]').click()
  await expect(page.getByRole('option')).toHaveCount(1)
  await expect(page.locator('[data-test="app-select-option-connection:chat-service"]')).toBeVisible()
  await expect(page.locator('[data-test="app-select-option-connection:responses-service"]')).toHaveCount(0)
  await expect(page.locator('[data-test="app-select-option-connection:messages-service"]')).toHaveCount(0)
  expect(state.configurationWrites).toEqual([])
})

test('create credentials, then save a reusable tool-credential-model configuration', async ({ page }) => {
  const state = await setup(page)
  await openCredentials(page)
  await expect(page.getByRole('tab')).toHaveText(['凭据', '模型'])
  await page.getByTestId('workspace-credential-create').click()
  await page.getByTestId('workspace-add-credential').click()
  await page.getByTestId('credential-kind-model_api').click()
  await page.getByTestId('workspace-credential-create-next').click()
  await page.getByTestId('workspace-credential-create-alias').fill('Team gateway key')
  await page.getByTestId('model-credential-issuer').locator('[data-test="app-select-trigger"]').click()
  await page.getByRole('option', { name: 'OpenAI', exact: true }).click()
  await expect(page.getByTestId('model-credential-create-connection')).toHaveAttribute('aria-checked', 'true')
  await expect(page.getByTestId('model-credential-connection-base-url')).toHaveValue('https://api.openai.com/v1')
  await page.getByTestId('model-credential-issuer').locator('[data-test="app-select-trigger"]').click()
  await page.getByRole('option', { name: '自定义服务商', exact: true }).click()
  await page.getByTestId('model-credential-provider-label').fill('Team gateway')
  await page.getByTestId('model-credential-connection-advanced').locator('summary').click()
  await page.getByTestId('model-credential-connection-name').fill('Independent gateway')
  await page.getByTestId('model-credential-connection-base-url').fill('https://gateway.example/v1')
  await page.getByTestId('model-credential-connection-protocol').locator('[data-test="app-select-trigger"]').click()
  await page.getByRole('option', { name: 'openai-responses', exact: true }).click()
  await page.getByTestId('workspace-credential-create-value').fill('test-key-no-provider-access')
  await expect(page.getByTestId('model-credential-connection-models')).toHaveCount(0)
  await page.getByTestId('workspace-credential-review-next').click()
  await expect(page.getByRole('dialog')).toContainText('https://gateway.example/v1')
  await expect(page.getByRole('dialog')).not.toContainText('test-key-no-provider-access')
  await page.getByTestId('workspace-credential-create-submit').click()
  await expect.poll(() => state.rows.length).toBe(1)
  expect(state.keyWrites).toHaveLength(1)
  expect(state.keyWrites[0]).toMatchObject({ purpose: 'model_api', issuer: 'Team gateway', provider: 'custom', value: 'test-key-no-provider-access' })
  expect(state.connectionWrites[0]).toMatchObject({ baseUrl: 'https://gateway.example/v1', credentialId: 'new-key', models: [] })
  expect(state.connectionWrites[0]).not.toHaveProperty('value')
  await page.getByRole('tab', { name: '模型', exact: true }).click()
  await expect(page.getByTestId('model-configurations-manager')).toBeVisible()
  await expect(page.getByText('模型目录', { exact: true })).toHaveCount(0)
  await expect(page.getByTestId('model-catalog-panel')).toHaveCount(0)
  await addModelConfiguration(page)
  await page.getByTestId('model-configuration-name').fill('Team Codex model')
  await page.getByTestId('model-configuration-backend-codex').click()
  await page.getByTestId('model-configuration-next').click()
  await chooseCredential(page, 'connection:new-service')
  await page.getByTestId('model-configuration-next').click()
  await enterModel(page, 'vendor/raw/model')
  await page.getByTestId('model-configuration-save').click()
  await expect(page.getByTestId('model-configuration-configured-model')).toContainText('Team Codex model')
  expect(state.configurationWrites[0]).toMatchObject({ name: 'Team Codex model', backend: 'codex', binding: { kind: 'connection', connectionId: 'new-service', modelId: 'vendor/raw/model' } })
  expect(state.rows[0]).toMatchObject({ credentialId: 'new-key', baseUrl: 'https://gateway.example/v1', models: [expect.objectContaining({ id: 'vendor/raw/model' })] })
  expect(state.keyWrites).toHaveLength(1)
})

test('named model configurations preserve tool and credential identity when edited or disabled', async ({ page }) => {
  const state = await setup(page, [connection('responses-account', 'openai-responses', 'vendor/raw/model')])
  state.configurations.push({ id: 'configured-model', name: 'Team Codex model', backend: 'codex', binding: { kind: 'connection', connectionId: 'responses-account', modelId: 'vendor/raw/model' }, state: 'active', revision: 1 })
  await openCredentials(page)
  await page.getByRole('tab', { name: '模型', exact: true }).click()
  const row = page.getByTestId('model-configuration-configured-model')
  await expect(row).toContainText('Team Codex model')
  await row.getByRole('button', { name: '编辑', exact: true }).click()
  await page.getByTestId('model-configuration-name').fill('Updated Codex model')
  await expect(page.getByTestId('model-configuration-backend-codex')).toBeDisabled()
  await page.getByTestId('model-configuration-next').click()
  await page.getByTestId('model-configuration-next').click()
  await page.getByTestId('model-configuration-save').click()
  await expect(row).toContainText('Updated Codex model')
  expect(state.configurations[0]).toMatchObject({ backend: 'codex', binding: { kind: 'connection', connectionId: 'responses-account', modelId: 'vendor/raw/model' } })
  const enabled = row.getByTestId('model-configuration-toggle-configured-model')
  await expect(enabled).toHaveRole('switch')
  await expect(enabled).toHaveAttribute('aria-checked', 'true')
  await enabled.click()
  await expect(enabled).toHaveAttribute('aria-checked', 'false')
  expect(state.configurations[0]!.state).toBe('disabled')
  expect(state.keyWrites).toHaveLength(0)
})

test('Agent binds a saved tool-credential-model configuration by ID', async ({ page }) => {
  const state = await setup(page, [connection('chat-account', 'openai-chat-completions', 'vendor/raw/chat'), connection('responses-account', 'openai-responses', 'vendor/raw/responses')])
  state.configurations.push({ id: 'configured-model', name: 'Team Codex model', backend: 'codex', binding: { kind: 'connection', connectionId: 'responses-account', modelId: 'vendor/raw/responses' }, state: 'active', revision: 1 })
  await page.goto('/agent-create-wizard.html')
  await page.locator('#wiz-name-input').fill('bound-agent')
  await page.getByRole('button', { name: /^下一步/ }).click()
  await expect(page.getByTestId('coding-tool-picker')).toHaveCount(0)
  await chooseBoundConfiguration(page, 'configured-model')
  await page.getByRole('button', { name: /^下一步/ }).click()
  await page.getByRole('button', { name: /^跳过/ }).click()
  await page.getByRole('button', { name: /创建并进入 Studio/ }).click()
  await expect.poll(() => state.agentWrites.length).toBe(1)
  expect(state.agentWrites[0]).toMatchObject({ acpBackend: 'codex', modelBinding: { kind: 'configuration', configurationId: 'configured-model' } })
  expect(state.agentWrites[0]).not.toHaveProperty('value')
  expect((state.agentWrites[0]?.env || {}) as object).not.toHaveProperty('ACP_BRIDGE_MODEL')
})

test('Agent defaults save a model and multiple Git authorizations, and a new Agent inherits them after reload', async ({ page }, testInfo) => {
  const state = await setup(page, [connection('responses-service', 'openai-responses', 'vendor/raw/responses')])
  state.configurations.push({ id: 'team-codex', name: 'Team Codex default', backend: 'codex', binding: { kind: 'connection', connectionId: 'responses-service', modelId: 'vendor/raw/responses' }, state: 'active', revision: 1 })
  state.credentials.push(
    { id: 'gitlab-team', name: 'GitLab team', purpose: 'git', type: 'git', provider: 'gitlab', host: 'https://git.example.test', configured: true, enabled: true },
    { id: 'github-team', name: 'GitHub team', purpose: 'git', type: 'git', provider: 'github', host: 'https://github.com', configured: true, enabled: true },
  )
  await page.goto(`/workspace-config.html?view=${encodeURIComponent('/agents?view=defaults')}`)
  await page.getByTestId('shared-agent-subtab-meta').click()
  await expect(page.getByTestId('coding-tool-picker')).toHaveCount(0)
  await chooseBoundConfiguration(page, 'team-codex')
  await expect(page.getByTestId('meta-config-root')).toHaveCount(0)
  await expect(page.getByTestId('meta-workspace-dir')).toHaveCount(0)
  await expect(page.getByTestId('agent-runtime-bindings').locator('a[href="/settings/credentials"]')).toHaveCount(0)
  for (const id of ['gitlab-team', 'github-team']) {
    await page.getByTestId('git-credential-add').locator('[data-test="app-select-trigger"]').click()
    await page.locator(`[data-test="app-select-option-${id}"]`).click()
  }
  await expect(page.getByTestId('git-preferred-github-team')).toHaveCount(0)
  await expect(page.getByText('可添加多个已保存的授权，并设置首选。')).toHaveCount(0)
  await expect(page.getByText('所选授权均可在沙箱中使用。')).toHaveCount(0)
  await page.getByTestId('shared-agent-save').click()
  await expect.poll(() => state.sharedWrites.length).toBe(1)
  expect(state.sharedWrites[0]).toMatchObject({
    acpBackend: 'codex', modelBinding: { kind: 'configuration', configurationId: 'team-codex' },
    layout: expect.objectContaining({ configRoot: '/root/.codex' }),
    gitBindings: { items: [{ credentialId: 'gitlab-team' }, { credentialId: 'github-team' }] },
  })
  await page.reload()
  await page.getByTestId('shared-agent-subtab-meta').click()
  await expect(page.getByTestId('model-binding-configuration-select')).toContainText('Team Codex default')
  await expect(page.getByTestId('git-preferred-github-team')).toHaveCount(0)
  await captureRuntimeBindings(page, 'agent-default-model-git', testInfo)

  await page.goto('/agent-create-wizard.html')
  await page.locator('#wiz-name-input').fill('inherits-team-defaults')
  await page.getByRole('button', { name: /^下一步/ }).click()
  await expect(page.getByTestId('agent-inherit-model')).toHaveAttribute('aria-checked', 'true')
  await expect(page.getByTestId('model-binding-inherited-summary')).toContainText('Team Codex default')
  await expect(page.getByTestId('model-binding-configuration-select')).toHaveCount(0)
  await page.getByRole('button', { name: /^下一步/ }).click()
  await expect(page.getByTestId('agent-inherit-git')).toHaveAttribute('aria-checked', 'true')
  await expect(page.getByTestId('git-credential-row-gitlab-team')).toBeVisible()
  await expect(page.getByTestId('git-credential-row-github-team')).toBeVisible()
  await expect(page.getByTestId('git-credential-add')).toHaveCount(0)
  await expect(page.getByTestId('git-options-github-team')).toHaveCount(0)
  await page.getByRole('button', { name: /^下一步/ }).click()
  await expect(page.locator('.step-pane')).toContainText('Team Codex default')
  await expect(page.locator('.step-pane')).toContainText('GitHub team')
  await page.getByRole('button', { name: /创建并进入 Studio/ }).click()
  await expect(page.getByTestId('created-name')).toHaveText('inherits-team-defaults')
  expect(state.agentWrites).toHaveLength(1)
  expect(state.agentWrites[0]).toMatchObject({ inheritModel: true, inheritGit: true, gitBindings: { items: [] } })
  expect(state.agentWrites[0]).not.toHaveProperty('modelBinding')
  expect(JSON.stringify(state.agentWrites[0])).not.toContain('vendor/raw/responses')
  expect(JSON.stringify(state.agentWrites[0])).not.toContain('gitlab-team')

  await page.goto(`/workspace-config.html?view=${encodeURIComponent('/agents?agent=inherits-team-defaults&studioTab=meta')}`)
  await expect(page.getByTestId('agent-inherit-default-switch')).toHaveAttribute('aria-checked', 'true')
  await expect(page.getByTestId('agent-inherit-default')).toHaveCount(1)
  const inheritedCard = page.getByTestId('agent-inherit-default').getByTestId('model-binding-inherited-summary')
  await expect(inheritedCard).toContainText('Codex')
  await expect(inheritedCard).toContainText('工作区默认')
  await expect(inheritedCard).not.toContainText('Team Codex default')
  await expect(inheritedCard.locator('[title="Team Codex default"]')).toHaveCount(1)
  for (const id of ['agent-inherit-model', 'agent-inherit-git', 'model-binding-configuration-select', 'meta-config-root', 'meta-workspace-dir', 'git-credential-row-github-team', 'git-credential-row-gitlab-team', 'git-inherited-preferred-github-team', 'git-credential-add']) await expect(page.getByTestId(id)).toHaveCount(0)
  await expect(page.getByTestId('agent-git-credential-picker')).toBeHidden()
  await page.setViewportSize({ width: 1440, height: 960 })
  const card = page.getByTestId('agent-inherit-default')
  const cardSize = () => card.evaluate(element => {
    const { width, height } = element.getBoundingClientRect()
    return { width, height }
  })
  const inheritedSize = await cardSize()
  expect(inheritedSize.height).toBe(120)
  await expect(inheritedCard.locator('[data-provider-logo="codex"]')).toBeVisible()
  await mkdir(testInfo.outputDir, { recursive: true })
  await page.screenshot({ path: testInfo.outputPath('agent-inherit-card-on-desktop.png'), fullPage: true, animations: 'disabled' })
  await page.getByTestId('agent-inherit-default-switch').click()
  await expect(page.getByTestId('agent-inherit-default-switch')).toHaveAttribute('aria-checked', 'false')
  await expect(card.getByTestId('agent-inherit-custom-summary')).toContainText('自定义配置')
  await expect(card.getByTestId('agent-inherit-custom-summary')).toContainText('仅用于当前 Agent')
  await expect(inheritedCard).toHaveCount(0)
  await expect(page.getByTestId('model-binding-configuration-select')).toBeVisible()
  for (const id of ['meta-config-root', 'meta-workspace-dir', 'agent-credential-bindings']) await expect(page.getByTestId(id)).toHaveCount(0)
  await expect(page.getByTestId('agent-runtime-bindings').locator('a[href="/settings/credentials"]')).toHaveCount(0)
  await expect.poll(cardSize).toEqual(inheritedSize)
  await page.screenshot({ path: testInfo.outputPath('agent-inherit-card-off-desktop.png'), fullPage: true, animations: 'disabled' })
  await page.getByTestId('agent-inherit-default-switch').click()
  await expect(page.getByTestId('agent-inherit-default-switch')).toHaveAttribute('aria-checked', 'true')
  await expect(card.getByTestId('agent-inherit-custom-summary')).toHaveCount(0)
  await expect(inheritedCard.locator('[data-provider-logo="codex"]')).toBeVisible()
  await expect(inheritedCard).toContainText('工作区默认')
  await expect.poll(cardSize).toEqual(inheritedSize)
  await captureRuntimeBindings(page, 'agent-inherit-model-git', testInfo)
})

test('API accounts offer the provider model catalog and persist only the chosen primary and auxiliary models', async ({ page }, testInfo) => {
  const saved = connection('tokenhub-account', 'openai-chat-completions', 'saved/manual-model')
  const primary = { id: 'gpt-5.5', name: 'TokenHub GPT 5.5', source: 'discovered', capabilities: { vision: true, reasoning: true }, input: ['text', 'image'], contextWindow: 256000, reasoningEfforts: ['low', 'high'] }
  const auxiliary = { id: 'kimi-k3.1-pro', name: 'TokenHub Kimi', source: 'discovered' }
  const unused = { id: 'glm-5.1', name: 'TokenHub GLM', source: 'discovered' }
  const unusedHunyuan = { id: 'hy3-pro', name: 'TokenHub HY', source: 'discovered' }
  const state = await setup(page, [saved])
  state.modelCatalogs[saved.id] = { models: [primary, auxiliary, unused, unusedHunyuan] }
  await openCredentials(page)
  await page.getByTestId('credentials-tab-models').click()
  await addModelConfiguration(page)
  await page.getByTestId('model-configuration-name').fill('Team provider model')
  await page.getByTestId('model-configuration-backend-opencode').click()
  await page.getByTestId('model-configuration-next').click()
  await chooseCredential(page, `connection:${saved.id}`)
  expect(state.modelCatalogReads).toHaveLength(0)
  await page.getByTestId('model-configuration-next').click()
  await expect.poll(() => state.modelCatalogReads).toEqual([saved.id])
  const field = page.getByTestId('model-configuration-model-id')
  await expect(field.locator('[data-test="app-select-trigger"]')).not.toContainText(primary.name)
  await field.locator('[data-test="app-select-trigger"]').click()
  let panel = page.locator('[data-test="app-select-panel"]:not([aria-hidden="true"])')
  await panel.locator('[data-test="app-select-search"]').fill('TokenHub')
  await expect(panel.locator(`[data-test="app-select-option-${primary.id}"]`)).toContainText(primary.id)
  for (const [id, vendor] of [[primary.id, 'openai'], [auxiliary.id, 'kimi'], [unused.id, 'zhipu'], [unusedHunyuan.id, 'hunyuan']]) {
    const image = panel.locator(`[data-test="app-select-option-${id}"] [data-vendor-mark="${vendor}"] img`)
    await expect(image).toBeVisible()
    await expect.poll(() => image.evaluate((node) => (node as HTMLImageElement).naturalWidth)).toBeGreaterThan(0)
  }
  await settleVisualTransitions(page)
  await page.screenshot({ path: testInfo.outputPath('api-account-discovered-models.png'), fullPage: true, animations: 'disabled' })
  await panel.locator('[data-test="app-select-search"]').press('Enter')
  await expect(field.locator('[data-test="app-select-trigger"]')).toContainText(primary.name)
  await expect(field.locator('[data-test="app-select-trigger"] [data-vendor-mark="openai"] img')).toBeVisible()
  await page.getByTestId('model-configuration-add-model').click()
  await page.getByTestId('model-configuration-aux-select-0').locator('[data-test="app-select-trigger"]').click()
  panel = page.locator('[data-test="app-select-panel"]:not([aria-hidden="true"])')
  await panel.locator('[data-test="app-select-search"]').fill(auxiliary.name)
  await panel.locator('[data-test="app-select-search"]').press('Enter')
  await expect(page.getByTestId('model-configuration-aux-select-0')).toContainText(auxiliary.name)
  await expect(page.getByTestId('model-configuration-aux-select-0').locator('[data-test="app-select-trigger"] [data-vendor-mark="kimi"] img')).toBeVisible()
  expect(state.connectionWrites).toHaveLength(0)
  await page.getByTestId('model-configuration-save').click()
  await expect(page.getByTestId('model-configuration-configured-model')).toContainText('Team provider model')
  expect(state.configurationWrites).toEqual([expect.objectContaining({ backend: 'opencode', binding: { kind: 'connection', connectionId: saved.id, modelId: primary.id, auxiliaryModelIds: [auxiliary.id] } })])
  expect(state.connectionWrites).toEqual([expect.objectContaining({ models: [...saved.models, primary, auxiliary] })])
  expect(state.rows[0]!.models.some((model) => model.id === unused.id)).toBe(false)
  expect(state.rows[0]!.models.some((model) => model.id === unusedHunyuan.id)).toBe(false)
  expect(state.keyWrites).toHaveLength(0)
})

for (const backend of ['opencode', 'deepseek_harness']) {
  test(`${backend} saves its official authorization and binds an Agent without an API connection`, async ({ page }) => {
    const state = await setup(page)
    await openCredentials(page)
    await page.getByTestId('workspace-credential-create').click()
    await page.getByTestId('workspace-add-credential').click()
    await page.getByTestId(`credential-kind-${backend}`).click()
    await page.getByTestId('workspace-credential-create-next').click()
    await expect(page.getByTestId('model-credential-issuer')).toHaveCount(0)
    await expect(page.getByTestId('model-credential-create-connection')).toHaveCount(0)
    await expect(page.getByTestId('model-credential-connection-base-url')).toHaveCount(0)
    await expect(page.getByTestId('workspace-credential-create-value')).toHaveAttribute('type', 'password')
    await page.getByTestId('workspace-credential-create-alias').fill(`Team ${backend} official account`)
    await page.getByTestId('workspace-credential-create-value').fill('test-official-tool-key')
    await page.getByTestId('workspace-credential-review-next').click()
    await expect(page.getByRole('dialog')).not.toContainText('test-official-tool-key')
    await page.getByTestId('workspace-credential-create-submit').click()
    await expect(page.getByTestId('credentials-tab-credentials')).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByTestId('model-configuration-form')).toHaveCount(0)
    const saved = page.getByTestId('workspace-credential-section-coding').getByTestId('workspace-credential-row').filter({ hasText: `Team ${backend} official account` })
    await expect(saved).toBeVisible()
    await expect(page.getByTestId('workspace-credential-section-api')).not.toContainText(`Team ${backend} official account`)
    expect(state.keyWrites).toEqual([{ type: 'ai', purpose: 'tool_auth', toolId: backend, provider: backend, secretKind: 'api_key', envKey: backend === 'opencode' ? 'GRASP_OPENCODE_API_KEY' : 'GRASP_DEEPSEEK_API_KEY', name: `Team ${backend} official account`, value: 'test-official-tool-key' }])
    expect(state.connectionWrites).toHaveLength(0)

    await page.getByTestId('credentials-tab-models').click()
    await addModelConfiguration(page)
    await page.getByTestId('model-configuration-name').fill(`Team ${backend} model`)
    await page.getByTestId(`model-configuration-backend-${backend}`).click()
    await page.getByTestId('model-configuration-next').click()
    await chooseCredential(page, 'platform:new-key')
    await page.getByTestId('model-configuration-next').click()
    await page.getByTestId('model-configuration-save').click()
    await expect(page.getByTestId('model-configuration-configured-model')).toContainText(`Team ${backend} model`)
    expect(state.configurationWrites).toEqual([expect.objectContaining({ backend, binding: { kind: 'platform', credentialId: 'new-key' } })])

    await page.goto('/agent-create-wizard.html')
    await page.locator('#wiz-name-input').fill(`${backend}-bound-agent`)
    await page.getByRole('button', { name: /^下一步/ }).click()
    await expect(page.getByTestId('coding-tool-picker')).toHaveCount(0)
    await chooseBoundConfiguration(page, 'configured-model')
    await page.getByRole('button', { name: /^下一步/ }).click()
    await page.getByRole('button', { name: /^跳过/ }).click()
    await page.getByRole('button', { name: /创建并进入 Studio/ }).click()
    await expect.poll(() => state.agentWrites.length).toBe(1)
    expect(state.agentWrites[0]).toMatchObject({ acpBackend: backend, modelBinding: { kind: 'configuration', configurationId: 'configured-model' } })
    expect(state.keyWrites).toHaveLength(1)
    expect(state.connectionWrites).toHaveLength(0)
    expect(state.configurationWrites).toHaveLength(1)
  })
}

test('model wizard preserves selections on Back and handles an incompatible tool in a narrow viewport', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 844 })
  const state = await setup(page, [connection('chat-service', 'openai-chat-completions', 'vendor/raw/chat')])
  await openCredentials(page)
  await page.getByTestId('credentials-tab-models').click()
  await addModelConfiguration(page)
  await page.getByTestId('model-configuration-name').fill('Narrow OpenCode model')
  const tool = page.getByTestId('model-configuration-backend-opencode')
  await expect(tool).toHaveRole('radio')
  await tool.click()
  await expect(tool).toHaveAttribute('aria-checked', 'true')
  await page.getByTestId('model-configuration-next').click()
  await chooseCredential(page, 'connection:chat-service')
  await page.getByTestId('model-configuration-next').click()
  const model = page.getByTestId('model-configuration-model-id').locator('[data-test="app-select-trigger"]')
  await model.click()
  const popup = page.locator('[data-test="app-select-panel"]:not([aria-hidden="true"])')
  const search = popup.locator('[data-test="app-select-search"]')
  await search.fill('vendor/raw/chat')
  const popupBox = await popup.boundingBox()
  expect(popupBox).not.toBeNull()
  expect(popupBox!.x).toBeGreaterThanOrEqual(0)
  expect(popupBox!.x + popupBox!.width).toBeLessThanOrEqual(320)
  await search.press('Enter')
  await expect(model).toContainText('vendor/raw/chat')
  await page.getByTestId('model-configuration-back').click()
  await chooseCredential(page, 'connection:chat-service')
  await page.getByTestId('model-configuration-next').click()
  await expect(model).toContainText('vendor/raw/chat')
  await page.getByTestId('model-configuration-back').click()
  await page.getByTestId('model-configuration-back').click()
  await page.getByTestId('model-configuration-backend-codex').click()
  await page.getByTestId('model-configuration-next').click()
  await expect(page.getByTestId('model-configuration-credentials-empty')).toBeVisible()
  await expect(page.getByTestId('model-configuration-next')).toBeDisabled()
  await page.getByTestId('model-configuration-add-credential').click()
  await expect(page.getByTestId('credentials-tab-models')).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByTestId('workspace-credential-create-form')).toHaveAttribute('data-credential-step', '1')
  await expect(page.getByTestId('model-configuration-form')).toHaveCount(0)
  await page.getByRole('dialog').locator('button[aria-label="关闭"]').click()
  await expect(page.getByTestId('model-configuration-form')).toHaveAttribute('data-step', '2')
  await page.getByTestId('model-configuration-back').click()
  await expect(page.getByTestId('model-configuration-name')).toHaveValue('Narrow OpenCode model')
  await expect(page.getByTestId('model-configuration-backend-codex')).toHaveAttribute('aria-checked', 'true')
  expect(state.configurationWrites).toHaveLength(0)
  expect(state.connectionWrites).toHaveLength(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

for (const backend of ['cursor', 'opencode', 'deepseek_harness']) {
  test(`model wizard can cancel and create a missing ${backend} credential without losing its draft`, async ({ page }) => {
    const state = await setup(page)
    await openCredentials(page)
    await page.getByTestId('credentials-tab-models').click()
    await addModelConfiguration(page)
    await page.getByTestId('model-configuration-name').fill(`Keep ${backend} draft`)
    await page.getByTestId(`model-configuration-backend-${backend}`).click()
    await page.getByTestId('model-configuration-next').click()
    await expect(page.getByTestId('model-configuration-credentials-empty')).toBeVisible()
    await page.getByTestId('model-configuration-add-credential').click()
    await expect(page.getByTestId('credentials-tab-models')).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByTestId('workspace-credential-create-form')).toHaveAttribute('data-credential-step', '2')
    await page.getByRole('dialog').locator('button[aria-label="关闭"]').click()
    await expect(page.getByTestId('model-configuration-form')).toHaveAttribute('data-step', '2')
    await page.getByTestId('model-configuration-add-credential').click()
    await page.getByTestId('workspace-credential-create-alias').fill(`New ${backend} credential`)
    await expect(page.getByTestId('model-credential-issuer')).toHaveCount(0)
    await expect(page.getByTestId('model-credential-connection-base-url')).toHaveCount(0)
    await page.getByTestId('workspace-credential-create-value').fill('test-new-compatible-key')
    await page.getByTestId('workspace-credential-review-next').click()
    await page.getByTestId('workspace-credential-create-submit').click()
    await expect(page.getByTestId('model-configuration-form')).toHaveAttribute('data-step', '2')
    await expect(page.getByTestId('model-configuration-credential-choice').locator('[data-test="app-select-trigger"]')).toContainText(`New ${backend} credential`)
    await page.getByTestId('model-configuration-back').click()
    await expect(page.getByTestId('model-configuration-name')).toHaveValue(`Keep ${backend} draft`)
    await expect(page.getByTestId(`model-configuration-backend-${backend}`)).toHaveAttribute('aria-checked', 'true')
    await page.getByTestId('model-configuration-next').click()
    await page.getByTestId('model-configuration-next').click()
    await page.getByTestId('model-configuration-save').click()
    await expect(page.getByTestId('model-configuration-configured-model')).toContainText(`Keep ${backend} draft`)
    expect(state.keyWrites).toHaveLength(1)
    expect(state.keyWrites[0]).toMatchObject({ purpose: 'tool_auth', toolId: backend, provider: backend, secretKind: 'api_key' })
    expect(state.connectionWrites).toHaveLength(0)
    expect(state.configurationWrites[0]).toMatchObject({ backend, binding: { kind: 'platform', credentialId: 'new-key' } })
  })
}


test('Solo uses saved model names and sends exact model IDs and advertised effort values', async ({ page }, testInfo) => {
  const primary = 'kimi-k3.1-pro'
  const auxiliary = 'kimi-k2.5'
  const service = {
    ...connection('kimi-service', 'openai-chat-completions', primary),
    models: [{ id: primary, name: 'Kimi upstream model', source: 'manual', reasoningEfforts: ['low', 'high'] }, { id: auxiliary, name: 'Kimi 2.5', source: 'manual' }],
  }
  const state = await setup(page, [service])
  state.configurations.push({ id: 'kimi-config', name: 'Kimi', backend: 'deepseek_harness', binding: { kind: 'connection', connectionId: service.id, modelId: primary, auxiliaryModelIds: [auxiliary] }, state: 'active', revision: 1 })
  state.agents.push({ name: 'Solo', acpBackend: 'deepseek_harness', modelBinding: { kind: 'configuration', configurationId: 'kimi-config' }, gitBindings: { items: [] }, files: [], mcp: [], env: {}, layout: {} })
  const session = { id: 'solo-named-model', title: 'A Solo task', agentName: 'Solo', userId: 'preview', status: 'running', createdAt: '2026-10-26T00:00:00Z', updatedAt: '2026-10-26T00:00:00Z', restarts: 0, busy: false, waiting: 0, repos: [], initialModelId: '', currentModelId: '', currentModelSet: false, reasoningEffort: '', reasoningEfforts: ['low', 'high'] }
  let createBody: Record<string, unknown> | undefined
  const modelWrites: Record<string, unknown>[] = []
  await routeApi(page, '**/api/projects', route => route.fulfill({ json: [] }))
  await routeApi(page, '**/api/solo/sessions**', async route => {
    const pathname = new URL(route.request().url()).pathname
    if (pathname === '/api/solo/sessions') {
      if (route.request().method() === 'POST') {
        createBody = route.request().postDataJSON()
        session.initialModelId = String(createBody?.initialModelId || '')
        session.reasoningEffort = String(createBody?.reasoningEffort || '')
        return route.fulfill({ json: session })
      }
      return route.fulfill({ json: { sessions: createBody ? [session] : [], maxSandboxes: 8 } })
    }
    if (pathname.endsWith('/messages') || pathname.endsWith('/env')) return route.fulfill({ json: [] })
    if (pathname.endsWith('/model')) {
      const body = route.request().postDataJSON() as Record<string, unknown>
      modelWrites.push(body)
      if (Object.hasOwn(body, 'modelId')) {
        session.currentModelId = String(body.modelId || '')
        session.currentModelSet = true
        session.reasoningEfforts = session.currentModelId === auxiliary ? [] : ['low', 'high']
        if (!session.reasoningEfforts.includes(session.reasoningEffort)) session.reasoningEffort = ''
      }
      if (Object.hasOwn(body, 'reasoningEffort')) session.reasoningEffort = String(body.reasoningEffort || '')
      return route.fulfill({ json: session })
    }
    return route.fulfill({ json: session })
  })
  await page.routeWebSocket('**/api/solo/sessions/*/chat*', ws => ws.send(JSON.stringify({ type: 'session', event: 'queue_state', busy: false, items: [] })))
  await page.addInitScript(() => localStorage.setItem('grasp.home.mode', 'solo'))
  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto('http://127.0.0.1:5174/workspace-config.html?view=/dashboard')
  const trigger = page.getByTestId('solo-model-select-trigger')
  const expectModelPill = async (framed = true) => {
    await expect(trigger.locator('[data-vendor-mark="kimi"] img')).toBeVisible()
    await expect(trigger.locator('[data-vendor-mark="deepseek_harness"]')).toHaveCount(0)
    const border = await trigger.evaluate((node) => {
      const style = getComputedStyle(node)
      const opacity = style.borderTopColor.match(/^rgba\([^,]+,[^,]+,[^,]+,\s*([\d.]+)\)$/)
      return { style: style.borderTopStyle, width: parseFloat(style.borderTopWidth), alpha: style.borderTopColor === 'transparent' ? 0 : opacity ? Number(opacity[1]) : 1 }
    })
    expect(border.style).not.toBe('none')
    expect(border.width).toBeGreaterThan(0)
    if (framed) expect(border.alpha).toBeGreaterThan(0)
    else expect(border.alpha).toBe(0)
  }
  await expect(trigger).toContainText('Kimi')
  await expect(trigger).not.toContainText(primary)
  await expectModelPill()
  await trigger.click()
  await expect(page.getByTestId(`solo-model-option-${primary}`)).toContainText('Kimi')
  await expect(page.getByTestId('solo-model-select-panel')).not.toContainText(primary)
  await expect(page.getByTestId('solo-model-select-panel')).not.toContainText(auxiliary)
  await settleVisualTransitions(page)
  await page.screenshot({ path: testInfo.outputPath('solo-model-menu-home-light.png'), animations: 'disabled' })
  await page.getByTestId('solo-effort-open').click()
  const slider = page.getByTestId('solo-effort-slider')
  await expect(slider).toHaveAttribute('max', '2')
  await slider.press('End')
  await expect(slider).toHaveValue('2')
  await expect(page.getByTestId('solo-effort-option-ultra')).toHaveCount(0)
  await expect(page.locator('button[data-testid^="solo-effort-option-"]')).toHaveCount(0)
  await settleVisualTransitions(page)
  await page.screenshot({ path: testInfo.outputPath('solo-model-effort-home.png'), animations: 'disabled' })
  await page.keyboard.press('Escape')
  await page.getByTestId('shell-theme-toggle').click()
  await settleVisualTransitions(page)
  await expectModelPill()
  await trigger.click()
  await expect(page.getByTestId('solo-model-select-panel')).not.toContainText(primary)
  await settleVisualTransitions(page)
  await page.screenshot({ path: testInfo.outputPath('solo-model-menu-home-dark.png'), animations: 'disabled' })
  await page.getByTestId('solo-effort-open').click()
  await expect(slider).toHaveValue('2')
  await settleVisualTransitions(page)
  await page.screenshot({ path: testInfo.outputPath('solo-model-effort-home-dark.png'), animations: 'disabled' })
  await page.keyboard.press('Escape')
  await page.getByTestId('shell-theme-toggle').click()
  await settleVisualTransitions(page)
  await page.getByTestId('home-composer-input').fill('Use the named model')
  await page.getByTestId('home-composer-send').click()
  await expect(page.getByTestId('solo-session-pane')).toBeVisible()
  expect(createBody).toMatchObject({ agentName: 'Solo', reasoningEffort: 'high' })
  expect(createBody).not.toHaveProperty('initialModelId')
  await expect(trigger).toContainText('Kimi')
  await expect(trigger).not.toContainText(primary)
  await expectModelPill(false)
  await trigger.click()
  await expectModelPill(false)
  await expect(page.getByTestId('solo-model-select-panel')).not.toContainText(primary)
  await page.getByTestId('solo-effort-open').click()
  await expect(page.getByTestId('solo-effort-slider')).toHaveValue('2')
  await page.getByTestId('solo-effort-slider').press('Home')
  await expect.poll(() => modelWrites.at(-1)).toEqual({ reasoningEffort: '' })
  await page.getByTestId('solo-effort-slider').press('End')
  await expect.poll(() => modelWrites.at(-1)).toEqual({ reasoningEffort: 'high' })
  await page.getByTestId('solo-effort-reset').click()
  await expect.poll(() => modelWrites.at(-1)).toEqual({ reasoningEffort: '' })
  await page.setViewportSize({ width: 390, height: 900 })
  const panel = await page.getByTestId('solo-model-select-panel').boundingBox()
  expect(panel!.x).toBeGreaterThanOrEqual(0)
  expect(panel!.x + panel!.width).toBeLessThanOrEqual(391)
  await settleVisualTransitions(page)
  await page.screenshot({ path: testInfo.outputPath('solo-model-effort-session-mobile.png'), animations: 'disabled' })
  await page.getByTestId('solo-effort-back').click()
  await page.getByTestId(`solo-model-option-${auxiliary}`).click()
  await expect.poll(() => modelWrites.at(-1)?.modelId).toBe(auxiliary)
  await expect(trigger).toContainText('Kimi 2.5')
  await expect(page.getByTestId('solo-model-effort-label')).toHaveCount(0)
})
