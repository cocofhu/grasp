// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount, type DOMWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { fromDraft, fromDraftRaw, type AgentStudioDraft } from '@/lib/agent/agentStudioDraft'
import AgentMetaPanel from './AgentMetaPanel.vue'

const mocks = vi.hoisted(() => ({ listCredentials: vi.fn(), getSharedAgentConfig: vi.fn(), listModelConfigurations: vi.fn(), listModelConnections: vi.fn(), listToolCapabilities: vi.fn(), getModelDefaults: vi.fn() }))
vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api, ...mocks } }
})
vi.mock('@/lib/agent/cliCatalog', () => ({ loadCliModels: async () => [{ id: 'gpt-5', name: 'GPT-5', vendor: 'openai' }], cliCatalogFailed: () => false }))

const cursorKey = { id: 'cursor-account', name: 'Cursor team', purpose: 'tool_auth', toolId: 'cursor', configured: true, masked: '••••' }
const connection = { id: 'claude-service', name: 'Team service', providerId: 'custom', protocol: 'anthropic-messages', baseUrl: 'https://model.example', credentialId: 'model-key', auth: { scheme: 'x-api-key' }, models: [{ id: 'vendor/main', source: 'manual' }, { id: 'vendor/fast', source: 'manual' }], state: 'active', revision: 1 }
function draftFor(overrides: Record<string, unknown> = {}) {
  return reactive({ name: 'a', acpBackend: 'cursor', modelBinding: { kind: 'inherit' }, aiCredentialId: '', openCodeCredentialId: '', gitCredentialId: '', sshHostsCredentialId: '', gitCredentialType: undefined, files: [], mcp: [], env: [] as { k: string; v: string }[], layout: { configRoot: '/root/.cursor', workspaceDir: '/workspace' }, capabilities: null, ...overrides })
}
const mounted: { unmount: () => void }[] = []
function mountPanel(draft: ReturnType<typeof draftFor>) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  const wrapper = mount(AgentMetaPanel, { props: { draft, agentName: 'a' }, global: { plugins: [i18n], stubs: { Icon: true, RouterLink: true, RepoAccessPicker: true, AgentCredentialBindings: true, AppModal: { props: ['open'], template: '<div v-if="open" data-testid="backend-confirm"><slot/><slot name="footer"/></div>' } } }, attachTo: document.body })
  mounted.push(wrapper)
  return wrapper
}
async function selectOption(field: DOMWrapper<Element>, value: string) {
  await field.get('[data-test="app-select-trigger"]').trigger('click')
  await flushPromises()
  const option = Array.from(document.body.querySelectorAll<HTMLButtonElement>('[role="option"]'))
    .find((item) => item.dataset.test === `app-select-option-${value}`)
  expect(option).toBeTruthy()
  option!.click()
  await flushPromises()
}
async function chooseDirect(wrapper: ReturnType<typeof mountPanel>, mode: 'platform' | 'connection') {
  const advanced = wrapper.get('[data-testid="model-binding-advanced"]')
  if (advanced.element.tagName === 'DETAILS' && !(advanced.element as HTMLDetailsElement).open) await advanced.get('summary').trigger('click')
  await wrapper.get(`[data-testid="model-binding-${mode}"]`).trigger('click')
  await flushPromises()
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.listCredentials.mockResolvedValue({ items: [cursorKey, { id: 'model-key', name: 'Provider key', purpose: 'model_api', configured: true }] })
  mocks.getSharedAgentConfig.mockResolvedValue({ acpBackend: 'cursor', env: {} })
  mocks.listModelConfigurations.mockResolvedValue({ items: [] })
  mocks.listModelConnections.mockResolvedValue({ items: [connection] })
  mocks.getModelDefaults.mockResolvedValue({ bindingsByBackend: { cursor: { kind: 'platform', credentialId: cursorKey.id, modelId: 'gpt-5' } } })
  mocks.listToolCapabilities.mockResolvedValue({ items: [
    { toolId: 'cursor', modes: ['platform'], protocols: [], adapterState: 'existing' },
    { toolId: 'claude_code', modes: ['platform', 'connection'], protocols: ['anthropic-messages'], adapterState: 'existing', roleNames: ['main', 'fast', 'subagent', 'opus', 'sonnet', 'haiku'] },
    { toolId: 'opencode', modes: ['connection'], protocols: ['anthropic-messages'], adapterState: 'existing' },
  ] })
})
afterEach(() => { mounted.splice(0).forEach((w) => w.unmount()); document.body.innerHTML = '' })

describe('AgentMetaPanel independent model bindings', () => {
  it('inherits the complete current-tool default while keeping tool selection visible', async () => {
    const draft = draftFor()
    const wrapper = mountPanel(draft)
    await flushPromises()
    expect(wrapper.get('[data-testid="acp-backend-cursor"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[data-testid="model-binding-inherited"]').text()).toContain('gpt-5')
    expect(wrapper.find('[data-testid="model-binding-credential"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="model-binding-connection"]').attributes('disabled')).toBeDefined()
    expect(draft.modelBinding).toEqual({ kind: 'inherit' })
    expect(draft.env).toEqual([])
  })

  it('saves platform credential and model together without writing legacy model env', async () => {
    const draft = draftFor()
    const wrapper = mountPanel(draft)
    await flushPromises()
    await chooseDirect(wrapper, 'platform')
    await selectOption(wrapper.get('[data-testid="model-binding-credential"]'), cursorKey.id)
    await wrapper.get('[data-testid="cli-model-trigger"]').trigger('click')
    await flushPromises()
    ;(document.body.querySelector('[data-testid="cli-model-option-gpt-5"]') as HTMLElement).click()
    await flushPromises()
    expect(draft.modelBinding).toEqual({ kind: 'platform', credentialId: cursorKey.id, modelId: 'gpt-5' })
    expect(draft.env).toEqual([])
    await wrapper.get('[data-testid="cli-model-reset"]').trigger('click')
    expect(draft.modelBinding).toEqual({ kind: 'platform', credentialId: cursorKey.id, modelId: '' })
  })

  it('does not inherit a different tool or copy its credential', async () => {
    const draft = draftFor({ acpBackend: 'claude_code' })
    const wrapper = mountPanel(draft)
    await flushPromises()
    expect(wrapper.get('[data-testid="model-binding-inherited"]').text()).toContain('当前工具没有工作区默认')
    expect(wrapper.get('[data-testid="model-binding-problem"]').text()).toContain('当前工具没有可继承')
    expect(draft.acpBackend).toBe('claude_code')
    expect(draft.modelBinding).toEqual({ kind: 'inherit' })
  })

  it('preserves an unavailable model reference until a replacement is explicitly selected', async () => {
    mocks.listModelConfigurations.mockResolvedValue({ items: [{ id: 'cfg-repair', name: '可用模型', backend: 'cursor', state: 'active', revision: 1, binding: { kind: 'platform', credentialId: cursorKey.id, modelId: 'gpt-5' } }] })
    const draft = draftFor({ modelBinding: { kind: 'configuration', configurationId: 'cfg-removed' } })
    const wrapper = mountPanel(draft)
    await flushPromises()
    expect(draft.modelBinding).toEqual({ kind: 'configuration', configurationId: 'cfg-removed' })
    expect(wrapper.get('[data-testid="model-binding-configuration-select"] [data-test="app-select-trigger"]').text()).toContain('cfg-removed')
    expect(wrapper.get('[data-testid="model-binding-problem"]').text()).toContain('不存在或已停用')
    await selectOption(wrapper.get('[data-testid="model-binding-configuration-select"]'), 'cfg-repair')
    expect(draft.modelBinding).toEqual({ kind: 'configuration', configurationId: 'cfg-repair' })
    expect(wrapper.find('[data-testid="model-binding-problem"]').exists()).toBe(false)
    expect(draft.env).toEqual([])
  })

  it('keeps an explicit matching default pinned until inheritance is selected', async () => {
    const draft = draftFor({ modelBinding: { kind: 'platform', credentialId: cursorKey.id, modelId: 'gpt-5' } })
    const wrapper = mountPanel(draft)
    await flushPromises()
    expect(wrapper.get('[data-testid="model-binding-platform"]').attributes('aria-pressed')).toBe('true')
    await wrapper.get('[data-testid="model-binding-inherit"]').trigger('click')
    expect(draft.modelBinding).toEqual({ kind: 'inherit' })
    expect(draft.acpBackend).toBe('cursor')
  })

  it('binds an independent connection and preserves raw model IDs and role choices', async () => {
    const draft = draftFor({ acpBackend: 'claude_code' })
    const wrapper = mountPanel(draft)
    await flushPromises()
    await chooseDirect(wrapper, 'connection')
    await selectOption(wrapper.get('[data-testid="model-binding-connection-select"]'), connection.id)
    await selectOption(wrapper.get('[data-testid="model-binding-model"]'), 'vendor/main')
    await selectOption(wrapper.get('[data-testid="model-binding-role-haiku"]'), 'vendor/fast')
    expect(draft.modelBinding).toEqual({ kind: 'connection', connectionId: connection.id, modelId: 'vendor/main', toolSettings: { modelRoles: { haiku: 'vendor/fast' } } })
    expect(draft.env).toEqual([])
    expect(draft.aiCredentialId).toBe('')
    expect(draft.openCodeCredentialId).toBe('')
  })

  it('requires confirmation to switch tools and restores a previous full binding on return', async () => {
    const original = { kind: 'platform', credentialId: cursorKey.id, modelId: 'gpt-5' }
    const draft = draftFor({ modelBinding: original })
    const wrapper = mountPanel(draft)
    await flushPromises()
    await wrapper.get('[data-testid="acp-backend-claude_code"]').trigger('click')
    expect(draft.acpBackend).toBe('cursor')
    expect(draft.modelBinding).toEqual(original)
    await wrapper.get('[data-testid="confirm-model-backend-switch"]').trigger('click')
    expect(draft.acpBackend).toBe('claude_code')
    expect(draft.modelBinding).toEqual({ kind: 'inherit' })
    await wrapper.get('[data-testid="acp-backend-cursor"]').trigger('click')
    await wrapper.get('[data-testid="confirm-model-backend-switch"]').trigger('click')
    expect(draft.modelBinding).toEqual(original)
  })

  it.each([
    { backend: 'codex', aiCredentialId: 'legacy-codex', openCodeCredentialId: '', configRoot: '/root/.codex', modelEnv: { ACP_BRIDGE_MODEL: 'legacy-model', ACP_BRIDGE_REASONING_EFFORT: 'high' } },
    { backend: 'opencode', aiCredentialId: '', openCodeCredentialId: 'legacy-opencode', configRoot: '/root/.config/opencode', modelEnv: { ACP_BRIDGE_MODEL: 'custom/vendor/model', GRASP_OPENCODE_PROVIDER: 'custom', GRASP_OPENCODE_BASE_URL: 'https://model.example/v1', GRASP_OPENCODE_MODEL_VISION: '1' } },
    { backend: 'codebuddy', aiCredentialId: 'legacy-codebuddy', openCodeCredentialId: '', configRoot: '/root/.codebuddy', modelEnv: { ACP_BRIDGE_MODEL: 'legacy-model', GRASP_CODEBUDDY_REGION: 'special-team-region' } },
  ])('restores a legacy $backend credential and model after switching away and back', async ({ backend, aiCredentialId, openCodeCredentialId, configRoot, modelEnv }) => {
    const draft = draftFor({ acpBackend: backend, modelBinding: undefined, aiCredentialId, openCodeCredentialId, layout: { configRoot, workspaceDir: '/workspace' }, env: Object.entries(modelEnv).map(([k, v]) => ({ k, v })) })
    const original = fromDraftRaw(draft as AgentStudioDraft)
    const originalSave = fromDraft(draft as AgentStudioDraft)
    const wrapper = mountPanel(draft)
    await flushPromises()
    await wrapper.get('[data-testid="acp-backend-claude_code"]').trigger('click')
    await wrapper.get('[data-testid="confirm-model-backend-switch"]').trigger('click')
    expect(draft.aiCredentialId).toBe('')
    expect(draft.openCodeCredentialId).toBe('')
    expect(draft.env.some((entry) => entry.k === 'ACP_BRIDGE_MODEL')).toBe(false)
    // Edits to unrelated environment variables belong to the Agent, not a tool.
    draft.env.push({ k: 'PROJECT_MODE', v: 'updated' })
    await wrapper.get(`[data-testid="acp-backend-${backend}"]`).trigger('click')
    await wrapper.get('[data-testid="confirm-model-backend-switch"]').trigger('click')
    expect(draft.modelBinding).toBeUndefined()
    expect(fromDraftRaw(draft as AgentStudioDraft)).toEqual({ ...original, env: { ...original.env, PROJECT_MODE: 'updated' } })
    expect(fromDraft(draft as AgentStudioDraft)).toEqual({ ...originalSave, env: { ...originalSave.env, PROJECT_MODE: 'updated' } })
  })

  it('keeps unsaved tool choices across Studio tab remounts and isolates different drafts', async () => {
    const original = { kind: 'platform', credentialId: cursorKey.id, modelId: 'gpt-5' }
    const draft = draftFor({ modelBinding: original })
    const first = mountPanel(draft)
    await flushPromises()
    await first.get('[data-testid="acp-backend-claude_code"]').trigger('click')
    await first.get('[data-testid="confirm-model-backend-switch"]').trigger('click')
    first.unmount()
    const remounted = mountPanel(draft)
    await flushPromises()
    await remounted.get('[data-testid="acp-backend-cursor"]').trigger('click')
    await remounted.get('[data-testid="confirm-model-backend-switch"]').trigger('click')
    expect(draft.modelBinding).toEqual(original)
    remounted.unmount()
    const otherDraft = draftFor({ acpBackend: 'claude_code' })
    const other = mountPanel(otherDraft)
    await flushPromises()
    await other.get('[data-testid="acp-backend-cursor"]').trigger('click')
    await other.get('[data-testid="confirm-model-backend-switch"]').trigger('click')
    expect(otherDraft.modelBinding).toEqual({ kind: 'inherit' })
  })

  it('cancels switching without changing the tool, binding or layout', async () => {
    const draft = draftFor()
    const wrapper = mountPanel(draft)
    await flushPromises()
    await wrapper.get('[data-testid="acp-backend-opencode"]').trigger('click')
    const cancel = wrapper.get('[data-testid="backend-confirm"]').findAll('button').find((button) => button.text() === '取消')!
    await cancel.trigger('click')
    expect(draft.acpBackend).toBe('cursor')
    expect(draft.layout.configRoot).toBe('/root/.cursor')
    expect(wrapper.find('[data-testid="backend-confirm"]').exists()).toBe(false)
  })

  it('preserves an explicitly edited config root across tool changes', async () => {
    const draft = draftFor()
    const wrapper = mountPanel(draft)
    await flushPromises()
    await wrapper.get('[data-testid="meta-config-root"]').setValue('/custom/config')
    await wrapper.get('[data-testid="acp-backend-claude_code"]').trigger('click')
    await wrapper.get('[data-testid="confirm-model-backend-switch"]').trigger('click')
    expect(draft.layout.configRoot).toBe('/custom/config')
  })

  it('keeps legacy values unchanged until a new source is chosen', async () => {
    const draft = draftFor({ modelBinding: undefined, aiCredentialId: 'legacy', env: [{ k: 'ACP_BRIDGE_MODEL', v: 'old-model' }] })
    const wrapper = mountPanel(draft)
    await flushPromises()
    expect(wrapper.text()).toContain('历史配置')
    expect(draft.aiCredentialId).toBe('legacy')
    expect(draft.env).toEqual([{ k: 'ACP_BRIDGE_MODEL', v: 'old-model' }])
    await chooseDirect(wrapper, 'platform')
    expect(draft.aiCredentialId).toBe('')
    expect(draft.modelBinding).toEqual({ kind: 'platform', credentialId: '' })
  })

  it.each(['google', 'azure', 'xai'])('keeps a legacy OpenCode %s catalog selection when saving unrelated Agent edits', async (provider) => {
    const env = { ACP_BRIDGE_MODEL: `${provider}/native-model`, GRASP_OPENCODE_PROVIDER: provider, PROJECT_MODE: 'existing' }
    const draft = draftFor({ acpBackend: 'opencode', modelBinding: undefined, openCodeCredentialId: `legacy-${provider}`, env: Object.entries(env).map(([k, v]) => ({ k, v })), layout: { configRoot: '/root/.config/opencode', workspaceDir: '/workspace' } })
    mocks.listCredentials.mockResolvedValue({ items: [{ id: `legacy-${provider}`, type: 'ai', provider: 'opencode', configured: true, metadata: { provider, model: env.ACP_BRIDGE_MODEL } }] })
    const wrapper = mountPanel(draft)
    await flushPromises()
    expect(wrapper.text()).toContain('历史配置：保持兼容')
    expect(wrapper.getComponent({ name: 'ModelBindingEditor' }).emitted('update:modelValue')).toBeUndefined()
    await wrapper.get('[data-testid="meta-workspace-dir"]').setValue('/workspace/changed')
    const saved = fromDraft(draft as AgentStudioDraft)
    expect(saved).not.toHaveProperty('modelBinding')
    expect(saved.openCodeCredentialId).toBe(`legacy-${provider}`)
    expect(saved.env).toEqual(env)
    expect(saved.layout?.workspaceDir).toBe('/workspace/changed')
  })
})
