// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount, type DOMWrapper } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const mocks = vi.hoisted(() => ({
  getConfig: vi.fn(), listAgents: vi.fn(), putConfig: vi.fn(), createTest: vi.fn(),
  getCredentials: vi.fn(), getModelDefaults: vi.fn(), putModelDefaults: vi.fn(), listModelConfigurations: vi.fn(), listModelConnections: vi.fn(), listToolCapabilities: vi.fn(),
  success: vi.fn(), error: vi.fn(),
}))
vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api,
    getSharedAgentConfig: mocks.getConfig, listAgents: mocks.listAgents,
    putSharedAgentConfig: mocks.putConfig, createSharedAgentTest: mocks.createTest,
    listCredentials: mocks.getCredentials, getModelDefaults: mocks.getModelDefaults, putModelDefaults: mocks.putModelDefaults, listModelConfigurations: mocks.listModelConfigurations, listModelConnections: mocks.listModelConnections, listToolCapabilities: mocks.listToolCapabilities,
  } }
})
vi.mock('@/lib/agent/cliCatalog', () => ({ loadCliModels: async () => [{ id: 'gpt-5', name: 'GPT-5', vendor: 'openai' }], cliCatalogFailed: () => false }))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => mocks }))
import WorkspaceSharedAgentPanel from './WorkspaceSharedAgentPanel.vue'

const cfg = {
  acpBackend: 'cursor', gitCredentialType: '', files: [], mcp: [], env: {},
  layout: { configRoot: '~/.cursor', workspaceDir: '/workspace' },
}
const FilesStub = { name: 'AgentFilesPanel', props: ['draft', 'save'], methods: { openPathOrCreate: vi.fn() }, template: '<div data-testid="files"/>' }
function mountPanel() {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  return mount(WorkspaceSharedAgentPanel, {
    global: { plugins: [i18n], stubs: {
      Icon: true, RouterLink: true, RepoAccessPicker: true, AppButton: { template: '<button v-bind="$attrs"><slot/></button>' },
      AgentFilesPanel: FilesStub, AgentMcpPanel: { template: '<div data-testid="mcp"/>' },
      AgentEnvPanel: { emits: ['open-settings-file'], template: '<button data-testid="env-settings" @click="$emit(\'open-settings-file\')"/>' },
    } },
  })
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
function selectedText(field: DOMWrapper<Element>) { return field.get('[data-test="app-select-trigger"]').text() }

describe('WorkspaceSharedAgentPanel interactions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.getConfig.mockResolvedValue(cfg)
    mocks.getCredentials.mockResolvedValue({ items: [{ id: 'cursor-key', name: 'Cursor account', purpose: 'tool_auth', toolId: 'cursor', configured: true }] })
    mocks.getModelDefaults.mockResolvedValue({ bindingsByBackend: {} })
    mocks.putModelDefaults.mockImplementation(async (body: unknown) => body)
    mocks.listModelConfigurations.mockResolvedValue({ items: [] })
    mocks.listModelConnections.mockResolvedValue({ items: [] })
    mocks.listToolCapabilities.mockResolvedValue({ items: [{ toolId: 'cursor', modes: ['platform'], protocols: [], adapterState: 'existing' }, { toolId: 'claude_code', modes: ['platform', 'connection'], protocols: ['anthropic-messages'], adapterState: 'existing' }, { toolId: 'opencode', modes: ['connection'], protocols: ['openai-chat-completions'], adapterState: 'existing' }] })
    mocks.listAgents.mockResolvedValue([{ name: 'a1' }, { name: 'other' }])
    mocks.putConfig.mockImplementation(async (body: any) => ({ ...cfg, ...body }))
    mocks.createTest.mockResolvedValue({ id: 'sandbox' })
  })

  it('edits metadata, switches backend/region and saves normalized payload', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any
    await w.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="derived-paths-table"]').exists()).toBe(false)
    await w.get('[data-testid="acp-backend-codebuddy"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="acp-backend-codebuddy"]').attributes('aria-checked')).toBe('true')
    expect(w.get('[data-testid="acp-region-public"]').attributes('aria-checked')).toBe('true')
    await w.get('[data-testid="acp-region-internal"]').trigger('click')
    expect(w.get('[data-testid="acp-region-internal"]').attributes('aria-checked')).toBe('true')
    await w.get('[data-testid="meta-workspace-dir"]').setValue('/srv/work')
    expect(vm.dirty).toBe(true)
    expect(await vm.save()).toBe(true)
    expect(mocks.putConfig).toHaveBeenCalledWith(expect.objectContaining({
      layout: expect.objectContaining({ workspaceDir: '/srv/work' }),
      acpBackend: 'codebuddy',
      env: expect.objectContaining({ GRASP_CODEBUDDY_REGION: 'internal' }),
      files: [],
    }))
    expect(mocks.success).toHaveBeenCalled()
    expect(vm.dirty).toBe(false)
    w.unmount()
  })

  it('edits shared Git authorization as multiple credential references and reloads the saved preference', async () => {
    mocks.getCredentials.mockResolvedValue({ items: [{ id: 'shared-gh', name: 'GitHub access', provider: 'github', envKey: 'GITHUB_TOKEN', configured: true }, { id: 'shared-gl', name: 'GitLab access', provider: 'gitlab', envKey: 'GITLAB_TOKEN', configured: true }] })
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
    await flushPromises()
    expect((w.vm as any).dirty).toBe(false)
    await selectOption(w.get('[data-testid="git-credential-add"]'), 'shared-gh')
    await selectOption(w.get('[data-testid="git-credential-add"]'), 'shared-gl')
    await w.get('[data-testid="git-preferred-shared-gl"]').trigger('click')
    expect(await (w.vm as any).save()).toBe(true)
    expect(mocks.putConfig).toHaveBeenCalledWith(expect.objectContaining({ gitBindings: { items: [{ credentialId: 'shared-gh' }, { credentialId: 'shared-gl' }], preferredCredentialId: 'shared-gl' } }))
    expect((w.vm as any).dirty).toBe(false)
    expect(w.get('[data-testid="git-preferred-shared-gl"]').attributes('aria-pressed')).toBe('true')
    await w.get('[data-testid="git-remove-shared-gl"]').trigger('click')
    await w.get('[data-testid="git-remove-shared-gh"]').trigger('click')
    expect(await (w.vm as any).save()).toBe(true)
    expect(mocks.putConfig.mock.calls.at(-1)![0].gitBindings).toEqual({ items: [] })
    w.unmount()
  })

  it('requires an explicit source before a current-tool default can be saved', async () => {
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="workspace-model-default-save"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-testid="model-binding-inherit"]').exists()).toBe(false)
    expect(w.find('[data-testid="cli-model-trigger"]').exists()).toBe(false)
    w.unmount()
  })

  it('saves a whole default binding separately while preserving other tool defaults', async () => {
    const other = { kind: 'platform', credentialId: 'claude-key', modelId: 'claude-model' }
    mocks.getModelDefaults.mockResolvedValue({ bindingsByBackend: { claude_code: other } })
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
    await flushPromises()
    await w.get('[data-testid="model-binding-platform"]').trigger('click')
    await selectOption(w.get('[data-testid="model-binding-credential"]'), 'cursor-key')
    await w.get('[data-testid="workspace-model-default-save"]').trigger('click')
    await flushPromises()
    expect(mocks.putModelDefaults).toHaveBeenCalledWith({ bindingsByBackend: { claude_code: other, cursor: { kind: 'platform', credentialId: 'cursor-key', modelId: undefined } } })
    expect(mocks.putConfig).not.toHaveBeenCalled()
    expect(JSON.stringify(mocks.putModelDefaults.mock.calls)).not.toContain('secret-value')
    w.unmount()
  })

  it('surfaces load/save failures and retries/discards', async () => {
    mocks.getConfig.mockRejectedValueOnce(new Error('load failed'))
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any
    expect(w.text()).toContain('load failed')
    mocks.getConfig.mockResolvedValue(cfg)
    await vm.load()
    vm.draft.layout.workspaceDir = '/dirty'
    mocks.putConfig.mockRejectedValueOnce(new Error('save failed'))
    expect(await vm.save()).toBe(false)
    expect(mocks.error).toHaveBeenCalledWith('save failed')
    vm.discard()
    await flushPromises()
    expect(vm.draft.layout.workspaceDir).toBe('/workspace')
    vm.draft = null
    expect(await vm.save()).toBe(false)
    vm.discard()
    vm.selectAcpBackend('cursor')
    vm.selectRegion('global')
    vm.openSettingsInFiles()
    w.unmount()
  })

  it('opens settings via env without a dialogue-test subtab', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any
    await w.get('[data-testid="shared-agent-subtab-env"]').trigger('click')
    await w.get('[data-testid="env-settings"]').trigger('click')
    await flushPromises()
    expect(vm.subTab).toBe('files')
    expect(w.find('[data-testid="shared-agent-subtab-test"]').exists()).toBe(false)
    w.unmount()
  })

  it('renders every remaining subpanel plus special-region state, without an SSH block', async () => {
    mocks.getConfig.mockResolvedValueOnce({ ...cfg, acpBackend: 'codebuddy', env: { GRASP_CODEBUDDY_REGION: 'legacy-special' } })
    const w = mountPanel(); await flushPromises()
    const vm = w.vm as any
    for (const tab of ['mcp', 'env', 'meta']) {
      await w.get(`[data-testid="shared-agent-subtab-${tab}"]`).trigger('click')
      await flushPromises()
    }
    vm.subTab = 'meta'; await w.vm.$nextTick()
    await flushPromises()
    expect(w.find('[data-test="shared-ssh-private-key"]').exists()).toBe(false)
    expect(w.find('[data-testid="model-binding-editor"]').exists()).toBe(true)
    expect(w.get('[data-testid="coding-tool-site"] [role="status"]').text()).toContain('legacy-special')
    expect(w.get('[data-testid="acp-region-public"]').attributes('aria-checked')).toBe('false')
    expect(w.get('[data-testid="acp-region-internal"]').attributes('aria-checked')).toBe('false')
    vm.draft.layout.configRoot = ''
    await w.vm.$nextTick()
    expect(w.find('[data-testid="derived-paths-table"]').exists()).toBe(false)
    w.unmount()
  })

  it('clears a selected platform key without substituting another key', async () => {
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
    await flushPromises()
    await w.get('[data-testid="model-binding-platform"]').trigger('click')
    await selectOption(w.get('[data-testid="model-binding-credential"]'), 'cursor-key')
    expect(w.get('[data-testid="workspace-model-default-save"]').attributes('disabled')).toBeUndefined()
    await selectOption(w.get('[data-testid="model-binding-credential"]'), '')
    expect(w.get('[data-testid="workspace-model-default-save"]').attributes('disabled')).toBeDefined()
    expect(w.get('[data-testid="model-binding-problem"]').text()).toContain('未配置')
    expect(mocks.putModelDefaults).not.toHaveBeenCalled()
    w.unmount()
  })

  it('keeps separate complete defaults when switching the tool being configured', async () => {
    mocks.getModelDefaults.mockResolvedValue({ bindingsByBackend: { cursor: { kind: 'platform', credentialId: 'cursor-key' }, claude_code: { kind: 'platform', credentialId: 'claude-key' } } })
    mocks.getCredentials.mockResolvedValue({ items: [{ id: 'cursor-key', name: 'Cursor account', purpose: 'tool_auth', toolId: 'cursor', configured: true }, { id: 'claude-key', name: 'Claude account', purpose: 'tool_auth', toolId: 'claude_code', configured: true }] })
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
    await flushPromises()
    const defaults = w.get('[data-testid="workspace-model-defaults"]')
    expect(selectedText(defaults.get('[data-testid="model-binding-credential"]'))).toBe('Cursor account')
    await selectOption(defaults.get('[data-testid="workspace-model-default-backend"]'), 'claude_code')
    expect(selectedText(defaults.get('[data-testid="model-binding-credential"]'))).toBe('Claude account')
    await selectOption(defaults.get('[data-testid="workspace-model-default-backend"]'), 'cursor')
    expect(selectedText(defaults.get('[data-testid="model-binding-credential"]'))).toBe('Cursor account')
    expect(mocks.putModelDefaults).not.toHaveBeenCalled()
    w.unmount()
  })

  it('keeps an OpenCode connection independent from shared tool selection and legacy key fields', async () => {
    mocks.getConfig.mockResolvedValue({ ...cfg, acpBackend: 'opencode', openCodeCredentialId: 'legacy-model-key' })
    mocks.getModelDefaults.mockResolvedValue({ bindingsByBackend: { opencode: { kind: 'connection', connectionId: 'conn-oc', modelId: 'vendor/model' } } })
    mocks.listModelConnections.mockResolvedValue({ items: [{ id: 'conn-oc', name: 'Provider connection', providerId: 'custom', protocol: 'openai-chat-completions', baseUrl: 'https://model.example/v1', credentialId: 'model-key', auth: { scheme: 'bearer' }, models: [{ id: 'vendor/model' }], state: 'active' }] })
    mocks.getCredentials.mockResolvedValue({ items: [{ id: 'model-key', purpose: 'model_api', name: 'Key', configured: true }] })
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
    await flushPromises()
    const defaults = w.get('[data-testid="workspace-model-defaults"]')
    await selectOption(defaults.get('[data-testid="workspace-model-default-backend"]'), 'opencode')
    expect(selectedText(defaults.get('[data-testid="model-binding-model"]'))).toBe('vendor/model')
    await w.get('[data-testid="acp-backend-cursor"]').trigger('click')
    await flushPromises()
    expect((w.vm as any).draft.openCodeCredentialId).toBe('legacy-model-key')
    expect(selectedText(defaults.get('[data-testid="model-binding-model"]'))).toBe('vendor/model')
    expect(mocks.putModelDefaults).not.toHaveBeenCalled()
    w.unmount()
  })

  it('shows connection sources for OpenCode without requiring an OpenCode-specific key picker', async () => {
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
    await flushPromises()
    await selectOption(w.get('[data-testid="workspace-model-default-backend"]'), 'opencode')
    expect(w.get('[data-testid="model-binding-platform"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="model-binding-connection"]').trigger('click')
    expect(w.find('[data-testid="model-binding-connection-select"]').exists()).toBe(true)
    expect(w.find('[data-testid="credential-opencode"]').exists()).toBe(false)
    w.unmount()
  })
})
