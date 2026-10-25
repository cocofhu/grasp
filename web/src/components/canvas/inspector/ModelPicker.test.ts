// @vitest-environment happy-dom
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ModelPicker from './ModelPicker.vue'
const mocks = vi.hoisted(() => ({ listModelConfigurations: vi.fn(), listModelConnections: vi.fn(), getSharedAgentConfig: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: mocks }))
const key = { id: 'key', name: 'Team key', purpose: 'model_api', configured: true }
const service = { id: 'service', name: 'API', credentialId: 'key', state: 'active', protocol: 'openai-chat-completions', baseUrl: 'https://model.example', models: [{ id: 'vendor/main' }] }
const config = { id: 'cfg', name: 'Team model', backend: 'opencode', state: 'active', binding: { kind: 'connection', connectionId: 'service', modelId: 'vendor/main' } }
function setup(agent: any = { name: 'dev', acpBackend: 'opencode', modelBinding: { kind: 'configuration', configurationId: 'cfg' } }, credentials: any = [key]) {
 return mount(ModelPicker, { props: { agent, credentials }, global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: {} } })], stubs: { RouterLink: true } } })
}
beforeEach(() => { vi.clearAllMocks(); mocks.getSharedAgentConfig.mockResolvedValue({ acpBackend: 'opencode', modelBinding: { kind: 'configuration', configurationId: 'cfg' } }); mocks.listModelConfigurations.mockResolvedValue({ items: [config] }); mocks.listModelConnections.mockResolvedValue({ items: [service] }) })
describe('workflow model summary', () => {
 it('follows the Agent named model read-only, including its raw model ID and edit destination', async () => {
  const w = setup(); await flushPromises()
  expect(w.get('[data-testid="model-picker-binding-configuration"]').text()).toBe('Team model')
  expect(w.get('[data-testid="model-picker-binding-model"]').text()).toBe('vendor/main')
  expect(w.get('[data-testid="model-picker-binding-source"]').text()).toContain('Team key')
  expect(w.find('select').exists()).toBe(false)
  expect(w.emitted()).toEqual({}); w.unmount()
 })
 it('requires an Agent and an explicit configuration', async () => {
  const w = setup(null); expect(w.find('[data-testid="model-picker-no-agent"]').exists()).toBe(true)
  await w.setProps({ agent: { name: 'empty', acpBackend: 'opencode' } }); await flushPromises()
  expect(w.get('[data-testid="model-picker-binding-error"]').text()).toContain('missing or disabled'); w.unmount()
 })
 it('shows disabled configurations, tool mismatches and revoked credentials instead of falling back', async () => {
  const w = setup(); await flushPromises()
  await w.setProps({ agent: { name: 'dev', acpBackend: 'cursor', modelBinding: { kind: 'configuration', configurationId: 'cfg' } } }); await flushPromises()
  expect(w.get('[role="status"]').text()).toContain('different coding tool')
  await w.setProps({ agent: { name: 'dev', acpBackend: 'opencode', modelBinding: { kind: 'configuration', configurationId: 'cfg' } }, credentials: [{ ...key, configured: false }] }); await flushPromises()
  expect(w.get('[role="status"]').text()).toContain('key is missing'); w.unmount()
 })
 it('surfaces API failures and missing connection models', async () => {
  mocks.listModelConfigurations.mockRejectedValueOnce(new Error('offline'))
  const w = setup(); await flushPromises(); expect(w.get('[role="status"]').text()).toContain('offline'); w.unmount()
  mocks.listModelConnections.mockResolvedValue({ items: [{ ...service, models: [] }] })
  const missing = setup(); await flushPromises(); expect(missing.get('[role="status"]').text()).toContain('Select a model'); missing.unmount()
 })
 it('waits for credentials and handles a platform configuration without fetching connections', async () => {
  mocks.listModelConfigurations.mockResolvedValue({ items: [{ ...config, backend: 'cursor', binding: { kind: 'platform', credentialId: 'cursor', modelId: '' } }] })
  const w = setup({ name: 'cursor', acpBackend: 'cursor', modelBinding: { kind: 'configuration', configurationId: 'cfg' } }, null); await flushPromises()
  expect(w.text()).toContain('Loading')
  await w.setProps({ credentials: [{ id: 'cursor', name: 'Cursor', purpose: 'tool_auth', toolId: 'cursor', configured: true }] })
  expect(w.get('[data-testid="model-picker-binding-model"]').text()).toContain('Tool default')
  expect(mocks.listModelConnections).not.toHaveBeenCalled(); w.unmount()
 })
 it('shows an inherited model using shared defaults even when the Agent has no local reference', async () => {
  const w = setup({ name: 'inherited', acpBackend: 'cursor', inheritModel: true }); await flushPromises()
  expect(mocks.getSharedAgentConfig).toHaveBeenCalledTimes(1)
  expect(w.get('[data-testid="model-picker-binding-configuration"]').text()).toBe('Team model')
  expect(w.get('[data-testid="model-picker-binding-model"]').text()).toBe('vendor/main')
  expect(w.find('[data-testid="model-picker-binding-error"]').exists()).toBe(false)
  await w.setProps({ agent: { name: 'inherited', acpBackend: 'cursor', inheritModel: false } }); await flushPromises()
  expect(w.get('[data-testid="model-picker-binding-error"]').text()).toContain('missing or disabled'); w.unmount()
 })
 it('never substitutes a local model when inherited defaults are missing or unavailable', async () => {
  mocks.getSharedAgentConfig.mockResolvedValueOnce({})
  const w = setup({ name: 'inherited', acpBackend: 'opencode', inheritModel: true, modelBinding: { kind: 'configuration', configurationId: 'cfg' } }); await flushPromises()
  expect(w.get('[data-testid="model-picker-binding-error"]').text()).toContain('missing or disabled')
  expect(w.find('[data-testid="model-picker-binding-configuration"]').exists()).toBe(false); w.unmount()
  mocks.getSharedAgentConfig.mockRejectedValueOnce(new Error('defaults offline'))
  const failed = setup({ name: 'inherited', acpBackend: 'cursor', inheritModel: true }); await flushPromises()
  expect(failed.get('[data-testid="model-picker-binding-error"]').text()).toContain('defaults offline'); failed.unmount()
 })

})
