// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { ModelConnection, ModelConnectionBody } from '@/lib/api/api'
import ModelCatalogPanel from './ModelCatalogPanel.vue'

const mocks = vi.hoisted(() => ({ list: vi.fn(), update: vi.fn(), success: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: { listModelConnections: mocks.list, updateModelConnection: mocks.update } }))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => ({ success: mocks.success }) }))

const original: ModelConnection = {
  id: 'service-a', name: 'Team gateway', providerId: 'gateway', protocol: 'openai-responses',
  baseUrl: 'https://gateway.example/region/v1', credentialId: 'existing-key', auth: { scheme: 'bearer' },
  service: { region: 'regional', product: 'subscription' }, state: 'active', revision: 4,
  models: [{ id: 'vendor/original/model', name: 'Existing model', source: 'discovered', input: ['text'], contextWindow: 128000, maxTokens: 4096, capabilities: { toolCalls: true }, reasoningEfforts: ['low', 'high'] }],
}
let current: ModelConnection[]
function setup() {
  return mount(ModelCatalogPanel, { global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: {} } })], stubs: { Teleport: true, Icon: true, CredentialProviderLogo: true }, directives: { 'hover-ink': {} } } })
}
beforeEach(() => {
  vi.restoreAllMocks()
  vi.clearAllMocks()
  current = structuredClone([original])
  mocks.list.mockImplementation(async () => ({ items: structuredClone(current) }))
  mocks.update.mockImplementation(async (id: string, body: ModelConnectionBody) => {
    const saved = { ...current.find((row) => row.id === id), ...body, id, revision: 5 }
    current = current.map((row) => row.id === id ? saved : row)
    return structuredClone(saved)
  })
})

async function chooseSearchOption(wrapper: ReturnType<typeof setup>, testId: string, value: string) {
  const field = wrapper.get(`[data-testid="${testId}"]`)
  await field.get('[data-test="app-select-trigger"]').trigger('click')
  await field.get('[data-test="app-select-search"]').setValue(value)
  await field.get(`[data-test="app-select-option-${value}"]`).trigger('click')
}

describe('ModelCatalogPanel', () => {
  it('guides an empty catalog to service setup without requesting another API key', async () => {
    current = []
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.find('[data-testid="model-catalog-empty"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="model-catalog-add"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="model-catalog-add-service"]').trigger('click')
    expect(wrapper.emitted('add-service')).toHaveLength(1)
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
    expect(mocks.update).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('adds a raw model ID to the selected service and preserves its latest connection metadata', async () => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-testid="model-catalog-add"]').trigger('click')
    await wrapper.get('[data-testid="model-catalog-model-id"]').setValue('other-vendor/new/model')
    await wrapper.get('[data-testid="model-catalog-model-name"]').setValue('New model')
    // Service editing on the first tab must survive this model-only mutation.
    current[0] = { ...current[0]!, baseUrl: 'https://new.example/region/v1', credentialId: 'rotated-key', service: { region: 'new-region', product: 'new-plan' }, state: 'disabled' }
    await wrapper.get('[data-testid="model-catalog-form"]').trigger('submit')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith('service-a', {
      name: original.name, providerId: original.providerId, protocol: original.protocol,
      baseUrl: 'https://new.example/region/v1', credentialId: 'rotated-key', auth: original.auth,
      service: { region: 'new-region', product: 'new-plan' }, state: 'disabled',
      models: [...original.models, { id: 'other-vendor/new/model', name: 'New model', source: 'manual' }],
    })
    expect(wrapper.emitted('changed')).toHaveLength(1)
    expect(wrapper.find('[data-model-id="other-vendor/new/model"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="model-catalog-form"]').exists()).toBe(false)
    const body = mocks.update.mock.calls[0]![1]
    expect(body).not.toHaveProperty('value')
    expect(body).not.toHaveProperty('id')
    expect(body).not.toHaveProperty('revision')
    wrapper.unmount()
  })

  it('edits name and optional capabilities while retaining discovered model identity', async () => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-testid="model-catalog-edit"]').trigger('click')
    expect(wrapper.get('[data-testid="model-catalog-service"] [data-test="app-select-trigger"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="model-catalog-model-name"]').setValue('Renamed display')
    await wrapper.get('[data-testid="model-catalog-context-window"]').setValue('262144')
    await wrapper.get('[data-testid="model-catalog-max-tokens"]').setValue('8192')
    await chooseSearchOption(wrapper, 'model-catalog-input', 'text,image')
    await chooseSearchOption(wrapper, 'model-catalog-capability-vision', 'yes')
    await chooseSearchOption(wrapper, 'model-catalog-capability-reasoning', 'no')
    await wrapper.get('[data-testid="model-catalog-reasoning-efforts"]').setValue('low, high, high')
    await wrapper.get('[data-testid="model-catalog-form"]').trigger('submit')
    await flushPromises()
    expect(mocks.update.mock.calls[0]![1].models).toEqual([{ ...original.models[0], name: 'Renamed display', contextWindow: 262144, maxTokens: 8192, input: ['text', 'image'], capabilities: { toolCalls: true, vision: true, reasoning: false } }])
    wrapper.unmount()
  })

  it('uses searchable controls and preserves unset, false and true capability values', async () => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-testid="model-catalog-edit"]').trigger('click')
    expect(wrapper.find('select').exists()).toBe(false)
    await chooseSearchOption(wrapper, 'model-catalog-input', '')
    await chooseSearchOption(wrapper, 'model-catalog-capability-toolCalls', 'no')
    await chooseSearchOption(wrapper, 'model-catalog-capability-vision', 'yes')
    await chooseSearchOption(wrapper, 'model-catalog-capability-reasoning', '')
    await wrapper.get('[data-testid="model-catalog-form"]').trigger('submit')
    await flushPromises()
    const model = mocks.update.mock.calls[0]![1].models[0]
    expect(model).not.toHaveProperty('input')
    expect(model.capabilities).toEqual({ toolCalls: false, vision: true })
    expect(model.capabilities).not.toHaveProperty('reasoning')
    wrapper.unmount()
  })

  it('requires explicit confirmation for renaming an ID and deletion without rewriting Agent bindings', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-testid="model-catalog-edit"]').trigger('click')
    await wrapper.get('[data-testid="model-catalog-model-id"]').setValue('vendor/replacement')
    await wrapper.get('[data-testid="model-catalog-form"]').trigger('submit')
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining('old ID unavailable'))
    expect(mocks.update).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="model-catalog-cancel"]').trigger('click')
    await wrapper.get('[data-testid="model-catalog-delete"]').trigger('click')
    expect(confirm).toHaveBeenLastCalledWith(expect.stringContaining('tool defaults referencing it will become unavailable'))
    expect(mocks.update).not.toHaveBeenCalled()
    confirm.mockReturnValue(true)
    await wrapper.get('[data-testid="model-catalog-delete"]').trigger('click')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith('service-a', expect.objectContaining({ models: [] }))
    expect(wrapper.find('[data-testid="model-catalog-row"]').exists()).toBe(false)
    expect(wrapper.emitted('changed')).toHaveLength(1)
    wrapper.unmount()
  })

  it('selects an existing service without copying models across services', async () => {
    current.push({ ...structuredClone(original), id: 'service-b', name: 'Another account', credentialId: 'other-key', models: [] })
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-testid="model-catalog-add"]').trigger('click')
    await chooseSearchOption(wrapper, 'model-catalog-service', 'service-b')
    await wrapper.get('[data-testid="model-catalog-model-id"]').setValue('vendor/original/model')
    await wrapper.get('[data-testid="model-catalog-form"]').trigger('submit')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith('service-b', expect.objectContaining({ credentialId: 'other-key', models: [{ id: 'vendor/original/model', source: 'manual' }] }))
    expect(current[0]!.models).toEqual(original.models)
    wrapper.unmount()
  })

  it('rejects duplicate model IDs and zero token limits before writing a service', async () => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-testid="model-catalog-add"]').trigger('click')
    await wrapper.get('[data-testid="model-catalog-model-id"]').setValue('vendor/original/model')
    await wrapper.get('[data-testid="model-catalog-form"]').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[data-testid="model-catalog-error"]').text()).toContain('already has that model ID')
    expect(mocks.update).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="model-catalog-model-id"]').setValue('new-model')
    await wrapper.get('[data-testid="model-catalog-max-tokens"]').setValue('0')
    expect(wrapper.get('[data-testid="model-catalog-save"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="model-catalog-form"]').trigger('submit')
    expect(wrapper.get('[data-testid="model-catalog-error"]').text()).toContain('positive whole numbers')
    expect(mocks.update).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps an unsaved model available for retry when the API write fails', async () => {
    mocks.update.mockRejectedValueOnce(new Error('service update failed'))
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[data-testid="model-catalog-add"]').trigger('click')
    await wrapper.get('[data-testid="model-catalog-model-id"]').setValue('new/model')
    await wrapper.get('[data-testid="model-catalog-form"]').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[data-testid="model-catalog-error"]').text()).toContain('service update failed')
    expect((wrapper.get('[data-testid="model-catalog-model-id"]').element as HTMLInputElement).value).toBe('new/model')
    expect(wrapper.emitted('changed')).toBeUndefined()
    await wrapper.get('[data-testid="model-catalog-form"]').trigger('submit')
    await flushPromises()
    expect(wrapper.emitted('changed')).toHaveLength(1)
    expect(current[0]!.models.filter((model) => model.id === 'new/model')).toHaveLength(1)
    wrapper.unmount()
  })

  it('displays a fetch failure and retries without inventing services', async () => {
    mocks.list.mockRejectedValueOnce(new Error('catalog unavailable'))
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.text()).toContain('catalog unavailable')
    expect(wrapper.find('[data-testid="model-catalog-empty"]').exists()).toBe(false)
    await wrapper.get('[data-testid="model-catalog-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="model-catalog-row"]').exists()).toBe(true)
    expect(mocks.update).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
