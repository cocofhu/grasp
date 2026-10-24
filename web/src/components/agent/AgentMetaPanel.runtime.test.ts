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
const mocks = vi.hoisted(() => ({ listCredentials: vi.fn(), listModelConfigurations: vi.fn(), listModelConnections: vi.fn(), listToolCapabilities: vi.fn(), getSharedAgentConfig: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: mocks }))
const key = { id: 'cursor-account', name: 'Cursor team', purpose: 'tool_auth', toolId: 'cursor', configured: true }
const buddy = { id: 'buddy', name: 'Team Buddy', backend: 'codebuddy', state: 'active', binding: { kind: 'platform', credentialId: 'buddy-key', modelId: 'auto' } }
const configuration = { id: 'cfg', name: 'Team Cursor', backend: 'cursor', state: 'active', binding: { kind: 'platform', credentialId: key.id, modelId: 'gpt-5' } }
function setup(overrides = {}) {
 const draft = reactive(toDraft({ name: 'a', acpBackend: 'cursor', modelBinding: { kind: 'configuration', configurationId: 'cfg' }, gitBindings: { items: [] }, ...overrides }))
 const w = mount(AgentMetaPanel, { props: { draft, agentName: 'a' }, global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })], stubs: { AgentCredentialBindings: true, EnvCredentialHelpModal: true, Teleport: true } } })
 return { w, draft }
}
beforeEach(() => {
 mocks.listCredentials.mockResolvedValue({ items: [key, { id: 'buddy-key', name: 'Buddy account', purpose: 'tool_auth', toolId: 'codebuddy', configured: true }, { id: 'gh', name: 'GitHub team', purpose: 'git', type: 'git', provider: 'github', configured: true }] }); mocks.listModelConfigurations.mockResolvedValue({ items: [configuration, buddy] })
 mocks.getSharedAgentConfig.mockResolvedValue({ acpBackend: 'codebuddy', modelBinding: { kind: 'configuration', configurationId: 'buddy' }, gitBindings: { items: [{ credentialId: 'gh' }], preferredCredentialId: 'gh' }, files: [], mcp: [], env: {}, layout: {} })
 mocks.listModelConnections.mockResolvedValue({ items: [] }); mocks.listToolCapabilities.mockResolvedValue({ items: [] })
})
describe('Agent saved configuration editing', () => {
 it('selects a named model without a duplicate coding-tool list', async () => {
  const { w, draft } = setup(); await flushPromises()
  expect(w.find('[data-testid="coding-tool-picker"]').exists()).toBe(false)
  expect(w.get('[data-testid="model-binding-configuration-select"]').text()).toContain('Team Cursor')
  expect(w.find('select').exists()).toBe(false)
  expect(fromDraft(draft).modelBinding).toEqual({ kind: 'configuration', configurationId: 'cfg' }); w.unmount()
 })
 it('derives the CLI and default config path from the selected model', async () => {
  const { w, draft } = setup(); await flushPromises()
  await w.get('[data-testid="model-binding-configuration-select"] [data-test="app-select-trigger"]').trigger('click')
  await w.get('[data-test="app-select-option-buddy"]').trigger('click')
  expect(draft.acpBackend).toBe('codebuddy')
  expect(draft.modelBinding).toEqual({ kind: 'configuration', configurationId: 'buddy' })
  expect(draft.layout.configRoot).toBe('/root/.codebuddy'); w.unmount()
 })
 it('preserves an edited config path and unrelated env when selecting another model', async () => {
  const { w, draft } = setup({ env: { FEATURE: 'on' } }); await flushPromises()
  await w.get('[data-testid="meta-config-root"]').setValue('/custom/config')
  await w.get('[data-testid="model-binding-configuration-select"] [data-test="app-select-trigger"]').trigger('click')
  await w.get('[data-test="app-select-option-buddy"]').trigger('click')
  expect(draft.layout.configRoot).toBe('/custom/config')
  expect(fromDraft(draft).env).toEqual({ FEATURE: 'on', GRASP_CODEBUDDY_REGION: 'public' }); w.unmount()
 })
 it('independently inherits defaults and restores local selections when switches turn off', async () => {
  const { w, draft } = setup(); await flushPromises()
  await w.get('[data-testid="agent-inherit-model"]').trigger('click')
  expect(w.get('[data-testid="model-binding-configuration-select"]').text()).toContain('Team Buddy')
  expect(w.get('[data-testid="model-binding-configuration-select"] button').attributes('disabled')).toBeDefined()
  expect(draft.modelBinding?.configurationId).toBe('cfg')
  await w.get('[data-testid="agent-inherit-git"]').trigger('click'); await flushPromises()
  expect(w.get('[data-testid="git-credential-row-gh"]').text()).toContain('GitHub team')
  expect(draft.gitBindings?.items).toEqual([])
  expect(fromDraft(draft)).toMatchObject({ inheritModel: true, inheritGit: true })
  await w.get('[data-testid="agent-inherit-model"]').trigger('click')
  await w.get('[data-testid="agent-inherit-git"]').trigger('click')
  expect(w.get('[data-testid="model-binding-configuration-select"]').text()).toContain('Team Cursor')
  expect(w.find('[data-testid="git-credential-row-gh"]').exists()).toBe(false)
  expect(fromDraft(draft)).toMatchObject({ inheritModel: false, inheritGit: false }); w.unmount()
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
