// @vitest-environment happy-dom
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { CredentialItem, ModelConfiguration, ModelConnection, ModelConnectionBody, ToolCapability } from '@/lib/api/api'
import { vHoverInk } from '@/lib/shared/hoverInkDirective'
import ModelConfigurationManager from './ModelConfigurationManager.vue'

const mocks = vi.hoisted(() => ({ configurations: vi.fn(), connections: vi.fn(), credentials: vi.fn(), capabilities: vi.fn(), create: vi.fn(), update: vi.fn(), remove: vi.fn(), updateConnection: vi.fn(), success: vi.fn(), cliModels: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: { listModelConfigurations: mocks.configurations, listModelConnections: mocks.connections, listCredentials: mocks.credentials, listToolCapabilities: mocks.capabilities, createModelConfiguration: mocks.create, updateModelConfiguration: mocks.update, deleteModelConfiguration: mocks.remove, updateModelConnection: mocks.updateConnection } }))
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

function setup(options: { hoverInk?: boolean } = {}) {
  return mount(ModelConfigurationManager, {
    global: {
      plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: {} } })],
      ...(options.hoverInk ? { directives: { 'hover-ink': vHoverInk } } : {}),
      stubs: {
        Teleport: true, Icon: true,
        CliModelSelect: defineComponent({
          props: { modelValue: String, backend: String, hideReset: Boolean, excludeIds: { type: Array, default: () => [] } }, emits: ['update:modelValue'],
          template: '<select data-testid="platform-model" :data-hide-reset="String(!!hideReset)" :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option value="">Tool default</option><option value="platform-model-v1">Platform model</option><option value="platform-model-v2">Second</option></select>',
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

async function chooseModel(wrapper: ReturnType<typeof setup>, value: string, custom = false) {
  const field = wrapper.get('[data-testid="model-configuration-model-id"]')
  await field.get('[data-test="app-select-trigger"]').trigger('click')
  await field.get('[data-test="app-select-search"]').setValue(value)
  await field.get(`[data-test="${custom ? 'app-select-custom' : `app-select-option-${value}`}"]`).trigger('click')
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
    await wrapper.get('[data-testid="model-configuration-save"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith({ name: 'Team configuration', backend: 'cursor', binding: { kind: 'platform', credentialId: platformKey.id, ...(modelId ? { modelId } : {}) }, state: 'active' })
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="model-configuration-form"]').exists()).toBe(false)
    expect(wrapper.emitted('changed')).toHaveLength(1)
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
    await wrapper.get('[data-testid="model-configuration-role-fast"]').setValue('fast/raw/model')
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
    await wrapper.get('[data-testid="model-configuration-role-fast"]').setValue('fast/draft/model')
    await wrapper.get('[data-testid="model-configuration-back"]').trigger('click')
    expect(wrapper.get('[data-testid="model-configuration-form"]').attributes('data-step')).toBe('2')
    await chooseCredential(wrapper, `connection:${service.id}`)
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    expect(wrapper.get('[data-testid="model-configuration-model-id"] [data-test="app-select-trigger"]').text()).toContain('draft/vendor/model')
    expect(wrapper.get('[data-testid="model-configuration-role-fast"]').element).toHaveProperty('value', 'fast/draft/model')
    await wrapper.get('[data-testid="model-configuration-back"]').trigger('click')
    await chooseCredential(wrapper, 'connection:other-service')
    await wrapper.get('[data-testid="model-configuration-next"]').trigger('click')
    expect(wrapper.get('[data-testid="model-configuration-model-id"] [data-test="app-select-trigger"]').text()).not.toContain('draft/vendor/model')
    expect(wrapper.get('[data-testid="model-configuration-role-fast"]').element).toHaveProperty('value', '')
    expect(wrapper.get('[data-testid="model-configuration-save"]').attributes('disabled')).toBeDefined()
    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.updateConnection).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('requires a new credential after changing coding tools and saves no stale connection or role overrides', async () => {
    const wrapper = setup()
    await openConnectionModelStep(wrapper)
    await chooseModel(wrapper, 'draft/vendor/model', true)
    await wrapper.get('[data-testid="model-configuration-role-fast"]').setValue('fast/draft/model')
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
