// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { fromDraft, toDraft } from '@/lib/agent/agentStudioDraft'
import AgentMetaPanel from './AgentMetaPanel.vue'
import ModelBindingEditor from './ModelBindingEditor.vue'
import AgentGitCredentialPicker from './AgentGitCredentialPicker.vue'
const mocks = vi.hoisted(() => ({ listCredentials: vi.fn(), listModelConfigurations: vi.fn(), listModelConnections: vi.fn(), listToolCapabilities: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: mocks }))
const key = { id: 'cursor-account', name: 'Cursor team', purpose: 'tool_auth', toolId: 'cursor', configured: true }
const configuration = { id: 'cfg', name: 'Team Cursor', backend: 'cursor', state: 'active', binding: { kind: 'platform', credentialId: key.id, modelId: 'gpt-5' } }
function setup(overrides = {}) {
 const draft = reactive(toDraft({ name: 'a', acpBackend: 'cursor', modelBinding: { kind: 'configuration', configurationId: 'cfg' }, gitBindings: { items: [] }, ...overrides }))
 const w = mount(AgentMetaPanel, { props: { draft, agentName: 'a' }, global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })], stubs: { AgentCredentialBindings: true, EnvCredentialHelpModal: true, Teleport: true } } })
 return { w, draft }
}
beforeEach(() => {
 mocks.listCredentials.mockResolvedValue({ items: [key] }); mocks.listModelConfigurations.mockResolvedValue({ items: [configuration] })
 mocks.listModelConnections.mockResolvedValue({ items: [] }); mocks.listToolCapabilities.mockResolvedValue({ items: [] })
})
describe('Agent saved configuration editing', () => {
 it('shows tool and named model on one page and keeps only the saved reference', async () => {
  const { w, draft } = setup(); await flushPromises()
  expect(w.get('[data-testid="acp-backend-cursor"]').attributes('aria-checked')).toBe('true')
  expect(w.get('[data-testid="model-binding-configuration-select"]').text()).toContain('Team Cursor')
  expect(w.find('select').exists()).toBe(false)
  expect(fromDraft(draft).modelBinding).toEqual({ kind: 'configuration', configurationId: 'cfg' }); w.unmount()
 })
 it('clears the configuration when changing tools and requires a new explicit selection', async () => {
  const { w, draft } = setup(); await flushPromises()
  await w.get('[data-testid="acp-backend-opencode"]').trigger('click')
  expect(draft.acpBackend).toBe('opencode'); expect(draft.modelBinding).toEqual({ kind: 'configuration', configurationId: '' })
  expect(draft.layout.configRoot).toBe('/root/.config/opencode')
  await w.get('[data-testid="acp-backend-cursor"]').trigger('click')
  expect(draft.modelBinding?.configurationId).toBe(''); w.unmount()
 })
 it('preserves an edited config path and unrelated env while changing tools', async () => {
  const { w, draft } = setup({ env: { FEATURE: 'on' } }); await flushPromises()
  await w.get('[data-testid="meta-config-root"]').setValue('/custom/config')
  await w.get('[data-testid="acp-backend-codebuddy"]').trigger('click')
  expect(draft.layout.configRoot).toBe('/custom/config')
  expect(fromDraft(draft).env).toEqual({ FEATURE: 'on', GRASP_CODEBUDDY_REGION: 'public' }); w.unmount()
 })
 it('writes named model and multiple Git references independently', async () => {
  const { w, draft } = setup(); await flushPromises()
  w.getComponent(ModelBindingEditor).vm.$emit('update:modelValue', { kind: 'configuration', configurationId: 'replacement' })
  w.getComponent(AgentGitCredentialPicker).vm.$emit('update:modelValue', { items: [{ credentialId: 'gh' }, { credentialId: 'ssh', sshHostsCredentialId: 'hosts' }], preferredCredentialId: 'ssh' })
  const payload = fromDraft(draft)
  expect(payload.modelBinding).toEqual({ kind: 'configuration', configurationId: 'replacement' })
  expect(payload.gitBindings?.items).toHaveLength(2)
  expect(payload.gitBindings?.preferredCredentialId).toBe('ssh'); w.unmount()
 })
 it('retains a missing reference with a visible error until a replacement is explicitly selected', async () => {
  const { w, draft } = setup({ modelBinding: { kind: 'configuration', configurationId: 'missing' } }); await flushPromises()
  expect(w.get('[data-testid="model-binding-problem"]').text()).toContain('不存在或已停用')
  expect(draft.modelBinding?.configurationId).toBe('missing'); w.unmount()
 })
})
