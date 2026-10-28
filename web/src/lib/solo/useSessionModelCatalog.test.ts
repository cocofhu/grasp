// @vitest-environment happy-dom
import { effectScope, ref } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
const mocks = vi.hoisted(() => ({ listAgents: vi.fn(), listModelConfigurations: vi.fn(), listModelConnections: vi.fn(), listCredentials: vi.fn(), getSharedAgentConfig: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: mocks }))
vi.mock('@/lib/agent/cliCatalog', () => ({ loadCliModels: vi.fn(async () => []), cliCatalogFailed: () => false }))
import { useSessionModelCatalog } from './useSessionModelCatalog'
import { invalidateModelCatalog } from '@/lib/api/modelCatalogEvents'
const a = { id: 'a', name: 'First', backend: 'codex', state: 'active', binding: { kind: 'connection', connectionId: 'conn', modelId: 'same' } }
const b = { id: 'b', name: 'Second', backend: 'opencode', state: 'active', binding: { kind: 'connection', connectionId: 'conn', modelId: 'same' } }
beforeEach(() => {
  vi.clearAllMocks()
  mocks.listAgents.mockResolvedValue([{ name: 'Coder', modelBinding: { kind: 'configuration', configurationId: 'a', configurationIds: ['a', 'b'] } }])
  mocks.listModelConfigurations.mockResolvedValue({ items: [a, b] })
  mocks.listCredentials.mockResolvedValue({ items: [{ id: 'key', configured: true, purpose: 'model_api' }] })
  mocks.listModelConnections.mockResolvedValue({ items: [{ id: 'conn', credentialId: 'key', name: 'Team account', state: 'active', models: [{ id: 'same', name: 'Real model' }] }] })
})
function setup() {
  const scope = effectScope()
  const catalog = scope.run(() => useSessionModelCatalog(ref('Coder')))!
  return { scope, catalog }
}
describe('reactive session group catalog', () => {
  it('refreshes an open selector on saves and validates group membership', async () => {
    const { scope, catalog } = setup(); await flushPromises()
    expect(catalog.choices.value).toHaveLength(2)
    expect(catalog.modelInConfiguration('same', 'b')).toBe(true)
    mocks.listModelConfigurations.mockResolvedValue({ items: [a, { ...b, state: 'disabled' }] })
    invalidateModelCatalog(); await flushPromises()
    expect(catalog.choices.value).toHaveLength(1)
    expect(catalog.modelInConfiguration('same', 'b')).toBe(false)
    expect(catalog.modelInConfiguration('same', 'not-bound')).toBe(false)
    scope.stop()
  })
  it('preserves the last directory after a transient error and ignores older responses', async () => {
    const { scope, catalog } = setup(); await flushPromises()
    mocks.listAgents.mockRejectedValueOnce(new Error('offline'))
    invalidateModelCatalog(); await flushPromises()
    expect(catalog.blocked.value).toBe(true)
    expect(catalog.choices.value).toHaveLength(2)
    expect(catalog.modelInConfiguration('same', 'b')).toBe(null)
    let old!: (value: { items: typeof a[] }) => void
    mocks.listModelConfigurations.mockImplementationOnce(() => new Promise(resolve => { old = resolve }))
    invalidateModelCatalog(); await flushPromises()
    mocks.listModelConfigurations.mockResolvedValue({ items: [{ ...a, name: 'Newest' }, b] })
    invalidateModelCatalog(); await flushPromises()
    old({ items: [{ ...a, name: 'Stale' }] }); await flushPromises()
    expect(catalog.choices.value[0]?.configurationName).toBe('Newest')
    scope.stop()
  })
  it('refreshes credential availability after a successful save notification', async () => {
    const { scope, catalog } = setup(); await flushPromises()
    expect(catalog.choices.value.every(choice => !choice.unavailable)).toBe(true)
    mocks.listCredentials.mockResolvedValue({ items: [{ id: 'key', configured: false, purpose: 'model_api' }] })
    invalidateModelCatalog(); await flushPromises()
    expect(catalog.choices.value.every(choice => choice.unavailable)).toBe(true)
    expect(catalog.choices.value).toHaveLength(2)
    scope.stop()
  })

  it('uses inherited groups and refreshes them when Agent defaults change', async () => {
    mocks.listAgents.mockResolvedValue([{ name: 'Coder', inheritModel: true, modelBinding: { kind: 'configuration', configurationId: 'unused-local' } }])
    mocks.getSharedAgentConfig.mockResolvedValue({ acpBackend: 'codex', modelBinding: { kind: 'configuration', configurationId: 'a', configurationIds: ['a', 'b'] } })
    const { scope, catalog } = setup(); await flushPromises()
    expect(catalog.choices.value).toHaveLength(2)
    mocks.getSharedAgentConfig.mockResolvedValue({ acpBackend: 'opencode', modelBinding: { kind: 'configuration', configurationId: 'b' } })
    invalidateModelCatalog(); await flushPromises()
    expect(catalog.defaultConfigurationId.value).toBe('b')
    expect(catalog.choices.value).toHaveLength(1)
    expect(catalog.choices.value[0]?.configurationId).toBe('b')
    expect(catalog.modelInConfiguration('same', 'a')).toBe(false)
    scope.stop()
  })

})
