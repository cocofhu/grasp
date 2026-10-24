// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { fromDraft, toDraft } from '@/lib/agent/agentStudioDraft'
import AgentRuntimeBindings from './AgentRuntimeBindings.vue'

const mocks = vi.hoisted(() => ({ getSharedAgentConfig: vi.fn(), listCredentials: vi.fn(), listModelConfigurations: vi.fn(), listModelConnections: vi.fn(), listToolCapabilities: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: mocks }))
const binding = { kind: 'configuration' as const, configurationId: 'local-model' }
const model = { id: 'local-model', name: 'Local Cursor', backend: 'cursor', state: 'active', binding: { kind: 'platform', credentialId: 'key' } }
const defaults = { files: [], mcp: [], env: {}, layout: {}, modelBinding: binding, gitBindings: { items: [{ credentialId: 'git' }], preferredCredentialId: 'git' } }
function setup(overrides = {}, inheritance = true) {
  const draft = reactive(toDraft({ name: 'a', inheritModel: false, inheritGit: false, acpBackend: 'cursor', modelBinding: binding, ...overrides }))
  const wrapper = mount(AgentRuntimeBindings, { props: { draft, inheritance }, global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })], stubs: { RouterLink: { template: '<a><slot /></a>' }, Teleport: true } } })
  return { wrapper, draft }
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.getSharedAgentConfig.mockResolvedValue(defaults)
  mocks.listCredentials.mockResolvedValue({ items: [{ id: 'key', name: 'Cursor key', purpose: 'tool_auth', toolId: 'cursor', configured: true }, { id: 'git', name: 'Team GitLab', purpose: 'git', type: 'git', provider: 'gitlab', configured: true }] })
  mocks.listModelConfigurations.mockResolvedValue({ items: [model] })
  mocks.listModelConnections.mockResolvedValue({ items: [] })
  mocks.listToolCapabilities.mockResolvedValue({ items: [] })
})
describe('explicit default binding choices', () => {
  it('respects explicitly disabled model and Git inheritance', async () => {
    const { wrapper, draft } = setup(); await flushPromises()
    expect(wrapper.get('[data-testid="agent-inherit-model"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.get('[data-testid="agent-inherit-git"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.find('[data-testid="git-credential-row-git"]').exists()).toBe(false)
    expect(draft.gitBindings?.items).toEqual([])
    expect(wrapper.emitted('modelValidity')?.at(-1)).toEqual([true]); wrapper.unmount()
  })
  it('uses defaults without selection controls and restores local bindings when inheritance is off', async () => {
    const localGit = { items: [{ credentialId: 'other-git' }], preferredCredentialId: 'other-git' }
    mocks.listCredentials.mockResolvedValue({ items: [{ id: 'key', purpose: 'tool_auth', toolId: 'cursor', configured: true }, { id: 'git', name: 'Team GitLab', purpose: 'git', provider: 'gitlab', configured: true }, { id: 'other-git', name: 'Local GitHub', purpose: 'git', provider: 'github', configured: true }] })
    const { wrapper, draft } = setup({ inheritModel: true, inheritGit: true, gitBindings: localGit }); await flushPromises()
    expect(wrapper.get('[data-testid="model-binding-inherited-summary"]').text()).toContain('Local Cursor')
    for (const id of ['model-binding-configuration-select', 'git-credential-add', 'git-preferred-git', 'git-remove-git', 'git-options-git']) expect(wrapper.find(`[data-testid="${id}"]`).exists()).toBe(false)
    expect(wrapper.get('[data-testid="git-inherited-preferred-git"]').text()).toBe('首选')
    expect(wrapper.emitted('modelValidity')?.at(-1)).toEqual([true])
    expect(wrapper.emitted('gitValidity')?.at(-1)).toEqual([true])
    expect(draft.modelBinding).toEqual(binding)
    expect(draft.gitBindings).toEqual(localGit)
    await wrapper.get('[data-testid="agent-inherit-model"]').trigger('click')
    await wrapper.get('[data-testid="agent-inherit-git"]').trigger('click')
    expect(wrapper.find('[data-testid="model-binding-configuration-select"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="git-credential-add"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="git-credential-row-other-git"]').text()).toContain('Local GitHub')
    expect(fromDraft(draft)).toMatchObject({ inheritModel: false, inheritGit: false, modelBinding: binding, gitBindings: localGit }); wrapper.unmount()
  })
  it('blocks a missing default model instead of using the available local selection', async () => {
    mocks.getSharedAgentConfig.mockResolvedValue({ ...defaults, modelBinding: undefined })
    const { wrapper, draft } = setup({ inheritModel: true }); await flushPromises()
    expect(wrapper.text()).toContain('请先在 Agent 默认配置中选择模型')
    expect(wrapper.emitted('modelValidity')?.at(-1)).toEqual([false])
    expect(draft.modelBinding).toEqual(binding)
    expect(wrapper.find('[data-testid="model-binding-configuration-select"]').exists()).toBe(false); wrapper.unmount()
  })
  it('blocks failed default Git loading and retries without replacing local references', async () => {
    mocks.getSharedAgentConfig.mockRejectedValueOnce(new Error('offline'))
    const { wrapper, draft } = setup({ inheritGit: true }); await flushPromises()
    expect(wrapper.text()).toContain('默认配置不可用')
    expect(wrapper.emitted('gitValidity')?.at(-1)).toEqual([false])
    await wrapper.get('[role="status"] button').trigger('click'); await flushPromises()
    expect(wrapper.emitted('gitValidity')?.at(-1)).toEqual([true])
    expect(wrapper.get('[data-testid="git-credential-row-git"]').text()).toContain('Team GitLab')
    expect(draft.gitBindings?.items).toEqual([]); wrapper.unmount()
  })
  it('explains invalid inherited scopes after a Git account changes host without exposing selection controls', async () => {
    mocks.getSharedAgentConfig.mockResolvedValue({ ...defaults, gitBindings: { items: [{ credentialId: 'git', scope: 'git.old.test' }], preferredCredentialId: 'git' } })
    mocks.listCredentials.mockResolvedValue({ items: [{ id: 'git', name: 'Team GitLab', purpose: 'git', provider: 'gitlab', host: 'https://git.new.test', configured: true }] })
    const { wrapper } = setup({ inheritGit: true }); await flushPromises()
    expect(wrapper.emitted('gitValidity')?.at(-1)).toEqual([false])
    expect(wrapper.get('[data-testid="git-credential-row-git"] [role="alert"]').text()).toContain('与凭据的 Host、协议一致')
    expect(wrapper.find('[data-testid="git-credential-add"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="git-options-git"]').exists()).toBe(false); wrapper.unmount()
  })
  it('reports a revoked inherited model and never falls back to the local model', async () => {
    mocks.getSharedAgentConfig.mockResolvedValue({ ...defaults, modelBinding: { kind: 'configuration', configurationId: 'deleted' } })
    const { wrapper, draft } = setup({ inheritModel: true }); await flushPromises()
    expect(wrapper.get('[data-testid="model-binding-problem"]').text()).toContain('不存在或已停用')
    expect(wrapper.emitted('modelValidity')?.at(-1)).toEqual([false])
    expect(fromDraft(draft).modelBinding).toEqual(binding); wrapper.unmount()
  })
  it('allows defaults to clear the model without clearing selected Git credentials', async () => {
    const { wrapper, draft } = setup({ gitBindings: defaults.gitBindings }, false); await flushPromises()
    expect(mocks.getSharedAgentConfig).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="agent-inherit-model"]').exists()).toBe(false)
    await wrapper.get('[data-testid="default-model-reset"]').trigger('click')
    expect(draft.modelBinding).toBeUndefined()
    expect(draft.gitBindings).toEqual(defaults.gitBindings); wrapper.unmount()
  })
})
