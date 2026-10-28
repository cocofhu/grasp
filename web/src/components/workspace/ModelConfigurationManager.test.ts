// @vitest-environment happy-dom
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { CredentialItem, ModelConfiguration, ModelConnection, ModelConnectionBody, ToolCapability } from '@/lib/api/api'
import { vHoverInk } from '@/lib/shared/hoverInkDirective'
import { theme } from '@/lib/shared/theme'
import openaiLogo from '@/assets/provider-logos/openai.svg'
import claudeLogo from '@/assets/provider-logos/claude.svg'
import geminiLogo from '@/assets/provider-logos/gemini.svg'
import opencodeLogo from '@/assets/provider-logos/opencode.svg'
import kimiLogo from '@/assets/provider-logos/kimi.svg'
import kimiDarkLogo from '@/assets/provider-logos/kimi-dark.svg'
import zhipuLogo from '@/assets/provider-logos/zhipu.svg'
import hunyuanLogo from '@/assets/provider-logos/hunyuan.svg'
import ModelConfigurationManager from './ModelConfigurationManager.vue'

const mocks = vi.hoisted(() => ({ configurations: vi.fn(), connections: vi.fn(), connectionModels: vi.fn(), credentials: vi.fn(), capabilities: vi.fn(), create: vi.fn(), update: vi.fn(), remove: vi.fn(), updateConnection: vi.fn(), success: vi.fn(), cliModels: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: { listModelConfigurations: mocks.configurations, listModelConnections: mocks.connections, listModelConnectionModels: mocks.connectionModels, listCredentials: mocks.credentials, listToolCapabilities: mocks.capabilities, createModelConfiguration: mocks.create, updateModelConfiguration: mocks.update, deleteModelConfiguration: mocks.remove, updateModelConnection: mocks.updateConnection } }))
vi.mock('@/lib/agent/cliCatalog', () => ({ loadCliModels: (...args: unknown[]) => mocks.cliModels(...args) }))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => ({ success: mocks.success }) }))

const platformKey: CredentialItem = { id: 'cursor-key', type: 'ai', name: 'Team Cursor account', purpose: 'tool_auth', toolId: 'cursor', configured: true, masked: 'cu-…1234' }
const modelKey: CredentialItem = { id: 'model-key', type: 'ai', name: 'Team API key', purpose: 'model_api', issuer: 'deepseek', configured: true, masked: 'sk-…9999' }
const service: ModelConnection = { id: 'service', name: 'Independent API service', providerId: 'deepseek', protocol: 'openai-chat-completions', baseUrl: 'https://models.example.test/v1', credentialId: modelKey.id, auth: { scheme: 'bearer' }, service: { region: 'us', product: 'API' }, models: [{ id: 'vendor/original/model', source: 'manual', input: ['text', 'image'], contextWindow: 128000, maxTokens: 8192 }], state: 'active', revision: 1 }
const configuration: ModelConfiguration = { id: 'saved', name: 'OpenCode production', backend: 'opencode', binding: { kind: 'connection', connectionId: service.id, modelId: service.models[0]!.id }, state: 'active', revision: 1 }
const capabilities: ToolCapability[] = [
  { toolId: 'cursor', modes: ['platform'], adapterState: 'existing' },
  { toolId: 'codebuddy', modes: ['platform', 'connection'], protocols: ['openai-chat-completions'], roleNames: ['main', 'fast', 'reasoning', 'subagent'], adapterState: 'existing' },
  { toolId: 'opencode', modes: ['connection'], protocols: ['openai-chat-completions'], roleNames: ['main', 'fast'], adapterState: 'existing' },
]
let services: ModelConnection[]

function setup(options: { hoverInk?: boolean; inline?: boolean } = {}) {
  return mount(ModelConfigurationManager, {
    props: { inline: options.inline },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: {} } })],
      ...(options.hoverInk ? { directives: { 'hover-ink': vHoverInk } } : {}),
      stubs: {
        Teleport: true, Icon: true,
        CliModelSelect: defineComponent({
          props: { modelValue: String, backend: String, hideReset: Boolean, excludeIds: { type: Array, default: () => [] }, allowedVendors: { type: Array, default: () => [] } }, emits: ['update:modelValue'],
          template: '<select data-testid="platform-model" :data-hide-reset="String(!!hideReset)" :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option value="">Tool default</option><option value="platform-model-v1">Platform model</option><option value="platform-model-v2">Second</option><option value="opencode/gpt-5.5">GPT-5.5</option><option value="opencode/gpt-5.6-sol">GPT-5.6 Sol</option><option value="opencode/gpt-5.5-pro">GPT-5.5 Pro</option><option value="opencode/deepseek-v4.1-flash">DeepSeek V4.1 Flash</option><option value="opencode/deepseek-v4-pro">DeepSeek V4 Pro</option><option value="opencode/claude-haiku-4-5">Haiku 4.5</option><option value="opencode/claude-haiku-5-5">Haiku 5.5</option><option value="opencode/qwen3.5-plus">Qwen 3.5 Plus</option><option value="opencode/qwen3.8-max">Qwen 3.8 Max</option><option value="opencode/minimax-m3-free">MiniMax M3 Free</option><option value="opencode/minimax-m3">MiniMax M3</option><option value="opencode/kimi-k2.7-code">Kimi</option></select>',
        }),
      },
    },
  })
}

async function openStepTwo(wrapper: ReturnType<typeof setup>, backend = 'cursor') {
  await flushPromises()
  await wrapper.get('[data-testid="model-configuration-add"]').trigger('click')
  await wrapper.get('[data-testid="model-configuration-name"]').setValue('Team configuration')
  await wrapper.get(`[data-testid="model-configuration-backend-${backend}"]`).trigger('click')
  await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
  expect(wrapper.get('[data-testid="model-configuration-form"]').attributes('data-step')).toBe('2')
}

async function chooseCredential(wrapper: ReturnType<typeof setup>, value: string) {
  const field = wrapper.get('[data-testid="model-configuration-credential-choice"]')
  await field.get('[data-test="app-select-trigger"]').trigger('click')
  await field.get(`[data-test="app-select-option-${value}"]`).trigger('click')
}

async function chooseRole(wrapper: ReturnType<typeof setup>, value: string) {
  const field = wrapper.get('[data-testid="model-configuration-role-fast"]')
  await field.get('[data-test="app-select-trigger"]').trigger('click')
  await field.get('[data-test="app-select-search"]').setValue(value)
  await field.get('[data-test="app-select-custom"]').trigger('click')
}

async function chooseModel(wrapper: ReturnType<typeof setup>, value: string, custom = false) {
  const field = wrapper.get('[data-testid="model-configuration-model-id"]')
  await field.get('[data-test="app-select-trigger"]').trigger('click')
  await field.get('[data-test="app-select-search"]').setValue(value)
  await field.get(`[data-test="${custom ? 'app-select-custom' : `app-select-option-${value}`}"]`).trigger('click')
}

async function chooseImageInput(wrapper: ReturnType<typeof setup>, value: 'default' | 'text' | 'image', testId = 'model-configuration-image-input') {
  const field = wrapper.get(`[data-testid="${testId}"]`)
  await field.get('[data-test="app-select-trigger"]').trigger('click')
  await field.get(`[data-test="app-select-option-${value}"]`).trigger('click')
}

async function openConnectionModelStep(wrapper: ReturnType<typeof setup>, backend = 'opencode') {
  await openStepTwo(wrapper, backend)
  await chooseCredential(wrapper, `connection:${service.id}`)
  await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
  expect(wrapper.get('[data-testid="model-configuration-form"]').attributes('data-step')).toBe('3')
}

beforeEach(() => {
  vi.clearAllMocks()
  services = structuredClone([service])
  mocks.configurations.mockResolvedValue({ items: [] })
  mocks.connections.mockImplementation(async () => ({ items: structuredClone(services) }))
  mocks.connectionModels.mockResolvedValue({ models: [] })
  mocks.credentials.mockResolvedValue({ items: [platformKey, modelKey] })
  mocks.capabilities.mockResolvedValue({ items: capabilities })
  mocks.create.mockImplementation(async (body: Record<string, unknown>) => ({ id: 'created', ...body, revision: 1 }))
  mocks.update.mockImplementation(async (id: string, body: Record<string, unknown>) => ({ id, ...body, revision: 2 }))
  mocks.remove.mockResolvedValue({ status: 'ok' })
  mocks.cliModels.mockResolvedValue([])
  mocks.updateConnection.mockImplementation(async (id: string, body: ModelConnectionBody) => {
    const updated = { ...services.find((item) => item.id === id)!, ...body, revision: 2 }
    services = services.map((item) => item.id === id ? updated : item)
    return updated
  })
})

describe('ModelConfigurationManager', () => {
  it('offers the named-model workflow without a separate model catalog', async () => {
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.find('[data-testid="model-catalog-panel"]').exists()).toBe(false)
    expect(wrapper.get('h3').text()).toBe('Model groups')
    expect(wrapper.get('[data-testid="model-configuration-add"]').text()).toBe('New model group')
    expect(wrapper.find('.configuration-advanced').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Model catalog')
    await wrapper.get('[data-testid="model-configuration-add"]').trigger('click')
    expect(wrapper.find('[data-testid="model-configuration-form"]').exists()).toBe(true)
    wrapper.unmount()
  })
  it('guides users to credentials and keeps Cursor limited to platform authorization', async () => {
    mocks.credentials.mockResolvedValue({ items: [modelKey] })
    mocks.capabilities.mockResolvedValue({ items: [{ ...capabilities[0]!, modes: ['platform', 'connection'], protocols: ['openai-chat-completions'] }] })
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-testid="model-configuration-manage-credentials"]').trigger('click')
    expect(wrapper.emitted('manageCredentials')).toHaveLength(1)
    await openStepTwo(wrapper)
    expect(wrapper.find('[data-testid="model-configuration-source"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="model-configuration-next"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="model-configuration-connection"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="model-configuration-credentials-empty"]').exists()).toBe(true)
    await wrapper.get('[data-testid="model-configuration-add-credential"]').trigger('click')
    expect(wrapper.emitted('manageCredentials')).toHaveLength(2)
    expect(wrapper.find('[data-testid="model-configuration-form"]').exists()).toBe(false)
    expect(mocks.create).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each(['platform-model-v1', ''])('saves a platform tool, credential and selected model (%s) as one configuration', async (modelId) => {
    const wrapper = setup()
    await openStepTwo(wrapper)
    await chooseCredential(wrapper, `platform:${platformKey.id}`)
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    await wrapper.get('[data-testid="platform-model"]').setValue(modelId)
    expect(wrapper.find('[data-testid="model-configuration-image-input"]').exists()).toBe(false)
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith({ name: 'Team configuration', backend: 'cursor', binding: { kind: 'platform', credentialId: platformKey.id, ...(modelId ? { modelId } : {}) }, state: 'active' })
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="model-configuration-form"]').exists()).toBe(false)
    expect(wrapper.emitted('changed')).toHaveLength(1)
    wrapper.unmount()
  })

  it.each(['claude_code', 'codex'])('keeps %s image handling native while saving and enriching its connection models', async (backend) => {
    mocks.capabilities.mockResolvedValue({ items: [{ toolId: backend, modes: ['connection'], protocols: ['openai-chat-completions'], roleNames: ['main', 'fast'], adapterState: 'existing' }] })
    const primary: ModelConnection['models'][number] = { id: 'primary/model', source: 'manual', capabilities: {} }
    const auxiliary: ModelConnection['models'][number] = { id: 'aux/model', source: 'manual' }
    services = [{ ...service, models: [primary, auxiliary] }]
    mocks.connectionModels.mockResolvedValue({ models: [{ ...primary, input: ['text', 'image'], capabilities: { vision: true } }] })
    const wrapper = setup()
    await openConnectionModelStep(wrapper, backend)
    await flushPromises()
    await chooseModel(wrapper, primary.id)
    await wrapper.get('[data-testid="model-configuration-add-model"]').trigger('click')
    await chooseRole(wrapper, 'role/model')
    expect(wrapper.find('[data-testid="model-configuration-image-input"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="model-configuration-aux-image-input-0"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="model-configuration-role-image-input-fast"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Image input')
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ backend, binding: { kind: 'connection', connectionId: service.id, modelId: primary.id, auxiliaryModelIds: [auxiliary.id], toolSettings: { modelRoles: { fast: 'role/model' } } } }))
    expect(services[0]!.models).toEqual([{ ...primary, input: ['text', 'image'], capabilities: { vision: true } }, auxiliary, { id: 'role/model', source: 'manual' }])
    wrapper.unmount()
  })

  it('keeps unknown model input undeclared until the user enables images and saves', async () => {
    const model: ModelConnection['models'][number] = { id: 'kimi-k3.1-pro', source: 'discovered', capabilities: {} }
    services = [{ ...service, models: [model] }]
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    await chooseModel(wrapper, model.id)
    const field = wrapper.getComponent('[data-testid="model-configuration-image-input"]')
    expect(field.props('modelValue')).toBe('default')
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    await chooseImageInput(wrapper, 'image')
    expect(field.props('modelValue')).toBe('image')
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.updateConnection).toHaveBeenCalledExactlyOnceWith(service.id, expect.objectContaining({ models: [{ ...model, input: ['text', 'image'], capabilities: { vision: true } }] }))
    expect(services[0]!.models.map(item => item.id)).toEqual([model.id])
    wrapper.unmount()
  })

  it.each<['default' | 'text' | 'image', Partial<ModelConnection['models'][number]>]>([
    ['default', { capabilities: {} }],
    ['image', { input: ['text', 'image'] }],
    ['text', { capabilities: { vision: false } }],
  ])('does not rewrite a saved model when keeping its %s input', async (mode, metadata) => {
    services = [{ ...service, models: [{ id: 'unknown/model', source: 'manual', ...metadata }] }]
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    await chooseModel(wrapper, 'unknown/model')
    await chooseImageInput(wrapper, mode)
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    expect(mocks.create).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('enriches required saved models on save while preserving explicit text-only input and false capabilities', async () => {
    const unknown: ModelConnection['models'][number] = { id: 'unknown/model', source: 'manual', capabilities: {} }
    const textOnly: ModelConnection['models'][number] = { id: 'text/model', source: 'manual', input: ['text'], capabilities: { vision: false, reasoning: false } }
    const unselected: ModelConnection['models'][number] = { id: 'other/model', source: 'manual', capabilities: {} }
    services = [{ ...service, models: [unknown, textOnly, unselected] }]
    mocks.connectionModels.mockResolvedValue({ models: [unknown, textOnly, unselected].map(model => ({ ...model, input: ['text', 'image'], capabilities: { vision: true, reasoning: true } })) })
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    await flushPromises()
    await chooseModel(wrapper, unknown.id)
    expect(wrapper.getComponent('[data-testid="model-configuration-image-input"]').props('modelValue')).toBe('image')
    await wrapper.get('[data-testid="model-configuration-add-model"]').trigger('click')
    expect(wrapper.getComponent('[data-testid="model-configuration-aux-image-input-0"]').props('modelValue')).toBe('text')
    await wrapper.get('[data-testid="model-configuration-refresh-models"]').trigger('click')
    await flushPromises()
    expect(wrapper.getComponent('[data-testid="model-configuration-aux-image-input-0"]').props('modelValue')).toBe('text')
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(services[0]!.models).toEqual([
      { ...unknown, input: ['text', 'image'], capabilities: { vision: true, reasoning: true } },
      textOnly,
      unselected,
    ])
    wrapper.unmount()
  })

  it('saves independent primary, auxiliary and role image choices against the latest account metadata', async () => {
    const primary: ModelConnection['models'][number] = { id: 'primary/model', source: 'manual', capabilities: {} }
    const auxiliary: ModelConnection['models'][number] = { id: 'aux/model', source: 'manual' }
    const role: ModelConnection['models'][number] = { id: 'role/model', source: 'manual', input: ['text', 'image'], capabilities: { vision: true } }
    services = [{ ...service, models: [primary, auxiliary, role] }]
    const wrapper = setup()
    await openConnectionModelStep(wrapper, 'codebuddy')
    await chooseModel(wrapper, primary.id)
    await chooseImageInput(wrapper, 'image')
    await wrapper.get('[data-testid="model-configuration-add-model"]').trigger('click')
    await chooseImageInput(wrapper, 'text', 'model-configuration-aux-image-input-0')
    const roleField = wrapper.get('[data-testid="model-configuration-role-fast"]')
    await roleField.get('[data-test="app-select-trigger"]').trigger('click')
    await roleField.get(`[data-test="app-select-option-${role.id}"]`).trigger('click')
    await chooseImageInput(wrapper, 'default', 'model-configuration-role-image-input-fast')
    const concurrent: ModelConnection['models'][number] = { id: 'concurrent/model', source: 'manual', input: ['text'] }
    services = [{ ...service, name: 'Latest name', models: [{ ...primary, name: 'Latest primary', maxTokens: 8192, capabilities: { toolCalls: true } }, auxiliary, role, concurrent] }]
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.updateConnection).toHaveBeenCalledExactlyOnceWith(service.id, expect.objectContaining({ name: 'Latest name', models: [
      { ...primary, name: 'Latest primary', maxTokens: 8192, input: ['text', 'image'], capabilities: { toolCalls: true, vision: true } },
      { ...auxiliary, input: ['text'], capabilities: { vision: false } },
      { id: role.id, source: 'manual' },
      concurrent,
    ] }))
    wrapper.unmount()
  })

  it('discards image drafts when cancelling or switching the API account', async () => {
    const model: ModelConnection['models'][number] = { id: 'same/model', source: 'manual' }
    services = [{ ...service, models: [model] }, { ...service, id: 'other-service', name: 'Other API', models: [model] }]
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    await chooseModel(wrapper, model.id)
    await chooseImageInput(wrapper, 'image')
    await wrapper.get('[data-testid="model-configuration-back"]').trigger('click')
    await chooseCredential(wrapper, 'connection:other-service')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    await chooseModel(wrapper, model.id)
    expect(wrapper.getComponent('[data-testid="model-configuration-image-input"]').props('modelValue')).toBe('default')
    await chooseImageInput(wrapper, 'text')
    await wrapper.get('button[aria-label="Close"]').trigger('click')
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    await openConnectionModelStep(wrapper)
    await chooseModel(wrapper, model.id)
    expect(wrapper.getComponent('[data-testid="model-configuration-image-input"]').props('modelValue')).toBe('default')
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps the image draft through catalog refresh and revisiting the same account, and resets it for a different tool', async () => {
    const model: ModelConnection['models'][number] = { id: 'same/model', source: 'manual' }
    services = [{ ...service, models: [model] }]
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    await chooseModel(wrapper, model.id)
    await chooseImageInput(wrapper, 'image')
    mocks.connectionModels.mockResolvedValue({ models: [{ ...model, input: ['text'], capabilities: { vision: false } }] })
    await wrapper.get('[data-testid="model-configuration-refresh-models"]').trigger('click')
    await flushPromises()
    expect(wrapper.getComponent('[data-testid="model-configuration-image-input"]').props('modelValue')).toBe('image')
    await wrapper.get('[data-testid="model-configuration-back"]').trigger('click')
    await chooseCredential(wrapper, `connection:${service.id}`)
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    expect(wrapper.getComponent('[data-testid="model-configuration-image-input"]').props('modelValue')).toBe('image')
    await wrapper.get('[data-testid="model-configuration-back"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-back"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-backend-codebuddy"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    await chooseCredential(wrapper, `connection:${service.id}`)
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    await flushPromises()
    await chooseModel(wrapper, model.id)
    expect(wrapper.getComponent('[data-testid="model-configuration-image-input"]').props('modelValue')).toBe('text')
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('saves image drafts only for models that remain selected', async () => {
    const primary: ModelConnection['models'][number] = { id: 'primary/model', source: 'manual' }
    const auxiliary: ModelConnection['models'][number] = { id: 'aux/model', source: 'manual' }
    services = [{ ...service, models: [primary, auxiliary] }]
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    await chooseModel(wrapper, primary.id)
    await wrapper.get('[data-testid="model-configuration-add-model"]').trigger('click')
    await chooseImageInput(wrapper, 'image', 'model-configuration-aux-image-input-0')
    await wrapper.get('[data-testid="model-configuration-aux-remove-0"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    expect(services[0]!.models).toEqual([primary, auxiliary])
    wrapper.unmount()
  })

  it('saves the maintained OpenCode levels under a TokenHub model', async () => {
    const model = { id: 'deepseek-v4.1-flash', name: 'DeepSeek V4.1 Flash', source: 'discovered' as const }
    services = structuredClone([{ ...service, providerId: 'tokenhub', name: 'TokenHub', models: [model] }])
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    await chooseModel(wrapper, model.id)
    const menu = wrapper.get('[data-testid="model-configuration-effort"]')
    expect(menu.get('[data-testid="model-effort-slider"]').exists()).toBe(true)
    expect(menu.attributes('data-levels')).toBe('Default|Low|High|Max')
    expect(menu.get('[data-testid="model-effort-current"]').text()).toBe('Default')
    expect(menu.text()).not.toContain('Medium')
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({
      backend: 'opencode',
      binding: expect.objectContaining({ kind: 'connection', modelId: model.id }),
    }))
    expect(mocks.create.mock.calls[0]?.[0].binding.toolSettings).toBeUndefined()
    wrapper.unmount()

    const again = setup()
    await openConnectionModelStep(again)
    await chooseModel(again, model.id)
    await again.get('[data-testid="model-effort-slider"]').setValue('2')
    await again.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenLastCalledWith(expect.objectContaining({
      backend: 'opencode',
      binding: expect.objectContaining({ toolSettings: { modelEfforts: { [model.id]: 'high' } } }),
    }))
    again.unmount()
  })

  it('keeps TokenHub deepseek-v4-pro on the verified levels', async () => {
    const model = { id: 'deepseek-v4-pro', name: 'DeepSeek V4 Pro', source: 'discovered' as const }
    services = structuredClone([{ ...service, providerId: 'tokenhub', name: 'TokenHub', models: [model] }])
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    await chooseModel(wrapper, model.id)
    expect(wrapper.get('[data-testid="model-configuration-effort"]').attributes('data-levels')).toBe('Default|Low|High|Max')
    wrapper.unmount()
  })

  it('shows the Codex slider for TokenHub Kimi K3.1 Pro', async () => {
    const model = { id: 'kimi-k3.1-pro', name: 'Kimi K3.1 Pro', source: 'discovered' as const }
    services = structuredClone([{ ...service, providerId: 'tokenhub', name: 'TokenHub', models: [model] }])
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    await chooseModel(wrapper, model.id)
    expect(wrapper.get('[data-testid="model-configuration-effort"]').attributes('data-levels')).toBe('Default|Low|High|Max')
    wrapper.unmount()
  })

  it('shows the Codex slider for a dated TokenHub model id', async () => {
    const model = { id: 'DeepSeek-V4.1-Flash-1017', name: 'DeepSeek-V4.1-Flash-1017', source: 'discovered' as const }
    services = structuredClone([{ ...service, providerId: 'tokenhub', name: 'TokenHub', models: [model] }])
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    await chooseModel(wrapper, model.id)
    const menu = wrapper.get('[data-testid="model-configuration-effort"]')
    expect(menu.get('[data-testid="model-effort-slider"]').exists()).toBe(true)
    expect(menu.attributes('data-levels')).toBe('Default|Low|High|Max')
    expect(menu.text()).toContain('DeepSeek-V4.1-Flash-1017')
    wrapper.unmount()
  })

  it('shows each official OpenCode model its own variants and remembers a custom level', async () => {
    mocks.capabilities.mockResolvedValue({ items: [{ toolId: 'opencode', modes: ['platform'], adapterState: 'existing' }] })
    mocks.credentials.mockResolvedValue({ items: [{ id: 'zen', type: 'ai', name: 'OpenCode', purpose: 'tool_auth', toolId: 'opencode', configured: true }] })
    const wrapper = setup()
    await openStepTwo(wrapper, 'opencode')
    await chooseCredential(wrapper, 'platform:zen')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    const menus: Record<string, string[]> = {
      'opencode/gpt-5.5': ['Default', 'None', 'Low', 'Medium', 'High', 'Extra high'],
      'opencode/gpt-5.6-sol': ['Default', 'None', 'Low', 'Medium', 'High', 'Extra high', 'Max'],
      'opencode/gpt-5.5-pro': ['Default', 'Medium', 'High', 'Extra high'],
      'opencode/deepseek-v4.1-flash': ['Default', 'Low', 'High', 'Max'],
      'opencode/deepseek-v4-pro': ['Default', 'High', 'Max'],
      'opencode/claude-haiku-4-5': ['Default', 'High', 'Max'],
      'opencode/claude-haiku-5-5': ['Default', 'None', 'Low', 'Medium', 'High', 'Extra high', 'Max'],
      'opencode/qwen3.5-plus': ['Default', 'None', 'High', 'Max'],
      'opencode/qwen3.8-max': ['Default', 'Low', 'Medium', 'Extra high'],
      'opencode/minimax-m3-free': ['Default', 'None', 'thinking'],
    }
    for (const [modelId, labels] of Object.entries(menus)) {
      await wrapper.get('[data-testid="platform-model"]').setValue(modelId)
      const menu = wrapper.get('[data-testid="model-configuration-effort"]')
      expect(menu.get('[data-testid="model-effort-slider"]').exists()).toBe(true)
      expect(menu.attributes('data-levels')).toBe(labels.join('|'))
    }
    for (const modelId of ['opencode/minimax-m3', 'opencode/kimi-k2.7-code']) {
      await wrapper.get('[data-testid="platform-model"]').setValue(modelId)
      expect(wrapper.find('[data-testid="model-configuration-effort"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="model-configuration-effort-custom"]').exists()).toBe(true)
    }
    await wrapper.get('[data-testid="platform-model"]').setValue('opencode/gpt-5.5')
    await wrapper.get('[data-testid="model-effort-mode-fixed"]').trigger('click')
    expect(wrapper.find('[data-testid="model-effort-slider"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="model-effort-fixed-note"]').text()).toContain('cannot change')
    await wrapper.get('[data-testid="model-configuration-effort-custom"]').setValue('turbo')
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({
      backend: 'opencode',
      binding: expect.objectContaining({ modelId: 'opencode/gpt-5.5', toolSettings: { modelEfforts: { 'opencode/gpt-5.5': 'turbo' } } }),
    }))
    wrapper.unmount()
  })

  it('restores the saved variant when the model group is opened again', async () => {
    const model = { id: 'deepseek-v4-flash', name: 'DeepSeek V4 Flash', source: 'discovered' as const }
    services = structuredClone([{ ...service, providerId: 'tokenhub', name: 'TokenHub', models: [model] }])
    const saved: ModelConfiguration = {
      id: 'saved-effort', name: 'TokenHub group', backend: 'opencode', state: 'active', revision: 1,
      binding: { kind: 'connection', connectionId: service.id, modelId: model.id, toolSettings: { modelEfforts: { [model.id]: 'max' } } },
    }
    mocks.configurations.mockResolvedValue({ items: [saved] })
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-testid="model-configuration-edit-saved-effort"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    expect(wrapper.get('[data-testid="model-effort-current"]').text()).toBe('Max')
    expect((wrapper.get('[data-testid="model-effort-slider"]').element as HTMLInputElement).value).toBe('3')
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(saved.id, expect.objectContaining({
      binding: expect.objectContaining({ toolSettings: { modelEfforts: { [model.id]: 'max' } } }),
    }))
    wrapper.unmount()
  })

  it('hides the effort slider for an OpenCode model we have not listed', async () => {
    services = structuredClone([{ ...service, providerId: 'tokenhub', name: 'TokenHub' }])
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    await chooseModel(wrapper, service.models[0]!.id)
    expect(wrapper.find('[data-testid="model-configuration-effort"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="model-configuration-effort-custom"]').exists()).toBe(true)
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith({ name: 'Team configuration', backend: 'opencode', binding: { kind: 'connection', connectionId: service.id, modelId: service.models[0]!.id }, state: 'active' })
    wrapper.unmount()
  })

  it.each(['opencode', 'deepseek_harness'])('offers the official %s authorization alongside compatible API accounts', async (backend) => {
    const officialKey: CredentialItem = { id: 'official-key', type: 'ai', name: 'Official tool account', purpose: 'tool_auth', toolId: backend, provider: backend, secretKind: 'api_key', configured: true }
    const otherTool: CredentialItem = { ...officialKey, id: 'other-tool-key', name: 'Another tool account', toolId: backend === 'opencode' ? 'deepseek_harness' : 'opencode' }
    mocks.credentials.mockResolvedValue({ items: [officialKey, otherTool, modelKey] })
    mocks.capabilities.mockResolvedValue({ items: [{ toolId: backend, modes: ['platform', 'connection'], protocols: ['openai-chat-completions'], adapterState: 'existing' }] })
    const wrapper = setup()
    await openStepTwo(wrapper, backend)
    const field = wrapper.get('[data-testid="model-configuration-credential-choice"]')
    await field.get('[data-test="app-select-trigger"]').trigger('click')
    expect(field.find('[data-test="app-select-option-platform:official-key"]').exists()).toBe(true)
    expect(field.find('[data-test="app-select-option-connection:service"]').exists()).toBe(true)
    expect(field.find('[data-test="app-select-option-platform:other-tool-key"]').exists()).toBe(false)
    expect(field.find('[data-test="app-select-option-platform:model-key"]').exists()).toBe(false)
    await field.get('[data-test="app-select-option-platform:official-key"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    const modelId = backend === 'opencode' ? 'opencode/kimi-k2.7-code' : 'platform-model-v1'
    await wrapper.get('[data-testid="platform-model"]').setValue(modelId)
    expect(wrapper.find('[data-testid="model-configuration-effort"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="model-configuration-effort-custom"]').exists()).toBe(backend === 'opencode')
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.cliModels).toHaveBeenCalledWith(backend)
    expect(mocks.create).toHaveBeenCalledWith({ name: 'Team configuration', backend, binding: { kind: 'platform', credentialId: officialKey.id, modelId }, state: 'active' })
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    expect(mocks.connectionModels).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('locks OpenCode Go and OpenCode to one family and rejects a typed id from the other', async () => {
    const officialKey: CredentialItem = { id: 'go-key', type: 'ai', name: 'Go subscription', purpose: 'tool_auth', toolId: 'opencode', configured: true }
    mocks.credentials.mockResolvedValue({ items: [officialKey] })
    mocks.capabilities.mockResolvedValue({ items: [{ toolId: 'opencode', modes: ['platform'], adapterState: 'existing' }] })
    mocks.cliModels.mockResolvedValue([
      { id: 'opencode/kimi-k3', name: 'Kimi K3', vendor: 'opencode' },
      { id: 'opencode-go/mimo-v2.5', name: 'MiMo V2.5', vendor: 'opencode-go' },
      { id: 'opencode-go/mimo-v2.5-pro', name: 'MiMo V2.5 Pro', vendor: 'opencode-go' },
    ])
    const wrapper = setup()
    await openStepTwo(wrapper, 'opencode')
    await chooseCredential(wrapper, 'platform:go-key')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="model-configuration-official-family"]').text()).toContain('opencode-go/<model>')
    await wrapper.getComponent('[data-testid="platform-model"]').vm.$emit('update:modelValue', 'opencode-go/mimo-v2.5-pro')
    expect(wrapper.getComponent('[data-testid="platform-model"]').props('allowedVendors')).toEqual(['opencode-go'])
    await wrapper.get('[data-testid="model-configuration-add-model"]').trigger('click')
    const aux = wrapper.get('[data-testid="model-configuration-aux-select-0"]')
    await aux.get('[data-test="app-select-trigger"]').trigger('click')
    expect(aux.text()).toContain('MiMo V2.5')
    expect(aux.text()).not.toContain('Kimi K3')
    await aux.get('[data-test="app-select-search"]').setValue('opencode/kimi-k3')
    await aux.get('[data-test="app-select-custom"]').trigger('click')
    expect(wrapper.get('[data-testid="model-configuration-official-problem"]').text()).toContain('one family')
    expect(wrapper.get('[data-testid="model-configuration-save"]').attributes('disabled')).toBeDefined()
    await aux.get('[data-test="app-select-trigger"]').trigger('click')
    await aux.get('[data-test="app-select-search"]').setValue('opencode-go/a/b')
    await aux.get('[data-test="app-select-custom"]').trigger('click')
    expect(wrapper.get('[data-testid="model-configuration-official-problem"]').text()).toContain('opencode/<model> or opencode-go/<model>')
    expect(wrapper.get('[data-testid="model-configuration-save"]').attributes('disabled')).toBeDefined()
    await aux.get('[data-test="app-select-trigger"]').trigger('click')
    await aux.get('[data-test="app-select-option-opencode-go/mimo-v2.5"]').trigger('click')
    expect(wrapper.find('[data-testid="model-configuration-official-problem"]').exists()).toBe(false)
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({
      backend: 'opencode',
      binding: expect.objectContaining({ kind: 'platform', credentialId: 'go-key', modelId: 'opencode-go/mimo-v2.5-pro', auxiliaryModelIds: ['opencode-go/mimo-v2.5'] }),
    }))
    wrapper.unmount()
  })

  it('loads API-account models at the model step and saves only the selected discovered models with their metadata', async () => {
    const primary: ModelConnection['models'][number] = { id: 'vendor/Mixed.Case:primary', name: 'Provider primary', source: 'discovered', capabilities: { vision: true, reasoning: true }, input: ['text', 'image'], contextWindow: 256000, maxTokens: 16384, reasoningEfforts: ['low', 'high'] }
    const auxiliary: ModelConnection['models'][number] = { id: 'vendor/auxiliary', name: 'Provider auxiliary', source: 'discovered', capabilities: { toolCalls: true } }
    const unused: ModelConnection['models'][number] = { id: 'vendor/unselected', name: 'Do not import', source: 'discovered' }
    const savedModel = { ...service.models[0]!, name: 'Saved model name' }
    services = [{ ...service, models: [savedModel] }]
    mocks.connectionModels.mockResolvedValue({ models: [primary, auxiliary, unused, { ...service.models[0]!, name: 'Discovered duplicate', maxTokens: 1 }] })
    const wrapper = setup()
    await openStepTwo(wrapper, 'opencode')
    await chooseCredential(wrapper, `connection:${service.id}`)
    expect(mocks.connectionModels).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    await flushPromises()
    expect(mocks.connectionModels).toHaveBeenCalledExactlyOnceWith(service.id)
    const primaryField = wrapper.get('[data-testid="model-configuration-model-id"]')
    expect(wrapper.getComponent('[data-testid="model-configuration-model-id"]').props('modelValue')).toBe('')
    await primaryField.get('[data-test="app-select-trigger"]').trigger('click')
    expect(primaryField.text()).toContain('Provider primary')
    expect(primaryField.text()).not.toContain('Discovered duplicate')
    await primaryField.get(`[data-test="app-select-option-${primary.id}"]`).trigger('click')
    await wrapper.get('[data-testid="model-configuration-add-model"]').trigger('click')
    const auxiliaryField = wrapper.get('[data-testid="model-configuration-aux-select-0"]')
    await auxiliaryField.get('[data-test="app-select-trigger"]').trigger('click')
    await auxiliaryField.get(`[data-test="app-select-option-${auxiliary.id}"]`).trigger('click')
    const roleField = wrapper.get('[data-testid="model-configuration-role-fast"]')
    await roleField.get('[data-test="app-select-trigger"]').trigger('click')
    for (const id of [primary.id, auxiliary.id, unused.id]) expect(roleField.find(`[data-test="app-select-option-${id}"]`).exists()).toBe(true)
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.updateConnection).toHaveBeenCalledWith(service.id, expect.objectContaining({ models: [savedModel, primary, auxiliary] }))
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ backend: 'opencode', binding: { kind: 'connection', connectionId: service.id, modelId: primary.id, auxiliaryModelIds: [auxiliary.id] } }))
    expect(services[0]!.models.some((model) => model.id === unused.id)).toBe(false)
    wrapper.unmount()
  })

  it.each(['api-error', 'network-error', 'malformed'])('keeps saved and manual models usable when discovery returns %s and supports refresh', async (failure) => {
    if (failure === 'network-error') mocks.connectionModels.mockRejectedValueOnce(new Error('Provider unavailable'))
    else mocks.connectionModels.mockResolvedValueOnce(failure === 'api-error' ? { models: [], error: 'Provider unavailable' } : { models: null })
    mocks.configurations.mockResolvedValue({ items: [configuration] })
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-testid="model-configuration-edit-saved"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="model-configuration-model-id"]').text()).toContain(service.models[0]!.id)
    expect(wrapper.get('[data-testid="model-configuration-save"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="model-configuration-model-catalog"]').text()).not.toBe('')
    await chooseModel(wrapper, 'manual/Exact.Model:custom', true)
    mocks.connectionModels.mockResolvedValueOnce({ models: [{ id: 'vendor/recovered', name: 'Recovered provider model', source: 'discovered' }] })
    await wrapper.get('[data-testid="model-configuration-refresh-models"]').trigger('click')
    await flushPromises()
    expect(mocks.connectionModels).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-testid="model-configuration-model-id"]').text()).toContain('manual/Exact.Model:custom')
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.updateConnection).toHaveBeenCalledWith(service.id, expect.objectContaining({ models: [...service.models, { id: 'manual/Exact.Model:custom', source: 'manual' }] }))
    expect(mocks.update).toHaveBeenCalledWith(configuration.id, expect.objectContaining({ binding: { kind: 'connection', connectionId: service.id, modelId: 'manual/Exact.Model:custom' } }))
    wrapper.unmount()
  })

  it('shows each API model brand in its options and selected primary and auxiliary slots', async () => {
    services = [{ ...service, providerId: 'tokenhub', models: [] }]
    const initialKimiAsset = theme.value === 'dark' ? kimiDarkLogo : kimiLogo
    const brands = [
      { id: 'gpt-5.5', name: 'GPT 5.5', vendor: 'openai', asset: openaiLogo },
      { id: 'kimi-k3.1-pro', name: 'Kimi K3.1 Pro', vendor: 'kimi', asset: initialKimiAsset },
      { id: 'glm-5.1', name: 'GLM 5.1', vendor: 'zhipu', asset: zhipuLogo },
      { id: 'hy3-pro', name: 'HY 3 Pro', vendor: 'hunyuan', asset: hunyuanLogo },
    ]
    mocks.connectionModels.mockResolvedValue({ models: brands.map(({ id, name }) => ({ id, name, source: 'discovered' })) })
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    const field = wrapper.get('[data-testid="model-configuration-model-id"]')
    await field.get('[data-test="app-select-trigger"]').trigger('click')
    for (const brand of brands) {
      const option = field.get(`[data-test="app-select-option-${brand.id}"]`)
      expect(option.get(`[data-vendor-mark="${brand.vendor}"] img`).attributes('src')).toBe(brand.asset)
    }
    const previousTheme = theme.value
    try {
      theme.value = 'dark'
      await flushPromises()
      expect(field.get('[data-vendor-mark="openai"] img').classes()).toContain('invert')
      expect(field.get('[data-vendor-mark="kimi"] img').attributes('src')).toBe(kimiDarkLogo)
      expect(field.get('[data-vendor-mark="kimi"] img').classes()).not.toContain('invert')
      for (const vendor of ['zhipu', 'hunyuan']) {
        const mark = field.get(`[data-vendor-mark="${vendor}"]`)
        expect(mark.classes()).toContain('vendor-mark--color')
        expect(mark.get('img').classes()).not.toContain('invert')
      }
    } finally {
      theme.value = previousTheme
    }
    await field.get('[data-test="app-select-option-gpt-5.5"]').trigger('click')
    expect(field.get('[data-test="app-select-trigger"] [data-vendor-mark="openai"] img').attributes('src')).toBe(openaiLogo)
    await wrapper.get('[data-testid="model-configuration-add-model"]').trigger('click')
    const auxiliary = wrapper.get('[data-testid="model-configuration-aux-select-0"]')
    expect(auxiliary.get('[data-test="app-select-trigger"] [data-vendor-mark="kimi"] img').attributes('src')).toBe(initialKimiAsset)
    await chooseModel(wrapper, 'glm-5.private', true)
    expect(field.get('[data-test="app-select-trigger"] [data-vendor-mark="zhipu"] img').attributes('src')).toBe(zhipuLogo)
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    expect(mocks.create).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('marks official OpenCode auxiliary models by family with an official provider logo fallback', async () => {
    mocks.capabilities.mockResolvedValue({ items: [{ toolId: 'opencode', modes: ['platform'], adapterState: 'existing' }] })
    mocks.credentials.mockResolvedValue({ items: [{ id: 'zen', type: 'ai', name: 'OpenCode', purpose: 'tool_auth', toolId: 'opencode', configured: true }] })
    mocks.cliModels.mockResolvedValue([
      { id: 'opencode/gemini-3.5-flash', name: 'Gemini 3.5 Flash', vendor: 'opencode' },
      { id: 'opencode/claude-sonnet-5', name: 'Claude Sonnet 5', vendor: 'opencode' },
      { id: 'opencode/custom-private', name: 'Private model', vendor: 'opencode' },
    ])
    const wrapper = setup()
    await openStepTwo(wrapper, 'opencode')
    await chooseCredential(wrapper, 'platform:zen')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="model-configuration-add-model"]').trigger('click')
    const auxiliary = wrapper.get('[data-testid="model-configuration-aux-select-0"]')
    await auxiliary.get('[data-test="app-select-trigger"]').trigger('click')
    const gemini = auxiliary.get('[data-test="app-select-option-opencode/gemini-3.5-flash"]')
    const claude = auxiliary.get('[data-test="app-select-option-opencode/claude-sonnet-5"]')
    const providerFallback = auxiliary.get('[data-test="app-select-option-opencode/custom-private"]')
    expect(gemini.get('[data-vendor-mark="google"] img').attributes('src')).toBe(geminiLogo)
    expect(gemini.get('[data-vendor-mark="google"]').classes()).toContain('vendor-mark--color')
    expect(gemini.get('[data-vendor-mark="google"] img').classes()).not.toContain('invert')
    expect(claude.get('[data-vendor-mark="anthropic"] img').attributes('src')).toBe(claudeLogo)
    expect(claude.find('[data-vendor-mark="google"]').exists()).toBe(false)
    expect(providerFallback.get('[data-vendor-mark="opencode"] img').attributes('src')).toBe(opencodeLogo)
    expect(providerFallback.find('[data-vendor-mark="google"]').exists()).toBe(false)
    expect(providerFallback.find('[data-vendor-mark="anthropic"]').exists()).toBe(false)
    await gemini.trigger('click')
    const selected = auxiliary.get('[data-test="app-select-trigger"] [data-vendor-mark="google"] img')
    expect(selected.attributes('src')).toBe(geminiLogo)
    expect(selected.classes()).not.toContain('invert')
    wrapper.unmount()
  })

  it.each(['switch', 'back', 'close'])('discards a delayed provider catalog after %s and loads the current model step', async (action) => {
    const otherService = { ...structuredClone(service), id: 'other-service', name: 'Other API account' }
    services.push(otherService)
    let resolveStale!: (value: { models: ModelConnection['models'] }) => void
    const pending = new Promise<{ models: ModelConnection['models'] }>((resolve) => { resolveStale = resolve })
    mocks.connectionModels.mockReturnValueOnce(pending).mockResolvedValue({ models: [{ id: 'current/model', name: 'Current provider model', source: 'discovered' }] })
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    expect(mocks.connectionModels).toHaveBeenCalledTimes(1)
    if (action === 'close') {
      await wrapper.get('button[aria-label="Close"]').trigger('click')
      await openConnectionModelStep(wrapper)
    } else {
      await wrapper.get('[data-testid="model-configuration-back"]').trigger('click')
      if (action === 'switch') await chooseCredential(wrapper, `connection:${otherService.id}`)
      await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    }
    await flushPromises()
    resolveStale({ models: [{ id: 'stale/model', name: 'Stale provider model', source: 'discovered' }] })
    await flushPromises()
    const field = wrapper.get('[data-testid="model-configuration-model-id"]')
    await field.get('[data-test="app-select-trigger"]').trigger('click')
    expect(field.text()).toContain('Current provider model')
    expect(field.text()).not.toContain('Stale provider model')
    expect(mocks.connectionModels.mock.calls.map(([id]) => id)).toEqual([service.id, action === 'switch' ? otherService.id : service.id])
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    expect(mocks.create).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('reuses a connection and its original model ID without rewriting the API service', async () => {
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    await chooseModel(wrapper, service.models[0]!.id)
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith({ name: 'Team configuration', backend: 'opencode', binding: { kind: 'connection', connectionId: service.id, modelId: 'vendor/original/model' }, state: 'active' })
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    expect(mocks.create.mock.calls[0]![0]).not.toHaveProperty('credentialId')
    expect(mocks.create.mock.calls[0]![0]).not.toHaveProperty('value')
    wrapper.unmount()
  })

  it('selects a native route with the searchable control and preserves its route ID', async () => {
    services[0] = { ...services[0]!, protocol: 'anthropic-messages', baseUrl: 'https://api.deepseek.com/anthropic', auth: { scheme: 'x-api-key' } }
    mocks.capabilities.mockResolvedValue({ items: [{ toolId: 'deepseek_harness', modes: ['connection'], protocols: ['anthropic-messages'], routeProfiles: ['deepseek-native'], adapterState: 'existing' }] })
    const wrapper = setup()
    await openConnectionModelStep(wrapper, 'deepseek_harness')
    const route = wrapper.get('[data-testid="model-configuration-route-profile"]')
    await route.get('[data-test="app-select-trigger"]').trigger('click')
    await route.get('[data-test="app-select-search"]').setValue('DeepSeek')
    await route.get('[data-test="app-select-option-deepseek-native"]').trigger('click')
    await chooseModel(wrapper, service.models[0]!.id)
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ backend: 'deepseek_harness', binding: expect.objectContaining({ toolSettings: { routeProfile: 'deepseek-native' } }) }))
    wrapper.unmount()
  })

  it('adds manual main and role models to the latest catalog and retries a failed save without duplicates', async () => {
    mocks.create.mockRejectedValueOnce(new Error('configuration service unavailable'))
    const wrapper = setup()
    await openConnectionModelStep(wrapper, 'codebuddy')
    await chooseModel(wrapper, 'new/vendor/raw/model', true)
    await chooseRole(wrapper, 'fast/raw/model')
    const concurrentModel = { id: 'concurrent/model', source: 'manual' as const, maxTokens: 2048 }
    services = [{ ...service, baseUrl: 'https://latest.example.test/v1', models: [...service.models, concurrentModel] }]
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.updateConnection).toHaveBeenCalledWith(service.id, expect.objectContaining({ baseUrl: 'https://latest.example.test/v1', service: service.service, models: [...service.models, concurrentModel, { id: 'new/vendor/raw/model', source: 'manual' }, { id: 'fast/raw/model', source: 'manual' }] }))
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ backend: 'codebuddy', binding: { kind: 'connection', connectionId: service.id, modelId: 'new/vendor/raw/model', toolSettings: { modelRoles: { fast: 'fast/raw/model' } } } }))
    expect(wrapper.get('[data-testid="model-configuration-error"]').text()).toContain('configuration service unavailable')
    expect(wrapper.get('[data-testid="model-configuration-model-id"] [data-test="app-select-trigger"]').text()).toContain('new/vendor/raw/model')
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.updateConnection).toHaveBeenCalledTimes(1)
    expect(mocks.create).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-testid="model-configuration-form"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('does not save a configuration if its service disappeared while the form was open', async () => {
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    await chooseModel(wrapper, service.models[0]!.id)
    services = []
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="model-configuration-error"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="model-configuration-form"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps the model and role draft when going back to the same credential, and clears them for a different service', async () => {
    services.push({ ...structuredClone(service), id: 'other-service', name: 'Other compatible API', models: [{ id: 'other/model', source: 'manual' }] })
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    await chooseModel(wrapper, 'draft/vendor/model', true)
    await chooseRole(wrapper, 'fast/draft/model')
    await wrapper.get('[data-testid="model-configuration-back"]').trigger('click')
    expect(wrapper.get('[data-testid="model-configuration-form"]').attributes('data-step')).toBe('2')
    await chooseCredential(wrapper, `connection:${service.id}`)
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    expect(wrapper.get('[data-testid="model-configuration-model-id"] [data-test="app-select-trigger"]').text()).toContain('draft/vendor/model')
    expect(wrapper.get('[data-testid="model-configuration-role-fast"] [data-test="app-select-trigger"]').text()).toContain('fast/draft/model')
    await wrapper.get('[data-testid="model-configuration-back"]').trigger('click')
    await chooseCredential(wrapper, 'connection:other-service')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    expect(wrapper.get('[data-testid="model-configuration-model-id"] [data-test="app-select-trigger"]').text()).not.toContain('draft/vendor/model')
    expect(wrapper.get('[data-testid="model-configuration-role-fast"] [data-test="app-select-trigger"]').text()).toContain('Same as main model')
    expect(wrapper.get('[data-testid="model-configuration-save"]').attributes('disabled')).toBeDefined()
    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('requires a new credential after changing coding tools and saves no stale connection or role overrides', async () => {
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    await chooseModel(wrapper, 'draft/vendor/model', true)
    await chooseRole(wrapper, 'fast/draft/model')
    await wrapper.get('[data-testid="model-configuration-back"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-back"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-backend-cursor"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    expect(wrapper.get('[data-testid="model-configuration-next"]').attributes('disabled')).toBeDefined()
    await chooseCredential(wrapper, `platform:${platformKey.id}`)
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    expect(wrapper.find('[data-testid="model-configuration-model-id"]').exists()).toBe(false)
    await wrapper.get('[data-testid="platform-model"]').setValue('platform-model-v1')
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith({ name: 'Team configuration', backend: 'cursor', binding: { kind: 'platform', credentialId: platformKey.id, modelId: 'platform-model-v1' }, state: 'active' })
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('starts a fresh draft after cancelling without saving a model or modifying the service', async () => {
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    await chooseModel(wrapper, 'cancelled/raw/model', true)
    await wrapper.get('button[aria-label="Close"]').trigger('click')
    expect(wrapper.find('[data-testid="model-configuration-form"]').exists()).toBe(false)
    await wrapper.get('[data-testid="model-configuration-add"]').trigger('click')
    expect(wrapper.get('[data-testid="model-configuration-form"]').attributes('data-step')).toBe('1')
    expect(wrapper.get('[data-testid="model-configuration-name"]').element).toHaveProperty('value', '')
    expect(wrapper.get('[data-testid="model-configuration-backend-cursor"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[data-testid="model-configuration-next"]').attributes('disabled')).toBeDefined()
    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('resumes an edited draft after credential creation is cancelled without losing its name, binding or editing identity', async () => {
    mocks.configurations.mockResolvedValue({ items: [configuration] })
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-testid="model-configuration-edit-saved"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-name"]').setValue('Unsaved edited name')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-add-credential"]').trigger('click')
    expect(wrapper.emitted('manageCredentials')).toEqual([['opencode']])
    await wrapper.vm.resumeWithCredential()
    expect(wrapper.get('[data-testid="model-configuration-form"]').attributes('data-step')).toBe('2')
    expect(wrapper.getComponent('[data-testid="model-configuration-credential-choice"]').props('modelValue')).toBe(`connection:${service.id}`)
    await wrapper.get('[data-testid="model-configuration-back"]').trigger('click')
    expect(wrapper.get('[data-testid="model-configuration-name"]').element).toHaveProperty('value', 'Unsaved edited name')
    expect(wrapper.get('[data-testid="model-configuration-backend-opencode"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    expect(wrapper.get('[data-testid="model-configuration-model-id"] [data-test="app-select-trigger"]').text()).toContain(service.models[0]!.id)
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(configuration.id, { name: 'Unsaved edited name', backend: configuration.backend, binding: configuration.binding, state: 'active' })
    expect(mocks.create).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps inline editing active throughout a delayed credential resume and post-save refresh', async () => {
    const wrapper = setup({ inline: true })
    await openStepTwo(wrapper)
    await wrapper.get('[data-testid="model-configuration-add-credential"]').trigger('click')
    let finishRefresh!: (value: { items: CredentialItem[] }) => void
    mocks.credentials.mockImplementationOnce(() => new Promise((resolve) => { finishRefresh = resolve }))
    const resume = wrapper.vm.resumeWithCredential(platformKey.id)
    await flushPromises()
    expect(wrapper.emitted('editing')).toEqual([[true]])
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    finishRefresh({ items: [platformKey] })
    await resume
    await flushPromises()
    expect(wrapper.get('[data-testid="model-configuration-form"]').attributes('data-step')).toBe('2')
    expect(wrapper.emitted('editing')).toEqual([[true]])
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    mocks.credentials.mockImplementationOnce(() => new Promise((resolve) => { finishRefresh = resolve }))
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('editing')).toEqual([[true]])
    finishRefresh({ items: [platformKey] })
    await flushPromises()
    expect(wrapper.emitted('editing')).toEqual([[true], [false]])
    expect(wrapper.emitted('saved')?.[0]?.[0]).toMatchObject({ id: 'created', name: 'Team configuration' })
    wrapper.unmount()
  })

  it('keeps the coding tool fixed while editing an existing configuration', async () => {
    mocks.configurations.mockResolvedValue({ items: [configuration] })
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-testid="model-configuration-edit-saved"]').trigger('click')
    expect(wrapper.get('[data-testid="model-configuration-backend-opencode"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="model-configuration-backend-opencode"]').attributes('aria-checked')).toBe('true')
    await wrapper.get('[data-testid="model-configuration-name"]').setValue('Edited model configuration')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(configuration.id, { name: 'Edited model configuration', backend: 'opencode', binding: configuration.binding, state: 'active' })
    expect(mocks.create).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('disables or deletes only the saved combination and leaves its reusable service intact', async () => {
    mocks.configurations.mockResolvedValue({ items: [configuration] })
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const wrapper = setup()
    await flushPromises()
    const row = wrapper.get('[data-testid="model-configuration-saved"]')
    expect(row.text()).toContain('OpenCode')
    expect(row.get('h4').text()).toBe(configuration.name)
    expect(row.text()).toContain('vendor/original/model')
    expect(row.find('input, textarea').exists()).toBe(false)
    const openActions = async () => {
      const more = wrapper.get('[data-testid="model-configuration-more-saved"]')
      vi.spyOn(more.element, 'getBoundingClientRect').mockReturnValue(new DOMRect(180, 20, 28, 28))
      await more.trigger('click')
      await flushPromises()
    }
    expect(wrapper.get('[data-testid="model-configuration-toggle-saved"]').attributes('role')).toBe('switch')
    await wrapper.get('[data-testid="model-configuration-toggle-saved"]').trigger('click')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(configuration.id, { name: configuration.name, backend: configuration.backend, binding: configuration.binding, state: 'disabled' })
    await openActions()
    await wrapper.get('[data-testid="model-configuration-delete-saved"]').trigger('click')
    expect(mocks.remove).not.toHaveBeenCalled()
    confirm.mockReturnValue(true)
    await openActions()
    await wrapper.get('[data-testid="model-configuration-delete-saved"]').trigger('click')
    await flushPromises()
    expect(mocks.remove).toHaveBeenCalledWith(configuration.id)
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    confirm.mockRestore()
    wrapper.unmount()
  })

  it('adds and removes an auxiliary model and rejects a duplicate of the primary', async () => {
    services = [{ ...service, models: [...service.models, { id: 'vendor/aux', source: 'manual' }] }]
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    await chooseModel(wrapper, service.models[0]!.id)
    expect(wrapper.text()).toContain('Primary model')
    expect(wrapper.text()).not.toContain('Set as primary')
    expect(wrapper.text()).not.toContain('Restore automatic')
    await wrapper.get('[data-testid="model-configuration-add-model"]').trigger('click')
    expect(wrapper.get('[data-testid="model-configuration-aux-select-0"] [data-test="app-select-trigger"]').text()).toContain('vendor/aux')
    expect(wrapper.get('[data-testid="model-configuration-add-model"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="model-configuration-aux-remove-0"]').trigger('click')
    expect(wrapper.find('[data-testid="model-configuration-aux-0"]').exists()).toBe(false)
    await wrapper.get('[data-testid="model-configuration-add-model"]').trigger('click')
    const aux = wrapper.get('[data-testid="model-configuration-aux-select-0"]')
    await aux.get('[data-test="app-select-trigger"]').trigger('click')
    await aux.get('[data-test="app-select-search"]').setValue(service.models[0]!.id)
    await aux.get('[data-test="app-select-custom"]').trigger('click')
    expect(wrapper.text()).toContain('Each model can only be selected once')
    expect(wrapper.get('[data-testid="model-configuration-save"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="model-configuration-aux-remove-0"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-add-model"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({
      binding: { kind: 'connection', connectionId: service.id, modelId: 'vendor/original/model', auxiliaryModelIds: ['vendor/aux'] },
    }))
    wrapper.unmount()
  })

  it('clears auxiliary models when the credential changes and does not move a cleared primary into the list', async () => {
    services = [
      { ...service, models: [...service.models, { id: 'vendor/aux', source: 'manual' }] },
      { ...structuredClone(service), id: 'other-service', name: 'Other compatible API', models: [{ id: 'other/model', source: 'manual' }] },
    ]
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    await chooseModel(wrapper, service.models[0]!.id)
    await wrapper.get('[data-testid="model-configuration-add-model"]').trigger('click')
    await wrapper.get('[data-testid="model-configuration-back"]').trigger('click')
    await chooseCredential(wrapper, 'connection:other-service')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    expect(wrapper.find('[data-testid="model-configuration-aux-0"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="model-configuration-model-id"] [data-test="app-select-trigger"]').text()).not.toContain('vendor/original/model')
    wrapper.unmount()
  })

  it('selects automatic in the primary slot without moving the previous model into auxiliary', async () => {
    mocks.cliModels.mockResolvedValue([
      { id: 'platform-model-v1', name: 'Platform model', vendor: 'cursor' },
      { id: 'platform-model-v2', name: 'Second', vendor: 'xai' },
    ])
    const wrapper = setup()
    await openStepTwo(wrapper)
    await chooseCredential(wrapper, `platform:${platformKey.id}`)
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    const picker = wrapper.get('[data-testid="platform-model"]')
    expect(picker.attributes('data-hide-reset')).toBe('true')
    await picker.setValue('platform-model-v1')
    await picker.setValue('')
    expect(wrapper.find('[data-testid="model-configuration-aux-0"]').exists()).toBe(false)
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith({ name: 'Team configuration', backend: 'cursor', binding: { kind: 'platform', credentialId: platformKey.id }, state: 'active' })
    wrapper.unmount()
  })

  it('shows the primary model and the auxiliary count without role marks in the list', async () => {
    mocks.configurations.mockResolvedValue({ items: [{ ...configuration, binding: { ...configuration.binding, auxiliaryModelIds: ['vendor/a', 'vendor/b'] } }] })
    const wrapper = setup()
    await flushPromises()
    const row = wrapper.get('[data-testid="model-configuration-saved"]')
    expect(row.text()).toContain('vendor/original/model')
    expect(row.text()).toContain('2 more auxiliary models')
    expect(row.text()).not.toContain('Primary model')
    wrapper.unmount()
  })

  it('keeps an empty name on step one and does not hover the disabled next button', async () => {
    vi.stubGlobal(
      'matchMedia',
      vi.fn((query: string) => ({
        matches: query.includes('hover: hover') && query.includes('pointer: fine'),
        media: query,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      })),
    )
    const wrapper = setup({ hoverInk: true })
    try {
      await flushPromises()
      await wrapper.get('[data-testid="model-configuration-add"]').trigger('click')
      const next = wrapper.get('[data-testid="model-configuration-next"]')
      const cancel = wrapper.get('[data-testid="model-configuration-back"]')
      expect(next.attributes('disabled')).toBeDefined()
      expect(next.classes()).toContain('text-white')
      expect(next.classes()).toContain('bg-accent')
      expect(next.element.style.getPropertyValue('--hover-ink-color')).toBe('')
      expect(next.findComponent({ name: 'Icon' }).props()).toMatchObject({ name: 'arrow-right', size: 16 })
      expect(cancel.findComponent({ name: 'Icon' }).props()).toMatchObject({ name: 'close', size: 16 })
      expect(cancel.element.style.getPropertyValue('--hover-ink-color')).toBe('')
      expect(cancel.classes().join(' ')).not.toMatch(/hover:bg-/)
      expect(cancel.classes()).toContain('enabled:hover:text-txt')

      next.element.dispatchEvent(new PointerEvent('pointerenter', { clientX: 8, clientY: 8, pointerType: 'mouse', bubbles: true }))
      expect(next.element.classList.contains('is-hover-ink')).toBe(false)
      expect(next.element.classList.contains('is-leave-ink')).toBe(false)
      expect(next.element.style.getPropertyValue('--hover-ink-color')).toBe('')
      cancel.element.dispatchEvent(new PointerEvent('pointerenter', { clientX: 4, clientY: 4, pointerType: 'mouse', bubbles: true }))
      expect(cancel.element.classList.contains('is-hover-ink')).toBe(false)
      expect(cancel.element.style.getPropertyValue('--hover-ink-color')).toBe('')

      await next.trigger('click')
      expect(wrapper.get('[data-testid="model-configuration-form"]').attributes('data-step')).toBe('1')
      await wrapper.get('[data-testid="model-configuration-name"]').setValue('   ')
      expect(wrapper.get('[data-testid="model-configuration-next"]').attributes('disabled')).toBeDefined()
      await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
      expect(wrapper.get('[data-testid="model-configuration-form"]').attributes('data-step')).toBe('1')

      await wrapper.get('[data-testid="model-configuration-name"]').setValue('Named model')
      const ready = wrapper.get('[data-testid="model-configuration-next"]')
      expect(ready.attributes('disabled')).toBeUndefined()
      expect(ready.classes()).toContain('text-white')
      const ink = ready.element.style.getPropertyValue('--hover-ink-color')
      expect(ink).toBe('rgb(var(--c-accent-hover))')
      expect(ink).not.toContain('--c-elevated')
      Object.defineProperty(ready.element, 'getBoundingClientRect', {
        configurable: true,
        value: () => ({ left: 0, top: 0, width: 96, height: 36, right: 96, bottom: 36, x: 0, y: 0, toJSON: () => ({}) }),
      })
      ready.element.dispatchEvent(new PointerEvent('pointerenter', { clientX: 10, clientY: 10, pointerType: 'mouse', bubbles: true }))
      expect(ready.element.classList.contains('is-hover-ink')).toBe(true)
      expect(ready.classes()).toContain('text-white')

      await wrapper.get('[data-testid="model-configuration-name"]').setValue('  ')
      const blocked = wrapper.get('[data-testid="model-configuration-next"]')
      expect(blocked.attributes('disabled')).toBeDefined()
      expect(blocked.element.classList.contains('is-hover-ink')).toBe(false)
      expect(blocked.element.classList.contains('is-ink-suppressed')).toBe(true)
      expect(blocked.element.style.getPropertyValue('--hover-ink-color')).toBe('')
      expect(blocked.element.style.getPropertyValue('--ink-d')).toBe('0px')
      await blocked.trigger('click')
      expect(wrapper.get('[data-testid="model-configuration-form"]').attributes('data-step')).toBe('1')
    } finally {
      wrapper.unmount()
      vi.unstubAllGlobals()
    }
  })
})
