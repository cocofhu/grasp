// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import enCommon from '@/locales/en/common.json'
import pages from '@/locales/zh-CN/pages.json'
import enPages from '@/locales/en/pages.json'
import type { CredentialItem, ModelConnection } from '@/lib/api/api'
import WorkspaceCredentialsPanel from './WorkspaceCredentialsPanel.vue'

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  create: vi.fn(),
  put: vi.fn(),
  del: vi.fn(),
  remove: vi.fn(),
  providers: vi.fn(),
  models: vi.fn(),
  connections: vi.fn(),
  connectionModels: vi.fn(),
  configurations: vi.fn(),
  capabilities: vi.fn(),
  createConnection: vi.fn(),
  createConfiguration: vi.fn(),
  updateConnection: vi.fn(),
  success: vi.fn(),
  error: vi.fn(),
}))

vi.mock('@/lib/api/api', () => ({
  api: {
    listCredentials: mocks.get,
    createCredential: mocks.create,
    putCredential: mocks.put,
    deleteCredential: mocks.del,
    removeCredential: mocks.remove,
    openCodeProviders: mocks.providers,
    openCodeModels: mocks.models,
    listModelConnections: mocks.connections,
    listModelConnectionModels: mocks.connectionModels,
    listModelConfigurations: mocks.configurations,
    listToolCapabilities: mocks.capabilities,
    createModelConnection: mocks.createConnection,
    createModelConfiguration: mocks.createConfiguration,
    updateModelConnection: mocks.updateConnection,
  },
}))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => mocks }))

const config = {
  items: [
    {
      id: 'cursor-api', type: 'ai', kind: 'ai', name: 'Cursor API key', purpose: 'tool_auth', toolId: 'cursor', provider: 'cursor',
      envKey: 'GRASP_CURSOR_API_KEY', configured: true, masked: 'sk-…1234', updatedAt: '2026-01-02T03:04:00Z',
    },
    {
      id: 'github', type: 'git', kind: 'git', name: 'GitHub token', purpose: 'git', provider: 'github',
      envKey: 'GITHUB_TOKEN', configured: false,
    },
    {
      id: 'ssh', type: 'ssh', purpose: 'ssh', secretKind: 'ssh_private_key', kind: 'ssh', name: 'SSH private key', provider: 'ssh',
      configured: false,
    },
  ],
}

const serviceKey: CredentialItem = { id: 'model-key', type: 'ai', name: 'Shared model key', purpose: 'model_api', issuer: 'deepseek', configured: true, masked: 'sk-…9999' }
const modelService: ModelConnection = { id: 'service', name: 'Team model service', providerId: 'deepseek', protocol: 'openai-chat-completions', baseUrl: 'https://models.example.test/v1', credentialId: serviceKey.id, auth: { scheme: 'bearer' }, models: [{ id: 'vendor/original/model', source: 'manual', input: ['text', 'image'], contextWindow: 128000, maxTokens: 8192 }], state: 'active', revision: 3 }

function mountPanel(response: { items: CredentialItem[] } = config, locale: 'zh-CN' | 'en' = 'zh-CN', props: { inline?: boolean; scope?: 'all' | 'model' | 'git'; view?: 'credentials' | 'models'; setup?: boolean; configurationId?: string } = {}) {
  const i18n = createI18n({
    legacy: false,
    locale,
    messages: {
      'zh-CN': { ...common, ...pages },
      en: { ...enCommon, ...enPages },
    },
  })
  mocks.get.mockResolvedValue(response)
  return mount(WorkspaceCredentialsPanel, {
    props,
    global: { plugins: [i18n], stubs: { Icon: true, Teleport: true, RouterLink: { template: '<a><slot /></a>' } } },
  })
}

function rowByAlias(wrapper: ReturnType<typeof mountPanel>, name: string) {
  const row = wrapper.findAll('[data-testid="workspace-credential-row"]').find((node) => node.get('[data-testid="workspace-credential-alias"]').text() === name)
  if (!row) throw new Error(`missing credential row ${name}`)
  return row
}

async function finishStepTransition() {
  await new Promise((resolve) => setTimeout(resolve, 220))
  await flushPromises()
}

async function openCredentialWizard(wrapper: ReturnType<typeof mountPanel>) {
  await wrapper.get('[data-testid="workspace-credential-create"]').trigger('click')
  await flushPromises()
  await wrapper.get('[data-testid="workspace-add-credential"]').trigger('click')
  await flushPromises()
}

async function openModelKeyForm(wrapper: ReturnType<typeof mountPanel>) {
  await flushPromises()
  await openCredentialWizard(wrapper)
  await wrapper.get('[data-testid="credential-kind-model_api"]').trigger('click')
  await wrapper.get('[data-testid="workspace-credential-create-next"]').trigger('click')
  await finishStepTransition()
  await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('Team model key')
  await wrapper.get('[data-testid="workspace-credential-create-value"]').setValue('sk-private-example')
}

async function reviewCredential(wrapper: ReturnType<typeof mountPanel>) {
  await wrapper.get('[data-testid="workspace-credential-review-next"]').trigger('click')
  await finishStepTransition()
  expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-credential-step')).toBe('3')
}

async function chooseSearchOption(wrapper: ReturnType<typeof mountPanel>, testId: string, value: string) {
  const field = wrapper.get(`[data-testid="${testId}"]`)
  await field.get('[data-test="app-select-trigger"]').trigger('click')
  await field.get('[data-test="app-select-search"]').setValue(value)
  await field.get(`[data-test="app-select-option-${value}"]`).trigger('click')
}

describe('WorkspaceCredentialsPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.get.mockResolvedValue(config)
    mocks.put.mockImplementation(async (id: string) => ({
      ...config.items.find((item) => item.id === id), configured: true, masked: '••••', updatedAt: '2026-01-03T00:00:00Z',
    }))
    mocks.create.mockImplementation(async ({ value: _value, ...body }: Record<string, unknown>) => ({ id: 'cred-created', type: 'ai', ...body, configured: true, masked: '••••' }))
    mocks.del.mockResolvedValue({ status: 'ok' })
    mocks.remove.mockResolvedValue({ status: 'removed' })
    mocks.providers.mockResolvedValue({ providers: [{ id: 'openai', name: 'OpenAI' }] })
    mocks.models.mockResolvedValue({ models: [{ id: 'gpt-4o', name: 'GPT-4o' }] })
    mocks.connections.mockResolvedValue({ items: [] })
    mocks.connectionModels.mockResolvedValue({ models: [] })
    mocks.configurations.mockResolvedValue({ items: [] })
    mocks.capabilities.mockResolvedValue({ items: [] })
    mocks.createConfiguration.mockImplementation(async (body: Record<string, unknown>) => ({ id: 'configuration-created', ...body, revision: 1 }))
    mocks.createConnection.mockImplementation(async (body: Record<string, unknown>) => ({ id: 'connection-created', ...body, revision: 1 }))
    mocks.updateConnection.mockImplementation(async (id: string, body: Record<string, unknown>) => ({ id, ...body, revision: 4 }))
  })

  it('creates a Git credential inline, filters unrelated providers and reports the saved reference', async () => {
    const wrapper = mountPanel(config, 'zh-CN', { inline: true, scope: 'git' })
    document.body.appendChild(wrapper.element)
    await flushPromises()
    expect(wrapper.find('[data-testid="workspace-add-model"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-section-coding"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-section-git"]').exists()).toBe(true)
    const opener = wrapper.get('[data-testid="workspace-add-credential"]')
    ;(opener.element as HTMLElement).focus()
    await opener.trigger('click')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="credential-inline-editor"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="credential-kind-model_api"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="credential-kind-cursor"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="credential-kind-ssh_key"]').exists()).toBe(true)
    await wrapper.get('[data-testid="credential-kind-github"]').trigger('click')
    await wrapper.get('[data-testid="workspace-credential-create-next"]').trigger('click')
    await finishStepTransition()
    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('My GitHub')
    await wrapper.get('[data-testid="workspace-credential-create-value"]').setValue('private-test-token')
    await reviewCredential(wrapper)
    expect(wrapper.text()).not.toContain('private-test-token')
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('credentialSaved')?.[0]?.[0]).toMatchObject({ id: 'cred-created', purpose: 'git', name: 'My GitHub' })
    expect(wrapper.emitted('changed')).toHaveLength(1)
    expect(wrapper.emitted('editing')).toEqual([[true], [false]])
    expect(document.activeElement).toBe(opener.element)
    expect(wrapper.find('[data-testid="credential-inline-editor"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('starts coding-tool setup directly and resumes its draft after creating a credential inline', async () => {
    mocks.capabilities.mockResolvedValue({ items: [{ toolId: 'cursor', modes: ['platform'], adapterState: 'existing' }] })
    const wrapper = mountPanel({ items: [] }, 'en', { inline: true, scope: 'model', view: 'models', setup: true })
    await flushPromises()
    expect(wrapper.get('[data-testid="model-configuration-form"]').attributes('data-step')).toBe('1')
    expect(wrapper.get('[data-testid="model-configuration-backend-cursor"]').isVisible()).toBe(true)
    expect(wrapper.get('.credentials-header').attributes('style')).toContain('display: none')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    await wrapper.get('[data-testid="model-configuration-name"]').setValue('My coding tool')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-add-credential"]').trigger('click')
    await flushPromises()
    await finishStepTransition()
    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('My account')
    await wrapper.get('[data-testid="workspace-credential-create-value"]').setValue('private-test-key')
    await reviewCredential(wrapper)
    mocks.get.mockResolvedValue({ items: [{ id: 'cred-created', name: 'My account', purpose: 'tool_auth', toolId: 'cursor', configured: true }] })
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="model-configuration-form"]').attributes('data-step')).toBe('2')
    expect(wrapper.get('[data-testid="model-configuration-credential-choice"]').text()).toContain('My account')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.createConfiguration).toHaveBeenCalledWith(expect.objectContaining({ name: 'My coding tool', backend: 'cursor', binding: { kind: 'platform', credentialId: 'cred-created' } }))
    expect(wrapper.emitted('modelSaved')?.[0]?.[0]).toMatchObject({ id: 'configuration-created', name: 'My coding tool' })
    wrapper.unmount()
  })

  it.each(['opencode', 'deepseek_harness'])('saves the official %s credential inline without opening model setup', async (backend) => {
    const wrapper = mountPanel({ items: [] }, 'zh-CN', { inline: true, scope: 'model', view: 'credentials' })
    await flushPromises()
    expect(wrapper.find('[role="tablist"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-add-model"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-credential-step')).toBe('1')
    expect(wrapper.get('[data-testid="credential-kind-model_api"]').isVisible()).toBe(true)
    await wrapper.get(`[data-testid="credential-kind-${backend}"]`).trigger('click')
    await wrapper.get('[data-testid="workspace-credential-create-next"]').trigger('click')
    await finishStepTransition()
    expect(wrapper.find('[data-testid="model-credential-issuer"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="model-credential-create-connection"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="model-credential-connection-base-url"]').exists()).toBe(false)
    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('My official key')
    await wrapper.get('[data-testid="workspace-credential-create-value"]').setValue('private-test-key')
    await reviewCredential(wrapper)
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith({ type: 'ai', purpose: 'tool_auth', toolId: backend, provider: backend, secretKind: 'api_key', envKey: backend === 'opencode' ? 'GRASP_OPENCODE_API_KEY' : 'GRASP_DEEPSEEK_API_KEY', name: 'My official key', value: 'private-test-key' })
    expect(mocks.createConnection).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="workspace-credential-row"]').text()).toContain('My official key')
    expect(wrapper.find('[data-testid="model-configurations-manager"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="model-configuration-form"]').exists()).toBe(false)
    expect(wrapper.emitted('editing')).toEqual([[true], [false]])
    expect(mocks.createConfiguration).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps credential and model views separate and creates models from the saved credentials', async () => {
    const wrapper = mountPanel(config, 'zh-CN', { inline: true, scope: 'model', view: 'credentials' })
    mocks.capabilities.mockResolvedValue({ items: [{ toolId: 'cursor', adapterState: 'existing', modes: ['platform'] }] })
    await flushPromises()
    expect(wrapper.find('[data-testid="workspace-add-model"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="workspace-credential-row"]').text()).toContain('Cursor API key')
    await wrapper.setProps({ view: 'models' })
    await flushPromises()
    expect(wrapper.find('[role="tablist"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-add-credential"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="model-configuration-manage-credentials"]').exists()).toBe(false)
    await wrapper.get('[data-testid="workspace-add-model"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="model-configuration-name"]').setValue('My saved-key model')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    expect(wrapper.find('[data-testid="model-configuration-add-credential"]').exists()).toBe(false)
    await chooseSearchOption(wrapper, 'model-configuration-credential-choice', 'platform:cursor-api')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.createConfiguration).toHaveBeenCalledWith(expect.objectContaining({ name: 'My saved-key model', backend: 'cursor', binding: { kind: 'platform', credentialId: 'cursor-api' } }))
    expect(mocks.create).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps a missing credential message in model-only view without opening credential creation', async () => {
    const wrapper = mountPanel({ items: [] }, 'zh-CN', { inline: true, scope: 'model', view: 'models' })
    mocks.capabilities.mockResolvedValue({ items: [{ toolId: 'cursor', adapterState: 'existing', modes: ['platform'] }] })
    await flushPromises()
    expect(wrapper.find('[data-testid="workspace-add-credential"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="model-configuration-manage-credentials"]').exists()).toBe(false)
    await wrapper.get('[data-testid="workspace-add-model"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="model-configuration-name"]').setValue('Needs credentials')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    expect(wrapper.find('[data-testid="model-configuration-credentials-empty"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="model-configuration-next"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="model-configuration-add-credential"]').exists()).toBe(false)
    await wrapper.get('[data-testid="model-configuration-back"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-back"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('editing')).toEqual([[true], [false]])
    wrapper.unmount()
  })

  it('keeps an inline model draft while creating its missing credential and selects the new key', async () => {
    const wrapper = mountPanel({ items: [] }, 'zh-CN', { inline: true, scope: 'model' })
    document.body.appendChild(wrapper.element)
    mocks.capabilities.mockResolvedValue({ items: [{ toolId: 'cursor', adapterState: 'existing', modes: ['platform'] }] })
    mocks.create.mockImplementation(async ({ value: _value, ...body }: Record<string, unknown>) => {
      const key = { id: 'cred-created', type: 'ai', ...body, configured: true, masked: '••••' }
      mocks.get.mockResolvedValue({ items: [key] })
      return key
    })
    await flushPromises()
    const opener = wrapper.get('[data-testid="workspace-add-model"]')
    ;(opener.element as HTMLElement).focus()
    await opener.trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="model-configuration-name"]').setValue('My first model')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-add-credential"]').trigger('click')
    await finishStepTransition()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="credential-inline-editor"]')).toHaveLength(1)
    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('My Cursor')
    await wrapper.get('[data-testid="workspace-credential-create-value"]').setValue('private-cursor-key')
    await reviewCredential(wrapper)
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="model-configuration-form"]').attributes('data-step')).toBe('2')
    expect(wrapper.get('[data-testid="model-configuration-credential-choice"]').text()).toContain('My Cursor')
    expect(document.activeElement).toBe(wrapper.get('[data-testid="model-configuration-credential-choice"] [data-test="app-select-trigger"]').element)
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.createConfiguration).toHaveBeenCalledWith(expect.objectContaining({ name: 'My first model', backend: 'cursor', binding: { kind: 'platform', credentialId: 'cred-created' } }))
    expect(wrapper.emitted('modelSaved')?.[0]?.[0]).toMatchObject({ id: 'configuration-created', name: 'My first model' })
    expect(document.activeElement).toBe(opener.element)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('edits a model service inline without exposing destructive account actions', async () => {
    mocks.connections.mockResolvedValue({ items: [modelService] })
    const wrapper = mountPanel({ items: [serviceKey] }, 'zh-CN', { inline: true, scope: 'model' })
    await flushPromises()
    await wrapper.get('[data-testid="credentials-tab-credentials"]').trigger('click')
    await wrapper.get(`[data-testid="workspace-credential-edit-${serviceKey.id}"]`).trigger('click')
    await wrapper.get('[data-testid="model-credential-edit-service-service"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="model-connection-form"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="api-account-more-model-key"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-clear-model-key"]').exists()).toBe(false)
    await wrapper.get('[data-testid="model-connection-base-url"]').setValue('https://updated.example.test/v1')
    await wrapper.get('[data-testid="model-connection-form"]').trigger('submit')
    await flushPromises()
    expect(mocks.updateConnection).toHaveBeenCalledWith(modelService.id, expect.objectContaining({ baseUrl: 'https://updated.example.test/v1' }))
    expect(wrapper.emitted('changed')).toHaveLength(1)
    wrapper.unmount()
  })

  it('offers credential and model actions from the same header menu on both tabs', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    const trigger = wrapper.get('[data-testid="workspace-credential-create"]')
    expect(trigger.text()).toBe('新增')
    await trigger.trigger('click')
    await flushPromises()
    const menu = wrapper.get('[data-testid="workspace-add-menu"]')
    expect(menu.findAll('[role="menuitem"]').map((item) => item.text())).toEqual(['新增凭据', '新建模型组'])
    expect(menu.find('[name="key"]').exists()).toBe(true)
    expect(menu.find('[name="sparkles"]').exists()).toBe(true)
    await wrapper.get('[data-testid="workspace-add-model"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="credentials-tab-models"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-testid="model-configuration-form"]').attributes('data-step')).toBe('1')
    expect(wrapper.find('[data-testid="model-configuration-add"]').exists()).toBe(false)
    await wrapper.getComponent({ name: 'ModelConfigurationManager' }).getComponent({ name: 'AppModal' }).vm.$emit('close')
    await flushPromises()
    expect(trigger.isVisible()).toBe(true)
    await openCredentialWizard(wrapper)
    expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-credential-step')).toBe('1')
    expect(wrapper.get('[data-testid="credentials-tab-models"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })

  it('supports menu arrow keys, Enter, Escape and outside dismissal without leaking focus', async () => {
    const wrapper = mountPanel()
    document.body.appendChild(wrapper.element)
    await flushPromises()
    const trigger = wrapper.get('[data-testid="workspace-credential-create"]')
    ;(trigger.element as HTMLElement).focus()
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    const first = wrapper.get('[data-testid="workspace-add-credential"]')
    expect(document.activeElement).toBe(first.element)
    await first.trigger('keydown', { key: 'ArrowDown' })
    const second = wrapper.get('[data-testid="workspace-add-model"]')
    expect(document.activeElement).toBe(second.element)
    await second.trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('[data-testid="workspace-add-menu"]').exists()).toBe(false)
    expect(document.activeElement).toBe(trigger.element)
    await trigger.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    document.body.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(wrapper.find('[data-testid="workspace-add-menu"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-create-form"]').exists()).toBe(false)
    await trigger.trigger('keydown', { key: 'ArrowUp' })
    await flushPromises()
    expect(document.activeElement).toBe(wrapper.get('[data-testid="workspace-add-model"]').element)
    await wrapper.get('[data-testid="workspace-add-model"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(wrapper.get('[data-testid="model-configuration-form"]').attributes('data-step')).toBe('1')
    wrapper.unmount()
  })

  it('uses the short English credential list copy', () => {
    const copy = enPages.pages.workspace.credentials
    expect(copy.groups.other.title).toBe('Other')
    expect(copy.typeAi).toBe('Model')
    expect(copy.typeGit).toBe('Repository')
    expect(copy.edit).toBe('Edit')
    expect(copy.fillIn).toBe('Fill in')
    expect(copy.summary.apiKeys).toBe('Model slots')
    expect(copy.replacePlaceholder).toBe('Enter a new value to replace it')
    expect(copy.valuePlaceholder).toBe('Enter a credential. It is sent only when you save.')
  })

  it('separates service credentials from saved tool and model configurations using exactly two tabs', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    const tabs = wrapper.findAll('[role="tab"]')
    expect(tabs.map((tab) => tab.get('.credentials-tab-label').text())).toEqual(['凭据', '模型组'])
    expect(wrapper.get('[data-testid="credentials-tab-count-credentials"]').text()).toBe('3')
    expect(wrapper.find('[data-testid="credentials-tab-count-models"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="credentials-tab-credentials"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-testid="credentials-tab-connections"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="credentials-tab-defaults"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-model-defaults"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="model-connections-manager"]').exists()).toBe(false)

    await wrapper.get('[data-testid="credentials-tab-models"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="credentials-tab-models"]').attributes('aria-selected')).toBe('true')
    const models = wrapper.get('[data-testid="model-configurations-manager"]')
    expect(wrapper.find('[data-testid="workspace-credential-row"]').exists()).toBe(false)
    await models.get('[data-testid="model-configuration-manage-credentials"]').trigger('click')
    await flushPromises()
    await finishStepTransition()
    expect(wrapper.find('[data-testid="workspace-credential-create-alias"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="model-connections-manager"]').exists()).toBe(false)
    expect(mocks.create).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('edits the API address within its fixed account without changing available models or adding another key', async () => {
    mocks.connections.mockResolvedValue({ items: [modelService] })
    const wrapper = mountPanel({ items: [...config.items, serviceKey] })
    await flushPromises()
    await wrapper.get(`[data-testid="workspace-credential-edit-${serviceKey.id}"]`).trigger('click')
    await wrapper.get('[data-testid="model-credential-edit-service-service"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="api-account-identity"]').text()).toBe(serviceKey.name)
    expect(wrapper.find('[data-testid="model-connection-credential"]').exists()).toBe(false)
    await wrapper.get('[data-testid="model-connection-base-url"]').setValue('https://new-models.example.test/v1')
    await wrapper.get('[data-testid="model-connection-form"]').trigger('submit')
    await flushPromises()
    expect(mocks.updateConnection).toHaveBeenCalledWith(modelService.id, expect.objectContaining({ baseUrl: 'https://new-models.example.test/v1', credentialId: serviceKey.id, models: modelService.models }))
    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.createConnection).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('loads workspace credentials and masks values in the form', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    expect(mocks.get).toHaveBeenCalledWith()
    expect(wrapper.find('[data-testid="workspace-credentials-panel"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="workspace-credential-group-cursor"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Cursor')
    expect(wrapper.text()).toContain('GitHub')
    expect(wrapper.text()).toContain('SSH 私钥')
    expect(wrapper.text()).not.toContain('AI / API Key')
    expect(wrapper.text()).not.toContain('不回显')
    expect(wrapper.text()).not.toContain('凭据由项目统一管理')
    expect(wrapper.text()).not.toContain('编码助手和模型调用使用的密钥')
    expect(wrapper.text()).not.toContain('拉取、推送和合并请求使用的凭据')
    expect(wrapper.text()).not.toContain('通过 SSH 访问仓库时使用')
    expect(wrapper.get('h1').text()).toBe('工作区凭据')
    expect(wrapper.get('[data-testid="workspace-credential-create"]').text()).toBe('新增')
    const mask = wrapper.get('[data-testid="workspace-credential-masked"]')
    expect(mask.text()).toContain('sk-…1234')
    expect(mask.text()).not.toContain('不回显')
    expect(wrapper.find('[data-testid="workspace-credential-input-cursor-api"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-input-ssh"]').exists()).toBe(false)
    expect(wrapper.find('input, textarea').exists()).toBe(false)
    const cursor = rowByAlias(wrapper, 'Cursor API key')
    expect(cursor.get('[data-testid="workspace-credential-edit-cursor-api"]').attributes('aria-label')).toBe('编辑')
    expect(cursor.get('[data-testid="workspace-credential-clear-cursor-api"]').attributes('aria-label')).toBe('清除')
    expect(cursor.get('[data-testid="workspace-credential-status"]').text()).toContain('已配置')
    const github = rowByAlias(wrapper, 'GitHub token')
    expect(github.get('[data-testid="workspace-credential-actions"]').text()).toContain('填写')
    expect(github.get('[data-testid="workspace-credential-status"]').text()).toContain('未配置')
    expect(github.find('[data-testid="workspace-credential-clear-github"]').exists()).toBe(false)
    expect(github.find('[data-testid="workspace-credential-masked"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('secret-value')
    expect(wrapper.find('[data-provider-logo="cursor"]').exists()).toBe(true)
    expect(wrapper.find('[data-provider-logo="github"]').exists()).toBe(true)
    expect(wrapper.find('[data-provider-logo="ssh-key"]').exists()).toBe(true)
    expect(wrapper.find('[data-provider-logo="cursor"]').attributes('aria-label')).toContain('Cursor')
    expect(wrapper.text()).not.toContain('GRASP_CURSOR_API_KEY')
    expect(wrapper.text()).not.toContain('GITHUB_TOKEN')
    expect(wrapper.get('[data-testid="workspace-credential-section-coding"]').text()).toContain('Cursor API key')
    expect(wrapper.get('[data-testid="workspace-credential-section-git"]').text()).toContain('GitHub token')
    expect(wrapper.get('[data-testid="workspace-credential-section-ssh"]').text()).toContain('SSH 私钥')
    expect(wrapper.find('[data-testid="workspace-credential-section-other"]').exists()).toBe(false)
    const credentials = wrapper.get('[data-testid="workspace-credential-group-cursor"]')
    expect(credentials.findAll('[data-testid="workspace-credential-row"]')).toHaveLength(1)
    expect(credentials.get('[data-testid="workspace-credential-alias"]').text()).toContain('Cursor')
    wrapper.unmount()
  })

  it('separates Agent CLI and API accounts while keeping their provider logos and types', async () => {
    const wrapper = mountPanel({ items: [...config.items, serviceKey] })
    await flushPromises()
    const accounts = wrapper.get('[data-testid="workspace-credential-section-api"]')
    const tools = wrapper.get('[data-testid="workspace-credential-section-coding"]')
    expect(tools.get('h3').text()).toBe('Agent CLI')
    expect(tools.get('.credentials-section-count').text()).toBe('1')
    expect(tools.get('[data-testid="workspace-credential-alias"]').text()).toBe('Cursor API key')
    expect(accounts.get('h3').text()).toBe('API 账号')
    expect(accounts.get('.credentials-section-count').text()).toBe('1')
    expect(accounts.get('[data-testid="workspace-credential-alias"]').text()).toBe('Shared model key')
    expect(rowByAlias(wrapper, 'Cursor API key').get('[data-provider-logo="cursor"]').exists()).toBe(true)
    expect(rowByAlias(wrapper, 'Shared model key').get('[data-provider-logo="deepseek"]').exists()).toBe(true)
    expect(rowByAlias(wrapper, 'Shared model key').get('[data-testid="workspace-credential-kind"]').text()).toBe('DeepSeek')
    expect(wrapper.findAll('.credentials-section').map(section => section.get('h3').text())).toEqual(['Agent CLI', 'API 账号', '代码仓库', 'SSH 访问'])
    expect(wrapper.find('[data-testid="model-credential-add"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('writes only on an explicit save and clears through DELETE', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('[data-testid="workspace-credential-input-github"]').exists()).toBe(false)
    await wrapper.get('[data-testid="workspace-credential-fill-github"]').trigger('click')
    const input = wrapper.get('[data-testid="workspace-credential-input-github"]')
    expect((input.element as HTMLInputElement).value).toBe('')
    expect(input.attributes('type')).toBe('password')
    const editor = wrapper.get('[data-testid="workspace-credential-editor-github"]')
    expect(editor.find('[data-testid="workspace-credential-save-github"]').exists()).toBe(true)
    expect(editor.find('[data-testid="workspace-credential-cancel-github"]').exists()).toBe(true)
    await input.setValue('   ')
    expect(wrapper.get('[data-testid="workspace-credential-save-github"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="workspace-credential-save-github"]').trigger('click')
    expect(mocks.put).not.toHaveBeenCalled()
    await input.setValue('ghp-new')
    await wrapper.find('[data-testid="workspace-credential-save-github"]').trigger('click')
    await flushPromises()
    expect(mocks.put).toHaveBeenCalledWith('github', expect.objectContaining({ value: 'ghp-new', envKey: 'GITHUB_TOKEN' }))
    expect(wrapper.find('[data-testid="workspace-credential-input-github"]').exists()).toBe(false)
    expect(rowByAlias(wrapper, 'GitHub token').get('[data-testid="workspace-credential-masked"]').text()).toContain('••••')

    await wrapper.find('[data-testid="workspace-credential-clear-cursor-api"]').trigger('click')
    await flushPromises()
    expect(mocks.del).toHaveBeenCalledWith('cursor-api')
    const cleared = rowByAlias(wrapper, 'Cursor API key')
    expect(cleared.find('[data-testid="workspace-credential-masked"]').exists()).toBe(false)
    expect(cleared.find('[data-testid="workspace-credential-fill-cursor-api"]').exists()).toBe(true)
    expect(cleared.find('[data-testid="workspace-credential-clear-cursor-api"]').exists()).toBe(false)
    expect(cleared.text()).not.toContain('sk-…1234')
    wrapper.unmount()
  })

  it('opens an empty editor for one row and cancels without saving', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('[data-testid="workspace-credential-edit-cursor-api"]').trigger('click')
    const input = wrapper.get('[data-testid="workspace-credential-input-cursor-api"]')
    expect(input.element.tagName).toBe('INPUT')
    expect(input.attributes('type')).toBe('password')
    expect((input.element as HTMLInputElement).value).toBe('')
    expect(input.element.textContent).not.toContain('sk-…1234')
    const editor = wrapper.get('[data-testid="workspace-credential-editor-cursor-api"]')
    expect(editor.find('[data-testid="workspace-credential-save-cursor-api"]').exists()).toBe(true)
    expect(editor.find('[data-testid="workspace-credential-cancel-cursor-api"]').exists()).toBe(true)
    expect(rowByAlias(wrapper, 'Cursor API key').get('[data-testid="workspace-credential-masked"]').text()).toContain('sk-…1234')
    expect(wrapper.find('[data-testid="workspace-credential-clear-cursor-api"]').exists()).toBe(true)

    await wrapper.get('[data-testid="workspace-credential-fill-ssh"]').trigger('click')
    expect(wrapper.get('[data-testid="workspace-credential-input-ssh"]').element.tagName).toBe('TEXTAREA')
    expect((wrapper.get('[data-testid="workspace-credential-input-ssh"]').element as HTMLTextAreaElement).value).toBe('')

    await input.setValue('sk-draft')
    await wrapper.get('[data-testid="workspace-credential-cancel-cursor-api"]').trigger('click')
    expect(mocks.put).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="workspace-credential-input-cursor-api"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-editor-ssh"]').exists()).toBe(true)
    expect((wrapper.get('[data-testid="workspace-credential-input-ssh"]').element as HTMLTextAreaElement).value).toBe('')
    expect(rowByAlias(wrapper, 'Cursor API key').text()).not.toContain('sk-draft')
    wrapper.unmount()
  })

  it('keeps the draft open when save fails and the mask when clear fails', async () => {
    mocks.put.mockRejectedValueOnce(new Error('save failed'))
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('[data-testid="workspace-credential-edit-cursor-api"]').trigger('click')
    await wrapper.get('[data-testid="workspace-credential-input-cursor-api"]').setValue('sk-next')
    await wrapper.get('[data-testid="workspace-credential-save-cursor-api"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="workspace-credential-editor-cursor-api"]').exists()).toBe(true)
    expect((wrapper.get('[data-testid="workspace-credential-input-cursor-api"]').element as HTMLInputElement).value).toBe('sk-next')
    expect(rowByAlias(wrapper, 'Cursor API key').get('[data-testid="workspace-credential-masked"]').text()).toContain('sk-…1234')
    expect(mocks.error).toHaveBeenCalled()

    mocks.del.mockRejectedValueOnce(new Error('clear failed'))
    await wrapper.get('[data-testid="workspace-credential-clear-cursor-api"]').trigger('click')
    await flushPromises()
    expect(rowByAlias(wrapper, 'Cursor API key').get('[data-testid="workspace-credential-masked"]').text()).toContain('sk-…1234')
    expect(rowByAlias(wrapper, 'Cursor API key').text()).toContain('已配置')
    wrapper.unmount()
  })

  it('leaves adapter-managed credentials read only', async () => {
    const wrapper = mountPanel({
      items: [
        ...config.items,
        {
          id: 'vault-key', type: 'custom', name: 'Vault token', provider: 'custom',
          configured: true, masked: 'vk-…8888', source: 'vault',
        },
      ],
    })
    await flushPromises()
    const row = rowByAlias(wrapper, 'Vault token')
    expect(wrapper.get('[data-testid="workspace-credential-section-other"]').text()).toContain('Vault token')
    expect(row.text()).toContain('由 vault 专用设置管理')
    expect(row.find('[data-testid="workspace-credential-edit-vault-key"]').exists()).toBe(false)
    expect(row.find('[data-testid="workspace-credential-fill-vault-key"]').exists()).toBe(false)
    expect(row.find('[data-testid="workspace-credential-input-vault-key"]').exists()).toBe(false)
    expect(row.find('[data-testid="workspace-credential-clear-vault-key"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('hides the removed explanations in English and uses Edit and Fill in', async () => {
    const wrapper = mountPanel(config, 'en')
    await flushPromises()
    expect(wrapper.get('h1').text()).toBe('Workspace credentials')
    expect(wrapper.get('[data-testid="workspace-credential-create"]').text()).toBe('Add')
    expect(rowByAlias(wrapper, 'Cursor API key').get('[data-testid="workspace-credential-edit-cursor-api"]').attributes('aria-label')).toBe('Edit')
    expect(rowByAlias(wrapper, 'Cursor API key').get('[data-testid="workspace-credential-clear-cursor-api"]').attributes('aria-label')).toBe('Clear')
    expect(rowByAlias(wrapper, 'GitHub token').get('[data-testid="workspace-credential-actions"]').text()).toContain('Fill in')
    expect(wrapper.text()).not.toContain('Hidden')
    expect(wrapper.text()).not.toContain('Keys used by coding assistants and model calls.')
    expect(wrapper.find('[data-testid="workspace-credential-input-cursor-api"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('selects a credential kind before showing the alias and secret fields', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await openCredentialWizard(wrapper)
    expect(wrapper.find('[data-testid="workspace-credential-create-type"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-create-provider"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-create-env"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-credential-step')).toBe('1')
    expect(wrapper.find('[data-testid="workspace-credential-create-alias"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-create-value"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="workspace-credential-create-next"]').attributes('disabled')).toBeDefined()
    const cards = wrapper.findAll('[data-testid^="credential-kind-"]')
    expect(wrapper.find('[data-testid="credential-kind-model_api"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="credential-kind-opencode"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="credential-kind-deepseek_harness"]').exists()).toBe(true)
    for (const card of cards.filter((item) => item.attributes('data-testid') !== 'credential-kind-model_api')) {
      expect(card.find('[data-provider-logo]').exists()).toBe(true)
    }
    await wrapper.get('[data-testid="credential-kind-github"]').trigger('click')
    expect(wrapper.get('[data-testid="workspace-credential-create-next"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="workspace-credential-create-next"]').trigger('click')
    expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-credential-step')).toBe('2')
    expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-step-motion')).toBe('forward')
    expect(wrapper.text()).toContain('别名')
    await new Promise((resolve) => setTimeout(resolve, 220))
    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('工作号')
    await wrapper.get('[data-testid="workspace-credential-create-value"]').setValue('ghp-secret')
    await wrapper.get('[data-testid="workspace-credential-review-next"]').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 220))
    expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-credential-step')).toBe('3')
    expect(wrapper.text()).not.toContain('ghp-secret')
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({
      type: 'git', purpose: 'git', provider: 'github', name: '工作号', envKey: 'GITHUB_TOKEN', value: 'ghp-secret',
    }))
    expect(wrapper.find('[data-testid="workspace-credential-create-form"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('creates a model-service key without selecting a tool or model', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await openCredentialWizard(wrapper)
    await wrapper.get('[data-testid="credential-kind-model_api"]').trigger('click')
    await wrapper.get('[data-testid="workspace-credential-create-next"]').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 220))
    expect(wrapper.find('[data-test="opencode-provider"]').exists()).toBe(false)
    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('DeepSeek team')
    wrapper.getComponent('[data-testid="model-credential-issuer"]').vm.$emit('update:modelValue', 'deepseek')
    await flushPromises()
    await wrapper.get('[data-testid="model-credential-create-connection"]').trigger('click')
    await wrapper.get('[data-testid="workspace-credential-create-value"]').setValue('dsh-example-key')
    await wrapper.get('[data-testid="workspace-credential-review-next"]').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 220))
    expect(wrapper.text()).toContain('DeepSeek team')
    expect(wrapper.text()).not.toContain('dsh-example-key')
    expect(mocks.create).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith({ purpose: 'model_api', issuer: 'deepseek', secretKind: 'api_key', name: 'DeepSeek team', value: 'dsh-example-key' })
    expect(mocks.createConnection).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each(['opencode', 'deepseek_harness'])('keeps the official %s key separate from generic API accounts', async (backend) => {
    const alias = `Team ${backend} account`
    const wrapper = mountPanel()
    await flushPromises()
    await openCredentialWizard(wrapper)
    await wrapper.get(`[data-testid="credential-kind-${backend}"]`).trigger('click')
    await wrapper.get('[data-testid="workspace-credential-create-next"]').trigger('click')
    await finishStepTransition()
    expect(wrapper.find('[data-testid="model-credential-issuer"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="model-credential-create-connection"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="model-credential-connection-base-url"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="workspace-credential-create-value"]').attributes('type')).toBe('password')
    expect(wrapper.text()).toContain(backend === 'opencode' ? 'OpenCode 官方 Key' : 'DeepSeek 官方 Key')
    if (backend === 'opencode') expect(wrapper.get('[data-testid="workspace-credential-opencode-family"]').text()).toContain('opencode-go/<模型>')
    else expect(wrapper.find('[data-testid="workspace-credential-opencode-family"]').exists()).toBe(false)
    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue(alias)
    await wrapper.get('[data-testid="workspace-credential-create-value"]').setValue('test-official-key')
    await reviewCredential(wrapper)
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith({ type: 'ai', purpose: 'tool_auth', toolId: backend, provider: backend, secretKind: 'api_key', envKey: backend === 'opencode' ? 'GRASP_OPENCODE_API_KEY' : 'GRASP_DEEPSEEK_API_KEY', name: alias, value: 'test-official-key' })
    expect(mocks.createConnection).not.toHaveBeenCalled()
    expect(mocks.createConfiguration).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="credentials-tab-credentials"]').attributes('aria-selected')).toBe('true')
    const row = rowByAlias(wrapper, alias)
    expect(row.element.closest('[data-testid="workspace-credential-section-coding"]')).not.toBeNull()
    expect(wrapper.find('[data-testid="model-configuration-form"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('test-official-key')
    wrapper.unmount()
  })

  it.each(['cursor', 'opencode', 'deepseek_harness'])('creates a compatible %s credential from the model wizard and returns to the same draft', async (backend) => {
    const storedKeys: CredentialItem[] = []
    const storedServices: ModelConnection[] = []
    const wrapper = mountPanel({ items: [] })
    mocks.get.mockImplementation(async () => ({ items: [...storedKeys] }))
    mocks.connections.mockImplementation(async () => ({ items: [...storedServices] }))
    mocks.capabilities.mockResolvedValue({ items: [{ toolId: backend, modes: backend === 'cursor' ? ['platform'] : ['platform', 'connection'], protocols: ['openai-responses'], adapterState: 'existing' }] })
    mocks.create.mockImplementation(async ({ value: _value, ...body }: Record<string, unknown>) => {
      const saved = { id: 'new-compatible-key', ...body, configured: true, masked: '••••4242' } as CredentialItem
      storedKeys.push(saved)
      return saved
    })
    mocks.createConfiguration.mockImplementation(async (body: Record<string, unknown>) => ({ id: 'configuration-created', ...body, revision: 1 }))
    mocks.createConnection.mockImplementation(async (body: Record<string, unknown>) => {
      const saved = { id: 'new-compatible-service', ...body, revision: 1 } as ModelConnection
      storedServices.push(saved)
      return saved
    })
    await flushPromises()
    await wrapper.get('[data-testid="credentials-tab-models"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="workspace-credential-create"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="workspace-add-model"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="model-configuration-name"]').setValue('Keep this model draft')
    await wrapper.get(`[data-testid="model-configuration-backend-${backend}"]`).trigger('click')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-add-credential"]').trigger('click')
    await finishStepTransition()
    expect(wrapper.get('[data-testid="credentials-tab-models"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-credential-step')).toBe('2')
    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('New compatible credential')
    expect(wrapper.find('[data-testid="model-credential-issuer"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="model-credential-connection-base-url"]').exists()).toBe(false)
    await wrapper.get('[data-testid="workspace-credential-create-value"]').setValue('test-compatible-credential')
    await reviewCredential(wrapper)
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="workspace-credential-create-form"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="model-configuration-form"]').attributes('data-step')).toBe('2')
    expect(wrapper.getComponent('[data-testid="model-configuration-credential-choice"]').props('modelValue')).toBe('platform:new-compatible-key')
    expect(mocks.create).toHaveBeenCalledTimes(1)
    expect(mocks.put).not.toHaveBeenCalled()
    expect(mocks.create.mock.calls[0]![0]).toMatchObject({ purpose: 'tool_auth', toolId: backend, provider: backend, secretKind: 'api_key' })
    expect(mocks.createConnection).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="model-configuration-back"]').trigger('click')
    expect(wrapper.get('[data-testid="model-configuration-name"]').element).toHaveProperty('value', 'Keep this model draft')
    expect(wrapper.get(`[data-testid="model-configuration-backend-${backend}"]`).attributes('aria-checked')).toBe('true')
    wrapper.unmount()
  })

  it('offers common issuers and creates their API connection in the same confirmed flow', async () => {
    const wrapper = mountPanel()
    await openModelKeyForm(wrapper)
    const issuer = wrapper.get('[data-testid="model-credential-issuer"]')
    await issuer.get('[data-test="app-select-trigger"]').trigger('click')
    expect(issuer.findAll('[role="option"]').map((option) => option.attributes('data-test'))).toEqual(expect.arrayContaining(['deepseek', 'anthropic', 'openai', 'moonshot', 'zai', 'custom'].map((id) => `app-select-option-${id}`)))
    expect(wrapper.get('[data-testid="model-credential-create-connection"]').attributes('aria-checked')).toBe('true')

    await issuer.get('[data-test="app-select-option-anthropic"]').trigger('click')
    expect(wrapper.get('[data-testid="model-credential-connection-base-url"]').element).toHaveProperty('value', 'https://api.anthropic.com')
    expect(wrapper.getComponent('[data-testid="model-credential-connection-protocol"]').props('modelValue')).toBe('anthropic-messages')
    expect(wrapper.getComponent('[data-testid="model-credential-connection-auth"]').props('modelValue')).toBe('x-api-key')
    await issuer.get('[data-test="app-select-trigger"]').trigger('click')
    await issuer.get('[data-test="app-select-option-openai"]').trigger('click')
    expect(wrapper.get('[data-testid="model-credential-connection-base-url"]').element).toHaveProperty('value', 'https://api.openai.com/v1')
    expect(wrapper.getComponent('[data-testid="model-credential-connection-protocol"]').props('modelValue')).toBe('openai-responses')
    expect(wrapper.getComponent('[data-testid="model-credential-connection-auth"]').props('modelValue')).toBe('bearer')
    await wrapper.get('[data-testid="model-credential-connection-name"]').setValue('Team Responses')
    await reviewCredential(wrapper)
    expect(wrapper.text()).toContain('https://api.openai.com/v1')
    expect(wrapper.text()).not.toContain('sk-private-example')
    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.createConnection).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith({ purpose: 'model_api', issuer: 'openai', secretKind: 'api_key', name: 'Team model key', value: 'sk-private-example' })
    expect(mocks.createConnection).toHaveBeenCalledWith(expect.objectContaining({ name: 'Team Responses', providerId: 'openai', protocol: 'openai-responses', baseUrl: 'https://api.openai.com/v1', credentialId: 'cred-created', auth: { scheme: 'bearer' }, models: [] }))
    expect(mocks.create.mock.invocationCallOrder[0]).toBeLessThan(mocks.createConnection.mock.invocationCallOrder[0]!)
    expect(mocks.createConnection.mock.calls[0]![0]).not.toHaveProperty('value')
    expect(mocks.createConnection.mock.calls[0]![0]).not.toHaveProperty('toolId')
    expect(wrapper.find('[data-testid="workspace-credential-create-form"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps a customized endpoint, protocol and authentication when the current provider is selected again', async () => {
    const wrapper = mountPanel()
    await openModelKeyForm(wrapper)
    await wrapper.get('[data-testid="model-credential-connection-base-url"]').setValue('https://team-gateway.example/v1')
    await chooseSearchOption(wrapper, 'model-credential-connection-protocol', 'openai-responses')
    await chooseSearchOption(wrapper, 'model-credential-connection-auth', 'x-api-key')
    wrapper.getComponent('[data-testid="model-credential-issuer"]').vm.$emit('update:modelValue', 'deepseek')
    await flushPromises()
    expect(wrapper.get('[data-testid="model-credential-connection-base-url"]').element).toHaveProperty('value', 'https://team-gateway.example/v1')
    expect(wrapper.getComponent('[data-testid="model-credential-connection-protocol"]').props('modelValue')).toBe('openai-responses')
    expect(wrapper.getComponent('[data-testid="model-credential-connection-auth"]').props('modelValue')).toBe('x-api-key')
    wrapper.unmount()
  })

  it('configures a custom issuer and endpoint while leaving model selection to the models tab', async () => {
    const wrapper = mountPanel()
    await openModelKeyForm(wrapper)
    wrapper.getComponent('[data-testid="model-credential-issuer"]').vm.$emit('update:modelValue', 'custom')
    await flushPromises()
    await wrapper.get('[data-testid="model-credential-provider-label"]').setValue('Team gateway')
    await wrapper.get('[data-testid="model-credential-connection-name"]').setValue('Regional service')
    await chooseSearchOption(wrapper, 'model-credential-connection-protocol', 'openai-chat-completions')
    await wrapper.get('[data-testid="model-credential-connection-base-url"]').setValue('https://models.example.test/api/v1')
    expect(wrapper.find('[data-testid="model-credential-connection-models"]').exists()).toBe(false)
    await reviewCredential(wrapper)
    expect(wrapper.text()).toContain('Team gateway')
    expect(wrapper.text()).toContain('https://models.example.test/api/v1')
    expect(wrapper.text()).not.toContain('sk-private-example')
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith({ purpose: 'model_api', issuer: 'Team gateway', provider: 'custom', secretKind: 'api_key', name: 'Team model key', value: 'sk-private-example' })
    expect(mocks.create.mock.calls[0]![0]).not.toHaveProperty('metadata')
    expect(mocks.create.mock.calls[0]![0]).not.toHaveProperty('model')
    expect(mocks.create.mock.calls[0]![0]).not.toHaveProperty('baseUrl')
    expect(mocks.createConnection).toHaveBeenCalledWith(expect.objectContaining({ providerId: 'custom', baseUrl: 'https://models.example.test/api/v1', credentialId: 'cred-created', models: [] }))
    wrapper.unmount()
  })

  it.each(['codex', 'deepseek_harness', '团队模型服务'])('persists a custom issuer name %s without changing its machine provider', async (name) => {
    const wrapper = mountPanel()
    await openModelKeyForm(wrapper)
    wrapper.getComponent('[data-testid="model-credential-issuer"]').vm.$emit('update:modelValue', 'custom')
    await flushPromises()
    await wrapper.get('[data-testid="model-credential-provider-label"]').setValue(name)
    await wrapper.get('[data-testid="model-credential-connection-base-url"]').setValue('https://model.example/v1')
    expect(wrapper.find('select').exists()).toBe(false)
    await reviewCredential(wrapper)
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ purpose: 'model_api', issuer: name, provider: 'custom' }))
    expect(mocks.createConnection).toHaveBeenCalledWith(expect.objectContaining({ providerId: 'custom' }))
    const saved = await mocks.create.mock.results[0]!.value
    wrapper.unmount()
    const reloaded = mountPanel({ items: [saved] })
    await flushPromises()
    expect(rowByAlias(reloaded, 'Team model key').get('[data-testid="workspace-credential-kind"]').text()).toBe(name)
    reloaded.unmount()
  })

  it('discards an unsaved custom issuer name when its credential flow is cancelled', async () => {
    const wrapper = mountPanel()
    await openModelKeyForm(wrapper)
    wrapper.getComponent('[data-testid="model-credential-issuer"]').vm.$emit('update:modelValue', 'custom')
    await flushPromises()
    await wrapper.get('[data-testid="model-credential-provider-label"]').setValue('Unsaved issuer')
    await wrapper.get('[role="dialog"] button').trigger('click')
    await flushPromises()
    expect(mocks.create).not.toHaveBeenCalled()
    await openModelKeyForm(wrapper)
    wrapper.getComponent('[data-testid="model-credential-issuer"]').vm.$emit('update:modelValue', 'custom')
    await flushPromises()
    expect(wrapper.get('[data-testid="model-credential-provider-label"]').element).toHaveProperty('value', '')
    wrapper.unmount()
  })

  it('requires a valid connection URL only when creating a connection alongside the key', async () => {
    const wrapper = mountPanel()
    await openModelKeyForm(wrapper)
    await wrapper.get('[data-testid="model-credential-connection-base-url"]').setValue('not a URL')
    await wrapper.get('[data-testid="workspace-credential-review-next"]').trigger('click')
    expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-credential-step')).toBe('2')
    expect(mocks.create).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="model-credential-create-connection"]').trigger('click')
    await reviewCredential(wrapper)
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledTimes(1)
    expect(mocks.createConnection).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('never creates a connection when the key could not be saved', async () => {
    mocks.create.mockRejectedValueOnce(new Error('key service unavailable'))
    const wrapper = mountPanel()
    await openModelKeyForm(wrapper)
    await reviewCredential(wrapper)
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(mocks.createConnection).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-credential-step')).toBe('3')
    expect(mocks.error).toHaveBeenCalledWith(expect.stringContaining('key service unavailable'))
    await wrapper.get('[data-testid="workspace-credential-create-back"]').trigger('click')
    await finishStepTransition()
    expect(wrapper.get('[data-testid="workspace-credential-create-value"]').element).toHaveProperty('value', 'sk-private-example')
    wrapper.unmount()
  })

  it('retains the saved key and retries only the failed connection without exposing the secret again', async () => {
    mocks.createConnection.mockRejectedValueOnce(new Error('connection conflict'))
    const wrapper = mountPanel()
    await openModelKeyForm(wrapper)
    await wrapper.get('[data-testid="model-credential-connection-name"]').setValue('Initial connection')
    await reviewCredential(wrapper)
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledTimes(1)
    expect(mocks.createConnection).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-testid="model-credential-partial-error"]').text()).toContain('connection conflict')
    expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-credential-step')).toBe('3')
    expect(rowByAlias(wrapper, 'Team model key').get('[data-testid="workspace-credential-masked"]').text()).toContain('••••')
    expect(wrapper.text()).not.toContain('sk-private-example')

    await wrapper.get('[data-testid="workspace-credential-create-back"]').trigger('click')
    await finishStepTransition()
    const secret = wrapper.get('[data-testid="workspace-credential-create-value"]')
    expect(secret.element).toHaveProperty('value', '')
    expect(secret.attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="workspace-credential-create-alias"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="model-credential-issuer"] [data-test="app-select-trigger"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="model-credential-connection-name"]').setValue('Repaired connection')
    await reviewCredential(wrapper)
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledTimes(1)
    expect(mocks.createConnection).toHaveBeenCalledTimes(2)
    expect(mocks.createConnection).toHaveBeenLastCalledWith(expect.objectContaining({ name: 'Repaired connection', credentialId: 'cred-created' }))
    expect(mocks.del).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="workspace-credential-create-form"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps a successfully saved key when closing a failed connection flow and starts a fresh next draft', async () => {
    mocks.createConnection.mockRejectedValueOnce(new Error('connection conflict'))
    const wrapper = mountPanel()
    await openModelKeyForm(wrapper)
    await reviewCredential(wrapper)
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    await wrapper.get('[role="dialog"] button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="workspace-credential-create-form"]').exists()).toBe(false)
    expect(rowByAlias(wrapper, 'Team model key').text()).toContain('••••')
    expect(mocks.del).not.toHaveBeenCalled()

    await openModelKeyForm(wrapper)
    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('Second model key')
    expect(wrapper.find('[data-testid="model-credential-partial-error"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="workspace-credential-create-alias"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="workspace-credential-create-value"]').attributes('disabled')).toBeUndefined()
    await reviewCredential(wrapper)
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledTimes(2)
    expect(mocks.create).toHaveBeenLastCalledWith(expect.objectContaining({ name: 'Second model key', value: 'sk-private-example' }))
    wrapper.unmount()
  })

  it('reads the latest API account address before configuring Base when the initial service list failed', async () => {
    mocks.connections.mockRejectedValueOnce(new Error('initial list unavailable'))
    const wrapper = mountPanel({ items: [...config.items, serviceKey] })
    await flushPromises()
    await wrapper.get(`[data-testid="workspace-credential-edit-${serviceKey.id}"]`).trigger('click')
    mocks.connections.mockResolvedValue({ items: [modelService] })
    await wrapper.get(`[data-testid="model-credential-configure-service-${serviceKey.id}"]`).trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="model-connection-base-url"]').element).toHaveProperty('value', modelService.baseUrl)
    await wrapper.get('[data-testid="model-connection-base-url"]').setValue('https://updated.example.test/v1')
    await wrapper.get('[data-testid="model-connection-form"]').trigger('submit')
    await flushPromises()
    expect(mocks.updateConnection).toHaveBeenCalledWith(modelService.id, expect.objectContaining({ credentialId: serviceKey.id, models: modelService.models }))
    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.createConnection).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('rejects a duplicate alias and keeps the step while the animation is locked', async () => {
    const wrapper = mountPanel({
      items: [
        {
          id: 'cursor-api', type: 'ai', name: 'Cursor', purpose: 'tool_auth', toolId: 'cursor', provider: 'cursor',
          envKey: 'GRASP_CURSOR_API_KEY', configured: true, masked: 'sk-…1234',
        },
        {
          id: 'cursor-work', type: 'ai', name: '工作号', purpose: 'tool_auth', toolId: 'cursor', provider: 'cursor',
          envKey: 'GRASP_CURSOR_API_KEY', configured: true,
        },
      ],
    })
    await flushPromises()
    const titles = wrapper.findAll('[data-testid="workspace-credential-alias"]').map((node) => node.text())
    expect(titles).toEqual(expect.arrayContaining(['Cursor', '工作号']))
    expect(wrapper.get('[data-testid="workspace-credential-group-cursor"]').findAll('[data-testid="workspace-credential-row"]')).toHaveLength(2)
    expect(rowByAlias(wrapper, '工作号').get('[data-testid="workspace-credential-kind"]').text()).toBe('Cursor')

    await openCredentialWizard(wrapper)
    await wrapper.get('[data-testid="credential-kind-cursor"]').trigger('click')
    await wrapper.get('[data-testid="workspace-credential-create-next"]').trigger('click')
    expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-step-locked')).toBe('true')
    await wrapper.get('[data-testid="workspace-credential-create-back"]').trigger('click')
    expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-credential-step')).toBe('2')
    await new Promise((resolve) => setTimeout(resolve, 220))

    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('cursor')
    await wrapper.get('[data-testid="workspace-credential-create-value"]').setValue('sk-new')
    await wrapper.get('[data-testid="workspace-credential-review-next"]').trigger('click')
    expect(mocks.create).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="workspace-credential-alias-error"]').text()).toContain('这一类里已经有别名「Cursor」')
    expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-credential-step')).toBe('2')

    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('值班号')
    await wrapper.get('[data-testid="workspace-credential-review-next"]').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 220))
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({
      type: 'ai', purpose: 'tool_auth', toolId: 'cursor', provider: 'cursor', name: '值班号', envKey: 'GRASP_CURSOR_API_KEY', value: 'sk-new',
    }))
    wrapper.unmount()
  })

  it('lists model-service keys as ordinary credential rows and allows replacement or clearing', async () => {
    const opencode = { id: 'opencode', type: 'ai', name: 'DeepSeek', purpose: 'model_api', issuer: 'deepseek', secretKind: 'api_key', provider: 'custom', envKey: 'GRASP_MODEL_API_KEY', configured: true, masked: 'sk-…9999', metadata: { provider: 'deepseek', model: 'deepseek/deepseek-flash', vision: false } }
    const wrapper = mountPanel({ items: [...config.items, opencode] })
    await flushPromises()
    const row = rowByAlias(wrapper, 'DeepSeek')
    expect(wrapper.get('[data-testid="workspace-credential-section-api"]').text()).toContain('DeepSeek')
    expect(row.get('[data-testid="workspace-credential-masked"]').text()).toContain('sk-…9999')
    expect(wrapper.find('[data-test="opencode-provider-fields"]').exists()).toBe(false)
    await wrapper.get('[data-testid="workspace-credential-edit-opencode"]').trigger('click')
    await wrapper.get('[data-testid="workspace-credential-input-opencode"]').setValue('sk-new')
    mocks.put.mockResolvedValueOnce({ ...opencode, masked: '••••' })
    await wrapper.get('[data-testid="workspace-credential-save-opencode"]').trigger('click')
    await flushPromises()
    expect(mocks.put).toHaveBeenCalledWith('opencode', expect.objectContaining({ value: 'sk-new' }))
    await wrapper.get('[data-testid="workspace-credential-clear-opencode"]').trigger('click')
    await flushPromises()
    expect(mocks.del).toHaveBeenCalledWith('opencode')
    wrapper.unmount()
  })

  it('shows only current workspace credentials when an empty Cursor slot exists beside CursorDefault', async () => {
    const rows: CredentialItem[] = [
      { id: 'Cursor', type: 'ai', provider: 'cursor', name: 'Cursor', source: 'workspace', configured: false },
      { id: 'arbitrary-empty-id', type: 'git', provider: 'gitlab', name: 'Old empty GitLab', configured: false, secretRevision: 3 },
      { id: 'old-format-key', type: 'ai', provider: 'cursor', name: 'Unclassified key', configured: true, masked: '••••' },
      { id: 'cursor-default', type: 'ai', purpose: 'tool_auth', toolId: 'cursor', secretKind: 'api_key', provider: 'cursor', name: 'CursorDefault', configured: true, masked: 'cursor-…1234' },
    ]
    const wrapper = mountPanel({ items: rows }); await flushPromises()
    expect(wrapper.findAll('[data-testid="workspace-credential-alias"]').map(row => row.text())).toEqual(['CursorDefault'])
    const coding = wrapper.get('[data-testid="workspace-credential-section-coding"]')
    expect(coding.get('.credentials-section-count').text()).toBe('1')
    expect(wrapper.get('[data-testid="workspace-credential-group-cursor"]').findAll('[data-testid="workspace-credential-row"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="workspace-credential-masked"]').text()).toBe('cursor-…1234')
    expect(mocks.put).not.toHaveBeenCalled(); expect(mocks.del).not.toHaveBeenCalled(); expect(mocks.create).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps current cleared credentials and read-only adapter entries visible', async () => {
    const rows: CredentialItem[] = [
      { id: 'cred-builtin-current-cleared', type: 'ai', purpose: 'tool_auth', toolId: 'cursor', secretKind: 'api_key', provider: 'cursor', name: 'Current cleared Cursor', configured: false, state: 'cleared' },
      { id: 'current-empty', type: 'git', purpose: 'git', provider: 'gitlab', name: 'Current GitLab', configured: false },
      { id: 'cred-builtin-adapter-slot', type: 'custom', source: 'vault', name: 'Vault entry', configured: false },
    ]
    const wrapper = mountPanel({ items: rows }); await flushPromises()
    expect(wrapper.findAll('[data-testid="workspace-credential-row"]')).toHaveLength(3)
    expect(wrapper.find('[data-testid="workspace-credential-fill-cred-builtin-current-cleared"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="workspace-credential-fill-current-empty"]').exists()).toBe(true)
    const adapter = rowByAlias(wrapper, 'Vault entry')
    expect(adapter.text()).toContain('vault')
    expect(adapter.find('[data-testid="workspace-credential-fill-cred-builtin-adapter-slot"]').exists()).toBe(false)
    expect(mocks.del).not.toHaveBeenCalled(); wrapper.unmount()
  })

  it.each(['gitlab', 'github'] as const)('creates %s with a default Host and accepts a self-hosted origin in review', async provider => {
    const wrapper = mountPanel({ items: [] }); await flushPromises(); await openCredentialWizard(wrapper)
    await wrapper.get(`[data-testid="credential-kind-${provider}"]`).trigger('click')
    await wrapper.get('[data-testid="workspace-credential-create-next"]').trigger('click'); await finishStepTransition()
    const host = wrapper.get('[data-testid="workspace-credential-create-host"]')
    expect((host.element as HTMLInputElement).value).toBe(`${provider}.com`)
    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('Private host')
    await wrapper.get('[data-testid="workspace-credential-create-value"]').setValue('new-token')
    await host.setValue('https://user:secret@git.example.test/repo')
    expect(wrapper.get('[data-testid="workspace-credential-review-next"]').attributes('disabled')).toBeDefined()
    await host.setValue('http://git.example.test:8080')
    await reviewCredential(wrapper)
    expect(wrapper.get('[data-testid="workspace-credential-review-host"]').text()).toBe('http://git.example.test:8080')
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click'); await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ purpose: 'git', provider, host: 'http://git.example.test:8080', value: 'new-token' }))
    wrapper.unmount()
  })

  it('updates only Host without submitting or revealing an existing token, and cancels unsaved changes', async () => {
    const item: CredentialItem = { id: 'gitlab-host', type: 'git', provider: 'gitlab', purpose: 'git', secretKind: 'token', host: 'https://gitlab.com', name: 'GitLab team', configured: true, masked: 'gl-…1234' }
    const wrapper = mountPanel({ items: [item] }); await flushPromises()
    expect(wrapper.get('[data-testid="workspace-credential-host-label-gitlab-host"]').text()).toBe('gitlab.com')
    await wrapper.get('[data-testid="workspace-credential-edit-gitlab-host"]').trigger('click')
    expect(wrapper.get('[data-testid="workspace-credential-save-gitlab-host"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="workspace-credential-host-gitlab-host"]').setValue('https://ignored.example.test')
    await wrapper.get('[data-testid="workspace-credential-cancel-gitlab-host"]').trigger('click')
    await wrapper.get('[data-testid="workspace-credential-edit-gitlab-host"]').trigger('click')
    expect((wrapper.get('[data-testid="workspace-credential-host-gitlab-host"]').element as HTMLInputElement).value).toBe('gitlab.com')
    await wrapper.get('[data-testid="workspace-credential-host-gitlab-host"]').setValue('http://git.example.test:8080')
    mocks.put.mockResolvedValueOnce({ ...item, host: 'http://git.example.test:8080' })
    await wrapper.get('[data-testid="workspace-credential-save-gitlab-host"]').trigger('click'); await flushPromises()
    expect(mocks.put).toHaveBeenCalledWith(item.id, expect.objectContaining({ host: 'http://git.example.test:8080' }))
    expect(mocks.put.mock.calls.at(-1)?.[1]).not.toHaveProperty('value')
    expect(wrapper.get('[data-testid="workspace-credential-host-label-gitlab-host"]').text()).toBe('http://git.example.test:8080')
    expect(wrapper.get('[data-testid="workspace-credential-masked"]').text()).toBe('gl-…1234')
    wrapper.unmount()
  })

  it('shows a retry surface when loading fails', async () => {
    mocks.get.mockRejectedValueOnce(new Error('credentials unavailable'))
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.text()).toContain('credentials unavailable')
    mocks.get.mockResolvedValueOnce(config)
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="workspace-credential-row"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('clears the secret when going back and choosing another credential kind', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await openCredentialWizard(wrapper)
    await wrapper.get('[data-testid="credential-kind-model_api"]').trigger('click')
    await wrapper.get('[data-testid="workspace-credential-create-next"]').trigger('click')
    await flushPromises()
    await new Promise((resolve) => setTimeout(resolve, 220))

    const shell = wrapper.get('[data-testid="workspace-credential-create-form"]')
    expect(wrapper.find('[data-testid="model-credential-issuer"]').exists()).toBe(true)
    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('工作账号')
    const secret = wrapper.get('[data-testid="workspace-credential-create-value"]')
    await secret.setValue('sk-reachable')
    expect((secret.element as HTMLInputElement).value).toBe('sk-reachable')

    await wrapper.get('[data-testid="workspace-credential-create-back"]').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 220))
    expect(shell.attributes('data-credential-step')).toBe('1')
    expect(shell.attributes('data-step-motion')).toBe('back')
    expect(wrapper.find('[data-testid="workspace-credential-create-value"]').exists()).toBe(false)
    expect(mocks.create).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="credential-kind-github"]').trigger('click')
    await wrapper.get('[data-testid="workspace-credential-create-next"]').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 220))
    expect(shell.attributes('data-credential-step')).toBe('2')
    expect(wrapper.find('[data-test="opencode-provider"]').exists()).toBe(false)
    expect((wrapper.get('[data-testid="workspace-credential-create-alias"]').element as HTMLInputElement).value).toBe('工作账号')
    expect((wrapper.get('[data-testid="workspace-credential-create-value"]').element as HTMLInputElement).value).toBe('')
    expect(wrapper.get('[data-testid="workspace-credential-review-next"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('keeps tall credential forms reachable without fixing the create step height', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await openCredentialWizard(wrapper)
    await wrapper.get('[data-testid="credential-kind-model_api"]').trigger('click')
    await wrapper.get('[data-testid="workspace-credential-create-next"]').trigger('click')
    await flushPromises()
    await new Promise((resolve) => setTimeout(resolve, 220))

    const shell = wrapper.get('[data-testid="workspace-credential-create-form"]')
    const shellEl = shell.element as HTMLElement
    const styleAttr = shell.attributes('style') || ''
    // A fixed height clipped the secret field when model settings grew after loading.
    expect(shellEl.style.height).toBe('')
    expect(styleAttr).not.toMatch(/(?:^|;)\s*height\s*:/)
    if (shellEl.style.minHeight) {
      expect(shellEl.style.minHeight).toMatch(/^\d+(\.\d+)?px$/)
    }

    const pane = shell.get('.credential-step-pane').element as HTMLElement
    pane.style.minHeight = '1600px'
    await flushPromises()

    const secret = wrapper.get('[data-testid="workspace-credential-create-value"]')
    expect(secret.exists()).toBe(true)
    await secret.setValue('sk-reachable')
    expect((secret.element as HTMLInputElement).value).toBe('sk-reachable')
    expect(shellEl.style.height).toBe('')

    await wrapper.get('[data-testid="workspace-credential-create-back"]').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 220))
    expect(shell.attributes('data-credential-step')).toBe('1')
    expect(shell.attributes('data-step-motion')).toBe('back')
    wrapper.unmount()
  })

  it('deletes an unconfigured workspace API account after confirmation and keeps other sections unchanged', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const foreign: CredentialItem = { id: 'foreign-api', type: 'ai', name: 'Imported', purpose: 'model_api', issuer: 'openai', configured: true, masked: 'sk-…1', source: 'vault' }
    const empty: CredentialItem = { id: 'empty-api', type: 'ai', name: '1', purpose: 'model_api', issuer: 'deepseek', provider: 'deepseek', configured: false, source: 'workspace' }
    const wrapper = mountPanel({ items: [...config.items, empty, serviceKey, foreign] })
    await flushPromises()
    const row = rowByAlias(wrapper, '1')
    expect(row.get('[data-testid="workspace-credential-fill-empty-api"]').text()).toContain('填写')
    expect(row.find('[data-testid="workspace-credential-clear-empty-api"]').exists()).toBe(false)
    expect(row.get('[data-testid="api-account-more-empty-api"]').attributes('aria-label')).toBe('更多操作 · 1')
    expect(wrapper.get('[data-testid="workspace-credential-section-api"]').get('.credentials-section-count').text()).toBe('3')
    for (const section of ['coding', 'git', 'ssh', 'other']) {
      const group = wrapper.find(`[data-testid="workspace-credential-section-${section}"]`)
      if (group.exists()) expect(group.find('[data-testid^="api-account-"]').exists()).toBe(false)
    }
    expect(rowByAlias(wrapper, 'Imported').find('[data-testid="api-account-more-foreign-api"]').exists()).toBe(false)
    await openApiAccountMenu(wrapper, 'empty-api')
    const remove = apiAccountDelete(wrapper, 'empty-api')
    expect(remove.getAttribute('aria-label')).toBe('删除 · 1')
    remove.click()
    await flushPromises()
    expect(confirm).toHaveBeenCalledTimes(1)
    const message = String(confirm.mock.calls[0]?.[0])
    expect(message).toContain('使用此 API 账号的模型配置会失效')
    expect(message).toContain('绑定这些配置的 Agent 将无法运行')
    expect(message).toContain('「1」')
    expect(mocks.remove).not.toHaveBeenCalled()
    expect(mocks.del).not.toHaveBeenCalled()
    expect(rowByAlias(wrapper, '1').text()).toContain('尚未添加凭据')

    confirm.mockReturnValue(true)
    await openApiAccountMenu(wrapper, 'empty-api')
    apiAccountDelete(wrapper, 'empty-api').click()
    await flushPromises()
    expect(mocks.remove).toHaveBeenCalledTimes(1)
    expect(mocks.remove).toHaveBeenCalledWith('empty-api')
    expect(mocks.del).not.toHaveBeenCalled()
    expect(wrapper.findAll('[data-testid="workspace-credential-alias"]').map((node) => node.text())).not.toContain('1')
    expect(wrapper.get('[data-testid="workspace-credential-section-api"]').get('.credentials-section-count').text()).toBe('2')
    expect(mocks.success).toHaveBeenCalledWith('API 账号已删除')
    expect(wrapper.get('[data-testid="workspace-credential-section-coding"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="workspace-credential-section-git"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="workspace-credential-section-ssh"]').exists()).toBe(true)
    wrapper.unmount()
    confirm.mockRestore()
  })

  it('clears a configured API account in place and deletes the whole account only after confirmation', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const wrapper = mountPanel({ items: [...config.items, serviceKey] })
    await flushPromises()
    await wrapper.get('[data-testid="workspace-credential-clear-model-key"]').trigger('click')
    await flushPromises()
    expect(mocks.del).toHaveBeenCalledWith('model-key')
    expect(mocks.remove).not.toHaveBeenCalled()
    const kept = rowByAlias(wrapper, 'Shared model key')
    expect(kept.text()).toContain('尚未添加凭据')
    expect(kept.find('[data-testid="workspace-credential-masked"]').exists()).toBe(false)
    expect(kept.find('[data-testid="workspace-credential-clear-model-key"]').exists()).toBe(false)
    expect(kept.get('[data-testid="workspace-credential-fill-model-key"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="workspace-credential-section-api"]').get('.credentials-section-count').text()).toBe('1')
    await openApiAccountMenu(wrapper, 'model-key')
    apiAccountDelete(wrapper, 'model-key').click()
    await flushPromises()
    expect(mocks.remove).toHaveBeenCalledWith('model-key')
    expect(wrapper.find('[data-testid="workspace-credential-section-api"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-section-git"]').exists()).toBe(true)
    wrapper.unmount()
    confirm.mockRestore()
  })

  it('keeps the API account and its model connection when deletion fails and ignores a second submit', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    mocks.connections.mockResolvedValue({ items: [modelService] })
    mocks.remove.mockRejectedValueOnce(new Error('delete failed'))
    const wrapper = mountPanel({ items: [...config.items, serviceKey] })
    document.body.appendChild(wrapper.element)
    await flushPromises()
    const listed = mocks.connections.mock.calls.length
    await openApiAccountMenu(wrapper, 'model-key')
    apiAccountDelete(wrapper, 'model-key').click()
    await flushPromises()
    expect(mocks.error).toHaveBeenCalledWith('删除 API 账号失败')
    expect(mocks.connections.mock.calls.length).toBe(listed)
    expect(rowByAlias(wrapper, 'Shared model key').get('[data-testid="workspace-credential-masked"]').text()).toContain('sk-…9999')
    await wrapper.get('[data-testid="workspace-credential-edit-model-key"]').trigger('click')
    expect(wrapper.get('[data-testid="model-credential-service-service"]').text()).toContain(modelService.baseUrl)
    wrapper.unmount()

    mocks.remove.mockClear()
    confirm.mockClear()
    let release: (value: { status: string }) => void = () => {}
    mocks.remove.mockImplementation(() => new Promise((resolve) => { release = resolve }))
    const pendingPanel = mountPanel({ items: [serviceKey] })
    document.body.appendChild(pendingPanel.element)
    await flushPromises()
    await openApiAccountMenu(pendingPanel, 'model-key')
    apiAccountDelete(pendingPanel, 'model-key').click()
    await flushPromises()
    expect(mocks.remove).toHaveBeenCalledTimes(1)
    await openApiAccountMenu(pendingPanel, 'model-key')
    apiAccountDelete(pendingPanel, 'model-key').click()
    await flushPromises()
    expect(confirm).toHaveBeenCalledTimes(1)
    expect(mocks.remove).toHaveBeenCalledTimes(1)
    release({ status: 'removed' })
    await flushPromises()
    expect(pendingPanel.find('[data-testid="workspace-credential-section-api"]').exists()).toBe(false)
    pendingPanel.unmount()
    confirm.mockRestore()
  })

  it('keeps other account connections when refreshing them fails after the account is deleted', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const other: CredentialItem = { id: 'other-key', type: 'ai', name: 'Other key', purpose: 'model_api', issuer: 'openai', configured: true, masked: 'sk-…2222' }
    const bound: ModelConnection = { ...modelService, id: 'bound-service', credentialId: serviceKey.id, baseUrl: 'https://bound.example.test/v1' }
    const otherService: ModelConnection = { ...modelService, id: 'other-service', name: 'Other service', credentialId: other.id, baseUrl: 'https://other.example.test/v1' }
    let listed = 0
    mocks.connections.mockImplementation(async () => {
      listed += 1
      if (listed === 1) return { items: [bound, otherService] }
      throw new Error('refresh failed')
    })
    const wrapper = mountPanel({ items: [serviceKey, other] })
    document.body.appendChild(wrapper.element)
    await flushPromises()
    await wrapper.get('[data-testid="workspace-credential-edit-other-key"]').trigger('click')
    expect(wrapper.get('[data-testid="model-credential-service-other-service"]').text()).toContain('https://other.example.test/v1')
    await openApiAccountMenu(wrapper, 'model-key')
    apiAccountDelete(wrapper, 'model-key').click()
    await flushPromises()
    expect(mocks.success).toHaveBeenCalledWith('API 账号已删除')
    expect(wrapper.findAll('[data-testid="workspace-credential-alias"]').map((node) => node.text())).not.toContain('Shared model key')
    expect(wrapper.find('[data-testid="model-credential-service-bound-service"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="model-credential-service-other-service"]').text()).toContain('https://other.example.test/v1')
    wrapper.unmount()
    confirm.mockRestore()
  })

  it('moves to the API account delete item with the keyboard and closes without deleting', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const empty: CredentialItem = { id: 'empty-api', type: 'ai', name: '1', purpose: 'model_api', issuer: 'deepseek', configured: false }
    const wrapper = mountPanel({ items: [empty] })
    document.body.appendChild(wrapper.element)
    await flushPromises()
    const trigger = wrapper.get('[data-testid="api-account-more-empty-api"]')
    vi.spyOn(trigger.element as HTMLElement, 'getBoundingClientRect').mockReturnValue(new DOMRect(180, 20, 28, 28))
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    const remove = apiAccountDelete(wrapper, 'empty-api')
    expect(remove.getAttribute('role')).toBe('menuitem')
    expect(remove.getAttribute('aria-label')).toContain('1')
    const menu = document.querySelector<HTMLElement>('[data-testid="api-account-menu-empty-api"]')
    expect(menu?.style.visibility).not.toBe('hidden')
    expect(document.activeElement).toBe(remove)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await flushPromises()
    expect(document.querySelector('[data-testid="api-account-delete-empty-api"]')).toBeNull()
    expect(mocks.remove).not.toHaveBeenCalled()
    expect(rowByAlias(wrapper, '1').exists()).toBe(true)
    wrapper.unmount()
    confirm.mockRestore()
  })
})

function openApiAccountMenu(wrapper: ReturnType<typeof mountPanel>, id: string) {
  const trigger = wrapper.get(`[data-testid="api-account-more-${id}"]`)
  vi.spyOn(trigger.element as HTMLElement, 'getBoundingClientRect').mockReturnValue(new DOMRect(180, 20, 28, 28))
  return trigger.trigger('click').then(() => flushPromises())
}

function apiAccountDelete(wrapper: ReturnType<typeof mountPanel>, id: string) {
  const selector = `[data-testid="api-account-delete-${id}"]`
  const found = wrapper.find(selector)
  const button = (found.exists() ? found.element : document.querySelector(selector)) as HTMLButtonElement | null
  if (!button) throw new Error(`missing API account delete ${id}`)
  return button
}

describe('WorkspaceCredentialsPanel overview', () => {
  it('computes credential stats from the loaded list and shows the page header with the add menu', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.get('[data-testid="settings-page-header"] h1').text()).toBe('工作区凭据')
    expect(wrapper.get('[data-testid="settings-page-header"]').text()).toContain(pages.pages.workspace.credentials.description)
    expect(wrapper.get('[data-testid="settings-page-header"] [data-testid="workspace-credential-create"]').exists()).toBe(true)
    const stat = (key: string) => wrapper.get(`[data-testid="workspace-credential-stats"] [data-testid="settings-stat-${key}"]`)
    expect(stat('total').get('[data-testid="settings-stat-value"]').text()).toBe('3')
    expect(stat('configured').get('[data-testid="settings-stat-value"]').text()).toBe('1')
    expect(stat('configured').text()).toContain('/ 3')
    expect(stat('git-hosts').get('[data-testid="settings-stat-value"]').text()).toBe('1')
    expect(stat('pending').get('[data-testid="settings-stat-value"]').text()).toBe('2')
    expect(stat('pending').classes()).toContain('settings-stat--warn')
    expect(wrapper.findAll('.credentials-section').length).toBeGreaterThan(0)
    wrapper.unmount()
  })

  it('shows the model group count on its tab once the manager reports the loaded rows', async () => {
    mocks.configurations.mockResolvedValue({ items: [{ id: 'one', name: 'One', backend: 'cursor', binding: { kind: 'platform', credentialId: 'cursor-api' }, state: 'active', revision: 1 }] })
    mocks.connections.mockResolvedValue({ items: [] })
    mocks.capabilities.mockResolvedValue({ items: [] })
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('[data-testid="credentials-tab-count-models"]').exists()).toBe(false)
    await wrapper.get('[data-testid="credentials-tab-models"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="credentials-tab-count-models"]').text()).toBe('1')
    expect(wrapper.find('[data-testid="workspace-credential-stats"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="model-configuration-stats"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps inline embeds compact without the page header or stats', async () => {
    const wrapper = mountPanel(config, 'zh-CN', { inline: true, scope: 'git' })
    await flushPromises()
    expect(wrapper.find('[data-testid="settings-page-header"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-stats"]').exists()).toBe(false)
    expect(wrapper.find('.credentials-header').exists()).toBe(true)
    expect(wrapper.find('.credentials-page').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows zero stats with the empty state for an empty list', async () => {
    const wrapper = mountPanel({ items: [] })
    await flushPromises()
    expect(wrapper.get('[data-testid="settings-stat-total"] [data-testid="settings-stat-value"]').text()).toBe('0')
    expect(wrapper.get('.credentials-empty').exists()).toBe(true)
    wrapper.unmount()
  })
})
