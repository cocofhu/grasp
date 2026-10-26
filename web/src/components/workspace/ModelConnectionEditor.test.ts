// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { CredentialItem, ModelConnection } from '@/lib/api/api'
import ModelConnectionEditor from './ModelConnectionEditor.vue'

const mocks = vi.hoisted(() => ({ list: vi.fn(), create: vi.fn(), update: vi.fn(), success: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: { listModelConnections: mocks.list, createModelConnection: mocks.create, updateModelConnection: mocks.update } }))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => ({ success: mocks.success }) }))
const key: CredentialItem = { id: 'key', name: 'Team API account', purpose: 'model_api', type: 'ai', issuer: 'deepseek', configured: true }
const service: ModelConnection = { id: 'existing', name: 'Saved account API', providerId: 'deepseek', protocol: 'openai-chat-completions', baseUrl: 'https://api.deepseek.com/v1', credentialId: key.id, auth: { scheme: 'bearer' }, models: [{ id: 'vendor/original/model', source: 'manual', contextWindow: 128000 }], state: 'active', revision: 1 }
function setup(account = key) { return mount(ModelConnectionEditor, { props: { credentials: [account] }, global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: {} } })], stubs: { Teleport: true, Icon: true } } }) }
async function open(wrapper: ReturnType<typeof setup>, id?: string) { await wrapper.vm.openForCredential(key.id, id); await flushPromises() }
async function save(wrapper: ReturnType<typeof setup>) { await wrapper.get('[data-testid="model-connection-form"]').trigger('submit'); await flushPromises() }
beforeEach(() => { vi.clearAllMocks(); mocks.list.mockResolvedValue({ items: [] }); mocks.create.mockResolvedValue({ id: 'created' }); mocks.update.mockResolvedValue(service) })

describe('API account address editor', () => {
  it('has no standalone list or create action and keeps the account fixed', async () => {
    const wrapper = setup(); await flushPromises()
    expect(wrapper.find('[data-testid="model-connections-manager"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="model-connection-add"]').exists()).toBe(false)
    expect(mocks.list).not.toHaveBeenCalled()
    await open(wrapper)
    expect(wrapper.get('[data-testid="api-account-identity"]').text()).toBe(key.name)
    expect(wrapper.find('[data-provider-logo="deepseek"]').exists()).toBe(true)
    for (const field of ['model-connection-provider', 'model-connection-name', 'model-connection-credential', 'model-connection-next']) expect(wrapper.find(`[data-testid="${field}"]`).exists()).toBe(false)
    expect(wrapper.get('[data-testid="model-connection-base-url"]').element).toHaveProperty('value', 'https://api.deepseek.com/v1')
    wrapper.unmount()
  })

  it('creates API settings only for the selected account without sending another secret', async () => {
    const wrapper = setup({ ...key, provider: 'custom', issuer: 'Team gateway' })
    await open(wrapper)
    await wrapper.get('[data-testid="model-connection-base-url"]').setValue('https://gateway.example.test/v1')
    wrapper.getComponent('[data-testid="model-connection-protocol"]').vm.$emit('update:modelValue', 'openai-responses')
    await flushPromises(); await save(wrapper)
    expect(mocks.create).toHaveBeenCalledWith({ name: key.name, providerId: 'custom', credentialId: key.id, baseUrl: 'https://gateway.example.test/v1', protocol: 'openai-responses', auth: { scheme: 'bearer' }, models: [], state: 'active' })
    expect(mocks.create.mock.calls[0]![0]).not.toHaveProperty('value')
    expect(mocks.update).not.toHaveBeenCalled()
    expect(wrapper.emitted('changed')).toHaveLength(1)
    wrapper.unmount()
  })

  it('preserves latest model metadata, disablement and account identity while updating Base', async () => {
    mocks.list.mockResolvedValue({ items: [service] })
    const wrapper = setup(); await open(wrapper, service.id)
    await wrapper.get('[data-testid="model-connection-base-url"]').setValue('https://updated.example.test/v1')
    const latest = { ...service, name: 'Renamed elsewhere', providerId: 'team-gateway', state: 'disabled', service: { region: 'eu', product: 'API' }, models: [...service.models, { id: 'another/raw/model', source: 'manual' }] }
    mocks.list.mockResolvedValue({ items: [latest] })
    await save(wrapper)
    expect(mocks.update).toHaveBeenCalledWith(service.id, expect.objectContaining({ name: latest.name, providerId: latest.providerId, credentialId: key.id, state: 'disabled', service: latest.service, models: latest.models, baseUrl: 'https://updated.example.test/v1' }))
    expect(mocks.create).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('loads an existing account configuration instead of creating a duplicate when the caller has no service list', async () => {
    mocks.list.mockResolvedValue({ items: [service] })
    const wrapper = setup(); await open(wrapper)
    await wrapper.get('[data-testid="model-connection-base-url"]').setValue('https://updated.example.test/v1')
    await save(wrapper)
    expect(mocks.update).toHaveBeenCalledWith(service.id, expect.objectContaining({ models: service.models }))
    expect(mocks.create).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('blocks saving after a failed load and retries the current account without creating another API configuration', async () => {
    mocks.list.mockRejectedValueOnce(new Error('API settings unavailable')).mockResolvedValue({ items: [service] })
    const wrapper = setup(); await open(wrapper)
    expect(wrapper.text()).toContain('API settings unavailable')
    expect(wrapper.get('[data-testid="model-connection-save"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="model-connection-form"]').exists()).toBe(false)
    await wrapper.get('[data-testid="api-account-address-retry"]').trigger('click'); await flushPromises()
    await save(wrapper)
    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.update).toHaveBeenCalledWith(service.id, expect.objectContaining({ credentialId: key.id }))
    wrapper.unmount()
  })

  it('keeps an edited Base after a failed save read and retries using the latest models', async () => {
    mocks.list.mockResolvedValue({ items: [service] })
    const wrapper = setup(); await open(wrapper)
    await wrapper.get('[data-testid="model-connection-base-url"]').setValue('https://updated.example.test/v1')
    mocks.list.mockRejectedValueOnce(new Error('refresh unavailable'))
    await save(wrapper)
    expect(wrapper.text()).toContain('refresh unavailable')
    expect(mocks.update).not.toHaveBeenCalled(); expect(mocks.create).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="model-connection-base-url"]').element).toHaveProperty('value', 'https://updated.example.test/v1')
    await save(wrapper)
    expect(mocks.update).toHaveBeenCalledWith(service.id, expect.objectContaining({ models: service.models, baseUrl: 'https://updated.example.test/v1' }))
    wrapper.unmount()
  })

  it('uses a configuration created while the editor was open and rejects a configuration moved to another account', async () => {
    const wrapper = setup(); await open(wrapper)
    mocks.list.mockResolvedValue({ items: [service] }); await save(wrapper)
    expect(mocks.update).toHaveBeenCalledWith(service.id, expect.objectContaining({ credentialId: key.id }))
    expect(mocks.create).not.toHaveBeenCalled()
    await open(wrapper, service.id)
    mocks.list.mockResolvedValue({ items: [{ ...service, credentialId: 'other-account' }] })
    mocks.update.mockClear(); await save(wrapper)
    expect(mocks.update).not.toHaveBeenCalled(); expect(mocks.create).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="model-connection-form"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('ignores an older account load after the dialog closes and another account opens', async () => {
    let finishOld!: (value: { items: ModelConnection[] }) => void
    let finishNew!: (value: { items: ModelConnection[] }) => void
    mocks.list.mockReturnValueOnce(new Promise(resolve => { finishOld = resolve })).mockReturnValueOnce(new Promise(resolve => { finishNew = resolve }))
    const wrapper = setup()
    const other = { ...key, id: 'other-key', name: 'Another API account' }
    await wrapper.setProps({ credentials: [key, other] })
    const first = wrapper.vm.openForCredential(key.id)
    await flushPromises()
    wrapper.getComponent({ name: 'AppModal' }).vm.$emit('close'); await flushPromises()
    const second = wrapper.vm.openForCredential(other.id)
    await flushPromises()
    finishOld({ items: [service] }); await first; await flushPromises()
    expect(wrapper.get('[data-testid="api-account-identity"]').text()).toBe(other.name)
    expect(wrapper.find('[data-testid="model-connection-form"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="model-connection-save"]').attributes('disabled')).toBeDefined()
    const current = { ...service, id: 'other-service', credentialId: other.id, baseUrl: 'https://other.example.test/v1' }
    finishNew({ items: [current] }); await second; await flushPromises()
    expect(wrapper.get('[data-testid="model-connection-base-url"]').element).toHaveProperty('value', current.baseUrl)
    mocks.list.mockResolvedValue({ items: [current] })
    await save(wrapper)
    expect(mocks.update).toHaveBeenCalledWith('other-service', expect.objectContaining({ credentialId: other.id, baseUrl: current.baseUrl }))
    wrapper.unmount()
  })

  it.each(['https://user@host/v1', 'https://user:secret@host/v1', 'file:///model-api', 'https://host/v1?token=secret', 'https://host/v1#fragment'])('rejects invalid Base: %s', async (baseUrl) => {
    const wrapper = setup(); await open(wrapper)
    await wrapper.get('[data-testid="model-connection-base-url"]').setValue(baseUrl)
    expect(wrapper.get('[data-testid="model-connection-save"]').attributes('disabled')).toBeDefined()
    await save(wrapper); expect(mocks.create).not.toHaveBeenCalled(); expect(mocks.update).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
