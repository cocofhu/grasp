// @vitest-environment happy-dom
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import zhCommon from '@/locales/zh-CN/common.json'
import zhPages from '@/locales/zh-CN/pages.json'
import enCommon from '@/locales/en/common.json'
import enPages from '@/locales/en/pages.json'
import AgentMetaPanel from './AgentMetaPanel.vue'
import WorkspaceSharedAgentPanel from '@/components/workspace/WorkspaceSharedAgentPanel.vue'
import { toDraft } from '@/lib/agent/agentStudioDraft'
const mocks = vi.hoisted(() => ({ getSharedAgentConfig: vi.fn(), listCredentials: vi.fn(), listModelConfigurations: vi.fn(), listModelConnections: vi.fn(), listToolCapabilities: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: mocks }))
function i18n(locale: string) { return createI18n({ legacy: false, locale, messages: { 'zh-CN': { ...zhCommon, ...zhPages }, en: { ...enCommon, ...enPages } } }) }
beforeEach(() => { mocks.getSharedAgentConfig.mockResolvedValue({ files: [], mcp: [], env: {}, layout: {} }); for (const method of ['listCredentials', 'listModelConfigurations', 'listModelConnections', 'listToolCapabilities'] as const) mocks[method].mockResolvedValue({ items: [] }) })
describe('model and shared settings copy', () => {
 it.each(['zh-CN', 'en'])('uses one inherited CLI card for Agent and explicit controls for workspace defaults (%s)', async locale => {
  mocks.getSharedAgentConfig.mockResolvedValue({ acpBackend: 'cursor', modelBinding: { kind: 'configuration', configurationId: 'cfg' }, files: [], mcp: [], env: {}, layout: {} })
  mocks.listModelConfigurations.mockResolvedValue({ items: [{ id: 'cfg', name: 'Team Cursor model', backend: 'cursor', state: 'active', binding: { kind: 'platform', credentialId: 'key', modelId: 'auto' } }] })
  mocks.listCredentials.mockResolvedValue({ items: [{ id: 'key', purpose: 'tool_auth', toolId: 'cursor', configured: true }] })
  const meta = mount(AgentMetaPanel, { props: { draft: toDraft({ name: 'dev', acpBackend: 'cursor', gitBindings: { items: [] } }), agentName: 'dev' }, global: { plugins: [i18n(locale)], stubs: { RouterLink: true, EnvCredentialHelpModal: true, Teleport: true } } }); await flushPromises()
  expect(meta.find('[data-testid="coding-tool-picker"]').exists()).toBe(false)
  expect(meta.findAll('[data-testid="agent-inherit-default-switch"]')).toHaveLength(1)
  expect(meta.get('[data-testid="agent-inherit-default-switch"]').attributes('aria-checked')).toBe('true')
  expect(meta.get('[data-testid="model-binding-inherited-summary"]').text()).toContain('Cursor')
  expect(meta.get('[data-testid="model-binding-inherited-summary"]').text()).toContain(locale === 'en' ? 'Workspace default' : '工作区默认')
  expect(meta.get('[title="Team Cursor model"]').exists()).toBe(true)
  for (const id of ['agent-inherit-model', 'agent-inherit-git', 'model-binding-configuration-select', 'git-credential-add', 'agent-credential-bindings']) expect(meta.find(`[data-testid="${id}"]`).exists()).toBe(false)
  await meta.get('[data-testid="agent-inherit-default-switch"]').trigger('click'); await flushPromises()
  expect(meta.find('[data-testid="model-binding-configuration-select"]').exists()).toBe(true)
  expect(meta.find('[data-testid="git-credential-add"]').exists()).toBe(true)
  expect(meta.find('[data-testid="meta-config-root"]').exists()).toBe(false)
  expect(meta.find('[data-testid="meta-workspace-dir"]').exists()).toBe(false)
  expect(meta.find('router-link-stub[to="/settings/credentials"]').exists()).toBe(false)
  expect(meta.find('[data-testid="agent-credential-bindings"]').exists()).toBe(false)
  expect(meta.text()).not.toContain((locale === 'en' ? enPages : zhPages).pages.agentStudio.meta.capabilitiesNote)
  expect(meta.find('[data-testid="model-binding-inherit"]').exists()).toBe(false)
  expect(meta.find('[data-testid="model-binding-advanced"]').exists()).toBe(false)
  expect(meta.find('[data-testid="derived-paths-table"]').exists()).toBe(false); meta.unmount()
  const shared = mount(WorkspaceSharedAgentPanel, { global: { plugins: [i18n(locale)], stubs: { AgentFilesPanel: true, AgentMcpPanel: true, AgentEnvPanel: true } } }); await flushPromises()
  await shared.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
  expect(shared.find('[data-testid="model-binding-editor"]').exists()).toBe(true)
  expect(shared.find('[data-testid="agent-git-credential-picker"]').exists()).toBe(true)
  expect(shared.find('[data-testid="agent-inherit-model"]').exists()).toBe(false)
  expect(shared.find('[data-testid="meta-config-root"]').exists()).toBe(false)
  expect(shared.find('[data-testid="meta-workspace-dir"]').exists()).toBe(false)
  expect(shared.find('a[href="/settings/credentials"]').exists()).toBe(false); shared.unmount()
 })
})
