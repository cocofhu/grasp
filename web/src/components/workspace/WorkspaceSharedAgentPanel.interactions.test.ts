// @vitest-environment happy-dom
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import WorkspaceSharedAgentPanel from './WorkspaceSharedAgentPanel.vue'
const mocks = vi.hoisted(() => ({ getSharedAgentConfig: vi.fn(), putSharedAgentConfig: vi.fn(), listModelConfigurations: vi.fn(), listModelConnections: vi.fn(), listCredentials: vi.fn(), listToolCapabilities: vi.fn(), success: vi.fn(), error: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: mocks }))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => mocks }))
const cfg = { files: [], mcp: [], env: { FEATURE: 'on' }, layout: { configRoot: '/config', workspaceDir: '/workspace' } }
function setup() { return mount(WorkspaceSharedAgentPanel, { global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })], stubs: { AgentFilesPanel: { template: '<div data-testid="files" />' }, AgentEnvPanel: { props: ['draft'], template: '<div data-testid="env"><input data-testid="shared-feature" v-model="draft.env[0].v" /></div>' }, AgentMcpPanel: { template: '<div data-testid="mcp" />' } } } }) }
beforeEach(() => { vi.clearAllMocks(); mocks.getSharedAgentConfig.mockResolvedValue(cfg); mocks.putSharedAgentConfig.mockImplementation(async body => body); for (const method of ['listModelConfigurations', 'listModelConnections', 'listCredentials', 'listToolCapabilities'] as const) mocks[method].mockResolvedValue({ items: [] }) })
describe('shared Agent defaults', () => {
 it('edits environment while keeping model and Git defaults empty until explicitly configured', async () => {
  const w = setup(); await flushPromises(); await w.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
  expect(w.find('[data-testid="model-binding-editor"]').exists()).toBe(true)
  expect(w.find('[data-testid="agent-git-credential-picker"]').exists()).toBe(true)
  expect(w.find('[data-testid="coding-tool-picker"]').exists()).toBe(false)
  expect(w.find('[data-testid="meta-config-root"]').exists()).toBe(false)
  expect(w.find('[data-testid="meta-workspace-dir"]').exists()).toBe(false)
  await w.get('[data-testid="shared-agent-subtab-env"]').trigger('click')
  await w.get('[data-testid="shared-feature"]').setValue('off')
  expect((w.vm as any).dirty).toBe(true); expect(await (w.vm as any).save()).toBe(true)
  expect(mocks.putSharedAgentConfig).toHaveBeenCalledWith({ ...cfg, env: { FEATURE: 'off' } })
  expect((w.vm as any).dirty).toBe(false); expect(mocks.success).toHaveBeenCalled(); w.unmount()
 })
 it('supports files, MCP and env alongside runtime defaults', async () => {
  const w = setup(); await flushPromises(); expect(w.find('[data-testid="files"]').exists()).toBe(true)
  await w.get('[data-testid="shared-agent-subtab-mcp"]').trigger('click'); expect(w.find('[data-testid="mcp"]').exists()).toBe(true)
  await w.get('[data-testid="shared-agent-subtab-env"]').trigger('click'); expect(w.find('[data-testid="env"]').exists()).toBe(true); w.unmount()
 })
 it('retries load failures, preserves unsaved edits on save errors, and discards explicitly', async () => {
  mocks.getSharedAgentConfig.mockRejectedValueOnce(new Error('offline'))
  const w = setup(); await flushPromises(); expect(w.text()).toContain('offline')
  await w.get('button').trigger('click'); await flushPromises()
  await w.get('[data-testid="shared-agent-subtab-env"]').trigger('click'); await w.get('[data-testid="shared-feature"]').setValue('dirty')
  mocks.putSharedAgentConfig.mockRejectedValueOnce(new Error('save failed'))
  expect(await (w.vm as any).save()).toBe(false); expect((w.vm as any).dirty).toBe(true); expect(mocks.error).toHaveBeenCalledWith('save failed')
  const discard = w.findAll('button').find(button => button.text() === '放弃修改')!
  await discard.trigger('click'); await flushPromises(); expect((w.vm as any).dirty).toBe(false)
  expect(w.get('[data-testid="shared-feature"]').element).toHaveProperty('value', 'on'); w.unmount()
 })
})
