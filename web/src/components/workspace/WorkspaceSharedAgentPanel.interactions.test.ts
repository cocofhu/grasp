// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const mocks = vi.hoisted(() => ({
  getConfig: vi.fn(), listAgents: vi.fn(), putConfig: vi.fn(), createTest: vi.fn(),
  getCredentials: vi.fn(),
  success: vi.fn(), error: vi.fn(),
}))
vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api,
    getSharedAgentConfig: mocks.getConfig, listAgents: mocks.listAgents,
    putSharedAgentConfig: mocks.putConfig, createSharedAgentTest: mocks.createTest,
    listCredentials: mocks.getCredentials,
  } }
})
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
      Icon: true, AppButton: { template: '<button v-bind="$attrs"><slot/></button>' },
      AgentFilesPanel: FilesStub, AgentMcpPanel: { template: '<div data-testid="mcp"/>' },
      AgentEnvPanel: { emits: ['open-settings-file'], template: '<button data-testid="env-settings" @click="$emit(\'open-settings-file\')"/>' },
    } },
  })
}

describe('WorkspaceSharedAgentPanel interactions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.getConfig.mockResolvedValue(cfg)
    mocks.getCredentials.mockResolvedValue({ items: [] })
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
    expect(vm.derivedPaths[0].path).toContain('mcp.json')
    vm.selectAcpBackend('claude_code')
    await flushPromises()
    expect(vm.draft.acpBackend).toBe('claude_code')
    if (vm.metaRegionOptions.length) vm.selectRegion(vm.metaRegionOptions[0].id)
    vm.draft.layout.workspaceDir = '/srv/work'
    expect(vm.dirty).toBe(true)
    expect(await vm.save()).toBe(true)
    expect(mocks.putConfig).toHaveBeenCalledWith(expect.objectContaining({
      layout: expect.objectContaining({ workspaceDir: '/srv/work' }),
      files: [],
    }))
    expect(mocks.success).toHaveBeenCalled()
    expect(vm.dirty).toBe(false)
    w.unmount()
  })

  it('keeps an empty workspace model on auto without a follow choice', async () => {
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="agent-cli-model-mode"]').exists()).toBe(false)
    expect(w.find('[data-test="agent-cli-model-inherited"]').exists()).toBe(false)
    expect(w.get('[data-testid="cli-model-trigger"]').text()).toContain('自动')
    expect(w.get('[data-testid="cli-model-command"] code').text()).toBe('cursor-agent')
    w.unmount()
  })

  it('sets the workspace default model, clears it on a backend switch and saves it', async () => {
    mocks.getConfig.mockResolvedValue({ ...cfg, env: { ACP_BRIDGE_MODEL: 'composer-2.5' } })
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any
    await w.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="agent-cli-runtime"]').exists()).toBe(true)
    expect(w.get('[data-testid="cli-model-trigger"]').text()).toContain('composer-2.5')

    vm.selectAcpBackend('codex')
    await flushPromises()
    expect(vm.draft.env.some((e: { k: string }) => e.k === 'ACP_BRIDGE_MODEL')).toBe(false)
    vm.draft.env.push({ k: 'ACP_BRIDGE_MODEL', v: 'gpt-6.1-sol' })
    expect(await vm.save()).toBe(true)
    expect(mocks.putConfig).toHaveBeenCalledWith(expect.objectContaining({
      acpBackend: 'codex',
      env: expect.objectContaining({ ACP_BRIDGE_MODEL: 'gpt-6.1-sol' }),
    }))
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
    mocks.getConfig.mockResolvedValueOnce({ ...cfg, env: { CURSOR_REGION: 'legacy-special' } })
    const w = mountPanel(); await flushPromises()
    const vm = w.vm as any
    for (const tab of ['mcp', 'env', 'meta']) {
      await w.get(`[data-testid="shared-agent-subtab-${tab}"]`).trigger('click')
      await flushPromises()
    }
    vm.subTab = 'meta'; await w.vm.$nextTick()
    await flushPromises()
    expect(w.find('[data-test="shared-ssh-private-key"]').exists()).toBe(false)
    expect(w.get('[data-testid="credential-alias-empty"]').text()).toContain('这一类还没有已保存的授权')
    vm.draft.layout.configRoot = ''
    await w.vm.$nextTick()
    expect(vm.derivedPaths[0].path).toContain('mcp.json')
    w.unmount()
  })

  it('selects, clears, and saves a generic credential id without a secret', async () => {
    mocks.getCredentials.mockResolvedValue({
      items: [
        {
          id: 'cred-work',
          name: '工作号',
          type: 'ai',
          provider: 'cursor',
          envKey: 'GRASP_CURSOR_API_KEY',
          configured: true,
          masked: 'sk-work',
        },
      ],
    })
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="shared-generic-credential"]').text()).toContain('单个 Agent 已选择时以单个 Agent 为准')
    expect(w.text()).not.toContain('这一类还没有已保存的授权')
    await w.get('[data-testid="credential-alias-option-cred-work"]').trigger('click')
    expect((w.vm as any).draft.aiCredentialId).toBe('cred-work')
    expect(await (w.vm as any).save()).toBe(true)
    const body = mocks.putConfig.mock.calls.at(-1)?.[0]
    expect(body.aiCredentialId).toBe('cred-work')
    expect(body.openCodeCredentialId).toBe('')
    expect(JSON.stringify(body)).not.toContain('sk-work')

    await w.get('[data-testid="credential-alias-clear"]').trigger('click')
    expect((w.vm as any).draft.aiCredentialId).toBe('')
    ;(w.vm as any).draft.openCodeCredentialId = 'cred-oc'
    ;(w.vm as any).selectAcpBackend('claude_code')
    await flushPromises()
    expect((w.vm as any).draft.aiCredentialId).toBe('')
    expect((w.vm as any).draft.openCodeCredentialId).toBe('cred-oc')
    expect(w.find('[data-testid="credential-alias-option-cred-work"]').exists()).toBe(false)
    w.unmount()
  })

  it('restores the saved credential and drops it when the backend changes', async () => {
    mocks.getConfig.mockResolvedValue({ ...cfg, aiCredentialId: 'cred-work' })
    mocks.getCredentials.mockResolvedValue({
      items: [
        {
          id: 'cred-work',
          name: '工作号',
          type: 'ai',
          provider: 'cursor',
          envKey: 'GRASP_CURSOR_API_KEY',
          configured: true,
        },
        {
          id: 'cred-claude',
          name: '克劳德',
          type: 'ai',
          provider: 'claude_code',
          envKey: 'GRASP_CLAUDE_API_KEY',
          configured: true,
        },
      ],
    })
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="credential-alias-option-cred-work"]').attributes('aria-pressed')).toBe('true')
    ;(w.vm as any).draft.aiCredentialId = ''
    ;(w.vm as any).discard()
    await flushPromises()
    expect((w.vm as any).draft.aiCredentialId).toBe('cred-work')
    ;(w.vm as any).selectAcpBackend('claude_code')
    await flushPromises()
    expect((w.vm as any).draft.aiCredentialId).toBe('')
    expect(w.find('[data-testid="credential-alias-option-cred-work"]').exists()).toBe(false)
    expect(w.find('[data-testid="credential-alias-option-cred-claude"]').exists()).toBe(true)
    w.unmount()
  })

  it('keeps the OpenCode credential after switching to Cursor and saving', async () => {
    mocks.getConfig.mockResolvedValue({
      ...cfg,
      acpBackend: 'opencode',
      openCodeCredentialId: 'cred-oc',
    })
    mocks.getCredentials.mockResolvedValue({
      items: [
        {
          id: 'cred-oc',
          name: '厂商',
          type: 'ai',
          provider: 'opencode',
          configured: true,
          metadata: { provider: 'openai', model: 'openai/gpt-4o' },
        },
        {
          id: 'cred-work',
          name: '工作号',
          type: 'ai',
          provider: 'cursor',
          envKey: 'GRASP_CURSOR_API_KEY',
          configured: true,
        },
      ],
    })
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
    await flushPromises()
    expect((w.vm as any).draft.openCodeCredentialId).toBe('cred-oc')
    ;(w.vm as any).selectAcpBackend('cursor')
    await flushPromises()
    expect((w.vm as any).draft.openCodeCredentialId).toBe('cred-oc')
    expect((w.vm as any).draft.aiCredentialId).toBe('')
    await w.get('[data-testid="credential-alias-option-cred-work"]').trigger('click')
    expect(await (w.vm as any).save()).toBe(true)
    const body = mocks.putConfig.mock.calls.at(-1)?.[0]
    expect(body.aiCredentialId).toBe('cred-work')
    expect(body.openCodeCredentialId).toBe('cred-oc')
    expect(JSON.stringify(body)).not.toContain('sk-')
    w.unmount()
  })

  it('uses the OpenCode picker without a create button and shows the empty state', async () => {
    mocks.getConfig.mockResolvedValue({ ...cfg, acpBackend: 'opencode' })
    mocks.getCredentials.mockResolvedValue({ items: [] })
    const w = mountPanel()
    await flushPromises()
    await w.get('[data-testid="shared-agent-subtab-meta"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="credential-opencode"]').exists()).toBe(true)
    expect(w.find('[data-testid="opencode-credential-add"]').exists()).toBe(false)
    expect(w.text()).toContain('还没有模型厂商 API Key')
    w.unmount()
  })
})
