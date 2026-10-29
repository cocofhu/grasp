// @vitest-environment happy-dom
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ModelBindingEditor from './ModelBindingEditor.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import type { ModelBinding } from '@/lib/api/api'
import { invalidateModelCatalog } from '@/lib/api/modelCatalogEvents'
const mocks = vi.hoisted(() => ({ listCredentials: vi.fn(), listModelConnections: vi.fn(), listModelConfigurations: vi.fn(), listToolCapabilities: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: mocks }))
const key = { id: 'key', name: 'Team key', purpose: 'model_api', configured: true }
const connection = { id: 'service', protocol: 'anthropic-messages', credentialId: 'key', auth: { scheme: 'bearer' }, models: [{ id: 'vendor/main' }], state: 'active' }
const configuration = { id: 'profile', name: 'Team Claude', backend: 'claude_code', binding: { kind: 'connection', connectionId: 'service', modelId: 'vendor/main' }, state: 'active' }
function setup(backend = 'claude_code', binding?: ModelBinding) {
  return mount(ModelBindingEditor, { props: { backend, modelValue: binding }, global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: {} } })], stubs: { Teleport: true } } })
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.listCredentials.mockResolvedValue({ items: [key] })
  mocks.listModelConnections.mockResolvedValue({ items: [connection] })
  mocks.listModelConfigurations.mockResolvedValue({ items: [configuration, { ...configuration, id: 'disabled', name: 'Disabled group', state: 'disabled' }, { ...configuration, id: 'other', name: 'OpenCode model', backend: 'opencode' }] })
  mocks.listToolCapabilities.mockResolvedValue({ items: [{ toolId: 'claude_code', modes: ['connection'], protocols: ['anthropic-messages'], adapterState: 'existing' }] })
})
describe('named Agent model selector', () => {
  it('requires explicit selection and searches only active models for this tool, with logos', async () => {
    const w = setup(); await flushPromises()
    expect(w.find('select').exists()).toBe(false)
    expect(w.emitted('validity')?.at(-1)).toEqual([false])
    const field = w.getComponent(AppSelect)
    await field.get('[data-test="app-select-trigger"]').trigger('click')
    expect(field.findAll('[role="option"]').map(row => row.text())).toEqual(['Team Claude'])
    expect(field.find('[data-provider-logo="claude"]').exists()).toBe(true)
    await field.get('[data-test="app-select-search"]').setValue('Team')
    await field.get('[data-test="app-select-option-profile"]').trigger('click')
    expect(w.emitted('update:modelValue')).toEqual([[{ kind: 'configuration', configurationId: 'profile' }]])
    await w.setProps({ modelValue: { kind: 'configuration', configurationId: 'profile' } })
    expect(w.emitted('validity')?.at(-1)).toEqual([true]); w.unmount()
  })
  it('offers models across tools when onboarding has not chosen a tool', async () => {
    const w = setup(''); await flushPromises()
    await w.get('[data-test="app-select-trigger"]').trigger('click')
    expect(w.findAll('[role="option"]').map(row => row.text())).toEqual(['Team Claude', 'OpenCode model']); w.unmount()
  })
  it('keeps missing references invalid without substituting a model', async () => {
    const w = setup('claude_code', { kind: 'configuration', configurationId: 'deleted' }); await flushPromises()
    expect(w.get('[data-test="app-select-trigger"]').text()).toContain('deleted')
    expect(w.get('[data-testid="model-binding-problem"]').text()).toContain('missing or disabled')
    expect(w.emitted('validity')?.at(-1)).toEqual([false])
    for (const id of ['', 'disabled', 'other', 'missing']) w.getComponent(AppSelect).vm.$emit('update:modelValue', id)
    expect(w.emitted('update:modelValue')).toBeUndefined(); w.unmount()
  })
  it('reports tool mismatches and revoked underlying keys', async () => {
    const w = setup('cursor', { kind: 'configuration', configurationId: 'profile' }); await flushPromises()
    expect(w.get('[role="status"]').text()).toContain('different coding tool')
    await w.setProps({ backend: 'claude_code' }); expect(w.emitted('validity')?.at(-1)).toEqual([true])
    w.unmount()
    mocks.listCredentials.mockResolvedValue({ items: [{ ...key, configured: false }] })
    const revoked = setup('claude_code', { kind: 'configuration', configurationId: 'profile' }); await flushPromises()
    expect(revoked.get('[role="status"]').text()).toContain('key is missing')
    expect(revoked.emitted('validity')?.at(-1)).toEqual([false]); revoked.unmount()
  })
  it('shows actual load failures and retries without selecting an available replacement', async () => {
    mocks.listModelConfigurations.mockRejectedValueOnce(new Error('offline'))
    const w = setup(); await flushPromises(); expect(w.get('[role="alert"]').text()).toContain('offline')
    await w.get('[role="alert"] button').trigger('click'); await flushPromises()
    expect(w.find('[role="alert"]').exists()).toBe(false)
    expect(w.emitted('update:modelValue')).toBeUndefined(); w.unmount()
  })
  it('adds a group from another tool and only changes the CLI when making it the default', async () => {
    mocks.listModelConnections.mockResolvedValue({ items: [{ ...connection, auth: { scheme: 'x-api-key' } }] })
    mocks.listToolCapabilities.mockResolvedValue({ items: [
      { toolId: 'claude_code', modes: ['connection'], protocols: ['anthropic-messages'], adapterState: 'existing' },
      { toolId: 'opencode', modes: ['connection'], protocols: ['anthropic-messages'], adapterState: 'existing' },
    ] })
    const w = setup('', { kind: 'configuration', configurationId: 'profile' }); await flushPromises()
    w.findAllComponents(AppSelect).at(-1)!.vm.$emit('update:modelValue', 'other')
    const binding = { kind: 'configuration' as const, configurationId: 'profile', configurationIds: ['profile', 'other'] }
    expect(w.emitted('update:modelValue')?.at(-1)).toEqual([binding])
    expect(w.emitted('selectConfiguration')).toBeUndefined()
    await w.setProps({ modelValue: binding })
    expect(w.emitted('validity')?.at(-1)).toEqual([true])
    await w.get('[data-testid="model-binding-default-other"]').trigger('click')
    expect(w.emitted('update:modelValue')?.at(-1)).toEqual([{ ...binding, configurationId: 'other' }])
    expect(w.emitted('selectConfiguration')?.at(-1)?.[0]).toMatchObject({ id: 'other', backend: 'opencode' })
    await w.get('[data-testid="model-binding-remove-other"]').trigger('click')
    expect(w.emitted('update:modelValue')?.at(-1)).toEqual([{ kind: 'configuration', configurationId: 'profile' }])
    w.unmount()
  })

  it('uses all model groups without serializing a snapshot and restores manual groups when turned off', async () => {
    const manual = { kind: 'configuration' as const, configurationId: 'profile', configurationIds: ['profile', 'other'] }
    const w = setup('', manual); await flushPromises()
    await w.get('[data-testid="model-binding-all-configurations"]').trigger('click')
    const all = { kind: 'configuration' as const, configurationId: 'profile', allConfigurations: true }
    expect(w.emitted('update:modelValue')?.at(-1)).toEqual([all])
    expect(w.emitted('selectConfiguration')).toBeUndefined()
    await w.setProps({ modelValue: all })
    expect(w.get('[data-testid="model-binding-all-configurations"]').attributes('aria-checked')).toBe('true')
    expect(w.find('[data-testid="model-binding-add-select"]').exists()).toBe(false)
    expect(w.find('[data-testid="model-binding-remove-other"]').exists()).toBe(false)
    const groups = w.get('[data-testid="model-binding-all-groups"]')
    expect(groups.get('[data-icon="layers"]').exists()).toBe(true)
    expect(groups.find('[data-provider-logo]').exists()).toBe(false)
    expect(groups.text()).not.toContain('Team Claude')
    expect(groups.text()).not.toContain('OpenCode model')
    expect(groups.text()).not.toContain('Disabled group')
    expect(w.find('[data-testid="model-binding-groups"]').exists()).toBe(false)
    const startupDefault = w.get('[data-testid="model-binding-startup-default"]')
    expect(startupDefault.text()).toContain('Default startup model group')
    expect(startupDefault.get('[data-testid="model-binding-configuration-select"]').text()).toContain('Team Claude')
    expect(startupDefault.find('[data-provider-logo="claude"]').exists()).toBe(true)
    expect(w.get('[data-testid="model-binding-all-hint"]').text()).toContain('included automatically')
    // A broken additional group must not prevent using the valid explicit default.
    expect(w.emitted('validity')?.at(-1)).toEqual([true])
    await w.get('[data-testid="model-binding-all-configurations"]').trigger('click')
    expect(w.emitted('update:modelValue')?.at(-1)).toEqual([manual])
    await w.setProps({ modelValue: manual })
    expect(w.find('[data-testid="model-binding-remove-other"]').exists()).toBe(true)
    w.unmount()
  })

  it('keeps all mode and the explicit default when the default or catalog changes', async () => {
    const all = { kind: 'configuration' as const, configurationId: 'profile', allConfigurations: true }
    const w = setup('', all); await flushPromises()
    expect(w.get('[data-testid="model-binding-all-groups"]').text()).not.toContain('OpenCode model')
    mocks.listModelConfigurations.mockResolvedValue({ items: [configuration, { ...configuration, id: 'new', name: 'New review group' }] })
    invalidateModelCatalog(); await flushPromises()
    expect(w.emitted('update:modelValue')).toBeUndefined()
    expect(w.get('[data-testid="model-binding-all-groups"]').text()).not.toContain('New review group')
    expect(w.get('[data-testid="model-binding-all-groups"]').text()).not.toContain('OpenCode model')
    expect(w.get('[data-testid="model-binding-startup-default"]').get('[data-testid="model-binding-configuration-select"]').text()).toContain('Team Claude')
    w.getComponent(AppSelect).vm.$emit('update:modelValue', 'new')
    const updated = { ...all, configurationId: 'new' }
    expect(w.emitted('update:modelValue')?.at(-1)).toEqual([updated])
    expect(w.emitted('selectConfiguration')?.at(-1)?.[0]).toMatchObject({ id: 'new' })
    await w.setProps({ modelValue: updated })
    expect(w.get('[data-testid="model-binding-startup-default"]').get('[data-testid="model-binding-configuration-select"]').text()).toContain('New review group')
    await w.get('[data-testid="model-binding-all-configurations"]').trigger('click')
    expect(w.emitted('update:modelValue')?.at(-1)).toEqual([{ kind: 'configuration', configurationId: 'new' }])
    w.unmount()
  })

  it('summarizes inherited all mode without exposing editing controls', async () => {
    const w = setup('', { kind: 'configuration', configurationId: 'profile', allConfigurations: true })
    await w.setProps({ summary: true, summarySource: 'Agent defaults' }); await flushPromises()
    const summary = w.get('[data-testid="model-binding-inherited-summary"]')
    expect(w.get('[data-testid="model-binding-all-summary"]').text()).toBe('Inherit all configured model groups')
    expect(summary.get('[data-icon="layers"]').exists()).toBe(true)
    expect(summary.find('[data-provider-logo]').exists()).toBe(false)
    expect(summary.text()).toContain('Default startup model group')
    expect(summary.text()).toContain('Team Claude')
    expect(w.text()).toContain('Agent defaults')
    expect(w.find('[role="switch"]').exists()).toBe(false)
    expect(w.findComponent(AppSelect).exists()).toBe(false)
    expect(w.emitted('update:modelValue')).toBeUndefined()
    w.unmount()
  })

  it('requires an explicit default and supports read-only or restricted forms', async () => {
    const w = setup(''); await flushPromises()
    expect(w.get('[data-testid="model-binding-all-configurations"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="model-binding-all-configurations"]').trigger('click')
    expect(w.emitted('update:modelValue')).toBeUndefined()
    await w.setProps({ modelValue: { kind: 'configuration', configurationId: 'profile' }, disabled: true })
    expect(w.get('[data-testid="model-binding-all-configurations"]').attributes('disabled')).toBeDefined()
    await w.setProps({ disabled: false, allowAllConfigurations: false })
    expect(w.find('[data-testid="model-binding-all-configurations"]').exists()).toBe(false)
    w.unmount()
  })

})
